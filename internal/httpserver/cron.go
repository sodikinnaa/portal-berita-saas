package httpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"porta-berita/internal/cms"
	core "porta-berita/internal/cms"
)

type RSSFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel RSSChannel `xml:"channel"`
}

type RSSChannel struct {
	Title string    `xml:"title"`
	Items []RSSItem `xml:"item"`
}

type RSSSource struct {
	URL  string `xml:"url,attr"`
	Name string `xml:",chardata"`
}

type RSSItem struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	PubDate     string    `xml:"pubDate"`
	Description string    `xml:"description"`
	Source      RSSSource `xml:"source"`
}

type DecodedFeedItem struct {
	Title       string `json:"title"`
	GoogleLink  string `json:"google_link"`
	OriginalURL string `json:"original_url"`
	PubDate     string `json:"pub_date"`
	Exists      bool   `json:"exists"`
	ArticleSlug string `json:"article_slug,omitempty"`
}

type dashboardCronViewData struct {
	User         *cms.User
	Settings     map[string]string
	Categories   []cms.Category
	Prompts      []customPromptItem
	Success      string
	Error        string
}

func (s *Server) DecodeGoogleNewsURL(sourceURL string) (string, error) {
	u, err := url.Parse(sourceURL)
	if err != nil {
		return "", fmt.Errorf("error parsing URL: %w", err)
	}
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) < 2 {
		return "", fmt.Errorf("invalid URL path structure")
	}
	base64Str := pathParts[len(pathParts)-1]

	// Use /rss/articles/ directly to avoid HTTP 429 Too Many Requests rate-limiting
	fetchURL := sourceURL
	if !strings.Contains(fetchURL, "/rss/articles/") {
		fetchURL = fmt.Sprintf("https://news.google.com/rss/articles/%s", base64Str)
	}

	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating GET request: %w", err)
	}
	
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36"
	req.Header.Set("User-Agent", userAgent)

	client := s.getProxyHTTPClient()

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error fetching article page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching article page returned HTTP status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %w", err)
	}
	body := string(bodyBytes)

	reSg := regexp.MustCompile(`data-n-a-sg="([^"]+)"`)
	reTs := regexp.MustCompile(`data-n-a-ts="([^"]+)"`)

	matchesSg := reSg.FindStringSubmatch(body)
	matchesTs := reTs.FindStringSubmatch(body)

	if len(matchesSg) < 2 || len(matchesTs) < 2 {
		var snippet string
		if len(body) > 200 {
			snippet = body[:200]
		} else {
			snippet = body
		}
		return "", fmt.Errorf("failed to extract decoding params (status=%d, body_snippet=%q)", resp.StatusCode, snippet)
	}

	signature := matchesSg[1]
	timestamp := matchesTs[1]

	postURL := "https://news.google.com/_/DotsSplashUi/data/batchexecute"

	innerStr := fmt.Sprintf(`["garturlreq",[["X","X",["X","X"],null,null,1,1,"US:en",null,1,null,null,null,null,null,0,1],"X","X",1,[1,1,1],1,1,null,0,0,null,0],"%s",%s,"%s"]`, base64Str, timestamp, signature)
	escapedInnerStr := strings.ReplaceAll(innerStr, `\`, `\\`)
	escapedInnerStr = strings.ReplaceAll(escapedInnerStr, `"`, `\"`)

	payloadStr := fmt.Sprintf(`[[["Fbv4je", "%s"]]]`, escapedInnerStr)
	encodedPayload := strings.ReplaceAll(url.QueryEscape(payloadStr), "+", "%20")
	bodyStr := "f.req=" + encodedPayload

	postReq, err := http.NewRequest("POST", postURL, strings.NewReader(bodyStr))
	if err != nil {
		return "", fmt.Errorf("error creating POST request: %w", err)
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	postReq.Header.Set("User-Agent", userAgent)
	postReq.Header.Set("Origin", "https://news.google.com")
	postReq.Header.Set("Referer", fetchURL)

	postResp, err := client.Do(postReq)
	if err != nil {
		return "", fmt.Errorf("error executing POST: %w", err)
	}
	defer postResp.Body.Close()

	postBodyBytes, err := io.ReadAll(postResp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading POST response: %w", err)
	}
	postBody := string(postBodyBytes)

	if postResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("batchexecute returned HTTP status %d", postResp.StatusCode)
	}

	if strings.HasPrefix(strings.TrimSpace(postBody), "<") {
		return "", fmt.Errorf("batchexecute returned HTML instead of JSON (blocked or rate-limited by Google)")
	}

	parts := strings.Split(postBody, "\n\n")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid batchexecute response format")
	}

	var outer []interface{}
	err = json.Unmarshal([]byte(parts[1]), &outer)
	if err != nil {
		return "", fmt.Errorf("error parsing batchexecute outer JSON: %w", err)
	}

	if len(outer) == 0 {
		return "", fmt.Errorf("outer JSON array is empty")
	}

	inner, ok := outer[0].([]interface{})
	if !ok || len(inner) < 3 {
		return "", fmt.Errorf("invalid inner array structure")
	}

	nestedJSONStr, ok := inner[2].(string)
	if !ok {
		return "", fmt.Errorf("nested JSON string not found in inner[2]")
	}

	var nested []interface{}
	err = json.Unmarshal([]byte(nestedJSONStr), &nested)
	if err != nil {
		return "", fmt.Errorf("error parsing nested JSON: %w", err)
	}

	if len(nested) < 2 {
		return "", fmt.Errorf("nested array too short")
	}

	decodedURL, ok := nested[1].(string)
	if !ok {
		return "", fmt.Errorf("decoded URL not found in nested[1]")
	}

	return decodedURL, nil
}

func (s *Server) decodeGoogleNewsURL(sourceURL string) (string, error) {
	s.decodedCacheMu.RLock()
	if decoded, exists := s.decodedCache[sourceURL]; exists {
		s.decodedCacheMu.RUnlock()
		return decoded, nil
	}
	s.decodedCacheMu.RUnlock()

	var decoded string
	var err error
	maxRetries := 3

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		decoded, err = s.DecodeGoogleNewsURL(sourceURL)
		if err == nil {
			s.decodedCacheMu.Lock()
			s.decodedCache[sourceURL] = decoded
			s.decodedCacheMu.Unlock()
			return decoded, nil
		}
		s.log.Warn("Gagal decode URL Google News, mencoba kembali dengan proxy lain...", "percobaan", i+1, "error", err)
	}

	return "", err
}

