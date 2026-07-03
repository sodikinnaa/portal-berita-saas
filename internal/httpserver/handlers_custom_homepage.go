package httpserver

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"porta-berita/internal/cms"
	"porta-berita/internal/web"
)

// renderSettingsView is a helper to render the settings page with feedback messages
func (s *Server) renderSettingsView(w http.ResponseWriter, r *http.Request, user *cms.User, success, err string) {
	settings := s.store.GetSettings()
	s.renderTemplate(w, "themes.html", dashboardSettingsViewData{
		User:     user,
		Settings: settings,
		Success:  success,
		Error:    err,
	})
}

// facebookSaveSettings / other settings toggle
func (s *Server) customHomepageToggle(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	enabled := r.FormValue("custom_homepage_enabled")
	if enabled != "true" {
		enabled = "false"
	}

	settings := s.store.GetSettings()
	settings["custom_homepage_enabled"] = enabled

	err := s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Gagal memperbarui status custom homepage: "+err.Error())
		return
	}

	statusText := "dinonaktifkan"
	if enabled == "true" {
		statusText = "diaktifkan"
	}
	s.renderSettingsView(w, r, user, "Status custom homepage berhasil "+statusText, "")
}

// customHomepageTemplate generates a beautiful static portal homepage template ZIP on the fly and serves it
func (s *Server) customHomepageTemplate(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Create zip in memory
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// Add files to zip writer
	files := []struct {
		Name string
		Body string
	}{
		{"index.html", defaultCustomHomepageHTML},
		{"style.css", defaultCustomHomepageCSS},
		{"script.js", defaultCustomHomepageJS},
		{"subpage/index.html", defaultCustomHomepageSubpageHTML},
		{"about-us/index.html", defaultCustomHomepageAboutHTML},
	}

	for _, file := range files {
		f, err := zipWriter.Create(file.Name)
		if err != nil {
			s.log.Error("failed to create file in zip writer", "file", file.Name, "error", err)
			http.Error(w, "Failed to generate template", http.StatusInternalServerError)
			return
		}
		_, err = f.Write([]byte(file.Body))
		if err != nil {
			s.log.Error("failed to write file body to zip writer", "file", file.Name, "error", err)
			http.Error(w, "Failed to generate template", http.StatusInternalServerError)
			return
		}
	}

	// Close zip writer to finalize zip structure
	err := zipWriter.Close()
	if err != nil {
		s.log.Error("failed to close zip writer", "error", err)
		http.Error(w, "Failed to generate template", http.StatusInternalServerError)
		return
	}

	// Send zip as attachment
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=custom_homepage_template.zip")
	w.Header().Set("Content-Length", string(rune(buf.Len())))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, buf)
}

// customHomepageUpload handles zip upload, validates content, extracts to uploads/custom_homepage/, and activates the setting
func (s *Server) customHomepageUpload(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Max 10MB zip upload
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		s.renderSettingsView(w, r, user, "", "File ZIP terlalu besar. Maksimal ukuran upload adalah 10 MB.")
		return
	}

	file, header, err := r.FormFile("homepage_zip")
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Gagal membaca file upload: "+err.Error())
		return
	}
	defer file.Close()

	// Check file extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		s.renderSettingsView(w, r, user, "", "File yang diunggah wajib berformat .zip!")
		return
	}

	// Save to temp file first to read using archive/zip
	tempDir := os.TempDir()
	tempFile, err := os.CreateTemp(tempDir, "homepage_upload_*.zip")
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Gagal membuat file sementara: "+err.Error())
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Gagal menulis file sementara: "+err.Error())
		return
	}

	// Open ZIP
	zipReader, err := zip.OpenReader(tempFile.Name())
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Gagal membuka file ZIP (file rusak/tidak valid): "+err.Error())
		return
	}
	defer zipReader.Close()

	// Validate index.html exists in the ZIP and check its template syntax BEFORE extraction
	var indexFileInZip *zip.File
	for _, f := range zipReader.File {
		// Clean file path to prevent zip slip vulnerability
		cleanName := filepath.Clean(f.Name)
		if cleanName == "index.html" || strings.HasSuffix(cleanName, "/index.html") {
			indexFileInZip = f
			break
		}
	}

	if indexFileInZip == nil {
		s.renderSettingsView(w, r, user, "", "Validasi Gagal: Berkas 'index.html' wajib berada di tingkat teratas (root) file ZIP!")
		return
	}

	// Validate Go HTML Template syntax in index.html inside the ZIP BEFORE extraction
	srcIndexFile, err := indexFileInZip.Open()
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Validasi Gagal: Gagal membuka berkas 'index.html' di dalam ZIP: "+err.Error())
		return
	}
	htmlContentBytes, err := io.ReadAll(srcIndexFile)
	srcIndexFile.Close()
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Validasi Gagal: Gagal membaca berkas 'index.html' di dalam ZIP: "+err.Error())
		return
	}

	// Parse as dynamic template to validate syntax
	_, err = web.ParseCustomTemplate("custom_homepage_validation", string(htmlContentBytes))
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Validasi Gagal: Berkas 'index.html' mengandung error sintaks Go HTML Template: "+err.Error())
		return
	}

	// Prepare extraction directory
	destDir := filepath.Join(s.cfg.UploadDir, "custom_homepage")
	
	// Clear destination directory to prevent stale files
	_ = os.RemoveAll(destDir)
	err = os.MkdirAll(destDir, 0755)
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Gagal membuat direktori penyimpanan: "+err.Error())
		return
	}

	// Extract files
	for _, f := range zipReader.File {
		// Check for Zip Slip vulnerability
		filePath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filePath, filepath.Clean(destDir)+string(os.PathSeparator)) && filePath != destDir {
			s.renderSettingsView(w, r, user, "", "Validasi Gagal: File ZIP terdeteksi mengandung jalur path tidak aman (Zip Slip).")
			return
		}

		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(filePath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			s.renderSettingsView(w, r, user, "", "Gagal mengekstrak folder: "+err.Error())
			return
		}

		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			s.renderSettingsView(w, r, user, "", "Gagal mengekstrak berkas: "+err.Error())
			return
		}

		srcFile, err := f.Open()
		if err != nil {
			dstFile.Close()
			s.renderSettingsView(w, r, user, "", "Gagal membuka berkas dalam ZIP: "+err.Error())
			return
		}

		_, err = io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()
		if err != nil {
			s.renderSettingsView(w, r, user, "", "Gagal menulis berkas ekstrak: "+err.Error())
			return
		}
	}

	// Automatically enable custom homepage in settings upon successful upload
	settings := s.store.GetSettings()
	settings["custom_homepage_enabled"] = "true"
	err = s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderSettingsView(w, r, user, "", "Berkas ZIP berhasil diekstrak, namun gagal mengaktifkan otomatis di database: "+err.Error())
		return
	}

	// Reload/update the custom homepage template cache in the server
	s.loadCustomHomepageTemplate()

	s.renderSettingsView(w, r, user, "Custom Homepage berhasil diunggah, diekstrak, dan diaktifkan secara otomatis! 🚀", "")
}
