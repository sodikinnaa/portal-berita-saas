package httpserver

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

type dashboardFacebookViewData struct {
	User     *cms.User
	Settings map[string]string
	Success  string
	Error    string
}

func (s *Server) dashboardFacebook(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	settings := s.store.GetSettings()

	if settings["site_url"] == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		settings["site_url"] = scheme + "://" + r.Host
	}

	s.renderTemplate(w, "facebook.html", dashboardFacebookViewData{
		User:     user,
		Settings: settings,
	})
}

func (s *Server) facebookSaveSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}

	settings := s.store.GetSettings()
	settings["fb_auto_post_enabled"] = r.FormValue("fb_auto_post_enabled")
	settings["fb_page_id"] = r.FormValue("fb_page_id")
	settings["fb_access_token"] = r.FormValue("fb_access_token")
	settings["fb_custom_prompt"] = r.FormValue("fb_custom_prompt")
	settings["fb_cron_interval"] = r.FormValue("fb_cron_interval")
	
	siteURL := strings.TrimSpace(r.FormValue("site_url"))
	if siteURL == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		siteURL = scheme + "://" + r.Host
	}
	settings["site_url"] = siteURL

	settings["fb_app_secret"] = r.FormValue("fb_app_secret")
	settings["fb_business_id"] = r.FormValue("fb_business_id")

	err := s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderTemplate(w, "facebook.html", dashboardFacebookViewData{
			User:     user,
			Settings: settings,
			Error:    "Gagal memperbarui konfigurasi: " + err.Error(),
		})
		return
	}

	s.renderTemplate(w, "facebook.html", dashboardFacebookViewData{
		User:     user,
		Settings: settings,
		Success:  "Konfigurasi Facebook Halaman berhasil disimpan",
	})
}