func (s *Server) FetchGoogleNewsRSS(feedURL string) ([]RSSItem, error) {
	if feedURL == "" {
		feedURL = "https://news.google.com/rss?hl=id&gl=ID&ceid=ID:id"
	}

	req, err := http.NewRequest("GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := s.getProxyHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status error: %d %s", resp.StatusCode, resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var feed RSSFeed
	if err := xml.Unmarshal(bodyBytes, &feed); err != nil {
		return nil, err
	}

	return feed.Channel.Items, nil
}

func (s *Server) dashboardCron(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	categories := s.store.ListCategories()
	prompts := s.loadCustomPrompts()
	settings := s.store.GetSettings()

	s.renderTemplate(w, "cron.html", dashboardCronViewData{
		User:       user,
		Settings:   settings,
		Categories: categories,
		Prompts:    prompts,
	})
}

func (s *Server) cronSaveSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}

	categories := s.store.ListCategories()
	prompts := s.loadCustomPrompts()

	settings := s.store.GetSettings()
	settings["cron_enabled"] = r.FormValue("cron_enabled")
	settings["cron_interval"] = r.FormValue("cron_interval")
	settings["cron_category"] = r.FormValue("cron_category")
	settings["cron_rss_url"] = r.FormValue("cron_rss_url")
	settings["cron_prompt"] = r.FormValue("cron_prompt")

	err := s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderTemplate(w, "cron.html", dashboardCronViewData{
			User:       user,
			Settings:   settings,
			Categories: categories,
			Prompts:    prompts,
			Error:      "Gagal memperbarui pengaturan: " + err.Error(),
		})
		return
	}

	s.renderTemplate(w, "cron.html", dashboardCronViewData{
		User:       user,
		Settings:   settings,
		Categories: categories,
		Prompts:    prompts,
		Success:    "Pengaturan auto-post berhasil diperbarui",
	})
}

func (s *Server) cronFetch(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()
	rssURL := settings["cron_rss_url"]
	if rssURL == "" {
		rssURL = "https://news.google.com/rss?hl=id&gl=ID&ceid=ID:id"
	}

	items, err := s.FetchGoogleNewsRSS(rssURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Gagal mengambil RSS: " + err.Error()})
		return
	}

	if len(items) > 15 {
		items = items[:15]
	}

	type resultStruct struct {
		index int
		item  DecodedFeedItem
		err   error
	}

	resultChan := make(chan resultStruct, len(items))
	sem := make(chan struct{}, 5) // limit to 5 concurrent decoding requests

	for i, item := range items {
		go func(idx int, rItem RSSItem) {
			sem <- struct{}{}
			defer func() { <-sem }()

			// Pre-check blacklist based on RSS source URL to save proxy bandwidth
			if rItem.Source.URL != "" {
				if parsedSrc, pErr := url.Parse(rItem.Source.URL); pErr == nil {
					srcHost := parsedSrc.Hostname()
					if srcHost != "" {
						if isBl, _ := s.store.IsDomainBlacklisted(srcHost); isBl {
							resultChan <- resultStruct{
								index: idx,
								item: DecodedFeedItem{
									Title:       rItem.Title,
									GoogleLink:  rItem.Link,
									OriginalURL: rItem.Source.URL,
									PubDate:     rItem.PubDate,
									Exists:      true,
								},
							}
							return
						}
					}
				}
			}

			var decodedURL string
			var err error
			if strings.Contains(rItem.Link, "news.google.com") {
				decodedURL, err = s.decodeGoogleNewsURL(rItem.Link)
				if err != nil {
					decodedURL = rItem.Link
				}
			} else {
				decodedURL = rItem.Link
			}

			exists, _ := s.store.ArticleExistsBySourceURL(decodedURL)
			var articleSlug string
			if exists {
				articleSlug, _ = s.store.ArticleSlugBySourceURL(decodedURL)
			}

			resultChan <- resultStruct{
				index: idx,
				item: DecodedFeedItem{
					Title:       rItem.Title,
					GoogleLink:  rItem.Link,
					OriginalURL: decodedURL,
					PubDate:     rItem.PubDate,
					Exists:      exists,
					ArticleSlug: articleSlug,
				},
				err: err,
			}
		}(i, item)
	}

	results := make([]DecodedFeedItem, len(items))
	for range items {
		res := <-resultChan
		results[res.index] = res.item
	}

	writeJSON(w, http.StatusOK, results)
}

type cronManualImportRequest struct {
	Title       string `json:"title"`
	GoogleLink  string `json:"google_link"`
	OriginalURL string `json:"original_url"`
}

