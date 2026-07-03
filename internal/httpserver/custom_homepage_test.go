package httpserver

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	core "porta-berita/internal/cms"
	"porta-berita/internal/web"
)

func newTestServerWithTemplates(t *testing.T, store *mockStore) *Server {
	t.Helper()
	server := newTestServer(t, store)
	templates, err := web.ParseTemplates()
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}
	server.templates = templates
	
	// Set mock theme to default to ensure we use parsed templates
	if store.settings == nil {
		store.settings = make(map[string]string)
	}
	store.settings["active_theme"] = "default"
	
	return server
}

func TestCustomHomepageToggle(t *testing.T) {
	store := &mockStore{}
	server := newTestServerWithTemplates(t, store)

	adminUser := &core.User{ID: "admin-id", Role: core.RoleAdmin}

	// 1. Initial State should not be true
	settings := store.GetSettings()
	if settings["custom_homepage_enabled"] == "true" {
		t.Error("expected custom_homepage_enabled to not be true initially")
	}

	// 2. Perform Toggle to true
	req := httptest.NewRequest("POST", "/dashboard/settings/custom-homepage/toggle", strings.NewReader("custom_homepage_enabled=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := withUser(context.Background(), adminUser)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.customHomepageToggle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", w.Code)
	}

	settings = store.GetSettings()
	if settings["custom_homepage_enabled"] != "true" {
		t.Error("expected custom_homepage_enabled to be set to true")
	}

	// 3. Perform Toggle back to false
	req = httptest.NewRequest("POST", "/dashboard/settings/custom-homepage/toggle", strings.NewReader("custom_homepage_enabled=false"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(context.Background(), adminUser))

	w = httptest.NewRecorder()
	server.customHomepageToggle(w, req)

	settings = store.GetSettings()
	if settings["custom_homepage_enabled"] != "false" {
		t.Error("expected custom_homepage_enabled to be set to false")
	}
}

func TestCustomHomepageTemplate(t *testing.T) {
	store := &mockStore{}
	server := newTestServerWithTemplates(t, store)

	adminUser := &core.User{ID: "admin-id", Role: core.RoleAdmin}

	req := httptest.NewRequest("GET", "/dashboard/settings/custom-homepage/template", nil)
	req = req.WithContext(withUser(context.Background(), adminUser))

	w := httptest.NewRecorder()
	server.customHomepageTemplate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", w.Code)
	}

	if contentType := w.Header().Get("Content-Type"); contentType != "application/zip" {
		t.Errorf("expected content type application/zip, got %s", contentType)
	}

	// Parse zip content to verify index.html, style.css, script.js exist
	bodyBytes := w.Body.Bytes()
	zipReader, err := zip.NewReader(bytes.NewReader(bodyBytes), int64(len(bodyBytes)))
	if err != nil {
		t.Fatalf("failed to parse returned body as ZIP: %v", err)
	}

	expectedFiles := map[string]bool{
		"index.html": false,
		"style.css":  false,
		"script.js":  false,
	}

	for _, file := range zipReader.File {
		if _, ok := expectedFiles[file.Name]; ok {
			expectedFiles[file.Name] = true
		}
	}

	for name, found := range expectedFiles {
		if !found {
			t.Errorf("expected file %s was not found in the template ZIP", name)
		}
	}
}

