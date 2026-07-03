package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"porta-berita/internal/cms"
)

func (s *Server) StartBlueskyCronJob() {
	s.loadBSkyLogs()
	go func() {
		s.logBSky(slog.LevelInfo, "Memulai pekerja latar belakang auto-post Bluesky...")
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			settings := s.store.GetSettings()
			if settings["bsky_auto_post_enabled"] != "true" {
				continue
			}

			intervalMinStr := settings["bsky_cron_interval"]
			if intervalMinStr == "" {
				intervalMinStr = "60"
			}
			var intervalMin int
			_, err := fmt.Sscanf(intervalMinStr, "%d", &intervalMin)
			if err != nil || intervalMin <= 0 {
				intervalMin = 60
			}

			lastRunStr := settings["bsky_cron_last_run"]
			var lastRun time.Time
			if lastRunStr != "" {
				lastRun, _ = time.Parse(time.RFC3339, lastRunStr)
			}

			if time.Since(lastRun) >= time.Duration(intervalMin)*time.Minute {
				s.bskyMu.Lock()
				if s.bskyRunning {
					s.bskyMu.Unlock()
					continue
				}
				s.bskyRunning = true
				s.bskyMu.Unlock()

				go func() {
					defer func() {
						s.bskyMu.Lock()
						s.bskyRunning = false
						s.bskyMu.Unlock()
					}()

					s.logBSky(slog.LevelInfo, "Interval cron Bluesky tercapai, mengeksekusi pekerjaan auto-post...")
					err := s.processBlueskyAutoPost(false)
					if err != nil {
						s.logBSky(slog.LevelError, "Pekerjaan Bluesky auto-post latar belakang gagal", "error", err)
					} else {
						s.logBSky(slog.LevelInfo, "Pekerjaan Bluesky auto-post latar belakang selesai sukses")
					}

					// ALWAYS update bsky_cron_last_run to prevent infinite retries every minute on failure
					nowStr := time.Now().Format(time.RFC3339)
					settings := s.store.GetSettings()
					settings["bsky_cron_last_run"] = nowStr
					systemUser := &cms.User{ID: "system", Role: cms.RoleAdmin}
					_ = s.store.UpdateSettings(systemUser, settings)
				}()
			}
		}
	}()
}

