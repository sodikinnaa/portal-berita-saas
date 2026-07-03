package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"porta-berita/internal/cms"
)

type apiKeysViewData struct {
	User      *cms.User
	Keys      []cms.APIKey
	NewSecret string
	Error     string
}

type dashboardSettingsViewData struct {
	User     *cms.User
	Settings map[string]string
	Success  string
	Error    string
}

type testModelsRequest struct {
	APIKey      string `json:"api_key"`
	EndpointURL string `json:"endpoint_url"`
	Model       string `json:"model"`
	EnableProxy bool   `json:"enable_proxy"`
}

type testModelsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type customPromptItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Instruction string `json:"instruction"`
}

type dashboardPromptsViewData struct {
	User    *cms.User
	Prompts []customPromptItem
	Error   string
	Success string
}

func (s *Server) dashboardAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	s.renderTemplate(w, "api_keys.html", apiKeysViewData{User: user, Keys: s.store.ListAPIKeys(user)})
}

func (s *Server) dashboardSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	settings := s.store.GetSettings()
	s.renderTemplate(w, "settings.html", dashboardSettingsViewData{
		User:     user,
		Settings: settings,
	})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Parse multipart form to support favicon uploading (max 5MB)
	_ = r.ParseMultipartForm(5 << 20)

	existingSettings := s.store.GetSettings()

	settings := map[string]string{
		"site_title":             r.FormValue("site_title"),
		"site_tagline":           r.FormValue("site_tagline"),
		"enable_landing_page":   "false",
		"site_description":       r.FormValue("site_description"),
		"social_facebook_url":    r.FormValue("social_facebook_url"),
		"social_facebook_count":  r.FormValue("social_facebook_count"),
		"social_twitter_url":     r.FormValue("social_twitter_url"),
		"social_twitter_count":   r.FormValue("social_twitter_count"),
		"social_youtube_url":     r.FormValue("social_youtube_url"),
		"social_youtube_count":   r.FormValue("social_youtube_count"),
		"social_instagram_url":   r.FormValue("social_instagram_url"),
		"social_instagram_count": r.FormValue("social_instagram_count"),
		"ai_api_key":             r.FormValue("ai_api_key"),
		"ai_endpoint_url":       r.FormValue("ai_endpoint_url"),
		"ai_default_model":      r.FormValue("ai_default_model"),
		"ai_enable_proxy":       strconv.FormatBool(r.FormValue("ai_enable_proxy") == "on" || r.FormValue("ai_enable_proxy") == "true"),
		"seo_google_verification": r.FormValue("seo_google_verification"),
		"seo_bing_verification":   r.FormValue("seo_bing_verification"),
		"page_about_content":     r.FormValue("page_about_content"),
		"page_contact_content":   r.FormValue("page_contact_content"),
		"page_privacy_content":   r.FormValue("page_privacy_content"),
		"page_ads_content":       r.FormValue("page_ads_content"),
		"active_theme":           r.FormValue("active_theme"),
		"ads_txt_content":       r.FormValue("ads_txt_content"),
		"adsense_head_script":   r.FormValue("adsense_head_script"),
		"mail_provider":          r.FormValue("mail_provider"),
		"mail_smtp_host":         r.FormValue("mail_smtp_host"),
		"mail_smtp_port":         r.FormValue("mail_smtp_port"),
		"mail_smtp_user":         r.FormValue("mail_smtp_user"),
		"mail_smtp_pass":         r.FormValue("mail_smtp_pass"),
		"mail_smtp_encryption":   r.FormValue("mail_smtp_encryption"),
		"mail_api_key":           r.FormValue("mail_api_key"),
	}

	// Handle favicon file upload
	file, header, err := r.FormFile("favicon_file")
	if err == nil {
		defer file.Close()
		media, saveErr := s.saveUpload(user, file, header)
		if saveErr == nil {
			settings["site_favicon"] = media.URL
		} else {
			s.renderTemplate(w, "settings.html", dashboardSettingsViewData{
				User:     user,
				Settings: settings,
				Error:    "Gagal menyimpan favicon: " + saveErr.Error(),
			})
			return
		}
	} else {
		// Keep the existing favicon if no new file was uploaded
		if val, ok := existingSettings["site_favicon"]; ok {
			settings["site_favicon"] = val
		}
	}

	// Handle HTML verification file upload
	htmlFile, htmlHeader, err := r.FormFile("verification_html_file")
	if err == nil {
		defer htmlFile.Close()
		filename := filepath.Base(htmlHeader.Filename)
		if strings.HasSuffix(strings.ToLower(filename), ".html") {
			contentBytes, readErr := io.ReadAll(htmlFile)
			if readErr == nil {
				settings["verification_html_filename"] = filename
				settings["verification_html_content"] = string(contentBytes)
			}
		}
	} else {
		// Keep existing if no file uploaded
		if val, ok := existingSettings["verification_html_filename"]; ok {
			settings["verification_html_filename"] = val
		}
		if val, ok := existingSettings["verification_html_content"]; ok {
			settings["verification_html_content"] = val
		}
	}

	// Check if clear checkbox was ticked
	if r.FormValue("clear_verification_html") == "true" {
		settings["verification_html_filename"] = ""
		settings["verification_html_content"] = ""
	}

	err = s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderTemplate(w, "settings.html", dashboardSettingsViewData{
			User:     user,
			Settings: settings,
			Error:    err.Error(),
		})
		return
	}

	s.renderTemplate(w, "settings.html", dashboardSettingsViewData{
		User:     user,
		Settings: settings,
		Success:  "Pengaturan berhasil diperbarui",
	})
}

