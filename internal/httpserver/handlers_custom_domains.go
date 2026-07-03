package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"porta-berita/internal/cms"
	core "porta-berita/internal/cms"
)

type CustomDomain struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RSSURL       string    `json:"rss_url"`
	Category     string    `json:"category"`
	CustomPrompt string    `json:"custom_prompt"`
	Status       string    `json:"status"` // "active" or "inactive"
	CreatedAt    time.Time `json:"created_at"`
}

type customDomainsViewData struct {
	User       *cms.User
	Settings   map[string]string
	Categories []cms.Category
	Domains    []CustomDomain
	Success    string
	Error      string
}

func (s *Server) loadCustomDomains() ([]CustomDomain, error) {
	settings := s.store.GetSettings()
	val := settings["custom_domains"]
	if val == "" {
		return []CustomDomain{}, nil
	}
	var domains []CustomDomain
	if err := json.Unmarshal([]byte(val), &domains); err != nil {
		return nil, err
	}
	return domains, nil
}

func (s *Server) saveCustomDomains(domains []CustomDomain) error {
	bytes, err := json.Marshal(domains)
	if err != nil {
		return err
	}
	settings := s.store.GetSettings()
	settings["custom_domains"] = string(bytes)
	systemUser, err := s.store.GetSystemUser()
	if err != nil {
		return err
	}
	return s.store.UpdateSettings(systemUser, settings)
}

func (s *Server) dashboardCustomDomains(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	categories := s.store.ListCategories()
	settings := s.store.GetSettings()
	domains, err := s.loadCustomDomains()
	if err != nil {
		s.log.Error("failed to load custom domains", "error", err)
	}

	successMsg := r.URL.Query().Get("success")

	s.renderTemplate(w, "custom_domains.html", customDomainsViewData{
		User:       user,
		Settings:   settings,
		Categories: categories,
		Domains:    domains,
		Success:    successMsg,
	})
}

func (s *Server) addCustomDomain(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	rssURL := r.FormValue("rss_url")
	category := r.FormValue("category")
	customPrompt := r.FormValue("custom_prompt")

	if name == "" || rssURL == "" || category == "" {
		s.renderCustomDomainsWithError(w, r, "Nama, RSS URL, dan Kategori harus diisi.")
		return
	}

	domains, _ := s.loadCustomDomains()
	
	newDomain := CustomDomain{
		ID:           cms.RandomID("cd"),
		Name:         name,
		RSSURL:       rssURL,
		Category:     category,
		CustomPrompt: customPrompt,
		Status:       "active",
		CreatedAt:    time.Now(),
	}

	domains = append(domains, newDomain)
	if err := s.saveCustomDomains(domains); err != nil {
		s.renderCustomDomainsWithError(w, r, "Gagal menyimpan domain kustom: "+err.Error())
		return
	}

	http.Redirect(w, r, "/dashboard/custom-domains?success=Domain+kustom+berhasil+ditambahkan", http.StatusSeeOther)
}

func (s *Server) editCustomDomain(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	id := r.FormValue("id")
	name := r.FormValue("name")
	rssURL := r.FormValue("rss_url")
	category := r.FormValue("category")
	customPrompt := r.FormValue("custom_prompt")
	status := r.FormValue("status")

	if id == "" || name == "" || rssURL == "" || category == "" {
		s.renderCustomDomainsWithError(w, r, "Semua data wajib diisi.")
		return
	}

	domains, _ := s.loadCustomDomains()
	found := false
	for i, dom := range domains {
		if dom.ID == id {
			domains[i].Name = name
			domains[i].RSSURL = rssURL
			domains[i].Category = category
			domains[i].CustomPrompt = customPrompt
			if status == "active" || status == "inactive" {
				domains[i].Status = status
			}
			found = true
			break
		}
	}

	if !found {
		s.renderCustomDomainsWithError(w, r, "Domain kustom tidak ditemukan.")
		return
	}

	if err := s.saveCustomDomains(domains); err != nil {
		s.renderCustomDomainsWithError(w, r, "Gagal mengupdate domain kustom: "+err.Error())
		return
	}

	http.Redirect(w, r, "/dashboard/custom-domains?success=Domain+kustom+berhasil+diperbarui", http.StatusSeeOther)
}