type cronManualImportResponse struct {
	ArticleID   string `json:"article_id,omitempty"`
	ArticleSlug string `json:"article_slug,omitempty"`
	Title       string `json:"title,omitempty"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
}

func (s *Server) cronManualImport(w http.ResponseWriter, r *http.Request) {
	var req cronManualImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, cronManualImportResponse{Success: false, Error: "Request body tidak valid"})
		return
	}

	user := userFromRequest(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, cronManualImportResponse{Success: false, Error: "Unauthorized"})
		return
	}

	parsedURL, err := url.Parse(req.OriginalURL)
	var host string
	if err == nil {
		host = parsedURL.Hostname()
	}
	if host != "" {
		isBlacklisted, _ := s.store.IsDomainBlacklisted(host)
		if isBlacklisted {
			writeJSON(w, http.StatusBadRequest, cronManualImportResponse{
				Success: false,
				Error:   "Halaman rujukan tidak dapat di-scrape karena domain website ini (" + host + ") terdaftar dalam blacklist.",
			})
			return
		}
	}

	settings := s.store.GetSettings()
	model := settings["ai_default_model"]
	if model == "" {
		model = "gemini-2.5-flash"
	}
	promptInstruction := settings["cron_prompt"]

	htmlContent, err := s.fetchHTMLContent(req.OriginalURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, cronManualImportResponse{
			Success: false,
			Error:   "Halaman rujukan tidak dapat di-scrape. Pastikan koneksi atau link benar. Detail error: " + err.Error(),
		})
		return
	}

	title, bodyContent := extractTitleAndContent(htmlContent)
	if (title == "Judul tidak ditemukan" || title == "") && req.Title != "" {
		title = req.Title
	}
	heroImage := extractThumbnail(htmlContent)

	// Validasi hasil scrape artikel
	cleanContent := strings.TrimSpace(bodyContent)
	cleanTitle := strings.TrimSpace(title)
	isFallbackContent := cleanContent == "Konten utama tidak berhasil diekstrak otomatis. Anda bisa menulis atau menyalinnya secara manual."
	if cleanTitle == "" || cleanTitle == "Judul tidak ditemukan" || cleanContent == "" || isFallbackContent {
		s.blacklistURLHost(req.OriginalURL)
		writeJSON(w, http.StatusBadRequest, cronManualImportResponse{
			Success: false,
			Error:   "Halaman rujukan tidak dapat di-scrape atau tidak memiliki konten berita yang valid. Domain website ini telah masuk ke blacklist.",
		})
		return
	}

	categories := s.store.ListCategories()
	rewrittenTitle, excerpt, content, categoryName, err := s.performAIRewriteSync(title, bodyContent, promptInstruction, model, categories)
	
	aiOutputContainsUnavailable := strings.Contains(strings.ToLower(rewrittenTitle), "konten utama tidak tersedia") ||
		strings.Contains(strings.ToLower(excerpt), "konten utama tidak tersedia") ||
		strings.Contains(strings.ToLower(content), "konten utama tidak tersedia")

	if err != nil || aiOutputContainsUnavailable {
		s.blacklistURLHost(req.OriginalURL)
		errMsg := "Gagal rewrite dengan AI"
		if err != nil {
			errMsg = errMsg + ": " + err.Error()
		} else {
			errMsg = errMsg + ": AI mendeteksi konten utama tidak tersedia"
		}
		writeJSON(w, http.StatusBadRequest, cronManualImportResponse{
			Success: false,
			Error:   errMsg + ". Domain website ini telah masuk ke blacklist.",
		})
		return
	}

	categoryName = strings.TrimSpace(categoryName)
	var matchedCategory *cms.Category
	for _, cat := range categories {
		if strings.EqualFold(cat.Name, categoryName) {
			matchedCategory = &cat
			break
		}
	}

	if matchedCategory == nil && categoryName != "" {
		adminUser := *user
		adminUser.Role = cms.RoleAdmin
		newCat, err := s.store.CreateCategory(&adminUser, cms.CategoryInput{
			Name: categoryName,
			Slug: cms.Slugify(categoryName),
		})
		if err == nil && newCat != nil {
			categoryName = newCat.Name
		} else {
			s.logCron(slog.LevelWarn, "Failed to create auto category in manual import", "category", categoryName, "error", err)
			if len(categories) > 0 {
				categoryName = categories[0].Name
			} else {
				categoryName = "Berita"
			}
		}
	} else if matchedCategory != nil {
		categoryName = matchedCategory.Name
	} else {
		categoryName = "Berita"
	}

	artInput := core.ArticleInput{
		Title:        rewrittenTitle,
		Content:      content,
		Excerpt:      excerpt,
		Category:     categoryName,
		HeroImageURL: heroImage,
		Status:       core.ArticlePublished,
		SourceURL:    req.OriginalURL,
		ImageSource:  determineImageSource(heroImage, req.OriginalURL),
	}

	slug := cms.Slugify(rewrittenTitle)
	isDuplicate, dErr := s.store.ArticleExistsByTitleOrSlug(rewrittenTitle, slug)
	if dErr == nil && isDuplicate {
		writeJSON(w, http.StatusBadRequest, cronManualImportResponse{
			Success: false,
			Error:   "Gagal mengimpor: artikel dengan judul atau slug yang serupa sudah terdaftar di portal berita (mencegah duplikasi).",
		})
		return
	}

	adminUser := *user
	adminUser.Role = cms.RoleAdmin
	createdArt, err := s.store.CreateArticle(&adminUser, artInput)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, cronManualImportResponse{Success: false, Error: "Gagal menyimpan artikel ke DB: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, cronManualImportResponse{
		ArticleID:   createdArt.ID,
		ArticleSlug: createdArt.Slug,
		Title:       createdArt.Title,
		Success:     true,
	})
}

func (s *Server) fetchHTMLContent(targetURL string) (string, error) {
	client := s.getProxyHTTPClient()
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("received HTTP status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(bodyBytes), nil
}

func (s *Server) performAIRewriteSync(title, content, promptInstruction, model string, categories []cms.Category) (string, string, string, string, error) {
	settings := s.store.GetSettings()
	apiKey := settings["ai_api_key"]
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	endpointURL := strings.TrimSpace(settings["ai_endpoint_url"])

	if apiKey == "" || model == "mock-rewrite" {
		mockTitle := fmt.Sprintf("[AUTO-REWRITE] %s", title)
		mockExcerpt := "Hasil rewrite otomatis berita Google News menggunakan Mock AI."
		if len(content) > 200 {
			mockExcerpt = content[:190] + "..."
		}
		mockContent := fmt.Sprintf("<p>Berikut ini adalah hasil tulis ulang otomatis dari berita rujukan:</p>\n\n<p>%s</p>", content)
		if promptInstruction != "" {
			mockContent += fmt.Sprintf("\n\n<p><em>(Catatan AI: Tulis ulang disesuaikan dengan instruksi kustom: \"%s\")</em></p>", promptInstruction)
		}
		mockCategory := "Berita"
		if len(categories) > 0 {
			mockCategory = categories[0].Name
		}
		return mockTitle, mockExcerpt, mockContent, mockCategory, nil
	}

	isGemini := endpointURL == "" || strings.Contains(endpointURL, "googleapis.com")
	var apiURL string
	var reqBytes []byte
	var err error

	promptBuilder := strings.Builder{}
	promptBuilder.WriteString(`Anda adalah seorang jurnalis profesional senior untuk portal berita lokal. Tugas Anda adalah melakukan rekonstruksi dan penulisan ulang (rewrite) mendalam terhadap artikel berita rujukan dengan standar jurnalisme tinggi, bukan sekadar menyalin atau menerjemahkan kata demi kata.

Berikut adalah prinsip penulisan yang wajib Anda ikuti:
1. Narasi & Sudut Pandang Orisinal: Sajikan informasi utama dari berita rujukan menggunakan struktur kalimat baru yang dinamis, kaya kosakata, dan mengalir secara alami (tidak kaku).
2. Gaya Jurnalistik Kredibel: Tulis artikel dengan nada objektif, kredibel, dan mudah dipahami oleh masyarakat lokal, menerapkan kaidah 5W+1H secara proporsional.
3. Judul Kuat & Memikat: Buat judul baru yang menarik perhatian pembaca lokal, formal, informatif, dan mutlak bebas dari unsur clickbait.
4. Excerpt/Rangkuman Ringkas: Sediakan ringkasan berita dalam 1-2 kalimat padat (maksimal 220 karakter) sebagai pemandu pembaca.
5. Struktur Paragraf & Format HTML: Bagi artikel ke dalam beberapa paragraf logis yang teratur (pisahkan dengan baris baru ganda). Setiap paragraf wajib dibungkus dengan tag HTML <p>. Hindari pengulangan kalimat.
6. Klasifikasi Kategori: Tentukan kategori yang paling tepat untuk artikel ini. Pilihlah salah satu nama kategori dari daftar kategori berikut yang paling relevan:
`)

	if len(categories) > 0 {
		var catNames []string
		for _, cat := range categories {
			catNames = append(catNames, cat.Name)
		}
		promptBuilder.WriteString(fmt.Sprintf("[%s]\n", strings.Join(catNames, ", ")))
	} else {
		promptBuilder.WriteString("[Berita]\n")
	}
	promptBuilder.WriteString("Jika tidak ada kategori dari daftar di atas yang cocok, Anda wajib membuat nama kategori baru yang sangat spesifik, relevan, dan singkat (1-2 kata saja).\n")

	if strings.TrimSpace(promptInstruction) != "" {
		promptBuilder.WriteString(fmt.Sprintf("\n5. %s", strings.TrimSpace(promptInstruction)))
	}

	promptBuilder.WriteString(fmt.Sprintf(`

Artikel Asli:
Judul: %s
Konten: %s`, title, content))

	prompt := promptBuilder.String()

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
		type geminiSchemaProperty struct {
			Type string `json:"type"`
		}
		type geminiSchema struct {
			Type       string                          `json:"type"`
			Properties map[string]geminiSchemaProperty `json:"properties"`
			Required   []string                        `json:"required"`
		}
		type geminiGenConfig struct {
			ResponseMimeType string        `json:"responseMimeType"`
			ResponseSchema   *geminiSchema `json:"responseSchema,omitempty"`
		}
		type geminiReq struct {
			Contents         []geminiContent  `json:"contents"`
			GenerationConfig *geminiGenConfig `json:"generationConfig,omitempty"`
		}

		gReq := geminiReq{
			Contents: []geminiContent{
				{
					Parts: []geminiPart{
						{Text: prompt},
					},
				},
			},
			GenerationConfig: &geminiGenConfig{
				ResponseMimeType: "application/json",
				ResponseSchema: &geminiSchema{
					Type: "OBJECT",
					Properties: map[string]geminiSchemaProperty{
						"title":    {Type: "STRING"},
						"excerpt":  {Type: "STRING"},
						"content":  {Type: "STRING"},
						"category": {Type: "STRING"},
					},
					Required: []string{"title", "excerpt", "content", "category"},
				},
			},
		}
		reqBytes, err = json.Marshal(gReq)
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
		type openAIResponseFormat struct {
			Type string `json:"type"`
		}
		type openAIReq struct {
			Model          string                `json:"model"`
			Messages       []openAIMessage       `json:"messages"`
			ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
		}

		oReq := openAIReq{
			Model: model,
			Messages: []openAIMessage{
				{
					Role:    "system",
					Content: "Anda adalah asisten AI jurnalis profesional yang ahli menulis ulang artikel berita ke bahasa Indonesia. Anda wajib merespon hanya dengan format JSON yang valid.",
				},
				{
					Role:    "user",
					Content: prompt + "\n\nRespon WAJIB berupa objek JSON dengan key: title, excerpt, content, category.",
				},
			},
			ResponseFormat: &openAIResponseFormat{Type: "json_object"},
		}
		reqBytes, err = json.Marshal(oReq)
	}

	if err != nil {
		return "", "", "", "", err
	}

	var client *http.Client
	if settings["ai_enable_proxy"] == "true" {
		client = s.getProxyHTTPClientWithTimeout(120 * time.Second)
	} else {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	httpReq, err := http.NewRequest("POST", apiURL, strings.NewReader(string(reqBytes)))
	if err != nil {
		return "", "", "", "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if !isGemini {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return "", "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyErr, _ := io.ReadAll(resp.Body)
		return "", "", "", "", fmt.Errorf("AI API status code: %d, error: %s", resp.StatusCode, string(bodyErr))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", "", err
	}

	var rawJSONText string
	if isGemini {
		type geminiCandidate struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		}
		type geminiResp struct {
			Candidates []geminiCandidate `json:"candidates"`
		}

		var gResp geminiResp
		if err := json.Unmarshal(bodyBytes, &gResp); err != nil {
			return "", "", "", "", err
		}
		if len(gResp.Candidates) == 0 || len(gResp.Candidates[0].Content.Parts) == 0 {
			return "", "", "", "", fmt.Errorf("empty response from Gemini")
		}
		rawJSONText = gResp.Candidates[0].Content.Parts[0].Text
	} else {
		type openAIChoice struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		type openAIResp struct {
			Choices []openAIChoice `json:"choices"`
		}

		var oResp openAIResp
		if err := json.Unmarshal(bodyBytes, &oResp); err != nil {
			return "", "", "", "", err
		}
		if len(oResp.Choices) == 0 {
			return "", "", "", "", fmt.Errorf("empty response from OpenAI")
		}
		rawJSONText = oResp.Choices[0].Message.Content
	}

	type structuredRewrite struct {
		Title    string `json:"title"`
		Excerpt  string `json:"excerpt"`
		Content  string `json:"content"`
		Category string `json:"category"`
	}

	cleanedJSONText := cleanJSONString(rawJSONText)
	var result structuredRewrite
	if err := json.Unmarshal([]byte(cleanedJSONText), &result); err != nil {
		mockCategory := "Berita"
		if len(categories) > 0 {
			mockCategory = categories[0].Name
		}
		return fmt.Sprintf("[REWRITE] %s", title), "Hasil penulisan ulang berita.", rawJSONText, mockCategory, nil
	}

	cat := strings.TrimSpace(result.Category)
	if cat == "" {
		if len(categories) > 0 {
			cat = categories[0].Name
		} else {
			cat = "Berita"
		}
	}

	return result.Title, result.Excerpt, result.Content, cat, nil
}

func (s *Server) ExecuteAutoPostJob() error {
	s.cronMu.Lock()
	if s.cronRunning {
		s.cronMu.Unlock()
		return fmt.Errorf("auto-post job is already running")
	}
	s.cronRunning = true
	s.cronMu.Unlock()

	defer func() {
		s.cronMu.Lock()
		s.cronRunning = false
		s.cronMu.Unlock()
	}()

	settings := s.store.GetSettings()
	
	apiKey := settings["ai_api_key"]
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		s.logCron(slog.LevelError, "Auto-post dihentikan karena kredensial AI (API Key) belum dikonfigurasi di menu Settings.")
		return fmt.Errorf("AI API Key is not configured")
	}

	primaryRSSURL := settings["cron_rss_url"]
	if primaryRSSURL == "" {
		primaryRSSURL = "https://news.google.com/rss?hl=id&gl=ID&ceid=ID:id"
	}

	systemUser, err := s.store.GetSystemUser()
	if err != nil {
		return fmt.Errorf("failed to get system user: %w", err)
	}

	// List of URLs to try
	urlsToTry := []string{primaryRSSURL}
	
	// Fallback URLs to try if the primary one yields 0 new articles (all blocked or already imported)
	fallbackURLs := []string{
		"https://news.google.com/rss/search?q=teknologi&hl=id&gl=ID&ceid=ID:id",         // Teknologi
		"https://news.google.com/rss/search?q=bisnis&hl=id&gl=ID&ceid=ID:id",            // Bisnis
		"https://news.google.com/rss/search?q=politik+nasional&hl=id&gl=ID&ceid=ID:id", // Nasional / Politik
		"https://news.google.com/rss/search?q=olahraga&hl=id&gl=ID&ceid=ID:id",          // Olahraga
		"https://news.google.com/rss/search?q=hiburan&hl=id&gl=ID&ceid=ID:id",           // Hiburan
		"https://news.google.com/rss/search?q=kesehatan&hl=id&gl=ID&ceid=ID:id",          // Kesehatan
	}
	for _, fURL := range fallbackURLs {
		if fURL != primaryRSSURL {
			urlsToTry = append(urlsToTry, fURL)
		}
	}

	newArticlesImported := 0
	targetNewArticles := 5

	for urlIndex, currentURL := range urlsToTry {
		if urlIndex > 0 {
			s.logCron(slog.LevelInfo, "Mencoba feed alternatif karena feed sebelumnya tidak menghasilkan artikel baru...", "url", currentURL)
		} else {
			s.logCron(slog.LevelInfo, "Mengambil feed RSS...", "url", currentURL)
		}

		items, err := s.FetchGoogleNewsRSS(currentURL)
		if err != nil {
			s.logCron(slog.LevelError, "Gagal mengambil feed RSS, mencoba feed berikutnya...", "url", currentURL, "error", err)
			continue
		}

		maxToScan := 100
		if len(items) > maxToScan {
			items = items[:maxToScan]
		}
		s.logCron(slog.LevelInfo, fmt.Sprintf("Berhasil mengambil %d item feed RSS untuk diproses", len(items)))

		for _, item := range items {
			if newArticlesImported >= targetNewArticles {
				break
			}

			// Pre-check blacklist based on RSS source URL to save proxy bandwidth
			if item.Source.URL != "" {
				if parsedSrc, pErr := url.Parse(item.Source.URL); pErr == nil {
					srcHost := parsedSrc.Hostname()
					if srcHost != "" {
						isBlacklisted, _ := s.store.IsDomainBlacklisted(srcHost)
						if isBlacklisted {
							s.logCron(slog.LevelInfo, "Mengabaikan item feed karena domain masuk blacklist (pre-check)", "title", item.Title, "host", srcHost)
							continue
						}
					}
				}
			}

			var decodedURL string
			var err error
			if strings.Contains(item.Link, "news.google.com") {
				decodedURL, err = s.decodeGoogleNewsURL(item.Link)
				if err != nil {
					s.logCron(slog.LevelError, "Gagal men-decode URL Google News", "link", item.Link, "error", err)
					continue
				}
			} else {
				decodedURL = item.Link
			}

			// Check if URL domain is blacklisted and skip it
			parsedURL, pErr := url.Parse(decodedURL)
			var host string
			if pErr == nil {
				host = parsedURL.Hostname()
			}
			if host != "" {
				isBlacklisted, _ := s.store.IsDomainBlacklisted(host)
				if isBlacklisted {
					s.logCron(slog.LevelInfo, "Mengabaikan URL karena domain masuk blacklist", "url", decodedURL, "host", host)
					continue
				}
			}

			exists, err := s.store.ArticleExistsBySourceURL(decodedURL)
			if err != nil {
				s.logCron(slog.LevelError, "Query database gagal untuk ArticleExistsBySourceURL", "url", decodedURL, "error", err)
				continue
			}
			if exists {
				s.logCron(slog.LevelInfo, "Artikel sudah pernah diimport, melewati", "url", decodedURL)
				continue
			}

			s.logCron(slog.LevelInfo, "Mendownload konten rujukan asli", "url", decodedURL)
			htmlContent, err := s.fetchHTMLContent(decodedURL)
			if err != nil {
				s.logCron(slog.LevelError, "Gagal mengunduh HTML rujukan", "url", decodedURL, "error", err)
				continue
			}

			title, bodyContent := extractTitleAndContent(htmlContent)
			heroImage := extractThumbnail(htmlContent)

			// Validasi hasil scrape artikel
			cleanContent := strings.TrimSpace(bodyContent)
			cleanTitle := strings.TrimSpace(title)
			isFallbackContent := cleanContent == "Konten utama tidak berhasil diekstrak otomatis. Anda bisa menulis atau menyalinnya secara manual."
			if cleanTitle == "" || cleanTitle == "Judul tidak ditemukan" || cleanContent == "" || isFallbackContent {
				s.logCron(slog.LevelError, "Halaman rujukan tidak memiliki konten berita yang valid", "url", decodedURL)
				s.blacklistURLHost(decodedURL) // Restored automatic blacklisting
				continue
			}

			promptInstruction := settings["cron_prompt"]
			model := settings["ai_default_model"]
			if model == "" {
				model = "gemini-2.5-flash"
			}

			s.logCron(slog.LevelInfo, "Melakukan penulisan ulang (rewrite) dengan AI", "title", title, "model", model)
			categories := s.store.ListCategories()
			rewrittenTitle, excerpt, content, categoryName, err := s.performAIRewriteSync(title, bodyContent, promptInstruction, model, categories)
			
			aiOutputContainsUnavailable := strings.Contains(strings.ToLower(rewrittenTitle), "konten utama tidak tersedia") ||
				strings.Contains(strings.ToLower(excerpt), "konten utama tidak tersedia") ||
				strings.Contains(strings.ToLower(content), "konten utama tidak tersedia")

			if err != nil || aiOutputContainsUnavailable {
				if err != nil {
					s.logCron(slog.LevelError, "Gagal menulis ulang artikel dengan AI", "title", title, "error", err)
				} else {
					s.logCron(slog.LevelError, "AI mendeteksi konten utama tidak tersedia", "title", title)
				}
				s.blacklistURLHost(decodedURL) // Restored automatic blacklisting
				continue
			}

			categoryName = strings.TrimSpace(categoryName)
			var matchedCategory *cms.Category
			for _, cat := range categories {
				if strings.EqualFold(cat.Name, categoryName) {
					matchedCategory = &cat
					break
				}
			}

			if matchedCategory == nil && categoryName != "" {
				newCat, err := s.store.CreateCategory(systemUser, cms.CategoryInput{
					Name: categoryName,
					Slug: cms.Slugify(categoryName),
				})
				if err == nil && newCat != nil {
					categoryName = newCat.Name
				} else {
					s.logCron(slog.LevelWarn, "Gagal membuat kategori otomatis", "category", categoryName, "error", err)
					if len(categories) > 0 {
						categoryName = categories[0].Name
					} else {
						categoryName = "Berita"
					}
				}
			} else if matchedCategory != nil {
				categoryName = matchedCategory.Name
			} else {
				categoryName = "Berita"
			}

			artInput := core.ArticleInput{
				Title:        rewrittenTitle,
				Content:      content,
				Excerpt:      excerpt,
				Category:     categoryName,
				HeroImageURL: heroImage,
				Status:       core.ArticlePublished,
				SourceURL:    decodedURL,
				ImageSource:  determineImageSource(heroImage, decodedURL),
			}

			slug := cms.Slugify(rewrittenTitle)
			isDuplicate, dErr := s.store.ArticleExistsByTitleOrSlug(rewrittenTitle, slug)
			if dErr == nil && isDuplicate {
				s.logCron(slog.LevelInfo, "Mengabaikan artikel otomatis karena terdeteksi duplikasi judul atau slug", "title", rewrittenTitle)
				continue
			}

			createdArt, err := s.store.CreateArticle(systemUser, artInput)
			if err != nil {
				s.logCron(slog.LevelError, "Gagal menyimpan artikel otomatis ke database", "title", rewrittenTitle, "error", err)
				continue
			}
			s.logCron(slog.LevelInfo, "Berhasil auto-post artikel", "id", createdArt.ID, "title", rewrittenTitle)
			newArticlesImported++
		}

		if newArticlesImported >= targetNewArticles {
			s.logCron(slog.LevelInfo, fmt.Sprintf("Target %d posting artikel baru telah tercapai. Menghentikan proses auto-post.", targetNewArticles))
			break
		}

		if newArticlesImported > 0 {
			s.logCron(slog.LevelInfo, fmt.Sprintf("Berhasil memposting %d artikel baru. Menghentikan proses auto-post.", newArticlesImported))
			break
		}
	}

	nowStr := time.Now().Format(time.RFC3339)
	settings = s.store.GetSettings()
	settings["cron_last_run"] = nowStr
	_ = s.store.UpdateSettings(systemUser, settings)
	return nil
}

func (s *Server) StartCronBackgroundJob() {
	s.loadCronLogs()
	go func() {
		s.logCron(slog.LevelInfo, "Memulai pekerja latar belakang auto-post...")
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			settings := s.store.GetSettings()
			if settings["cron_enabled"] != "true" {
				continue
			}

			intervalMinStr := settings["cron_interval"]
			if intervalMinStr == "" {
				intervalMinStr = "60"
			}
			var intervalMin int
			_, err := fmt.Sscanf(intervalMinStr, "%d", &intervalMin)
			if err != nil || intervalMin <= 0 {
				intervalMin = 60
			}

			lastRunStr := settings["cron_last_run"]
			var lastRun time.Time
			if lastRunStr != "" {
				lastRun, _ = time.Parse(time.RFC3339, lastRunStr)
			}

			if time.Since(lastRun) >= time.Duration(intervalMin)*time.Minute {
				s.logCron(slog.LevelInfo, "Interval cron tercapai, mengeksekusi pekerjaan auto-post...")
				err := s.ExecuteAutoPostJob()
				if err != nil {
					s.logCron(slog.LevelError, "Pekerjaan auto-post latar belakang gagal", "error", err)
				} else {
					s.logCron(slog.LevelInfo, "Pekerjaan auto-post latar belakang selesai dengan sukses")
				}
			}
		}
	}()
}

func (s *Server) loadCronLogs() {
	s.cronLogsMu.Lock()
	defer s.cronLogsMu.Unlock()

	if err := os.MkdirAll("data", 0755); err != nil {
		s.log.Error("Failed to create data directory", "error", err)
		return
	}

	filePath := filepath.Join("data", "cron_logs.json")
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.cronLogs = []CronLog{}
			return
		}
		s.log.Error("Failed to open cron logs file", "error", err)
		return
	}
	defer file.Close()

	var logs []CronLog
	if err := json.NewDecoder(file).Decode(&logs); err != nil {
		s.log.Error("Failed to decode cron logs", "error", err)
		s.cronLogs = []CronLog{}
		return
	}

	s.cronLogs = s.pruneOldCronLogs(logs)
}

func (s *Server) logCron(level slog.Level, msg string, args ...any) {
	s.log.Log(context.Background(), level, msg, args...)

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

	s.cronLogsMu.Lock()
	defer s.cronLogsMu.Unlock()

	s.cronLogs = s.pruneOldCronLogs(s.cronLogs)
	if len(s.cronLogs) >= 500 {
		s.cronLogs = s.cronLogs[1:]
	}
	s.cronLogs = append(s.cronLogs, CronLog{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   logMsg,
	})

	filePath := filepath.Join("data", "cron_logs.json")
	file, err := os.Create(filePath)
	if err != nil {
		s.log.Error("Failed to create cron logs file for writing", "error", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s.cronLogs); err != nil {
		s.log.Error("Failed to encode cron logs to file", "error", err)
	}
}

func (s *Server) cronGetLogs(w http.ResponseWriter, r *http.Request) {
	s.cronLogsMu.RLock()
	defer s.cronLogsMu.RUnlock()

	logs := s.cronLogs
	if logs == nil {
		logs = []CronLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) cronClearLogs(w http.ResponseWriter, r *http.Request) {
	s.cronLogsMu.Lock()
	s.cronLogs = []CronLog{}
	
	filePath := filepath.Join("data", "cron_logs.json")
	if file, err := os.Create(filePath); err == nil {
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(s.cronLogs)
		file.Close()
	}
	s.cronLogsMu.Unlock()

	s.logCron(slog.LevelInfo, "Log aktivitas dibersihkan oleh pengguna")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) cronRunManual(w http.ResponseWriter, r *http.Request) {
	s.cronMu.Lock()
	if s.cronRunning {
		s.cronMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "message": "Pekerjaan auto-post sedang berjalan di latar belakang"})
		return
	}
	s.cronMu.Unlock()
	
	settings := s.store.GetSettings()
	apiKey := settings["ai_api_key"]
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Gagal memulai: Kredensial AI (API Key) belum dikonfigurasi di menu Settings"})
		return
	}

	go func() {
		s.logCron(slog.LevelInfo, "Memulai eksekusi auto-post manual...")
		err := s.ExecuteAutoPostJob()
		if err != nil {
			s.logCron(slog.LevelError, "Eksekusi auto-post manual gagal", "error", err)
		} else {
			s.logCron(slog.LevelInfo, "Eksekusi auto-post manual selesai dengan sukses")
		}
	}()

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Auto-post job started in background"})
}

func determineImageSource(heroImage, sourceURL string) string {
	if heroImage != "" {
		if parsed, err := url.Parse(heroImage); err == nil && parsed.Hostname() != "" {
			return "Sumber: " + parsed.Hostname()
		}
	}
	if sourceURL != "" {
		if parsed, err := url.Parse(sourceURL); err == nil && parsed.Hostname() != "" {
			return "Sumber: " + parsed.Hostname()
		}
	}
	return "Sumber: Google News"
}

func (s *Server) processFacebookAutoPost(force bool) error {
	settings := s.store.GetSettings()
	if !force && settings["fb_auto_post_enabled"] != "true" {
		return nil
	}

	pageID := settings["fb_page_id"]
	accessToken := settings["fb_access_token"]
	if pageID == "" || accessToken == "" {
		return fmt.Errorf("Facebook Page ID dan Access Token wajib diisi untuk auto-posting")
	}

	// Ambil artikel yang diterbitkan (maksimal 10 terbaru)
	articles := s.store.ListPublishedArticles(10)
	if len(articles) == 0 {
		s.logFB(slog.LevelInfo, "Tidak ada artikel dengan status terbit (published) yang ditemukan.")
		return nil
	}

	// Cari artikel pertama yang belum diposting ke Facebook
	var targetArticle *cms.Article
	for _, art := range articles {
		posted, err := s.store.IsArticlePostedToFB(art.ID)
		if err != nil {
			s.logFB(slog.LevelError, "Gagal memeriksa status posting FB untuk artikel", "article_id", art.ID, "error", err)
			continue
		}
		if !posted {
			targetArticle = &art
			break
		}
	}

	if targetArticle == nil {
		s.logFB(slog.LevelInfo, "Semua artikel terbit sudah terposting ke Facebook sebelumnya.")
		return nil // Semua artikel sudah diposting
	}

	s.logFB(slog.LevelInfo, "Memulai proses auto-post ke Facebook untuk artikel", "title", targetArticle.Title)

	// Ambil model AI & kustom prompt
	model := settings["ai_default_model"]
	if model == "" {
		model = "gemini-2.5-flash"
	}
	customPrompt := settings["fb_custom_prompt"]

	// 1. Generate Caption dengan AI
	caption, err := s.performFBCaptionGenSync(targetArticle.Title, targetArticle.Excerpt, targetArticle.Content, customPrompt, model)
	if err != nil {
		return fmt.Errorf("gagal membuat caption AI: %w", err)
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

	s.logFB(slog.LevelInfo, "Mengirim postingan ke Facebook...", "link", articleLink)

	// 3. Posting ke Halaman Facebook
	fbPostID, err := s.postToFacebookPage(pageID, accessToken, caption, articleLink)
	if err != nil {
		return fmt.Errorf("gagal memposting ke Facebook: %w", err)
	}

	// 4. Tandai artikel sebagai terposting ke FB di DB
	err = s.store.MarkArticleAsPostedToFB(targetArticle.ID, fbPostID)
	if err != nil {
		s.logFB(slog.LevelError, "Gagal menyimpan status terposting FB di database", "article_id", targetArticle.ID, "fb_post_id", fbPostID, "error", err)
	} else {
		fbPostLink := fmt.Sprintf(`<a href="https://www.facebook.com/%s" target="_blank" style="color: #60a5fa; text-decoration: underline;">https://www.facebook.com/%s</a>`, fbPostID, fbPostID)
		s.logFB(slog.LevelInfo, "Berhasil memposting artikel ke Halaman Facebook!", "title", targetArticle.Title, "fb_post_id", fbPostID, "post_url", fbPostLink)
	}

	return nil
}

