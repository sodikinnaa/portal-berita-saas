package httpserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"porta-berita/internal/cms"
	"porta-berita/internal/web"
)

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/portal" {
		http.NotFound(w, r)
		return
	}
	settings := s.store.GetSettings()
	// Jika custom homepage aktif, halaman /portal harus selalu menampilkan portal berita utama
	// dan tidak boleh di-redirect ke / (karena / sedang menampilkan custom homepage)
	if settings["custom_homepage_enabled"] != "true" && settings["enable_landing_page"] == "false" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.renderNewsPortal(w, r, settings)
}

func (s *Server) renderNewsPortal(w http.ResponseWriter, r *http.Request, settings map[string]string) {
	articles := s.store.ListPublishedArticles(24)
	categories := s.store.ListCategories()
	data := homeViewData{
		Articles:         limitArticles(articles, 10),
		Categories:       categories,
		CategorySections: buildHomeCategorySections(categories, articles, 4),
		Popular:          limitArticles(articles, 4),
		EditorPicks:      limitArticles(articles, 4),
		Settings:         settings,
		AppUser:          s.getLoggedInAppUser(r),
	}
	if len(articles) > 0 {
		data.Featured = articles[0]
		data.HasFeatured = true
		data.HeroSide = limitArticles(articles[1:], 2)
	}
	s.renderTemplate(w, "home.html", data)
}