func (s *Server) deleteCustomDomain(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "Missing ID", http.StatusBadRequest)
		return
	}

	domains, _ := s.loadCustomDomains()
	newDomains := []CustomDomain{}
	found := false
	for _, dom := range domains {
		if dom.ID == id {
			found = true
			continue
		}
		newDomains = append(newDomains, dom)
	}

	if !found {
		s.renderCustomDomainsWithError(w, r, "Domain kustom tidak ditemukan.")
		return
	}

	if err := s.saveCustomDomains(newDomains); err != nil {
		s.renderCustomDomainsWithError(w, r, "Gagal menghapus domain kustom: "+err.Error())
		return
	}

	http.Redirect(w, r, "/dashboard/custom-domains?success=Domain+kustom+berhasil+dihapus", http.StatusSeeOther)
}

func (s *Server) toggleCustomDomain(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid form data"})
		return
	}

	id := r.FormValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Missing ID"})
		return
	}

	domains, _ := s.loadCustomDomains()
	found := false
	var newStatus string
	for i, dom := range domains {
		if dom.ID == id {
			if dom.Status == "active" {
				domains[i].Status = "inactive"
			} else {
				domains[i].Status = "active"
			}
			newStatus = domains[i].Status
			found = true
			break
		}
	}

	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "Domain kustom tidak ditemukan"})
		return
	}

	if err := s.saveCustomDomains(domains); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "status": newStatus})
}

func (s *Server) customDomainsSaveSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}

	settings := s.store.GetSettings()
	settings["custom_domains_cron_enabled"] = r.FormValue("custom_domains_cron_enabled")
	settings["custom_domains_cron_interval"] = r.FormValue("custom_domains_cron_interval")

	err := s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderCustomDomainsWithError(w, r, "Gagal memperbarui pengaturan: "+err.Error())
		return
	}

	http.Redirect(w, r, "/dashboard/custom-domains?success=Pengaturan+berhasil+diperbarui", http.StatusSeeOther)
}

func (s *Server) renderCustomDomainsWithError(w http.ResponseWriter, r *http.Request, errMsg string) {
	user := userFromRequest(r)
	categories := s.store.ListCategories()
	settings := s.store.GetSettings()
	domains, _ := s.loadCustomDomains()

	s.renderTemplate(w, "custom_domains.html", customDomainsViewData{
		User:       user,
		Settings:   settings,
		Categories: categories,
		Domains:    domains,
		Error:      errMsg,
	})
}