func (s *Server) performFBCaptionGenSync(title, excerpt, content, customPrompt, model string) (string, error) {
	settings := s.store.GetSettings()
	apiKey := settings["ai_api_key"]
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	endpointURL := strings.TrimSpace(settings["ai_endpoint_url"])

	if apiKey == "" || model == "mock-rewrite" {
		mockCaption := fmt.Sprintf("📢 Baca artikel menarik ini: %s\n\nRangkuman: %s", title, excerpt)
		if customPrompt != "" {
			mockCaption += fmt.Sprintf("\n\n(Dibuat dengan gaya promt kustom: %s)", customPrompt)
		}
		return mockCaption, nil
	}

	isGemini := endpointURL == "" || strings.Contains(endpointURL, "googleapis.com")
	var apiURL string
	
	promptBuilder := strings.Builder{}
	promptBuilder.WriteString(`Anda adalah pengelola media sosial (Social Media Manager) profesional untuk portal berita. Tugas Anda adalah membuat caption media sosial Facebook yang sangat menarik, natural, interaktif, dan memikat pembaca agar mengklik tautan berita.

Panduan Penulisan Caption:
1. Gaya Penulisan: Tulis dengan gaya yang alami/natural seperti manusia (tidak kaku/robotik). Gunakan nada yang ramah dan menarik audiens.
2. Panjang Konten: Tulis caption dengan panjang 2-3 paragraf pendek yang memicu rasa ingin tahu (curiosity hook), sertakan poin menarik dari berita jika relevan.
3. Panggilan Aksi (CTA): Di bagian akhir, ajak pembaca untuk membaca selengkapnya melalui link yang akan disertakan.
4. Emoji & Hashtags: Gunakan beberapa emoji yang relevan secara wajar. Tambahkan 2-3 hashtag populer yang berkaitan dengan topik berita.
5. PENTING: Hanya berikan hasil akhir caption. Jangan menyertakan kata pembuka, penutup, penjelasan, tag link placeholders, atau format markdown tebal/miring yang berlebihan.
`)

	if strings.TrimSpace(customPrompt) != "" {
		promptBuilder.WriteString(fmt.Sprintf("\nInstruksi Kustom Tambahan dari Admin:\n%s\n", strings.TrimSpace(customPrompt)))
	}

	promptBuilder.WriteString(fmt.Sprintf(`
Detail Berita:
Judul: %s
Ringkasan: %s
Konten: %s`, title, excerpt, content))

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

func (s *Server) getPageAccessToken(pageID, userAccessToken, appSecret string) (string, error) {
	var proof string
	if appSecret != "" {
		proof = computeAppSecretProof(userAccessToken, appSecret)
	}

	apiURL := fmt.Sprintf("https://graph.facebook.com/v20.0/%s", pageID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	q := req.URL.Query()
	q.Set("fields", "access_token")
	q.Set("access_token", userAccessToken)
	if proof != "" {
		q.Set("appsecret_proof", proof)
	}
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 15 * time.Second}
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
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(bodyBytes, &errResp)
		if errResp.Error.Message != "" {
			return "", fmt.Errorf("gagal mendapatkan Page Access Token: %s", errResp.Error.Message)
		}
		return "", fmt.Errorf("gagal mendapatkan Page Access Token (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", err
	}

	if res.AccessToken == "" {
		return "", fmt.Errorf("Page Access Token tidak ditemukan dalam respon API")
	}

	return res.AccessToken, nil
}

func (s *Server) postToFacebookPage(pageID, accessToken, message, link string) (string, error) {
	// 1. Dapatkan App Secret dari settings
	settings := s.store.GetSettings()
	appSecret := settings["fb_app_secret"]

	// 2. Tukarkan User Token dengan Page Access Token jika memungkinkan
	pageAccessToken := accessToken
	if appSecret != "" {
		pat, err := s.getPageAccessToken(pageID, accessToken, appSecret)
		if err != nil {
			s.logFB(slog.LevelWarn, "Gagal menukar User Token dengan Page Token, menggunakan token asli", "error", err)
		} else {
			pageAccessToken = pat
		}
	}

	// 3. Hitung appsecret_proof untuk Page Access Token jika app secret tersedia
	var proof string
	if appSecret != "" {
		proof = computeAppSecretProof(pageAccessToken, appSecret)
	}

	apiURL := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/feed", pageID)

	form := url.Values{}
	form.Set("message", message)
	form.Set("link", link)
	form.Set("access_token", pageAccessToken)
	if proof != "" {
		form.Set("appsecret_proof", proof)
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
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
		var errResp struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(bodyBytes, &errResp)
		if errResp.Error.Message != "" {
			return "", fmt.Errorf("FB API error: %s (code %d)", errResp.Error.Message, errResp.Error.Code)
		}
		return "", fmt.Errorf("FB API error status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var resObj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bodyBytes, &resObj); err != nil {
		return "", fmt.Errorf("failed to decode FB response: %w", err)
	}

	return resObj.ID, nil
}

func (s *Server) StartProxyScraperJob() {
	s.loadProxyScraperLogs()
	go func() {
		s.logProxyScraper(slog.LevelInfo, "Memulai pekerja latar belakang auto-scraper proxy...")
		// Check every 3 minutes
		ticker := time.NewTicker(3 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			settings := s.store.GetSettings()
			if settings["proxy_auto_scrape_enabled"] != "true" {
				continue
			}

			thresholdStr := settings["proxy_auto_scrape_threshold"]
			if thresholdStr == "" {
				thresholdStr = "10"
			}
			var threshold int
			_, err := fmt.Sscanf(thresholdStr, "%d", &threshold)
			if err != nil || threshold <= 0 {
				threshold = 10
			}

			activeProxies := s.store.ListActiveProxies()
			activeCount := len(activeProxies)

			if activeCount < threshold {
				s.logProxyScraper(slog.LevelInfo, "Jumlah proxy aktif di bawah batas minimal, memulai auto-scraping...", "aktif", activeCount, "batas", threshold)
				go s.runBackgroundProxyScrapeAndImport()
			}
		}
	}()
}

func (s *Server) runBackgroundProxyScrapeAndImport() {
	systemUser, err := s.store.GetSystemUser()
	if err != nil {
		s.logProxyScraper(slog.LevelError, "Auto-scraper proxy gagal: tidak dapat mengambil system user", "error", err)
		return
	}

	publicURLs := []string{
		"https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&timeout=3000&country=all&ssl=all&anonymity=all",
		"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/http.txt",
	}

	var rawProxies []string
	for _, apiURL := range publicURLs {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(apiURL)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				rawProxies = append(rawProxies, line)
			}
		}
		resp.Body.Close()
	}

	if len(rawProxies) == 0 {
		s.logProxyScraper(slog.LevelInfo, "Auto-scraper proxy: tidak ada proxy yang ditemukan dari sumber publik")
		return
	}

	// Filter unique and non-existing proxies
	existingProxies := s.store.ListProxies()
	existingMap := make(map[string]bool)
	for _, ep := range existingProxies {
		key := fmt.Sprintf("%s:%d", ep.IP, ep.Port)
		existingMap[key] = true
	}

	var candidates []*LocalProxyConfig
	for _, raw := range rawProxies {
		p, err := parseProxyStringLocal(raw, "http")
		if err == nil && p != nil {
			key := fmt.Sprintf("%s:%s", p.Host, p.Port)
			if !existingMap[key] {
				candidates = append(candidates, p)
			}
		}
	}

	if len(candidates) == 0 {
		s.logProxyScraper(slog.LevelInfo, "Auto-scraper proxy: semua proxy hasil scrape sudah terdaftar")
		return
	}

	// Limit to top 80 candidates for quick checking
	if len(candidates) > 80 {
		candidates = candidates[:80]
	}

	s.logProxyScraper(slog.LevelInfo, "Auto-scraper proxy: memulai pengujian paralel", "jumlah_calon", len(candidates))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 20)
	targetURL := "https://news.google.com/rss"
	timeout := 4 * time.Second
	importedCount := 0
	var importedMu sync.Mutex

	for _, p := range candidates {
		wg.Add(1)
		go func(pr *LocalProxyConfig) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			latency, err := testProxyLocal(pr, targetURL, timeout)
			if err == nil {
				portNum, _ := strconv.Atoi(pr.Port)
				dbProxy, err := s.store.CreateProxy(systemUser, cms.ProxyInput{
					IP:       pr.Host,
					Port:     portNum,
					Username: pr.Username,
					Password: pr.Password,
					Protocol: pr.Scheme,
				})
				if err == nil && dbProxy != nil {
					_ = s.store.UpdateProxyStatus(dbProxy.ID, "active", latency)
					importedMu.Lock()
					importedCount++
					importedMu.Unlock()
				}
			}
		}(p)
	}

	wg.Wait()
	s.logProxyScraper(slog.LevelInfo, "Auto-scraper proxy selesai", "diimpor", importedCount)
}