func (s *Server) articleIndex(w http.ResponseWriter, r *http.Request) {
	if p := r.URL.Query().Get("p"); p != "" {
		s.articleByID(w, r, p)
		return
	}

	const pageSize = 9
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category == "" {
		category = strings.TrimSpace(r.URL.Query().Get("cataegory"))
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	var total int
	var articles []cms.Article

	if category != "" || query != "" {
		total = s.store.CountPublishedArticlesFiltered(category, query)
	} else {
		total = s.store.CountPublishedArticles()
	}

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	if page > totalPages {
		page = totalPages
	}

	offset := (page - 1) * pageSize

	if category != "" || query != "" {
		articles = s.store.ListPublishedArticlesFiltered(category, query, offset, pageSize)
	} else {
		articles = s.store.ListPublishedArticlesPaginated(offset, pageSize)
	}

	pages := getPaginationPages(page, totalPages)

	data := articleIndexViewData{
		Articles:       s.articleListItems(articles),
		Total:          total,
		Page:           page,
		PageSize:       pageSize,
		TotalPages:     totalPages,
		HasPrev:        page > 1,
		HasNext:        page < totalPages,
		PrevPage:       page - 1,
		NextPage:       page + 1,
		Pages:          pages,
		Categories:     s.store.ListCategories(),
		Popular:        limitArticles(s.store.ListPublishedArticles(4), 4),
		EditorPicks:    limitArticles(s.store.ListPublishedArticles(4), 4),
		CategoryFilter: category,
		SearchQuery:    query,
		Settings:       s.store.GetSettings(),
		AppUser:        s.getLoggedInAppUser(r),
	}
	s.renderTemplate(w, "article_index.html", data)
}

func (s *Server) articleLegacy(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/artikel/wordpress-news-magazine-meraih-penghargaan", http.StatusFound)
}

func (s *Server) articleBySlug(w http.ResponseWriter, r *http.Request) {
	article, err := s.store.ArticleBySlug(r.PathValue("slug"), false)
	if err != nil || article == nil {
		http.NotFound(w, r)
		return
	}

	settings := s.store.GetSettings()
	structure := settings["permalink_structure"]
	if structure == "" {
		structure = "post_name"
	}

	canonicalPath := web.ResolvePermalink(article.Slug, article.ID, article.CreatedAt, structure)
	if structure == "plain" {
		if r.URL.Path != "/" || r.URL.Query().Get("p") != article.ID {
			http.Redirect(w, r, canonicalPath, http.StatusMovedPermanently)
			return
		}
	} else {
		if r.URL.Path != canonicalPath {
			http.Redirect(w, r, canonicalPath, http.StatusMovedPermanently)
			return
		}
	}

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	data := articleViewData{
		Article:        *article,
		AuthorName:     s.store.UserName(article.AuthorID),
		Related:        s.store.ListRandomPublishedArticles(4, article.ID),
		Popular:        limitArticles(s.store.ListPublishedArticles(4), 4),
		EditorPicks:    limitArticles(s.store.ListPublishedArticles(4), 4),
		Categories:     s.store.ListCategories(),
		CategoryFilter: article.Category,
		Settings:       s.store.GetSettings(),
		BaseURL:        baseURL,
		AppUser:        s.getLoggedInAppUser(r),
	}
	s.renderTemplate(w, "article.html", data)
}

type customPageViewData struct {
	Title          string
	Content        string
	Popular        []cms.Article
	EditorPicks    []cms.Article
	Categories     []cms.Category
	CategoryFilter string
	SearchQuery    string
	Settings       map[string]string
	BaseURL        string
	ActivePage     string
	AppUser        *cms.AppUser
}

type landingPageViewData struct {
	LatestArticles []cms.Article
	Settings       map[string]string
	Categories     []cms.Category
	AppUser        *cms.AppUser
}

func (s *Server) landingPage(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()

	// Serve ads.txt dynamically
	if r.URL.Path == "/ads.txt" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(settings["ads_txt_content"]))
		return
	}

	// Serve custom HTML verification files
	filename := strings.TrimPrefix(r.URL.Path, "/")
	if settings["verification_html_filename"] != "" && filename == settings["verification_html_filename"] {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(settings["verification_html_content"]))
		return
	}

	// Handle plain permalink fallback
	if p := r.URL.Query().Get("p"); p != "" && r.URL.Path == "/" {
		s.articleByID(w, r, p)
		return
	}

	if r.URL.Path != "/" {
		trimmedPath := strings.Trim(r.URL.Path, "/")
		if trimmedPath != "" {
			parts := strings.Split(trimmedPath, "/")
			slug := parts[len(parts)-1]

			isValidSlug := true
			for _, char := range slug {
				if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_') {
					isValidSlug = false
					break
				}
			}

			if isValidSlug {
				article, err := s.store.ArticleBySlug(slug, false)
				if err == nil && article != nil {
					settings := s.store.GetSettings()
					structure := settings["permalink_structure"]
					if structure == "" {
						structure = "post_name"
					}

					canonicalPath := web.ResolvePermalink(article.Slug, article.ID, article.CreatedAt, structure)
					if r.URL.Path != canonicalPath {
						http.Redirect(w, r, canonicalPath, http.StatusMovedPermanently)
						return
					}

					scheme := "http"
					if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
						scheme = "https"
					}
					baseURL := scheme + "://" + r.Host

					data := articleViewData{
						Article:        *article,
						AuthorName:     s.store.UserName(article.AuthorID),
						Related:        s.store.ListRandomPublishedArticles(4, article.ID),
						Popular:        limitArticles(s.store.ListPublishedArticles(4), 4),
						EditorPicks:    limitArticles(s.store.ListPublishedArticles(4), 4),
						Categories:     s.store.ListCategories(),
						CategoryFilter: article.Category,
						Settings:       settings,
						BaseURL:        baseURL,
						AppUser:        s.getLoggedInAppUser(r),
					}
					s.renderTemplate(w, "article.html", data)
					return
				}
			}
		}
	}

	// If custom homepage is enabled, attempt to serve it
	if settings["custom_homepage_enabled"] == "true" {
		cleanPath := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if cleanPath == "." || cleanPath == "" {
			cleanPath = "index.html"
		}

		filePath := filepath.Join(s.cfg.UploadDir, "custom_homepage", cleanPath)

		// Check if it's a directory, and if so, try to serve its index.html
		info, err := os.Stat(filePath)
		if err == nil && info.IsDir() {
			cleanPath = filepath.Join(cleanPath, "index.html")
			filePath = filepath.Join(s.cfg.UploadDir, "custom_homepage", cleanPath)
			info, err = os.Stat(filePath)
		}

		if err == nil && !info.IsDir() {
			// If it's an HTML file (like index.html), parse and serve it dynamically
			if strings.HasSuffix(strings.ToLower(cleanPath), ".html") {
				key := filepath.ToSlash(cleanPath)

				s.customHomepageTmplMu.RLock()
				tmpl := s.customHomepageTmpls[key]
				s.customHomepageTmplMu.RUnlock()

				if tmpl == nil {
					htmlContent, err := os.ReadFile(filePath)
					if err == nil {
						parsedTmpl, err := web.ParseCustomTemplate("custom_"+key, string(htmlContent))
						if err == nil {
							s.customHomepageTmplMu.Lock()
							s.customHomepageTmpls[key] = parsedTmpl
							s.customHomepageTmplMu.Unlock()
							tmpl = parsedTmpl
						} else {
							s.log.Error("gagal parse custom template saat dynamic fallback", "path", key, "error", err)
						}
					}
				}

				if tmpl != nil {
					// Fetch dynamic data
					articles := s.store.ListPublishedArticles(24)
					categories := s.store.ListCategories()
					data := landingPageViewData{
						LatestArticles: articles,
						Settings:       settings,
						Categories:     categories,
					}
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					if err := tmpl.Execute(w, data); err == nil {
						return
					}
					s.log.Error("gagal execute custom template dari cache, fallback ke static", "path", key)
				}
			}

			// Serve static asset (CSS, JS, images, etc.)
			http.ServeFile(w, r, filePath)
			return
		}
		
		// If requesting another path that doesn't exist, return 404
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
	} else {
		// Default behavior: only match exact "/" route
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
	}

	if settings["enable_landing_page"] == "false" {
		s.renderNewsPortal(w, r, settings)
		return
	}
	articles := s.store.ListPublishedArticles(3)
	categories := s.store.ListCategories()
	data := landingPageViewData{
		LatestArticles: articles,
		Settings:       settings,
		Categories:     categories,
		AppUser:        s.getLoggedInAppUser(r),
	}
	s.renderTemplate(w, "landing.html", data)
}