func (s *Server) processBlueskyAutoPost(force bool) error {
	settings := s.store.GetSettings()
	if !force && settings["bsky_auto_post_enabled"] != "true" {
		return nil
	}

	handle := settings["bsky_handle"]
	appPassword := settings["bsky_app_password"]
	if handle == "" || appPassword == "" {
		return fmt.Errorf("Bluesky handle dan App Password wajib diisi untuk auto-posting")
	}

	// Ambil artikel yang diterbitkan (maksimal 10 terbaru)
	articles := s.store.ListPublishedArticles(10)
	if len(articles) == 0 {
		s.logBSky(slog.LevelInfo, "Tidak ada artikel dengan status terbit (published) yang ditemukan.")
		return nil
	}

	// Cari artikel pertama yang belum diposting ke Bluesky
	var targetArticle *cms.Article
	for _, art := range articles {
		posted, err := s.store.IsArticlePostedToBSky(art.ID)
		if err != nil {
			s.logBSky(slog.LevelError, "Gagal memeriksa status posting Bluesky untuk artikel", "article_id", art.ID, "error", err)
			continue
		}
		if !posted {
			// Mengunci artikel di database dengan status pending untuk mencegah duplikasi oleh thread/proses lain
			locked, err := s.store.LockArticleForBSky(art.ID)
			if err != nil {
				s.logBSky(slog.LevelError, "Gagal mencoba mengunci artikel untuk Bluesky", "article_id", art.ID, "error", err)
				continue
			}
			if !locked {
				s.logBSky(slog.LevelInfo, "Artikel sedang diproses oleh proses lain atau sudah terkunci", "article_id", art.ID)
				continue
			}
			targetArticle = &art
			break
		}
	}

	if targetArticle == nil {
		s.logBSky(slog.LevelInfo, "Semua artikel terbit sudah terposting ke Bluesky sebelumnya.")
		return nil
	}

	s.logBSky(slog.LevelInfo, "Memulai proses auto-post ke Bluesky untuk artikel", "title", targetArticle.Title)

	// Ambil model AI
	model := settings["ai_default_model"]
	if model == "" {
		model = "gemini-2.5-flash"
	}
	customPrompt := settings["bsky_custom_prompt"]

	// 1. Generate Caption dengan AI dalam Bahasa Inggris (with robust fallback on failure)
	caption, err := s.performBSkyCaptionGenSync(targetArticle.Title, targetArticle.Excerpt, targetArticle.Content, customPrompt, model)
	if err != nil {
		s.logBSky(slog.LevelWarn, "Gagal membuat caption AI, menggunakan caption cadangan (fallback)", "error", err)
		if targetArticle.Excerpt != "" {
			caption = fmt.Sprintf("📢 Read our latest article: %s\n\n%s", targetArticle.Title, targetArticle.Excerpt)
		} else {
			caption = fmt.Sprintf("📢 Read our latest article: %s", targetArticle.Title)
		}
	}

	// 2. Tentukan link publik artikel
	baseURL := settings["site_url"]
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	articleLink := fmt.Sprintf("%sartikel/%s", baseURL, targetArticle.Slug)

	s.logBSky(slog.LevelInfo, "Mengirim postingan ke Bluesky...", "link", articleLink)

	// 3. Posting ke Bluesky (dengan gambar jika tersedia)
	bskyPostURI, err := s.postToBlueskyAPI(handle, appPassword, caption, articleLink, targetArticle.HeroImageURL)
	if err != nil {
		// Hapus kunci (unlock) di database jika postingan gagal agar bisa dicoba kembali nanti
		_ = s.store.UnmarkArticleAsPostedToBSky(targetArticle.ID)
		return fmt.Errorf("gagal memposting ke Bluesky: %w", err)
	}

	// 4. Tandai artikel sebagai terposting dengan URI asli ke Bluesky di DB
	err = s.store.MarkArticleAsPostedToBSky(targetArticle.ID, bskyPostURI)
	if err != nil {
		s.logBSky(slog.LevelError, "Gagal menyimpan status terposting Bluesky di database", "article_id", targetArticle.ID, "bsky_post_uri", bskyPostURI, "error", err)
	} else {
		s.logBSky(slog.LevelInfo, "Berhasil memposting artikel ke Bluesky!", "title", targetArticle.Title, "bsky_post_uri", bskyPostURI)
	}

	return nil
}