func (s *Server) serveFavicon(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()
	faviconURL, ok := settings["site_favicon"]
	if ok && faviconURL != "" {
		if strings.HasPrefix(faviconURL, "/uploads/") {
			filename := strings.TrimPrefix(faviconURL, "/uploads/")
			filePath := filepath.Join(s.cfg.UploadDir, filename)
			http.ServeFile(w, r, filePath)
			return
		}
		http.Redirect(w, r, faviconURL, http.StatusFound)
		return
	}
	http.Error(w, "Not Found", http.StatusNotFound)
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	input, err := parseAPIKeyForm(r)
	if err != nil {
		s.renderTemplate(w, "api_keys.html", apiKeysViewData{User: user, Keys: s.store.ListAPIKeys(user), Error: "Form tidak valid"})
		return
	}
	created, err := s.store.CreateAPIKey(user, input)
	if err != nil {
		s.renderTemplate(w, "api_keys.html", apiKeysViewData{User: user, Keys: s.store.ListAPIKeys(user), Error: err.Error()})
		return
	}
	s.renderTemplate(w, "api_keys.html", apiKeysViewData{User: user, Keys: s.store.ListAPIKeys(user), NewSecret: created.Secret})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RevokeAPIKey(userFromRequest(r), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), statusFromError(err))
		return
	}
	http.Redirect(w, r, "/dashboard/api-keys", http.StatusFound)
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAPIKey(userFromRequest(r), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), statusFromError(err))
		return
	}
	http.Redirect(w, r, "/dashboard/api-keys", http.StatusFound)
}

func (s *Server) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.store.AuthenticateAPIKey(r.Header.Get("Authorization"))
		if err != nil {
			writeJSON(w, statusFromError(err), map[string]string{"error": err.Error()})
			return
		}
		next.ServeHTTP(w, r.WithContext(withAPIPrincipal(r.Context(), principal)))
	}
}