func (s *Server) serveStaticPage(w http.ResponseWriter, r *http.Request, title, settingsKey, activePage string) {
	articles := s.store.ListPublishedArticles(24)
	categories := s.store.ListCategories()
	settings := s.store.GetSettings()
	content := settings[settingsKey]

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	siteTitle := settings["site_title"]
	if siteTitle == "" {
		siteTitle = "NewsPaper"
	}

	emailHost, _, _ := strings.Cut(r.Host, ":")

	// Dynamic replacements for media title and host/domain
	content = strings.ReplaceAll(content, "[Nama Media]", siteTitle)
	content = strings.ReplaceAll(content, "Siap Digital News", siteTitle+" News")
	content = strings.ReplaceAll(content, "Siap Digital", siteTitle)
	content = strings.ReplaceAll(content, "siapdigital.com", emailHost)
	content = strings.ReplaceAll(content, "[domain-anda].com", emailHost)
	content = strings.ReplaceAll(content, "[Domain]", r.Host)

	data := customPageViewData{
		Title:          title,
		Content:        content,
		Popular:        limitArticles(articles, 4),
		EditorPicks:    limitArticles(articles, 4),
		Categories:     categories,
		Settings:       settings,
		BaseURL:        baseURL,
		ActivePage:     activePage,
		AppUser:        s.getLoggedInAppUser(r),
	}
	s.renderTemplate(w, "page.html", data)
}

func (s *Server) aboutPage(w http.ResponseWriter, r *http.Request) {
	s.serveStaticPage(w, r, "Tentang Kami", "page_about_content", "tentang")
}

func (s *Server) contactPage(w http.ResponseWriter, r *http.Request) {
	s.serveStaticPage(w, r, "Hubungi Kami", "page_contact_content", "kontak")
}

func (s *Server) privacyPage(w http.ResponseWriter, r *http.Request) {
	s.serveStaticPage(w, r, "Kebijakan Privasi", "page_privacy_content", "privasi")
}

func (s *Server) adsPage(w http.ResponseWriter, r *http.Request) {
	s.serveStaticPage(w, r, "Informasi Iklan", "page_ads_content", "iklan")
}

func getPaginationPages(currentPage, totalPages int) []int {
	if totalPages <= 7 {
		pages := make([]int, totalPages)
		for i := range pages {
			pages[i] = i + 1
		}
		return pages
	}

	pages := make([]int, 0, 9)

	// Always show page 1
	pages = append(pages, 1)

	// Left ellipsis condition
	if currentPage > 3 {
		pages = append(pages, -1) // -1 represents "..."
	}

	// Determine start and end of the middle range
	start := currentPage - 1
	end := currentPage + 1

	if currentPage <= 3 {
		start = 2
		end = 4
	} else if currentPage >= totalPages - 2 {
		start = totalPages - 3
		end = totalPages - 1
	}

	for i := start; i <= end; i++ {
		pages = append(pages, i)
	}

	// Right ellipsis condition
	if currentPage < totalPages - 2 {
		pages = append(pages, -1) // -1 represents "..."
	}

	// Always show last page
	pages = append(pages, totalPages)

	return pages
}


