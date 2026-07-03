package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	core "porta-berita/internal/cms"
	"porta-berita/internal/web"
)

func TestCronSaveSettings(t *testing.T) {
	store := &mockStore{
		settings: make(map[string]string),
	}
	server := newTestServer(t, store)

	templates, err := web.ParseTemplates()
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}
	server.templates = templates


	form := url.Values{}
	form.Set("cron_enabled", "true")
	form.Set("cron_interval", "30")
	form.Set("cron_category", "Tech")
	form.Set("cron_rss_url", "https://news.google.com/test")
	form.Set("cron_prompt", "Write concisely")

	req := httptest.NewRequest("POST", "/dashboard/cron/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	ctx := withUser(req.Context(), &core.User{ID: "user-admin", Role: "admin"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.cronSaveSettings(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", w.Code)
	}

	settings := store.GetSettings()
	if settings["cron_enabled"] != "true" {
		t.Errorf("expected cron_enabled true, got %s", settings["cron_enabled"])
	}
	if settings["cron_interval"] != "30" {
		t.Errorf("expected cron_interval 30, got %s", settings["cron_interval"])
	}
}

func TestCronManualImportMockAI(t *testing.T) {
	store := &mockStore{
		settings: map[string]string{
			"ai_api_key":       "", // forces Mock AI
			"ai_default_model": "mock-rewrite",
		},
		articles: []core.Article{},
	}
	server := newTestServer(t, store)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Original Source Title</title><meta property="og:image" content="https://test.com/image.jpg"></head><body><p>This is paragraph one of the original content which is long enough to scrape.</p><p>This is paragraph two of the original content which is also long enough to scrape.</p></body></html>`))
	}))
	defer ts.Close()

	bodyMap := map[string]string{
		"title":        "Original Source Title",
		"google_link":  "https://news.google.com/test",
		"original_url": ts.URL,
	}
	bodyBytes, _ := json.Marshal(bodyMap)

	req := httptest.NewRequest("POST", "/dashboard/cron/import", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	
	ctx := withUser(req.Context(), &core.User{ID: "user-admin", Role: "admin"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.cronManualImport(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp cronManualImportResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected import success, got error: %s", resp.Error)
	}

	if len(store.articles) != 1 {
		t.Errorf("expected 1 imported article in store, got %d", len(store.articles))
	} else {
		art := store.articles[0]
		if !strings.Contains(art.Title, "Original Source Title") {
			t.Errorf("expected rewritten title to contain original title, got %s", art.Title)
		}
		if art.HeroImageURL != "https://test.com/image.jpg" {
			t.Errorf("expected hero image URL to be scraped, got %s", art.HeroImageURL)
		}
		if art.Status != core.ArticlePublished {
			t.Errorf("expected imported article status to be Published, got %s", art.Status)
		}
	}
}

func TestCronManualImportBlacklistedDomain(t *testing.T) {
	store := &mockStore{
		settings:  make(map[string]string),
		blacklist: []string{"badweb.com"},
	}
	server := newTestServer(t, store)

	bodyMap := map[string]string{
		"title":        "Blocked Title",
		"google_link":  "https://news.google.com/test",
		"original_url": "https://badweb.com/article/1",
	}
	bodyBytes, _ := json.Marshal(bodyMap)

	req := httptest.NewRequest("POST", "/dashboard/cron/import", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	
	ctx := withUser(req.Context(), &core.User{ID: "user-admin", Role: "admin"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.cronManualImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 Bad Request, got %d", w.Code)
	}

	var resp cronManualImportResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Success {
		t.Error("expected import success to be false")
	}
	if !strings.Contains(resp.Error, "blacklist") {
		t.Errorf("expected error message to mention blacklist, got: %s", resp.Error)
	}
}

func TestCronManualImportScrapeFailure(t *testing.T) {
	store := &mockStore{
		settings: make(map[string]string),
	}
	server := newTestServer(t, store)

	// Scrape failure simulation using an offline/invalid URL port
	bodyMap := map[string]string{
		"title":        "Failing Title",
		"google_link":  "https://news.google.com/test",
		"original_url": "http://127.0.0.1:9999/does-not-exist",
	}
	bodyBytes, _ := json.Marshal(bodyMap)

	req := httptest.NewRequest("POST", "/dashboard/cron/import", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	
	ctx := withUser(req.Context(), &core.User{ID: "user-admin", Role: "admin"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.cronManualImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 Bad Request, got %d", w.Code)
	}

	var resp cronManualImportResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Success {
		t.Error("expected import success to be false")
	}

	// Verify that the domain was added to the blacklist upon failure
	isBlacklisted, _ := store.IsDomainBlacklisted("127.0.0.1")
	if !isBlacklisted {
		t.Error("expected domain 127.0.0.1 to be blacklisted after scrape failure")
	}
}

func TestCronManualImportInvalidHTML(t *testing.T) {
	store := &mockStore{
		settings: make(map[string]string),
	}
	server := newTestServer(t, store)

	// Mock web server returning empty/garbage HTML
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body></body></html>`))
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	hostname := u.Hostname()

	bodyMap := map[string]string{
		"title":        "",
		"google_link":  "https://news.google.com/test",
		"original_url": ts.URL,
	}
	bodyBytes, _ := json.Marshal(bodyMap)

	req := httptest.NewRequest("POST", "/dashboard/cron/import", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	
	ctx := withUser(req.Context(), &core.User{ID: "user-admin", Role: "admin"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.cronManualImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 Bad Request, got %d", w.Code)
	}

	// Verify that the domain was blacklisted
	isBlacklisted, _ := store.IsDomainBlacklisted(hostname)
	if !isBlacklisted {
		t.Errorf("expected hostname %s to be blacklisted after scraping empty content", hostname)
	}
}

func TestCronManualImportAIRewriteFailure(t *testing.T) {
	// Triggers HTTP error in performAIRewriteSync by setting an active dummy API key
	// but using an invalid API endpoint that returns 500 Internal Server Error
	tsAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("AI Server Error"))
	}))
	defer tsAI.Close()

	store := &mockStore{
		settings: map[string]string{
			"ai_api_key":      "dummy-key",
			"ai_endpoint_url": tsAI.URL,
		},
	}
	server := newTestServer(t, store)

	tsSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Source Title</title></head><body><p>This is paragraph content that is long enough to scrape.</p></body></html>`))
	}))
	defer tsSource.Close()

	u, _ := url.Parse(tsSource.URL)
	hostname := u.Hostname()

	bodyMap := map[string]string{
		"title":        "Source Title",
		"google_link":  "https://news.google.com/test",
		"original_url": tsSource.URL,
	}
	bodyBytes, _ := json.Marshal(bodyMap)

	req := httptest.NewRequest("POST", "/dashboard/cron/import", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	
	ctx := withUser(req.Context(), &core.User{ID: "user-admin", Role: "admin"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.cronManualImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 Bad Request, got %d, body: %s", w.Code, w.Body.String())
	}

	// Verify that domain is blacklisted due to rewrite failure
	isBlacklisted, _ := store.IsDomainBlacklisted(hostname)
	if !isBlacklisted {
		t.Errorf("expected hostname %s to be blacklisted after AI rewrite failure", hostname)
	}
}

func TestCronManualImportAIFallbackMessage(t *testing.T) {
	// Triggers the fallback validation when AI returns "konten utama tidak tersedia"
	tsAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Mock Gemini response structure containing fallback text
		w.Write([]byte(`{
			"candidates": [
				{
					"content": {
						"parts": [
							{
								"text": "{\\"title\\": \\"konten utama tidak tersedia\\", \\"excerpt\\": \\"tidak tersedia\\", \\"content\\": \\"tidak tersedia\\", \\"category\\": \\"Berita\\"}"
							}
						]
					}
				}
			]
		}`))
	}))
	defer tsAI.Close()

	store := &mockStore{
		settings: map[string]string{
			"ai_api_key":      "dummy-key",
			"ai_endpoint_url": tsAI.URL,
		},
	}
	server := newTestServer(t, store)

	tsSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Source Title</title></head><body><p>This is paragraph content that is long enough to scrape.</p></body></html>`))
	}))
	defer tsSource.Close()

	u, _ := url.Parse(tsSource.URL)
	hostname := u.Hostname()

	bodyMap := map[string]string{
		"title":        "Source Title",
		"google_link":  "https://news.google.com/test",
		"original_url": tsSource.URL,
	}
	bodyBytes, _ := json.Marshal(bodyMap)

	req := httptest.NewRequest("POST", "/dashboard/cron/import", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	
	ctx := withUser(req.Context(), &core.User{ID: "user-admin", Role: "admin"})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.cronManualImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 Bad Request, got %d, body: %s", w.Code, w.Body.String())
	}

	// Verify that domain is blacklisted due to fallback content returned by AI
	isBlacklisted, _ := store.IsDomainBlacklisted(hostname)
	if !isBlacklisted {
		t.Errorf("expected hostname %s to be blacklisted after AI fallback content validation", hostname)
	}
}