func (s *Server) swaggerDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="id">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>API Docs - NewsPaper CMS</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>body{margin:0;background:#f6f6f6}.top{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:14px 20px;background:#111;color:#fff;font-family:Arial,sans-serif}.top a{color:#fff;text-decoration:none;font-weight:700}.brand{font-size:20px}.brand span{color:#e63329}#swagger-ui{max-width:1280px;margin:0 auto}</style>
</head>
<body>
<header class="top"><a class="brand" href="/">News<span>Paper</span> API Docs</a><a href="/dashboard/api-keys">API Keys</a></header>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
window.onload = function () {
    SwaggerUIBundle({
        url: '/docs/openapi.json',
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis],
        layout: 'BaseLayout'
    });
};
</script>
</body>
</html>`))
}

func (s *Server) openAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(openAPISpecJSON))
}

func (s *Server) apiListArticles(w http.ResponseWriter, r *http.Request) {
	principal := apiPrincipalFromRequest(r)
	articles, err := s.store.ListArticlesFromAPI(*principal)
	if err != nil {
		writeJSON(w, statusFromError(err), map[string]string{"error": err.Error()})
		return
	}

	sessionToken := r.Header.Get("X-App-Session")
	isLoggedIn := false
	if sessionToken != "" {
		_, err := s.store.AppUserBySession(sessionToken)
		if err == nil {
			isLoggedIn = true
		}
	}

	q := strings.ToLower(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	premiumStr := r.URL.Query().Get("premium")
	categoryQuery := strings.ToLower(r.URL.Query().Get("category"))

	var filtered []cms.Article
	for _, a := range articles {
		if categoryQuery != "" && strings.ToLower(a.Category) != categoryQuery {
			continue
		}
		if premiumStr == "true" && !a.IsPremium {
			continue
		}
		if status != "" && a.Status != status {
			continue
		} else if status == "" && a.Status != "published" {
			// Default to only return published articles for standard list
			continue
		}

		if q != "" && q != "recommended" {
			titleMatch := strings.Contains(strings.ToLower(a.Title), q)
			contentMatch := strings.Contains(strings.ToLower(a.Content), q)
			excerptMatch := strings.Contains(strings.ToLower(a.Excerpt), q)
			if !titleMatch && !contentMatch && !excerptMatch {
				continue
			}
		}

		// Enforce premium content paywall
		if a.IsPremium && !isLoggedIn {
			a.Content = `<div style="text-align: center; padding: 20px; border: 1px dashed #FF3B30; border-radius: 8px; margin: 20px 0; background-color: #FFF5F5;">
				<h3 style="color: #FF3B30; margin-bottom: 10px; font-weight: bold;">🔒 Konten Khusus Member Premium</h3>
				<p style="font-size: 16px; color: #555;">Detail berita ini dikunci dan hanya dapat dibaca oleh pengguna yang telah login di aplikasi.</p>
				<p style="font-weight: bold; margin-top: 15px; color: #007AFF;">Silakan Login atau Daftar melalui tab Profil untuk membaca selengkapnya.</p>
			</div>`
			a.Excerpt = "🔒 Konten Premium (Silakan Login)"
		}

		filtered = append(filtered, a)
	}

	// Pagination
	limitVal := 10 // Default limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limitVal = l
		}
	} else if pageSizeStr := r.URL.Query().Get("pageSize"); pageSizeStr != "" {
		if l, err := strconv.Atoi(pageSizeStr); err == nil && l > 0 {
			limitVal = l
		}
	}

	offsetVal := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offsetVal = o
		}
	} else if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 1 {
			offsetVal = (p - 1) * limitVal
		}
	}

	if offsetVal >= len(filtered) {
		writeJSON(w, http.StatusOK, []cms.Article{})
		return
	}

	endVal := offsetVal + limitVal
	if endVal > len(filtered) {
		endVal = len(filtered)
	}

	writeJSON(w, http.StatusOK, filtered[offsetVal:endVal])
}

func (s *Server) apiCreateArticle(w http.ResponseWriter, r *http.Request) {
	principal := apiPrincipalFromRequest(r)
	var input cms.ArticleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	article, err := s.store.CreateArticleFromAPI(*principal, input)
	if err != nil {
		writeJSON(w, statusFromError(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, article)
}

func (s *Server) apiUploadMedia(w http.ResponseWriter, r *http.Request) {
	principal := apiPrincipalFromRequest(r)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file too large or invalid form"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is required"})
		return
	}
	defer file.Close()
	media, err := s.saveUploadForAPI(*principal, file, header)
	if err != nil {
		writeJSON(w, statusFromError(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, media)
}

func (s *Server) apiCreateMediaURL(w http.ResponseWriter, r *http.Request) {
	principal := apiPrincipalFromRequest(r)
	var input cms.ExternalMediaInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	media, err := s.store.CreateExternalMediaURL(*principal, input)
	if err != nil {
		writeJSON(w, statusFromError(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, media)
}

func (s *Server) testAIModels(w http.ResponseWriter, r *http.Request) {
	var req testModelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Request JSON tidak valid"})
		return
	}

	apiKey := strings.TrimSpace(req.APIKey)
	endpointURL := strings.TrimSpace(req.EndpointURL)

	if apiKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "API Key tidak boleh kosong"})
		return
	}

	isGemini := endpointURL == "" || strings.Contains(endpointURL, "googleapis.com")
	var modelsURL string
	if isGemini {
		modelsURL = "https://generativelanguage.googleapis.com/v1beta/models?key=" + apiKey
	} else {
		baseURL := endpointURL
		baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
		baseURL = strings.TrimSuffix(baseURL, "chat/completions")
		baseURL = strings.TrimSuffix(baseURL, "/")
		modelsURL = baseURL + "/models"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if !isGemini {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	enableProxy := req.EnableProxy
	if !enableProxy {
		settings := s.store.GetSettings()
		enableProxy = settings["ai_enable_proxy"] == "true"
	}

	var client *http.Client
	if enableProxy {
		client = s.getProxyHTTPClientWithTimeout(20 * time.Second)
	} else {
		client = &http.Client{}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Gagal menghubungi API provider: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Gagal membaca response: " + err.Error()})
		return
	}

	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Provider mengembalikan status %d: %s", resp.StatusCode, string(bodyBytes))})
		return
	}

	var fetchedModels []modelInfo

	if isGemini {
		type geminiModelEntry struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		}
		type geminiModelsResp struct {
			Models []geminiModelEntry `json:"models"`
		}

		var gResp geminiModelsResp
		if err := json.Unmarshal(bodyBytes, &gResp); err == nil && len(gResp.Models) > 0 {
			for _, m := range gResp.Models {
				canGenerate := false
				for _, method := range m.SupportedGenerationMethods {
					if method == "generateContent" {
						canGenerate = true
						break
					}
				}
				if !canGenerate {
					continue
				}
				id := m.Name
				id = strings.TrimPrefix(id, "models/")
				fetchedModels = append(fetchedModels, modelInfo{
					ID:   id,
					Name: m.DisplayName,
				})
			}
		}
	} else {
		type openAIModelEntry struct {
			ID string `json:"id"`
		}
		type openAIModelsResp struct {
			Data []openAIModelEntry `json:"data"`
		}

		var oResp openAIModelsResp
		if err := json.Unmarshal(bodyBytes, &oResp); err == nil && len(oResp.Data) > 0 {
			for _, m := range oResp.Data {
				fetchedModels = append(fetchedModels, modelInfo{
					ID:   m.ID,
					Name: m.ID,
				})
			}
		}
	}

	if len(fetchedModels) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Tidak menemukan model yang kompatibel."})
		return
	}

	writeJSON(w, http.StatusOK, fetchedModels)
}

func (s *Server) loadCustomPrompts() []customPromptItem {
	settings := s.store.GetSettings()
	jsonStr := settings["custom_prompts_json"]
	if jsonStr == "" {
		return []customPromptItem{}
	}
	var items []customPromptItem
	if err := json.Unmarshal([]byte(jsonStr), &items); err != nil {
		return []customPromptItem{}
	}
	return items
}

func (s *Server) saveCustomPrompts(user *cms.User, items []customPromptItem) error {
	bytes, err := json.Marshal(items)
	if err != nil {
		return err
	}
	settings := s.store.GetSettings()
	settings["custom_prompts_json"] = string(bytes)
	return s.store.UpdateSettings(user, settings)
}

func (s *Server) dashboardPrompts(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	prompts := s.loadCustomPrompts()
	s.renderTemplate(w, "prompts.html", dashboardPromptsViewData{
		User:    user,
		Prompts: prompts,
	})
}

func (s *Server) savePrompt(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}
	id := r.FormValue("id")
	name := strings.TrimSpace(r.FormValue("name"))
	instruction := strings.TrimSpace(r.FormValue("instruction"))

	prompts := s.loadCustomPrompts()
	if name == "" || instruction == "" {
		s.renderTemplate(w, "prompts.html", dashboardPromptsViewData{
			User:    user,
			Prompts: prompts,
			Error:   "Nama dan instruksi prompt wajib diisi",
		})
		return
	}

	if id == "" {
		newID := fmt.Sprintf("prompt_%d", time.Now().UnixNano())
		prompts = append(prompts, customPromptItem{
			ID:          newID,
			Name:        name,
			Instruction: instruction,
		})
	} else {
		found := false
		for i, p := range prompts {
			if p.ID == id {
				prompts[i].Name = name
				prompts[i].Instruction = instruction
				found = true
				break
			}
		}
		if !found {
			prompts = append(prompts, customPromptItem{
				ID:          id,
				Name:        name,
				Instruction: instruction,
			})
		}
	}

	if err := s.saveCustomPrompts(user, prompts); err != nil {
		s.renderTemplate(w, "prompts.html", dashboardPromptsViewData{
			User:    user,
			Prompts: prompts,
			Error:   "Gagal menyimpan prompt: " + err.Error(),
		})
		return
	}

	http.Redirect(w, r, "/dashboard/prompts", http.StatusFound)
}

func (s *Server) deletePrompt(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}
	id := r.FormValue("id")
	prompts := s.loadCustomPrompts()
	newPrompts := []customPromptItem{}
	for _, p := range prompts {
		if p.ID != id {
			newPrompts = append(newPrompts, p)
		}
	}

	if err := s.saveCustomPrompts(user, newPrompts); err != nil {
		s.renderTemplate(w, "prompts.html", dashboardPromptsViewData{
			User:    user,
			Prompts: prompts,
			Error:   "Gagal menghapus prompt: " + err.Error(),
		})
		return
	}

	http.Redirect(w, r, "/dashboard/prompts", http.StatusFound)
}

func (s *Server) apiListPrompts(w http.ResponseWriter, r *http.Request) {
	prompts := s.loadCustomPrompts()
	writeJSON(w, http.StatusOK, prompts)
}

func parseAPIKeyForm(r *http.Request) (cms.APIKeyInput, error) {
	if err := r.ParseForm(); err != nil {
		return cms.APIKeyInput{}, err
	}
	var expiresAt *time.Time
	if value := strings.TrimSpace(r.FormValue("expires_at")); value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return cms.APIKeyInput{}, err
		}
		expiresAt = &parsed
	}
	return cms.APIKeyInput{Name: r.FormValue("name"), Scopes: r.Form["scopes"], ExpiresAt: expiresAt}, nil
}

func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin {
		http.Error(w, "Hanya Administrator yang dapat melakukan export database", http.StatusForbidden)
		return
	}

	backupData, err := s.store.ExportBackup()
	if err != nil {
		http.Error(w, "Gagal melakukan export database: "+err.Error(), http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("backup-portal-berita-%s.json", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(backupData)
}

func (s *Server) importBackup(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin {
		http.Error(w, "Hanya Administrator yang dapat melakukan import database", http.StatusForbidden)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB max memory
		s.renderSettingsError(w, user, "Gagal membaca form upload: "+err.Error())
		return
	}

	file, _, err := r.FormFile("backup_file")
	if err != nil {
		s.renderSettingsError(w, user, "File backup tidak ditemukan atau tidak valid")
		return
	}
	defer file.Close()

	backupBytes, err := io.ReadAll(file)
	if err != nil {
		s.renderSettingsError(w, user, "Gagal membaca isi file backup: "+err.Error())
		return
	}

	if err := s.store.ImportBackup(backupBytes); err != nil {
		s.renderSettingsError(w, user, "Gagal mengimpor database: "+err.Error())
		return
	}

	// Success
	settings := s.store.GetSettings()
	s.renderTemplate(w, "settings.html", dashboardSettingsViewData{
		User:     user,
		Settings: settings,
		Success:  "Database berhasil di-restore/di-import dari file backup!",
	})
}

func (s *Server) renderSettingsError(w http.ResponseWriter, user *cms.User, errorMsg string) {
	settings := s.store.GetSettings()
	s.renderTemplate(w, "settings.html", dashboardSettingsViewData{
		User:     user,
		Settings: settings,
		Error:    errorMsg,
	})
}

func (s *Server) dashboardThemes(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	settings := s.store.GetSettings()
	s.renderTemplate(w, "themes.html", dashboardSettingsViewData{
		User:     user,
		Settings: settings,
	})
}

func (s *Server) updateTheme(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	activeTheme := r.FormValue("active_theme")
	if activeTheme == "" {
		activeTheme = "default"
	}

	settings := s.store.GetSettings()
	settings["active_theme"] = activeTheme

	err := s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderTemplate(w, "themes.html", dashboardSettingsViewData{
			User:     user,
			Settings: settings,
			Error:    "Gagal mengubah tema: " + err.Error(),
		})
		return
	}

	s.renderTemplate(w, "themes.html", dashboardSettingsViewData{
		User:     user,
		Settings: settings,
		Success:  "Tema visual berhasil diperbarui",
	})
}

func (s *Server) addAILog(logType, model, proxy, status, details string) {
	s.aiLogsMu.Lock()
	defer s.aiLogsMu.Unlock()

	s.aiLogs = s.pruneOldAILogs(s.aiLogs)

	entry := AILogEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05 MST"),
		Type:      logType,
		Model:     model,
		Proxy:     proxy,
		Status:    status,
		Details:   details,
	}

	s.aiLogs = append(s.aiLogs, entry)
	if len(s.aiLogs) > 500 {
		s.aiLogs = s.aiLogs[1:]
	}
}

func (s *Server) apiGetAILogs(w http.ResponseWriter, r *http.Request) {
	s.aiLogsMu.RLock()
	defer s.aiLogsMu.RUnlock()

	writeJSON(w, http.StatusOK, s.aiLogs)
}

func (s *Server) apiClearAILogs(w http.ResponseWriter, r *http.Request) {
	s.aiLogsMu.Lock()
	s.aiLogs = []AILogEntry{}
	s.aiLogsMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) apiGetSiteInfo(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()
	title := settings["site_title"]
	if title == "" {
		title = "Porta News"
	}
	
	faviconURL := settings["site_favicon"]
	if faviconURL != "" {
		if strings.HasPrefix(faviconURL, "/") {
			scheme := "http"
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				scheme = "https"
			}
			faviconURL = fmt.Sprintf("%s://%s%s", scheme, r.Host, faviconURL)
		}
	} else {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		faviconURL = fmt.Sprintf("%s://%s/favicon.ico", scheme, r.Host)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"title":   title,
		"favicon": faviconURL,
	})
}