type fbPage struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type fbPageResponse struct {
	Data  []fbPage `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
	} `json:"error"`
}

const fbAPIVersion = "v20.0"

func computeAppSecretProof(accessToken, appSecret string) string {
	h := hmac.New(sha256.New, []byte(appSecret))
	h.Write([]byte(accessToken))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Server) fetchFBPages(ctx context.Context, path string, token, proof string) ([]fbPage, error) {
	apiURL := fmt.Sprintf("https://graph.facebook.com/%s/%s", fbAPIVersion, path)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Set("fields", "id,name")
	q.Set("access_token", token)
	q.Set("appsecret_proof", proof)
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rObj fbPageResponse
	if err := json.Unmarshal(bodyBytes, &rObj); err != nil {
		return nil, fmt.Errorf("gagal parsing respon: %w (body: %s)", err, string(bodyBytes))
	}

	if resp.StatusCode != http.StatusOK {
		if rObj.Error != nil && rObj.Error.Message != "" {
			return nil, fmt.Errorf("Meta Graph API error: %s", rObj.Error.Message)
		}
		return nil, fmt.Errorf("Meta Graph API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return rObj.Data, nil
}

func (s *Server) facebookDetectPages(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Form tidak valid"})
		return
	}

	token := strings.TrimSpace(r.FormValue("access_token"))
	appSecret := strings.TrimSpace(r.FormValue("app_secret"))
	businessID := strings.TrimSpace(r.FormValue("business_id"))

	if token == "" || appSecret == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Token dan Kunci Rahasia Aplikasi (App Secret) harus diisi"})
		return
	}

	proof := computeAppSecretProof(token, appSecret)
	ctx := r.Context()

	var allPages []fbPage
	var lastErr error

	// 1. Ambil halaman langsung
	pages, err := s.fetchFBPages(ctx, "me/accounts", token, proof)
	if err != nil {
		lastErr = err
		s.log.Error("Gagal mengambil halaman Facebook langsung", "error", err)
	} else {
		allPages = append(allPages, pages...)
	}

	// 2. Ambil halaman bisnis (jika ID Bisnis disediakan)
	if businessID != "" {
		ownedPath := fmt.Sprintf("%s/owned_pages", businessID)
		ownedPages, err := s.fetchFBPages(ctx, ownedPath, token, proof)
		if err != nil {
			lastErr = err
			s.log.Error("Gagal mengambil owned_pages untuk bisnis", "business_id", businessID, "error", err)
		} else {
			allPages = append(allPages, ownedPages...)
		}

		clientPath := fmt.Sprintf("%s/client_pages", businessID)
		clientPages, err := s.fetchFBPages(ctx, clientPath, token, proof)
		if err != nil {
			lastErr = err
			s.log.Error("Gagal mengambil client_pages untuk bisnis", "business_id", businessID, "error", err)
		} else {
			allPages = append(allPages, clientPages...)
		}
	}

	// Hapus duplikasi halaman berdasarkan ID
	uniquePages := make([]fbPage, 0)
	seen := make(map[string]bool)
	for _, p := range allPages {
		if !seen[p.ID] {
			seen[p.ID] = true
			uniquePages = append(uniquePages, p)
		}
	}

	if len(uniquePages) == 0 && lastErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": lastErr.Error()})
		return
	}

	writeJSON(w, http.StatusOK, uniquePages)
}

func (s *Server) facebookGetLogs(w http.ResponseWriter, r *http.Request) {
	s.loadFBLogs()
	s.fbLogsMu.RLock()
	defer s.fbLogsMu.RUnlock()
	writeJSON(w, http.StatusOK, s.fbLogs)
}

func (s *Server) facebookClearLogs(w http.ResponseWriter, r *http.Request) {
	s.fbLogsMu.Lock()
	s.fbLogs = []CronLog{}
	s.fbLogsMu.Unlock()

	filePath := filepath.Join("data", "fb_logs.json")
	_ = os.Remove(filePath)

	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) facebookRunManual(w http.ResponseWriter, r *http.Request) {
	s.fbMu.Lock()
	if s.fbRunning {
		s.fbMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "message": "Proses posting Facebook sedang berjalan"})
		return
	}
	s.fbRunning = true
	s.fbMu.Unlock()

	go func() {
		defer func() {
			s.fbMu.Lock()
			s.fbRunning = false
			s.fbMu.Unlock()
		}()

		s.logFB(slog.LevelInfo, "Memulai posting manual ke Facebook...")
		err := s.processFacebookAutoPost(true)
		if err != nil {
			s.logFB(slog.LevelError, "Posting Facebook manual gagal", "error", err)
		} else {
			s.logFB(slog.LevelInfo, "Posting Facebook manual selesai sukses")
		}
	}()

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Proses Facebook auto-post dimulai di latar belakang"})
}

func (s *Server) loadFBLogs() {
	s.fbLogsMu.Lock()
	defer s.fbLogsMu.Unlock()

	// Hanya memuat jika data log masih kosong di memory
	if len(s.fbLogs) > 0 {
		return
	}

	_ = os.MkdirAll("data", 0755)
	filePath := filepath.Join("data", "fb_logs.json")
	file, err := os.Open(filePath)
	if err != nil {
		s.fbLogs = []CronLog{}
		return
	}
	defer file.Close()

	var logs []CronLog
	if err := json.NewDecoder(file).Decode(&logs); err != nil {
		s.fbLogs = []CronLog{}
		return
	}

	s.fbLogs = s.pruneOldCronLogs(logs)
}

func (s *Server) logFB(level slog.Level, msg string, args ...any) {
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

	// Logging juga ke log cron utama agar muncul di cron logs
	s.logCron(level, "[FB Auto-Post] "+logMsg)

	s.fbLogsMu.Lock()
	defer s.fbLogsMu.Unlock()

	s.fbLogs = s.pruneOldCronLogs(s.fbLogs)
	if len(s.fbLogs) >= 500 {
		s.fbLogs = s.fbLogs[1:]
	}
	s.fbLogs = append(s.fbLogs, CronLog{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   logMsg,
	})

	filePath := filepath.Join("data", "fb_logs.json")
	file, err := os.Create(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(s.fbLogs)
}

func (s *Server) StartFacebookCronJob() {
	s.loadFBLogs()
	go func() {
		s.logFB(slog.LevelInfo, "Memulai pekerja latar belakang auto-post Facebook...")
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			settings := s.store.GetSettings()
			if settings["fb_auto_post_enabled"] != "true" {
				continue
			}

			intervalMinStr := settings["fb_cron_interval"]
			if intervalMinStr == "" {
				intervalMinStr = "60"
			}
			var intervalMin int
			_, err := fmt.Sscanf(intervalMinStr, "%d", &intervalMin)
			if err != nil || intervalMin <= 0 {
				intervalMin = 60
			}

			lastRunStr := settings["fb_cron_last_run"]
			var lastRun time.Time
			if lastRunStr != "" {
				lastRun, _ = time.Parse(time.RFC3339, lastRunStr)
			}

			if time.Since(lastRun) >= time.Duration(intervalMin)*time.Minute {
				// Cegah running ganda jika manual sedang berjalan
				s.fbMu.Lock()
				if s.fbRunning {
					s.fbMu.Unlock()
					continue
				}
				s.fbRunning = true
				s.fbMu.Unlock()

				go func() {
					defer func() {
						s.fbMu.Lock()
						s.fbRunning = false
						s.fbMu.Unlock()
					}()

					s.logFB(slog.LevelInfo, "Interval cron FB tercapai, mengeksekusi pekerjaan auto-post...")
					err := s.processFacebookAutoPost(false)
					if err != nil {
						s.logFB(slog.LevelError, "Pekerjaan Facebook auto-post latar belakang gagal", "error", err)
					} else {
						s.logFB(slog.LevelInfo, "Pekerjaan Facebook auto-post latar belakang selesai sukses")
						
						// Update fb_cron_last_run
						nowStr := time.Now().Format(time.RFC3339)
						settings := s.store.GetSettings()
						settings["fb_cron_last_run"] = nowStr
						systemUser := &cms.User{ID: "system", Role: cms.RoleAdmin}
						_ = s.store.UpdateSettings(systemUser, settings)
					}
				}()
			}
		}
	}()
}