func (s *Server) ExecuteCustomDomainsJob() error {
	s.customDomainMu.Lock()
	if s.customDomainRunning {
		s.customDomainMu.Unlock()
		return fmt.Errorf("custom domains auto-post job is already running")
	}
	s.customDomainRunning = true
	s.customDomainMu.Unlock()

	defer func() {
		s.customDomainMu.Lock()
		s.customDomainRunning = false
		s.customDomainMu.Unlock()
	}()

	settings := s.store.GetSettings()
	apiKey := settings["ai_api_key"]
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		s.logCustomDomain(slog.LevelError, "Auto-post dihentikan karena kredensial AI (API Key) belum dikonfigurasi di menu Settings.")
		return fmt.Errorf("AI API Key is not configured")
	}

	domains, err := s.loadCustomDomains()
	if err != nil {
		return fmt.Errorf("failed to load custom domains: %w", err)
	}

	activeDomains := []CustomDomain{}
	for _, dom := range domains {
		if dom.Status == "active" {
			activeDomains = append(activeDomains, dom)
		}
	}

	if len(activeDomains) == 0 {
		s.logCustomDomain(slog.LevelInfo, "Tidak ada kustom domain aktif untuk diproses")
		return nil
	}

	systemUser, err := s.store.GetSystemUser()
	if err != nil {
		return fmt.Errorf("failed to get system user: %w", err)
	}

	model := settings["ai_default_model"]
	if model == "" {
		model = "gemini-2.5-flash"
	}
	globalPromptInstruction := settings["cron_prompt"]

	newArticlesImported := 0
	targetNewArticles := 3

	for _, dom := range activeDomains {
		// Split URLs by newline or comma
		var rssURLs []string
		for _, line := range strings.Split(dom.RSSURL, "\n") {
			for _, part := range strings.Split(line, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					rssURLs = append(rssURLs, part)
				}
			}
		}

		if len(rssURLs) == 0 {
			s.logCustomDomain(slog.LevelWarn, "Tidak ada URL RSS yang valid untuk domain", "name", dom.Name)
			continue
		}

		s.logCustomDomain(slog.LevelInfo, "Memulai processing kustom domain", "name", dom.Name, "sources_count", len(rssURLs))

		domainImportedCount := 0
		for _, rssURL := range rssURLs {
			if domainImportedCount >= targetNewArticles {
				break
			}

			s.logCustomDomain(slog.LevelInfo, "Mengambil feed RSS...", "name", dom.Name, "url", rssURL)
			items, err := s.FetchGoogleNewsRSS(rssURL)
			if err != nil {
				s.logCustomDomain(slog.LevelError, "Gagal mengambil RSS", "name", dom.Name, "url", rssURL, "error", err)
				continue
			}

			s.logCustomDomain(slog.LevelInfo, fmt.Sprintf("Berhasil mengambil %d item RSS dari %s", len(items), rssURL), "name", dom.Name)

			for _, item := range items {
				if domainImportedCount >= targetNewArticles {
					break
				}

				decodedURL := item.Link

				// Check blacklist
				parsedURL, pErr := url.Parse(decodedURL)
				var host string
				if pErr == nil {
					host = parsedURL.Hostname()
				}
				if host != "" {
					isBlacklisted, _ := s.store.IsDomainBlacklisted(host)
					if isBlacklisted {
						s.logCustomDomain(slog.LevelInfo, "Mengabaikan URL karena domain masuk blacklist", "url", decodedURL, "host", host)
						continue
					}
				}

				// Check duplicate
				exists, err := s.store.ArticleExistsBySourceURL(decodedURL)
				if err != nil {
					s.logCustomDomain(slog.LevelError, "Query database gagal untuk ArticleExistsBySourceURL", "url", decodedURL, "error", err)
					continue
				}
				if exists {
					s.logCustomDomain(slog.LevelInfo, "Artikel sudah pernah diimport, melewati", "url", decodedURL)
					continue
				}

				s.logCustomDomain(slog.LevelInfo, "Mendownload konten asli", "url", decodedURL)
				htmlContent, err := s.fetchHTMLContent(decodedURL)
				if err != nil {
					s.logCustomDomain(slog.LevelError, "Gagal mengunduh HTML", "url", decodedURL, "error", err)
					continue
				}

				title, bodyContent := extractTitleAndContent(htmlContent)
				heroImage := extractThumbnail(htmlContent)

				// Validate scrape
				cleanContent := strings.TrimSpace(bodyContent)
				cleanTitle := strings.TrimSpace(title)
				isFallbackContent := cleanContent == "Konten utama tidak berhasil diekstrak otomatis. Anda bisa menulis atau menyalinnya secara manual."
				if cleanTitle == "" || cleanTitle == "Judul tidak ditemukan" || cleanContent == "" || isFallbackContent {
					s.logCustomDomain(slog.LevelError, "Konten rujukan tidak valid", "url", decodedURL)
					s.blacklistURLHost(decodedURL)
					continue
				}

				promptInstruction := dom.CustomPrompt
				if promptInstruction == "" {
					promptInstruction = globalPromptInstruction
				}

				s.logCustomDomain(slog.LevelInfo, "Melakukan rewrite dengan AI", "title", title, "model", model)
				categories := s.store.ListCategories()
				
				rewrittenTitle, excerpt, content, categoryName, err := s.performAIRewriteSync(title, bodyContent, promptInstruction, model, categories)
				
				aiOutputContainsUnavailable := strings.Contains(strings.ToLower(rewrittenTitle), "konten utama tidak tersedia") ||
					strings.Contains(strings.ToLower(excerpt), "konten utama tidak tersedia") ||
					strings.Contains(strings.ToLower(content), "konten utama tidak tersedia")

				if err != nil || aiOutputContainsUnavailable {
					if err != nil {
						s.logCustomDomain(slog.LevelError, "Gagal menulis ulang dengan AI", "title", title, "error", err)
					} else {
						s.logCustomDomain(slog.LevelError, "AI mendeteksi konten utama tidak tersedia", "title", title)
					}
					s.blacklistURLHost(decodedURL)
					continue
				}

				finalCategory := dom.Category
				if finalCategory == "" || finalCategory == "auto" {
					finalCategory = categoryName
				}
				finalCategory = strings.TrimSpace(finalCategory)

				var matchedCategory *cms.Category
				for _, cat := range categories {
					if strings.EqualFold(cat.Name, finalCategory) {
						matchedCategory = &cat
						break
					}
				}

				if matchedCategory == nil && finalCategory != "" {
					newCat, err := s.store.CreateCategory(systemUser, cms.CategoryInput{
						Name: finalCategory,
						Slug: cms.Slugify(finalCategory),
					})
					if err == nil && newCat != nil {
						finalCategory = newCat.Name
					} else {
						s.logCustomDomain(slog.LevelWarn, "Gagal membuat kategori otomatis", "category", finalCategory, "error", err)
						if len(categories) > 0 {
							finalCategory = categories[0].Name
						} else {
							finalCategory = "Berita"
						}
					}
				} else if matchedCategory != nil {
					finalCategory = matchedCategory.Name
				} else {
					finalCategory = "Berita"
				}

				artInput := core.ArticleInput{
					Title:        rewrittenTitle,
					Content:      content,
					Excerpt:      excerpt,
					Category:     finalCategory,
					HeroImageURL: heroImage,
					Status:       core.ArticlePublished,
					SourceURL:    decodedURL,
					ImageSource:  determineImageSource(heroImage, decodedURL),
				}

				slug := cms.Slugify(rewrittenTitle)
				isDuplicate, dErr := s.store.ArticleExistsByTitleOrSlug(rewrittenTitle, slug)
				if dErr == nil && isDuplicate {
					s.logCustomDomain(slog.LevelInfo, "Mengabaikan artikel terduplikasi", "title", rewrittenTitle)
					continue
				}

				createdArt, err := s.store.CreateArticle(systemUser, artInput)
				if err != nil {
					s.logCustomDomain(slog.LevelError, "Gagal menyimpan artikel ke DB", "title", rewrittenTitle, "error", err)
					continue
				}

				s.logCustomDomain(slog.LevelInfo, "Berhasil auto-post artikel", "id", createdArt.ID, "title", rewrittenTitle)
				newArticlesImported++
				domainImportedCount++
			}
		}
	}

	nowStr := time.Now().Format(time.RFC3339)
	settings = s.store.GetSettings()
	settings["custom_domains_cron_last_run"] = nowStr
	_ = s.store.UpdateSettings(systemUser, settings)
	return nil
}