func (s *Server) postToBlueskyAPI(handle, appPassword, message, link, imageURL string) (string, error) {
	// Limit message to fit within Bluesky's 300 character limit (leaving room for space and link)
	maxMessageLength := 285 - len(link)
	if len([]rune(message)) > maxMessageLength {
		runes := []rune(message)
		message = string(runes[:maxMessageLength-3]) + "..."
	}

	// A. Buat Sesi Autentikasi
	sessionPayload, _ := json.Marshal(map[string]string{
		"identifier": handle,
		"password":   appPassword,
	})

	resp, err := http.Post("https://bsky.social/xrpc/com.atproto.server.createSession", "application/json", bytes.NewBuffer(sessionPayload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gagal membuat sesi Bluesky (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var session struct {
		AccessJWT string `json:"accessJwt"`
		DID       string `json:"did"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", err
	}

	// B. Susun teks lengkap dan hitung posisi byte untuk Facets
	fullText := fmt.Sprintf("%s %s", message, link)
	byteStart := len(message) + 1 // spasi pemisah
	byteEnd := len(fullText)

	// D. Susun record post
	postRecord := map[string]interface{}{
		"$type":     "app.bsky.feed.post",
		"text":      fullText,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"facets": []map[string]interface{}{
			{
				"index": map[string]int{
					"byteStart": byteStart,
					"byteEnd":   byteEnd,
				},
				"features": []map[string]interface{}{
					{
						"$type": "app.bsky.richtext.facet#link",
						"uri":   link,
					},
				},
			},
		},
	}

	// E. Jika ada gambar, upload sebagai blob dan embed ke postingan
	if imageURL != "" {
		imgBytes, mimeType, err := s.fetchImageBytes(imageURL)
		if err != nil {
			s.logBSky(slog.LevelWarn, "Gagal memuat gambar artikel untuk auto-post, melewati upload gambar", "url", imageURL, "error", err)
		} else {
			blobRef, err := s.uploadBSkyBlob(session.AccessJWT, mimeType, imgBytes)
			if err != nil {
				s.logBSky(slog.LevelWarn, "Gagal mengunggah gambar ke Bluesky, memposting tanpa gambar", "error", err)
			} else if blobRef != nil {
				postRecord["embed"] = map[string]interface{}{
					"$type": "app.bsky.embed.images",
					"images": []map[string]interface{}{
						{
							"alt":   "Article Hero Image",
							"image": blobRef,
						},
					},
				}
			}
		}
	}

	// C. Kirim Postingan ke Bluesky
	postPayload := map[string]interface{}{
		"repo":       session.DID,
		"collection": "app.bsky.feed.post",
		"record":     postRecord,
	}

	postData, _ := json.Marshal(postPayload)
	req, err := http.NewRequest("POST", "https://bsky.social/xrpc/com.atproto.repo.createRecord", bytes.NewBuffer(postData))
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+session.AccessJWT)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	postResp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer postResp.Body.Close()

	bodyBytes, err := io.ReadAll(postResp.Body)
	if err != nil {
		return "", err
	}

	if postResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gagal memposting ke Bluesky (status %d): %s", postResp.StatusCode, string(bodyBytes))
	}

	var resObj struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(bodyBytes, &resObj); err != nil {
		return "", err
	}

	return resObj.URI, nil
}

func (s *Server) fetchImageBytes(imageURL string) ([]byte, string, error) {
	if imageURL == "" {
		return nil, "", fmt.Errorf("empty image URL")
	}

	// Jika URL eksternal
	if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(imageURL)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("failed to download image, status: %d", resp.StatusCode)
		}

		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", err
		}

		mimeType := resp.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = http.DetectContentType(bytes)
		}
		return bytes, mimeType, nil
	}

	// Jika file lokal (misal: /uploads/xxx.png atau uploads/xxx.png)
	localPath := imageURL
	if strings.HasPrefix(localPath, "/") {
		localPath = strings.TrimPrefix(localPath, "/")
	}

	// Baca file dari disk
	bytes, err := os.ReadFile(localPath)
	if err != nil {
		return nil, "", err
	}

	mimeType := http.DetectContentType(bytes)
	return bytes, mimeType, nil
}

func (s *Server) uploadBSkyBlob(accessJWT, mimeType string, imageBytes []byte) (map[string]any, error) {
	req, err := http.NewRequest("POST", "https://bsky.social/xrpc/com.atproto.repo.uploadBlob", bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessJWT)
	req.Header.Set("Content-Type", mimeType)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to upload blob, status: %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Blob map[string]any `json:"blob"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, err
	}

	return res.Blob, nil
}

func (s *Server) performBSkyCaptionGenSync(title, excerpt, content, customPrompt, model string) (string, error) {
	settings := s.store.GetSettings()
	apiKey := settings["ai_api_key"]
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	endpointURL := strings.TrimSpace(settings["ai_endpoint_url"])

	if apiKey == "" || model == "mock-rewrite" {
		mockCaption := fmt.Sprintf("📢 Read this interesting article: %s\n\nSummary: %s", title, excerpt)
		return mockCaption, nil
	}

	isGemini := endpointURL == "" || strings.Contains(endpointURL, "googleapis.com")
	var apiURL string
	
	promptBuilder := strings.Builder{}
	promptBuilder.WriteString(`You are a professional social media manager for a premium news portal. Your task is to write an engaging, natural, and interactive social media post/caption for Bluesky to share a new article.

CRITICAL REQUIREMENTS:
1. LANGUAGE: You MUST write the entire caption in ENGLISH, even if the input article title, excerpt, or content is in Indonesian or another language.
2. TONE & STYLE: Write in a natural, human-like, engaging tone.
3. MAXIMUM LENGTH: The caption must be extremely short and concise. It MUST be under 180 characters (approx. 20-25 words, 1-2 short sentences max). This is a strict limit because Bluesky has a maximum post limit of 300 characters including the URL!
4. HASHTAGS & EMOJIS: Include 1-2 highly relevant, popular hashtags and 1 fitting emoji.
5. NO MARKDOWN: Return ONLY the final caption text. Do not use bold/italic markdown (** or *) or other text formatting.
`)

	if strings.TrimSpace(customPrompt) != "" {
		promptBuilder.WriteString(fmt.Sprintf("\nAdditional Custom Instructions from Admin:\n%s\n", strings.TrimSpace(customPrompt)))
	}

	promptBuilder.WriteString(fmt.Sprintf(`
Article Details:
Title: %s
Excerpt: %s
Content: %s`, title, excerpt, content))

	prompt := promptBuilder.String()

	var reqBytes []byte
	var err error

	if isGemini {
		if endpointURL == "" {
			endpointURL = "https://generativelanguage.googleapis.com/v1beta/models/"
		}
		if !strings.HasSuffix(endpointURL, "/") {
			endpointURL += "/"
		}
		apiURL = fmt.Sprintf("%s%s:generateContent?key=%s", endpointURL, model, apiKey)

		type geminiPart struct {
			Text string `json:"text"`
		}
		type geminiContent struct {
			Parts []geminiPart `json:"parts"`
		}
		type geminiReq struct {
			Contents []geminiContent `json:"contents"`
		}

		reqObj := geminiReq{
			Contents: []geminiContent{
				{
					Parts: []geminiPart{
						{Text: prompt},
					},
				},
			},
		}
		reqBytes, err = json.Marshal(reqObj)
		if err != nil {
			return "", err
		}
	} else {
		apiURL = endpointURL
		if !strings.HasSuffix(apiURL, "/chat/completions") {
			if strings.HasSuffix(apiURL, "/") {
				apiURL += "chat/completions"
			} else {
				apiURL += "/chat/completions"
			}
		}
		type openAIMessage struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		type openAIReq struct {
			Model    string          `json:"model"`
			Messages []openAIMessage `json:"messages"`
		}
		reqObj := openAIReq{
			Model: model,
			Messages: []openAIMessage{
				{Role: "user", Content: prompt},
			},
		}
		reqBytes, err = json.Marshal(reqObj)
		if err != nil {
			return "", err
		}
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(string(reqBytes)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if !isGemini {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	var client *http.Client
	if settings["ai_enable_proxy"] == "true" {
		client = s.getProxyHTTPClientWithTimeout(30 * time.Second)
	} else {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI API error status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var generatedText string
	if isGemini {
		type geminiPart struct {
			Text string `json:"text"`
		}
		type geminiCandidate struct {
			Content struct {
				Parts []geminiPart `json:"parts"`
			} `json:"content"`
		}
		type geminiResp struct {
			Candidates []geminiCandidate `json:"candidates"`
		}
		var rObj geminiResp
		if err := json.Unmarshal(bodyBytes, &rObj); err != nil {
			return "", err
		}
		if len(rObj.Candidates) > 0 && len(rObj.Candidates[0].Content.Parts) > 0 {
			generatedText = rObj.Candidates[0].Content.Parts[0].Text
		}
	} else {
		type openAIChoice struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		type openAIResp struct {
			Choices []openAIChoice `json:"choices"`
		}
		var rObj openAIResp
		if err := json.Unmarshal(bodyBytes, &rObj); err != nil {
			return "", err
		}
		if len(rObj.Choices) > 0 {
			generatedText = rObj.Choices[0].Message.Content
		}
	}

	if generatedText == "" {
		return "", fmt.Errorf("AI generated empty response")
	}

	return strings.TrimSpace(generatedText), nil
}

func (s *Server) loadBSkyLogs() {
	s.bskyLogsMu.Lock()
	defer s.bskyLogsMu.Unlock()

	// Only load from disk if memory logs are empty
	if len(s.bskyLogs) > 0 {
		return
	}

	_ = os.MkdirAll("data", 0755)
	filePath := filepath.Join("data", "bsky_logs.json")
	file, err := os.Open(filePath)
	if err != nil {
		s.bskyLogs = []CronLog{}
		return
	}
	defer file.Close()

	var logs []CronLog
	if err := json.NewDecoder(file).Decode(&logs); err != nil {
		s.bskyLogs = []CronLog{}
		return
	}

	s.bskyLogs = s.pruneOldCronLogs(logs)
}

func (s *Server) logBSky(level slog.Level, msg string, args ...any) {
	s.log.Log(context.Background(), level, "[BSky Auto-Post] "+msg, args...)

	logMsg := msg
	if len(args) > 0 {
		var parts []string
		for i := 0; i < len(args); i += 2 {
			if i+1 < len(args) {
				parts = append(parts, fmt.Sprintf("%v=%v", args[i], args[i+1]))
			} else {
				parts = append(parts, fmt.Sprintf("%v", args[i]))
			}
		}
		if len(parts) > 0 {
			logMsg = fmt.Sprintf("%s (%s)", msg, strings.Join(parts, ", "))
		}
	}

	s.bskyLogsMu.Lock()
	defer s.bskyLogsMu.Unlock()

	s.bskyLogs = s.pruneOldCronLogs(s.bskyLogs)
	if len(s.bskyLogs) >= 500 {
		s.bskyLogs = s.bskyLogs[1:]
	}
	s.bskyLogs = append(s.bskyLogs, CronLog{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   logMsg,
	})

	filePath := filepath.Join("data", "bsky_logs.json")
	file, err := os.Create(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(s.bskyLogs)
}

type dashboardBlueskyViewData struct {
	User     *cms.User
	Settings map[string]string
	Success  string
	Error    string
}

func (s *Server) dashboardBluesky(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	settings := s.store.GetSettings()

	if settings["site_url"] == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		settings["site_url"] = scheme + "://" + r.Host
	}

	s.renderTemplate(w, "bluesky.html", dashboardBlueskyViewData{
		User:     user,
		Settings: settings,
	})
}

func (s *Server) blueskySaveSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}

	settings := s.store.GetSettings()
	settings["bsky_auto_post_enabled"] = r.FormValue("bsky_auto_post_enabled")
	settings["bsky_handle"] = r.FormValue("bsky_handle")
	settings["bsky_app_password"] = r.FormValue("bsky_app_password")
	settings["bsky_custom_prompt"] = r.FormValue("bsky_custom_prompt")
	settings["bsky_cron_interval"] = r.FormValue("bsky_cron_interval")
	
	siteURL := strings.TrimSpace(r.FormValue("site_url"))
	if siteURL == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		siteURL = scheme + "://" + r.Host
	}
	settings["site_url"] = siteURL

	err := s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderTemplate(w, "bluesky.html", dashboardBlueskyViewData{
			User:     user,
			Settings: settings,
			Error:    "Gagal memperbarui konfigurasi: " + err.Error(),
		})
		return
	}

	s.renderTemplate(w, "bluesky.html", dashboardBlueskyViewData{
		User:     user,
		Settings: settings,
		Success:  "Konfigurasi Bluesky berhasil disimpan",
	})
}

func (s *Server) blueskyGetLogs(w http.ResponseWriter, r *http.Request) {
	s.loadBSkyLogs()
	s.bskyLogsMu.RLock()
	defer s.bskyLogsMu.RUnlock()
	writeJSON(w, http.StatusOK, s.bskyLogs)
}

func (s *Server) blueskyClearLogs(w http.ResponseWriter, r *http.Request) {
	s.bskyLogsMu.Lock()
	s.bskyLogs = []CronLog{}
	s.bskyLogsMu.Unlock()

	filePath := filepath.Join("data", "bsky_logs.json")
	_ = os.Remove(filePath)

	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) blueskyRunManual(w http.ResponseWriter, r *http.Request) {
	s.bskyMu.Lock()
	if s.bskyRunning {
		s.bskyMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "message": "Proses posting Bluesky sedang berjalan"})
		return
	}
	s.bskyRunning = true
	s.bskyMu.Unlock()

	go func() {
		defer func() {
			s.bskyMu.Lock()
			s.bskyRunning = false
			s.bskyMu.Unlock()
		}()

		s.logBSky(slog.LevelInfo, "Memulai posting manual ke Bluesky...")
		err := s.processBlueskyAutoPost(true)
		if err != nil {
			s.logBSky(slog.LevelError, "Posting Bluesky manual gagal", "error", err)
		} else {
			s.logBSky(slog.LevelInfo, "Posting Bluesky manual selesai sukses")
		}
	}()

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Auto-post Bluesky dimulai di latar belakang"})
}