func TestCustomHomepageUploadAndServing(t *testing.T) {
	store := &mockStore{}
	server := newTestServerWithTemplates(t, store)

	// Create temp dir for upload extraction and cleanup
	tempUploadDir, err := os.MkdirTemp("", "portal_uploads_*")
	if err != nil {
		t.Fatalf("failed to create temp upload dir: %v", err)
	}
	defer os.RemoveAll(tempUploadDir)
	server.cfg.UploadDir = tempUploadDir

	adminUser := &core.User{ID: "admin-id", Role: core.RoleAdmin}

	// 1. Create a valid mock zip in memory containing index.html and style.css
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)
	
	htmlHeader, _ := zipWriter.Create("index.html")
	_, _ = htmlHeader.Write([]byte("<h1>Hello Static</h1>"))
	
	cssHeader, _ := zipWriter.Create("style.css")
	_, _ = cssHeader.Write([]byte("body { background: red; }"))
	
	_ = zipWriter.Close()

	// 2. Create multipart upload request
	bodyBuf := new(bytes.Buffer)
	multipartWriter := multipart.NewWriter(bodyBuf)
	fileWriter, err := multipartWriter.CreateFormFile("homepage_zip", "homepage.zip")
	if err != nil {
		t.Fatalf("failed to create multipart file writer: %v", err)
	}
	_, _ = io.Copy(fileWriter, zipBuf)
	_ = multipartWriter.Close()

	req := httptest.NewRequest("POST", "/dashboard/settings/custom-homepage/upload", bodyBuf)
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req = req.WithContext(withUser(context.Background(), adminUser))

	w := httptest.NewRecorder()
	server.customHomepageUpload(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status OK on upload, got %d", w.Code)
	}

	// 3. Verify files are extracted and setting is enabled
	settings := store.GetSettings()
	if settings["custom_homepage_enabled"] != "true" {
		t.Error("expected custom_homepage_enabled to be automatically enabled after upload")
	}

	destIndex := filepath.Join(server.cfg.UploadDir, "custom_homepage", "index.html")
	if _, err := os.Stat(destIndex); os.IsNotExist(err) {
		t.Error("expected index.html to be extracted on disk")
	}

	// 4. Verify serving homepage serves index.html
	reqHome := httptest.NewRequest("GET", "/", nil)
	wHome := httptest.NewRecorder()
	server.landingPage(wHome, reqHome)

	if wHome.Code != http.StatusOK {
		t.Errorf("expected status OK serving homepage, got %d", wHome.Code)
	}

	homeBody := wHome.Body.String()
	if !strings.Contains(homeBody, "Hello Static") {
		t.Errorf("expected served homepage to contain 'Hello Static', got: %s", homeBody)
	}

	// 5. Verify serving style.css
	reqCSS := httptest.NewRequest("GET", "/style.css", nil)
	wCSS := httptest.NewRecorder()
	server.landingPage(wCSS, reqCSS)

	if wCSS.Code != http.StatusOK {
		t.Errorf("expected status OK serving style.css, got %d", wCSS.Code)
	}

	cssBody := wCSS.Body.String()
	if !strings.Contains(cssBody, "background: red") {
		t.Errorf("expected served CSS to contain 'background: red', got: %s", cssBody)
	}

	// 6. Verify non-existent asset returns 404
	req404 := httptest.NewRequest("GET", "/non_existent_file.png", nil)
	w404 := httptest.NewRecorder()
	server.landingPage(w404, req404)

	if w404.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for non-existent asset, got %d", w404.Code)
	}
}

func TestCustomHomepageInvalidUpload(t *testing.T) {
	store := &mockStore{}
	server := newTestServerWithTemplates(t, store)

	adminUser := &core.User{ID: "admin-id", Role: core.RoleAdmin}

	// 1. Create a zip in memory that does NOT contain index.html (violates validation)
	zipBuf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuf)
	
	otherHeader, _ := zipWriter.Create("about.html")
	_, _ = otherHeader.Write([]byte("<h1>About Us Only</h1>"))
	
	_ = zipWriter.Close()

	// 2. Create multipart upload request
	bodyBuf := new(bytes.Buffer)
	multipartWriter := multipart.NewWriter(bodyBuf)
	fileWriter, _ := multipartWriter.CreateFormFile("homepage_zip", "homepage.zip")
	_, _ = io.Copy(fileWriter, zipBuf)
	_ = multipartWriter.Close()

	req := httptest.NewRequest("POST", "/dashboard/settings/custom-homepage/upload", bodyBuf)
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	req = req.WithContext(withUser(context.Background(), adminUser))

	w := httptest.NewRecorder()
	server.customHomepageUpload(w, req)

	// Since it failed validation, it renders the settings page displaying the error message
	if w.Code != http.StatusOK {
		t.Fatalf("expected status OK (rendering template), got %d", w.Code)
	}

	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "wajib berada di tingkat teratas") {
		t.Errorf("expected validation error message, got: %s", responseBody)
	}
}