func (s *Server) StartCustomDomainsBackgroundJob() {
	s.loadCustomDomainLogs()
	go func() {
		s.logCustomDomain(slog.LevelInfo, "Memulai pekerja latar belakang auto-post Kustom Domain...")
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			settings := s.store.GetSettings()
			if settings["custom_domains_cron_enabled"] != "true" {
				continue
			}

			intervalMinStr := settings["custom_domains_cron_interval"]
			if intervalMinStr == "" {
				intervalMinStr = "60"
			}
			var intervalMin int
			_, err := fmt.Sscanf(intervalMinStr, "%d", &intervalMin)
			if err != nil || intervalMin <= 0 {
				intervalMin = 60
			}

			lastRunStr := settings["custom_domains_cron_last_run"]
			var lastRun time.Time
			if lastRunStr != "" {
				lastRun, _ = time.Parse(time.RFC3339, lastRunStr)
			}

			if time.Since(lastRun) >= time.Duration(intervalMin)*time.Minute {
				s.logCustomDomain(slog.LevelInfo, "Interval cron kustom domain tercapai, mengeksekusi auto-post...")
				err := s.ExecuteCustomDomainsJob()
				if err != nil {
					s.logCustomDomain(slog.LevelError, "Pekerjaan auto-post kustom domain gagal", "error", err)
				} else {
					s.logCustomDomain(slog.LevelInfo, "Pekerjaan auto-post kustom domain selesai")
				}
			}
		}
	}()
}