func (s *Server) pruneOldCronLogs(logs []CronLog) []CronLog {
	cutoff := time.Now().Add(-72 * time.Hour)
	var active []CronLog
	for _, l := range logs {
		if l.Timestamp.After(cutoff) {
			active = append(active, l)
		}
	}
	return active
}

func (s *Server) pruneOldAILogs(logs []AILogEntry) []AILogEntry {
	cutoff := time.Now().Add(-72 * time.Hour)
	var active []AILogEntry
	for _, l := range logs {
		t, err := time.Parse("2006-01-02 15:04:05 MST", l.Timestamp)
		if err != nil {
			active = append(active, l)
			continue
		}
		if t.After(cutoff) {
			active = append(active, l)
		}
	}
	return active
}

func (s *Server) loadProxyScraperLogs() {
	s.proxyScraperLogsMu.Lock()
	defer s.proxyScraperLogsMu.Unlock()

	if err := os.MkdirAll("data", 0755); err != nil {
		s.log.Error("Failed to create data directory", "error", err)
		return
	}

	filePath := filepath.Join("data", "proxy_scraper_logs.json")
	file, err := os.Open(filePath)
	if err != nil {
		s.proxyScraperLogs = []CronLog{}
		return
	}
	defer file.Close()

	var logs []CronLog
	if err := json.NewDecoder(file).Decode(&logs); err != nil {
		s.log.Error("Failed to decode proxy scraper logs", "error", err)
		s.proxyScraperLogs = []CronLog{}
		return
	}

	s.proxyScraperLogs = s.pruneOldCronLogs(logs)
}

