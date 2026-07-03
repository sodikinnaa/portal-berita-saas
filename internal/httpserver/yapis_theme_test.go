package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	core "porta-berita/internal/cms"
	"porta-berita/internal/web"
)

func TestYapisThemeRendering(t *testing.T) {
	store := &mockStore{}
	server := newTestServer(t, store)

	// Parse all themes
	themes, err := web.ParseAllThemes()
	if err != nil {
		t.Fatalf("failed to parse all themes: %v", err)
	}
	server.themes = themes

	// Setup settings
	if store.settings == nil {
		store.settings = make(map[string]string)
	}
	store.settings["active_theme"] = "yapis"
	store.settings["site_title"] = "YAPIS"
	store.settings["site_tagline"] = "Unggul, Mandiri, dan Berbudaya"

	// Mock published articles
	now := time.Now()
	store.articles = []core.Article{
		{
			ID:           "art_1",
			Title:        "Pengumuman PPDB 2026",
			Slug:         "pengumuman-ppdb-2026",
			Excerpt:      "PPDB Gelombang 2 Resmi dibuka.",
			Content:      "Ini adalah isi konten pengumuman PPDB Yapis.",
			Category:     "Pengumuman",
			HeroImageURL: "/uploads/image.jpg",
			Status:       core.ArticlePublished,
			CreatedAt:    now,
			PublishedAt:  &now,
		},
		{
			ID:           "art_2",
			Title:        "Prestasi Siswa Yapis",
			Slug:         "prestasi-siswa-yapis",
			Excerpt:      "Siswa Yapis menjuarai lomba cerdas cermat.",
			Content:      "Ini isi konten prestasi siswa.",
			Category:     "Prestasi",
			HeroImageURL: "",
			Status:       core.ArticlePublished,
			CreatedAt:    now,
			PublishedAt:  &now,
		},
	}

	// Mock categories
	store.categories = []core.Category{
		{Name: "Pengumuman", Slug: "pengumuman"},
		{Name: "Prestasi", Slug: "prestasi"},
	}

	// 1. Test Homepage Rendering under /portal
	req := httptest.NewRequest("GET", "/portal", nil)
	w := httptest.NewRecorder()
	server.home(w, req)

	respBody := w.Body.String()
	if w.Code != http.StatusOK {
		t.Errorf("expected homepage response OK, got status %d", w.Code)
	}

	// Verify Yapis-specific HTML structure on homepage
	if !strings.Contains(respBody, "logo-bold") || !strings.Contains(respBody, "YAPIS") {
		t.Error("homepage response does not contain YAPIS theme logo markup")
	}
	if !strings.Contains(respBody, "hero-section") {
		t.Error("homepage response does not contain YAPIS hero-section markup")
	}
	if !strings.Contains(respBody, "Pengumuman PPDB 2026") {
		t.Error("homepage response does not contain dynamic article title")
	}
	if !strings.Contains(respBody, "/assets/yapis.css") {
		t.Error("homepage response does not link yapis.css stylesheet")
	}

	// 2. Test Article Detail Rendering
	reqDetail := httptest.NewRequest("GET", "/artikel/pengumuman-ppdb-2026", nil)
	reqDetail.SetPathValue("slug", "pengumuman-ppdb-2026")
	wDetail := httptest.NewRecorder()
	server.articleBySlug(wDetail, reqDetail)

	respBodyDetail := wDetail.Body.String()
	if wDetail.Code != http.StatusOK {
		t.Errorf("expected article detail response OK, got status %d. Body: %s", wDetail.Code, respBodyDetail)
	}

	// Verify Yapis-specific HTML structure on detail page
	if !strings.Contains(respBodyDetail, "article-container") {
		t.Error("detail page response does not contain article-container class")
	}
	if !strings.Contains(respBodyDetail, "article-body-text") {
		t.Error("detail page response does not contain article-body-text class")
	}
	if !strings.Contains(respBodyDetail, "PPDB Gelombang 2 Resmi dibuka") {
		t.Error("detail page response does not contain article excerpt")
	}
}