func (s *Server) loadCustomDomainLogs() {
	s.customDomainLogsMu.Lock()
	defer s.customDomainLogsMu.Unlock()

	if err := os.MkdirAll("data", 0755); err != nil {
		s.log.Error("Failed to create data directory", "error", err)
		return
	}

	filePath := filepath.Join("data", "custom_domains_logs.json")
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.customDomainLogs = []CronLog{}
			return
		}
		s.log.Error("Failed to open custom domain logs file", "error", err)
		return
	}
	defer file.Close()

	var logs []CronLog
	if err := json.NewDecoder(file).Decode(&logs); err != nil {
		s.log.Error("Failed to decode custom domain logs", "error", err)
		s.customDomainLogs = []CronLog{}
		return
	}

	s.customDomainLogs = s.pruneOldCronLogs(logs)
}

func (s *Server) logCustomDomain(level slog.Level, msg string, args ...any) {
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

	s.customDomainLogsMu.Lock()
	defer s.customDomainLogsMu.Unlock()

	s.customDomainLogs = s.pruneOldCronLogs(s.customDomainLogs)
	if len(s.customDomainLogs) >= 500 {
		s.customDomainLogs = s.customDomainLogs[1:]
	}
	s.customDomainLogs = append(s.customDomainLogs, CronLog{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   logMsg,
	})

	filePath := filepath.Join("data", "custom_domains_logs.json")
	file, err := os.Create(filePath)
	if err != nil {
		s.log.Error("Failed to create custom domain logs file for writing", "error", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s.customDomainLogs); err != nil {
		s.log.Error("Failed to encode custom domain logs to file", "error", err)
	}
}

func (s *Server) customDomainsGetLogs(w http.ResponseWriter, r *http.Request) {
	s.customDomainLogsMu.RLock()
	defer s.customDomainLogsMu.RUnlock()

	logs := s.customDomainLogs
	if logs == nil {
		logs = []CronLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) customDomainsClearLogs(w http.ResponseWriter, r *http.Request) {
	s.customDomainLogsMu.Lock()
	s.customDomainLogs = []CronLog{}
	
	filePath := filepath.Join("data", "custom_domains_logs.json")
	if file, err := os.Create(filePath); err == nil {
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(s.customDomainLogs)
		file.Close()
	}
	s.customDomainLogsMu.Unlock()

	s.logCustomDomain(slog.LevelInfo, "Log kustom domain dibersihkan oleh pengguna")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) customDomainsRunManual(w http.ResponseWriter, r *http.Request) {
	s.customDomainMu.Lock()
	if s.customDomainRunning {
		s.customDomainMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "message": "Pekerjaan auto-post kustom domain sedang berjalan"})
		return
	}
	s.customDomainMu.Unlock()

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
		s.logCustomDomain(slog.LevelInfo, "Memulai eksekusi auto-post kustom domain manual...")
		err := s.ExecuteCustomDomainsJob()
		if err != nil {
			s.logCustomDomain(slog.LevelError, "Eksekusi manual gagal", "error", err)
		} else {
			s.logCustomDomain(slog.LevelInfo, "Eksekusi manual selesai dengan sukses")
		}
	}()

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Custom domains auto-post job started in background"})
}