func (s *Server) logProxyScraper(level slog.Level, msg string, args ...any) {
	s.log.Log(context.Background(), level, "[Auto-Scraper] "+msg, args...)

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

	s.proxyScraperLogsMu.Lock()
	defer s.proxyScraperLogsMu.Unlock()

	s.proxyScraperLogs = s.pruneOldCronLogs(s.proxyScraperLogs)
	if len(s.proxyScraperLogs) >= 500 {
		s.proxyScraperLogs = s.proxyScraperLogs[1:]
	}
	s.proxyScraperLogs = append(s.proxyScraperLogs, CronLog{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   logMsg,
	})

	filePath := filepath.Join("data", "proxy_scraper_logs.json")
	file, err := os.Create(filePath)
	if err != nil {
		s.log.Error("Failed to create proxy scraper logs file for writing", "error", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s.proxyScraperLogs); err != nil {
		s.log.Error("Failed to encode proxy scraper logs to file", "error", err)
	}
}

func (s *Server) proxyScraperGetLogs(w http.ResponseWriter, r *http.Request) {
	s.proxyScraperLogsMu.RLock()
	defer s.proxyScraperLogsMu.RUnlock()

	logs := s.proxyScraperLogs
	if logs == nil {
		logs = []CronLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) proxyScraperClearLogs(w http.ResponseWriter, r *http.Request) {
	s.proxyScraperLogsMu.Lock()
	s.proxyScraperLogs = []CronLog{}
	
	filePath := filepath.Join("data", "proxy_scraper_logs.json")
	if file, err := os.Create(filePath); err == nil {
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(s.proxyScraperLogs)
		file.Close()
	}
	s.proxyScraperLogsMu.Unlock()

	s.logProxyScraper(slog.LevelInfo, "Log auto-scraper dibersihkan oleh pengguna")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}


