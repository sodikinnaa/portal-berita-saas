package httpserver

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	"porta-berita/internal/cms"
	"porta-berita/internal/web"
)

type PubRSSFeed struct {
	XMLName  xml.Name      `xml:"rss"`
	Version  string        `xml:"version,attr"`
	AtomNS   string        `xml:"xmlns:atom,attr"`
	Channel  PubRSSChannel `xml:"channel"`
}

type PubRSSChannel struct {
	Title       string         `xml:"title"`
	Link        string         `xml:"link"`
	Description string         `xml:"description"`
	Language    string         `xml:"language,omitempty"`
	PubDate     string         `xml:"pubDate,omitempty"`
	AtomLink    PubRSSAtomLink `xml:"atom:link"`
	Items       []PubRSSItem   `xml:"item"`
}

type PubRSSAtomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type PubRSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate,omitempty"`
	GUID        string `xml:"guid"`
}

type SitemapURLSet struct {
	XMLName xml.Name     `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []SitemapURL `xml:"url"`
}

type SitemapURL struct {
	Loc        string  `xml:"loc"`
	LastMod    string  `xml:"lastmod,omitempty"`
	ChangeFreq string  `xml:"changefreq,omitempty"`
	Priority   float64 `xml:"priority,omitempty"`
}

// categoryRSS generates an RSS 2.0 XML feed for the specified category or all categories
func (s *Server) categoryRSS(w http.ResponseWriter, r *http.Request) {
	categoryParam := r.PathValue("category")
	categoryName := strings.TrimSuffix(categoryParam, ".xml")
	categoryName = strings.TrimSpace(categoryName)

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	settings := s.store.GetSettings()
	siteTitle := settings["site_title"]
	if siteTitle == "" {
		siteTitle = "NewsPaper"
	}

	var articles []cms.Article
	var title string
	var description string
	var feedLink string

	if strings.ToLower(categoryName) == "category" || categoryName == "" {
		// General RSS feed (all categories)
		articles = s.store.ListPublishedArticles(50)
		title = fmt.Sprintf("Semua Artikel - %s", siteTitle)
		description = fmt.Sprintf("Feed RSS berita terbaru dari %s", siteTitle)
		feedLink = baseURL + "/rss/category.xml"
	} else {
		// Category-specific RSS feed
		// Convert category name for display, e.g., "hiburan" -> "Hiburan"
		displayCategoryName := strings.Title(strings.ToLower(categoryName))
		articles = s.store.ListPublishedArticlesFiltered(categoryName, "", 0, 50)
		title = fmt.Sprintf("Kategori %s - %s", displayCategoryName, siteTitle)
		description = fmt.Sprintf("Feed RSS berita kategori %s dari %s", displayCategoryName, siteTitle)
		feedLink = fmt.Sprintf("%s/rss/%s.xml", baseURL, categoryName)
	}

	rssItems := make([]PubRSSItem, 0, len(articles))
	for _, article := range articles {
		pubDate := article.PublishedAt.Format(time.RFC1123)
		articleLink := baseURL + web.ResolvePermalink(article.Slug, article.ID, article.CreatedAt, settings["permalink_structure"])

		rssItems = append(rssItems, PubRSSItem{
			Title:       article.Title,
			Link:        articleLink,
			Description: article.Excerpt,
			PubDate:     pubDate,
			GUID:        articleLink,
		})
	}

	feed := PubRSSFeed{
		Version: "2.0",
		AtomNS:  "http://www.w3.org/2005/Atom",
		Channel: PubRSSChannel{
			Title:       title,
			Link:        baseURL + "/portal",
			Description: description,
			Language:    "id-id",
			PubDate:     time.Now().Format(time.RFC1123),
			AtomLink: PubRSSAtomLink{
				Href: feedLink,
				Rel:  "self",
				Type: "application/rss+xml",
			},
			Items: rssItems,
		},
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// Write XML declaration first
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		s.log.Error("gagal menulis header xml", "error", err)
		return
	}
	
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		s.log.Error("gagal encode rss xml", "error", err)
	}
}

// sitemapXML generates a dynamic sitemap.xml for search engines
func (s *Server) sitemapXML(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	settings := s.store.GetSettings()
	enableLanding := settings["enable_landing_page"] == "true"

	var urls []SitemapURL

	// 1. Home / Landing Page
	urls = append(urls, SitemapURL{
		Loc:        baseURL + "/",
		LastMod:    time.Now().Format("2006-01-02"),
		ChangeFreq: "daily",
		Priority:   1.0,
	})

	// 2. Portal Main Page (if landing page is active)
	if enableLanding {
		urls = append(urls, SitemapURL{
			Loc:        baseURL + "/portal",
			LastMod:    time.Now().Format("2006-01-02"),
			ChangeFreq: "daily",
			Priority:   0.9,
		})
	}

	// 3. Static Pages (Tentang, Kontak, Privasi, Iklan)
	staticPages := []string{"/tentang", "/kontak", "/privasi", "/iklan"}
	for _, page := range staticPages {
		urls = append(urls, SitemapURL{
			Loc:        baseURL + page,
			LastMod:    time.Now().Format("2006-01-02"),
			ChangeFreq: "monthly",
			Priority:   0.5,
		})
	}

	// 4. Categories
	categories := s.store.ListCategories()
	for _, cat := range categories {
		urls = append(urls, SitemapURL{
			Loc:        fmt.Sprintf("%s/artikel?category=%s", baseURL, cat.Name),
			LastMod:    time.Now().Format("2006-01-02"),
			ChangeFreq: "daily",
			Priority:   0.7,
		})
	}

	// 5. Published Articles
	// Fetch up to 1000 published articles
	articles := s.store.ListPublishedArticles(1000)
	for _, article := range articles {
		lastMod := article.PublishedAt.Format("2006-01-02")
		urls = append(urls, SitemapURL{
			Loc:        baseURL + web.ResolvePermalink(article.Slug, article.ID, article.CreatedAt, settings["permalink_structure"]),
			LastMod:    lastMod,
			ChangeFreq: "weekly",
			Priority:   0.8,
		})
	}

	sitemap := SitemapURLSet{
		URLs: urls,
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// Write XML declaration first
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		s.log.Error("gagal menulis sitemap xml header", "error", err)
		return
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(sitemap); err != nil {
		s.log.Error("gagal encode sitemap xml", "error", err)
	}
}
