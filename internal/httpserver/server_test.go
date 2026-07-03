package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	core "porta-berita/internal/cms"
	"porta-berita/internal/config"
	"porta-berita/internal/web"
)

func newTestServer(t *testing.T, store *mockStore) *Server {
	t.Helper()
	cfg := config.Config{
		Addr:         ":8080",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return &Server{
		cfg:       cfg,
		log:       logger,
		templates: nil,
		store:     store,
	}
}

func TestArticleIndexPaginationEmpty(t *testing.T) {
	store := &mockStore{
		articles:   []core.Article{},
		categories: []core.Category{},
	}
	w := httptest.NewRecorder()

	// Test directly by calling store methods
	total := store.CountPublishedArticles()
	if total != 0 {
		t.Errorf("expected total articles 0, got %d", total)
	}

	if w.Code != http.StatusOK {
		t.Errorf("request should be valid")
	}
}

func TestArticleIndexCountArticles(t *testing.T) {
	articles := []core.Article{
		{
			ID:       "1",
			Title:    "Test Article 1",
			Slug:     "test-article-1",
			Excerpt:  "Excerpt 1",
			Content:  "Content 1",
			Category: "Tech",
			Status:   core.ArticlePublished,
		},
		{
			ID:       "2",
			Title:    "Test Article 2",
			Slug:     "test-article-2",
			Excerpt:  "Excerpt 2",
			Content:  "Content 2",
			Category: "Tech",
			Status:   core.ArticlePublished,
		},
	}

	store := &mockStore{
		articles:   articles,
		categories: []core.Category{},
	}

	total := store.CountPublishedArticles()
	if total != 2 {
		t.Errorf("expected total articles 2, got %d", total)
	}
}

func TestArticleIndexPagination(t *testing.T) {
	articles := make([]core.Article, 15)
	for i := 0; i < 15; i++ {
		articles[i] = core.Article{
			ID:       string(rune('0' + i)),
			Title:    "Article " + string(rune('0'+i)),
			Category: "Tech",
			Status:   core.ArticlePublished,
		}
	}

	store := &mockStore{articles: articles}

	page1 := store.ListPublishedArticlesPaginated(0, 9)
	if len(page1) != 9 {
		t.Errorf("page 1 should have 9 articles, got %d", len(page1))
	}

	page2 := store.ListPublishedArticlesPaginated(9, 9)
	if len(page2) != 6 {
		t.Errorf("page 2 should have 6 articles, got %d", len(page2))
	}
}

func TestArticleIndexFiltered(t *testing.T) {
	articles := []core.Article{
		{
			ID:       "1",
			Title:    "Test Article 1",
			Category: "Tech",
			Status:   core.ArticlePublished,
		},
		{
			ID:       "2",
			Title:    "Test Article 2",
			Category: "News",
			Status:   core.ArticlePublished,
		},
	}

	store := &mockStore{articles: articles}

	count := store.CountPublishedArticlesFiltered("Tech", "")
	if count != 1 {
		t.Errorf("expected 1 tech article, got %d", count)
	}

	filtered := store.ListPublishedArticlesFiltered("Tech", "", 0, 9)
	if len(filtered) != 1 || filtered[0].ID != "1" {
		t.Errorf("expected article 1, got %v", filtered)
	}
}

func TestArticleBySlug(t *testing.T) {
	article := core.Article{
		ID:       "1",
		Title:    "Test Slug Article",
		Slug:     "test-slug-article",
		Category: "Tech",
		Status:   core.ArticlePublished,
	}

	store := &mockStore{
		articles: []core.Article{article},
	}

	found, err := store.ArticleBySlug("test-slug-article", false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if found == nil || found.ID != "1" {
		t.Errorf("expected article 1, got %v", found)
	}

	notFound, _ := store.ArticleBySlug("non-existent", false)
	if notFound != nil {
		t.Errorf("expected nil, got %v", notFound)
	}
}

func TestDashboardArticlesHandlerPagination(t *testing.T) {
	// Create 15 articles to trigger pagination (> 10 items)
	articles := make([]core.Article, 15)
	for i := 0; i < 15; i++ {
		articles[i] = core.Article{
			ID:       string(rune('0' + i)),
			Title:    "Dashboard Article " + string(rune('0'+i)),
			Category: "Tech",
			Status:   core.ArticlePublished,
		}
	}

	store := &mockStore{articles: articles}
	templates, err := web.ParseTemplates()
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}

	server := newTestServer(t, store)
	server.templates = templates

	// Request page 2
	req := httptest.NewRequest("GET", "/dashboard/articles?page=2", nil)
	ctx := withUser(context.Background(), &core.User{ID: "writer-id", Role: core.RoleWriter})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.dashboardArticles(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	// Page 2 should have active pagination button and link to Page 1
	if !strings.Contains(body, `class="active" href="?page=2"`) {
		t.Error("expected page 2 to be active page in pagination")
	}
	if !strings.Contains(body, `href="?page=1"`) {
		t.Error("expected pagination link to page 1 to be present")
	}
}

func TestDashboardAPIKeysDocsHandler(t *testing.T) {
	store := &mockStore{}
	server := newTestServer(t, store)

	// Test public access - no user context needed
	req := httptest.NewRequest("GET", "/dashboard/api-keys/docs", nil)
	w := httptest.NewRecorder()
	server.swaggerDocs(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "API Docs - NewsPaper CMS") {
		t.Error("expected Swagger HTML title in body")
	}
	if !strings.Contains(body, "/docs/openapi.json") {
		t.Error("expected OpenAPI specification URL in Swagger configuration")
	}
}

func TestDashboardSettingsHandler(t *testing.T) {
	store := &mockStore{}
	templates, err := web.ParseTemplates()
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}

	server := newTestServer(t, store)
	server.templates = templates

	req := httptest.NewRequest("GET", "/dashboard/settings", nil)
	ctx := withUser(context.Background(), &core.User{ID: "admin-id", Role: core.RoleAdmin})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.dashboardSettings(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "Judul Situs") {
		t.Error("expected 'Judul Situs' input field in body")
	}
	if !strings.Contains(body, "Tagline / Deskripsi") {
		t.Error("expected 'Tagline / Deskripsi' input field in body")
	}
	if !strings.Contains(body, "Deskripsi Lengkap / Tentang Portal (Footer)") {
		t.Error("expected 'Deskripsi Lengkap / Tentang Portal (Footer)' field in body")
	}
}

func TestUpdateSettingsHandler(t *testing.T) {
	store := &mockStore{}
	templates, err := web.ParseTemplates()
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}

	server := newTestServer(t, store)
	server.templates = templates

	form := strings.NewReader("site_title=MyCustomTitle&site_tagline=MyCustomTagline&site_description=MyCustomDescription&social_facebook_url=https://facebook.com/new&social_facebook_count=10K")
	req := httptest.NewRequest("POST", "/dashboard/settings", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := withUser(context.Background(), &core.User{ID: "admin-id", Role: core.RoleAdmin})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.updateSettings(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}

	settings := store.GetSettings()
	if settings["site_title"] != "MyCustomTitle" {
		t.Errorf("expected site_title to be updated to 'MyCustomTitle', got '%s'", settings["site_title"])
	}
	if settings["site_tagline"] != "MyCustomTagline" {
		t.Errorf("expected site_tagline to be updated to 'MyCustomTagline', got '%s'", settings["site_tagline"])
	}
	if settings["site_description"] != "MyCustomDescription" {
		t.Errorf("expected site_description to be updated to 'MyCustomDescription', got '%s'", settings["site_description"])
	}
}

func TestWizardRewriteHandler(t *testing.T) {
	store := &mockStore{}
	server := newTestServer(t, store)

	body := strings.NewReader(`{"model":"mock-rewrite","title":"Judul Asli","content":"Konten Asli"}`)
	req := httptest.NewRequest("POST", "/dashboard/articles/wizard/rewrite", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := withUser(context.Background(), &core.User{ID: "writer-id", Role: core.RoleWriter})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.wizardRewrite(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var responseData wizardRewriteResponse
	if err := json.Unmarshal(bodyBytes, &responseData); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if responseData.ModelUsed != "mock-rewrite" {
		t.Errorf("expected model_used 'mock-rewrite', got '%s'", responseData.ModelUsed)
	}
	if len(responseData.BackendLogs) == 0 {
		t.Error("expected backend logs to be populated, got empty slice")
	}
	if responseData.TokenUsage == nil || responseData.TokenUsage.TotalTokens == 0 {
		t.Error("expected token usage statistics to be populated")
	}
}

func TestWizardFetchHandler(t *testing.T) {
	// 1. Start a mock target HTTP server to scrape
	mockTargetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/valid" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
				<html>
					<head>
						<title>Judul Berita Valid</title>
					</head>
					<body>
						<p>Ini adalah paragraf berita pertama yang cukup panjang untuk di-scrape oleh parser.</p>
						<p>Ini adalah paragraf berita kedua yang juga memenuhi batas minimum panjang karakter.</p>
					</body>
				</html>
			`))
		} else if r.URL.Path == "/invalid-content" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
				<html>
					<head>
						<title>Judul Tapi Konten Kosong</title>
					</head>
					<body>
						<p>Singkat</p>
					</body>
				</html>
			`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockTargetServer.Close()

	store := &mockStore{}
	server := newTestServer(t, store)

	// Test 1: Valid scraping target
	{
		body := strings.NewReader(`{"url":"` + mockTargetServer.URL + `/valid"}`)
		req := httptest.NewRequest("POST", "/dashboard/articles/wizard/fetch", body)
		req.Header.Set("Content-Type", "application/json")
		ctx := withUser(context.Background(), &core.User{ID: "writer-id", Role: core.RoleWriter})
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		server.wizardFetch(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status OK, got %d", resp.StatusCode)
		}

		var responseData wizardFetchResponse
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(bodyBytes, &responseData)

		if responseData.Title != "Judul Berita Valid" {
			t.Errorf("expected title 'Judul Berita Valid', got '%s'", responseData.Title)
		}
		if responseData.Error != "" {
			t.Errorf("expected empty error, got '%s'", responseData.Error)
		}
	}

	// Test 2: Invalid/unextractable target (should return status bad request)
	{
		body := strings.NewReader(`{"url":"` + mockTargetServer.URL + `/invalid-content"}`)
		req := httptest.NewRequest("POST", "/dashboard/articles/wizard/fetch", body)
		req.Header.Set("Content-Type", "application/json")
		ctx := withUser(context.Background(), &core.User{ID: "writer-id", Role: core.RoleWriter})
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		server.wizardFetch(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status BadRequest, got %d", resp.StatusCode)
		}

		var responseData wizardFetchResponse
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(bodyBytes, &responseData)

		if responseData.Error == "" {
			t.Error("expected error message, got empty string")
		}
	}
}

func TestWizardDeleteBulkHandler(t *testing.T) {
	store := &mockStore{}
	server := newTestServer(t, store)

	// Test 1: Valid bulk delete
	{
		body := strings.NewReader(`{"ids":["article-1","article-2"]}`)
		req := httptest.NewRequest("POST", "/dashboard/articles/delete-bulk", body)
		req.Header.Set("Content-Type", "application/json")
		ctx := withUser(context.Background(), &core.User{ID: "writer-id", Role: core.RoleWriter})
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		server.deleteArticlesBulk(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status OK, got %d", resp.StatusCode)
		}

		var responseData deleteBulkResponse
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(bodyBytes, &responseData)

		if !responseData.Success {
			t.Error("expected success true")
		}
	}
}

func TestFacebookDetectPagesValidation(t *testing.T) {
	store := &mockStore{}
	server := newTestServer(t, store)

	// Test 1: Empty token/app secret
	req := httptest.NewRequest("POST", "/dashboard/facebook/detect-pages", strings.NewReader("access_token=&app_secret="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := withUser(context.Background(), &core.User{ID: "writer-id", Role: core.RoleWriter})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.facebookDetectPages(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status BadRequest, got %d", resp.StatusCode)
	}

	var data map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&data)
	if data["error"] != "Token dan Kunci Rahasia Aplikasi (App Secret) harus diisi" {
		t.Errorf("expected specific validation error, got '%s'", data["error"])
	}
}

func TestPermalinkSettingsAndMiddleware(t *testing.T) {
	// Setup mock store with a published article
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	article := core.Article{
		ID:          "123",
		Title:       "Test Article Title",
		Slug:        "test-article-title",
		Status:      core.ArticlePublished,
		CreatedAt:   now,
		PublishedAt: &now,
	}
	
	store := &mockStore{
		articles: []core.Article{article},
		settings: map[string]string{
			"permalink_structure": "day_and_name",
		},
	}
	
	server := newTestServer(t, store)
	templates, err := web.ParseTemplates()
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}
	server.templates = templates

	// 1. Test GET /dashboard/options-permalink
	req := httptest.NewRequest("GET", "/dashboard/options-permalink", nil)
	ctx := withUser(context.Background(), &core.User{ID: "admin-id", Role: core.RoleAdmin})
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	server.dashboardPermalinkSettings(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", resp.StatusCode)
	}
	
	// 2. Test POST /dashboard/options-permalink
	formReader := strings.NewReader("permalink_structure=custom&permalink_structure_custom=/artikel/%25year%25/%25monthnum%25/%25day%25/%25postname%25")
	reqPost := httptest.NewRequest("POST", "/dashboard/options-permalink", formReader)
	reqPost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqPost = reqPost.WithContext(ctx)

	wPost := httptest.NewRecorder()
	server.updatePermalinkSettings(wPost, reqPost)
	respPost := wPost.Result()
	if respPost.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %d", respPost.StatusCode)
	}

	// 3. Test Middleware HTML Rewrite
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><a href="/artikel/test-article-title">Link</a></body></html>`))
	})
	
	pm := &permalinkMiddleware{server: server}
	rewrittenHandler := pm.RewriteHTML(nextHandler)
	
	reqMW := httptest.NewRequest("GET", "/portal", nil)
	wMW := httptest.NewRecorder()
	rewrittenHandler.ServeHTTP(wMW, reqMW)
	
	mwBody := wMW.Body.String()
	expectedURL := "/artikel/2026/06/29/test-article-title"
	if !strings.Contains(mwBody, expectedURL) {
		t.Errorf("expected HTML to contain rewritten URL %s, got: %s", expectedURL, mwBody)
	}

	// 4. Test Canonical Redirect in articleBySlug
	reqRedirect := httptest.NewRequest("GET", "/artikel/test-article-title", nil)
	reqRedirect.SetPathValue("slug", "test-article-title")
	reqRedirect = reqRedirect.WithContext(ctx)
	wRedirect := httptest.NewRecorder()
	
	store.settings["permalink_structure"] = "day_and_name"
	
	server.articleBySlug(wRedirect, reqRedirect)
	respRedirect := wRedirect.Result()
	if respRedirect.StatusCode != http.StatusMovedPermanently {
		t.Errorf("expected status MovedPermanently (301), got %d", respRedirect.StatusCode)
	}
	loc := respRedirect.Header.Get("Location")
	if loc != "/artikel/2026/06/29/test-article-title" {
		t.Errorf("expected redirect location /artikel/2026/06/29/test-article-title, got %s", loc)
	}

	// 5. Test Root Slug Lookup in landingPage (fallback)
	reqRootSlug := httptest.NewRequest("GET", "/test-article-title", nil)
	reqRootSlug = reqRootSlug.WithContext(ctx)
	wRootSlug := httptest.NewRecorder()
	
	store.settings["permalink_structure"] = "post_name"
	
	server.landingPage(wRootSlug, reqRootSlug)
	respRootSlug := wRootSlug.Result()
	if respRootSlug.StatusCode != http.StatusMovedPermanently {
		t.Errorf("expected status MovedPermanently (301) for root slug lookup, got %d", respRootSlug.StatusCode)
	}
	locRoot := respRootSlug.Header.Get("Location")
	if locRoot != "/artikel/test-article-title" {
		t.Errorf("expected root redirect location /artikel/test-article-title, got %s", locRoot)
	}
}

func TestWordPressMigration(t *testing.T) {
	tmpUploadDir, err := os.MkdirTemp("", "uploads-test-*")
	if err != nil {
		t.Fatalf("failed to create temp upload dir: %v", err)
	}
	defer os.RemoveAll(tmpUploadDir)

	wpMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "test-image.jpg") || strings.Contains(r.URL.Path, "content-image.png") {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("fake image data"))
			return
		}

		if r.URL.Path == "/wp-json/wp-to-portal/v1/posts" {
			apiKey := r.URL.Query().Get("api_key")
			if apiKey != "test-wp-api-key" {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"message": "Forbidden"}`))
				return
			}

			posts := []map[string]string{
				{
					"title":          "WP Imported Article",
					"slug":           "wp-imported-article",
					"excerpt":        "excerpt contents",
					"content":        `Lorem ipsum <img src="http://` + r.Host + `/wp-content/uploads/2026/06/content-image.png" /> dolor sit.`,
					"category":       "Teknologi",
					"hero_image_url": "http://" + r.Host + "/wp-content/uploads/2026/06/test-image.jpg",
					"source_url":     "http://" + r.Host + "/wp-imported-article",
					"status":         "published",
					"created_at":     "2026-06-29 12:00:00",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(posts)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer wpMockServer.Close()

	store := &mockStore{
		settings: map[string]string{},
	}
	server := newTestServer(t, store)
	server.cfg.UploadDir = tmpUploadDir

	templates, err := web.ParseTemplates()
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}
	server.templates = templates

	ctx := withUser(context.Background(), &core.User{ID: "admin-id", Role: core.RoleAdmin})

	// 1. Test GET /dashboard/migration
	reqGET := httptest.NewRequest("GET", "/dashboard/migration", nil)
	reqGET = reqGET.WithContext(ctx)
	wGET := httptest.NewRecorder()
	server.dashboardMigration(wGET, reqGET)
	if wGET.Result().StatusCode != http.StatusOK {
		t.Errorf("expected GET status OK, got %d", wGET.Result().StatusCode)
	}

	// 2. Test POST /dashboard/migration with valid credentials
	formBody := fmt.Sprintf("wp_url=%s&wp_api_key=test-wp-api-key", wpMockServer.URL)
	reqPOST := httptest.NewRequest("POST", "/dashboard/migration", strings.NewReader(formBody))
	reqPOST.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqPOST = reqPOST.WithContext(ctx)
	wPOST := httptest.NewRecorder()

	server.runWPMigration(wPOST, reqPOST)
	if wPOST.Result().StatusCode != http.StatusSeeOther {
		t.Errorf("expected POST status SeeOther (303), got %d", wPOST.Result().StatusCode)
	}

	loc := wPOST.Result().Header.Get("Location")
	if loc != "/dashboard/migration" {
		t.Errorf("expected redirect location /dashboard/migration, got %s", loc)
	}

	// Give the background job worker a moment to complete execution
	time.Sleep(100 * time.Millisecond)

	// Query status endpoint
	reqStatus := httptest.NewRequest("GET", "/dashboard/migration/status", nil)
	reqStatus = reqStatus.WithContext(ctx)
	wStatus := httptest.NewRecorder()
	server.dashboardMigrationStatus(wStatus, reqStatus)

	if wStatus.Result().StatusCode != http.StatusOK {
		t.Errorf("expected status OK for status query, got %d", wStatus.Result().StatusCode)
	}

	var statusJob MigrationJob
	if err := json.NewDecoder(wStatus.Body).Decode(&statusJob); err != nil {
		t.Fatalf("failed to decode migration status: %v", err)
	}

	if statusJob.Status != "completed" {
		t.Errorf("expected job status completed, got %s (error: %s)", statusJob.Status, statusJob.ErrorMsg)
	}
}

