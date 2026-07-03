package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	appcms "porta-berita/internal/application/cms"
	"porta-berita/internal/cms"
	"porta-berita/internal/config"
	"porta-berita/internal/web"
)

const sessionCookieName = "portal_session"

type CronLog struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

type AILogEntry struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`      // e.g. "Tarik Model", "Rewrite Artikel", "Caption FB", "Caption Bluesky"
	Model     string `json:"model"`
	Proxy     string `json:"proxy"`     // e.g. "176.111.37.216:39811" or "Direct"
	Status    string `json:"status"`    // e.g. "Success", "Failed"
	Details   string `json:"details"`   // e.g. details or error message
}

type Server struct {
	cfg                  config.Config
	log                  *slog.Logger
	templates            *template.Template
	themes               map[string]*template.Template
	store                appcms.ContentStore
	cronLogs             []CronLog
	cronLogsMu           sync.RWMutex
	fbLogs               []CronLog
	fbLogsMu             sync.RWMutex
	aiLogs               []AILogEntry
	aiLogsMu             sync.RWMutex
	proxyScraperLogs     []CronLog
	proxyScraperLogsMu   sync.RWMutex
	decodedCache         map[string]string
	decodedCacheMu       sync.RWMutex
	cronMu               sync.Mutex
	cronRunning          bool
	fbMu                 sync.Mutex
	fbRunning            bool
	bskyLogs             []CronLog
	bskyLogsMu           sync.RWMutex
	bskyMu               sync.Mutex
	bskyRunning          bool
	customDomainLogs     []CronLog
	customDomainLogsMu   sync.RWMutex
	customDomainMu       sync.Mutex
	customDomainRunning  bool
	customHomepageTmpls  map[string]*template.Template
	customHomepageTmplMu sync.RWMutex
	migrationJob         MigrationJob
	migrationJobMu       sync.RWMutex
}

type homeViewData struct {
	Articles         []cms.Article
	Featured         cms.Article
	HasFeatured      bool
	HeroSide         []cms.Article
	Categories       []cms.Category
	CategorySections []homeCategorySection
	Popular          []cms.Article
	EditorPicks      []cms.Article
	Settings         map[string]string
	AppUser          *cms.AppUser
}

type homeCategorySection struct {
	Category    cms.Category
	Articles    []cms.Article
	Featured    cms.Article
	HasFeatured bool
	Rest        []cms.Article
}

type articleListItem struct {
	Article    cms.Article
	AuthorName string
}

type articleIndexViewData struct {
	Articles       []articleListItem
	Total          int
	Page           int
	PageSize       int
	TotalPages     int
	HasPrev        bool
	HasNext        bool
	PrevPage       int
	NextPage       int
	Pages          []int
	Categories     []cms.Category
	Popular        []cms.Article
	EditorPicks    []cms.Article
	CategoryFilter string
	SearchQuery    string
	Settings       map[string]string
	AppUser        *cms.AppUser
}

type articleViewData struct {
	Article        cms.Article
	AuthorName     string
	Related        []cms.Article
	Popular        []cms.Article
	EditorPicks    []cms.Article
	Categories     []cms.Category
	CategoryFilter string
	SearchQuery    string
	Settings       map[string]string
	BaseURL        string
	AppUser        *cms.AppUser
}

type loginViewData struct {
	Email        string
	Error        string
	Next         string
	LoggedOut    bool
	IsProduction bool
}

type dashboardViewData struct {
	User      *cms.User
	Recent    []cms.Article
	Total     int
	Draft     int
	Submitted int
	Published int
	Today     int
	Settings  map[string]string
}

type profileViewData struct {
	User    *cms.User
	Profile *cms.User
	Error   string
	Success string
}

func New(cfg config.Config, log *slog.Logger, templates *template.Template, store appcms.ContentStore) *http.Server {
	themes, err := web.ParseAllThemes()
	if err != nil {
		log.Error("failed to parse all themes", "error", err)
	}

	app := &Server{
		cfg:                 cfg,
		log:                 log,
		templates:           templates,
		themes:              themes,
		store:               store,
		decodedCache:        make(map[string]string),
		customHomepageTmpls: make(map[string]*template.Template),
	}
	app.loadCustomHomepageTemplate()
	app.StartCronBackgroundJob()
	app.StartProxyScraperJob()
	app.StartFacebookCronJob()
	app.StartBlueskyCronJob()
	app.StartCustomDomainsBackgroundJob()

	return &http.Server{
		Addr:         cfg.Addr,
		Handler:      app.routes(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.landingPage)
	mux.HandleFunc("GET /portal", s.home)
	mux.HandleFunc("GET /tentang", s.aboutPage)
	mux.HandleFunc("GET /kontak", s.contactPage)
	mux.HandleFunc("GET /privasi", s.privacyPage)
	mux.HandleFunc("GET /iklan", s.adsPage)
	mux.HandleFunc("GET /favicon.ico", s.serveFavicon)
	mux.HandleFunc("GET /artikel", s.articleIndex)
	mux.HandleFunc("GET /artikel/", s.articleLegacy)
	mux.HandleFunc("GET /arsip/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.articleByID(w, r, r.PathValue("id"))
	})
	// Keep legacy routes to handle redirects to canonical paths
	mux.HandleFunc("GET /artikel/{slug}", s.articleBySlug)
	mux.HandleFunc("GET /artikel/{year}/{month}/{day}/{slug}", s.articleBySlug)
	mux.HandleFunc("GET /artikel/{year}/{month}/{slug}", s.articleBySlug)
	mux.HandleFunc("GET /artikel/arsip/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.articleByID(w, r, r.PathValue("id"))
	})
	mux.HandleFunc("GET /rss/{category}", s.categoryRSS)
	mux.HandleFunc("GET /sitemap.xml", s.sitemapXML)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(web.Assets())))
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /app/login", s.appLoginForm)
	mux.HandleFunc("POST /app/login", s.appLogin)
	mux.HandleFunc("POST /app/logout", s.appLogout)
	mux.HandleFunc("GET /dashboard", s.requireAuth(s.dashboard))
	mux.HandleFunc("GET /dashboard/articles", s.requireAuth(s.dashboardArticles))
	mux.HandleFunc("GET /dashboard/premium", s.requireAuth(s.dashboardPremium))
	mux.HandleFunc("POST /dashboard/premium/{id}/toggle", s.requireAuth(s.togglePremiumStatus))
	mux.HandleFunc("GET /dashboard/options-permalink", s.requireAuth(s.dashboardPermalinkSettings))
	mux.HandleFunc("POST /dashboard/options-permalink", s.requireAuth(s.updatePermalinkSettings))
	mux.HandleFunc("GET /dashboard/migration", s.requireAuth(s.dashboardMigration))
	mux.HandleFunc("POST /dashboard/migration", s.requireAuth(s.runWPMigration))
	mux.HandleFunc("GET /dashboard/migration/status", s.requireAuth(s.dashboardMigrationStatus))
	mux.HandleFunc("POST /dashboard/migration/cancel", s.requireAuth(s.cancelWPMigration))
	mux.HandleFunc("GET /dashboard/categories", s.requireAuth(s.dashboardCategories))
	mux.HandleFunc("POST /dashboard/categories", s.requireAuth(s.createCategory))
	mux.HandleFunc("GET /dashboard/categories/navbar", s.requireAuth(s.dashboardCategoriesNavbar))
	mux.HandleFunc("POST /dashboard/categories/navbar", s.requireAuth(s.updateCategoriesNavbar))
	mux.HandleFunc("GET /dashboard/profile", s.requireAuth(s.profile))
	mux.HandleFunc("POST /dashboard/profile", s.requireAuth(s.updateProfile))
	mux.HandleFunc("POST /dashboard/profile/avatar", s.requireAuth(s.updateProfileAvatar))
	mux.HandleFunc("GET /dashboard/api-keys", s.requireAuth(s.dashboardAPIKeys))
	mux.HandleFunc("GET /dashboard/api-keys/docs", s.requireAuth(s.swaggerDocs))
	mux.HandleFunc("GET /dashboard/mail/inbox", s.requireAuth(s.dashboardMailInbox))
	mux.HandleFunc("GET /dashboard/mail/compose", s.requireAuth(s.dashboardMailCompose))
	mux.HandleFunc("POST /dashboard/mail/compose", s.requireAuth(s.sendMailHandler))

	mux.HandleFunc("GET /dashboard/mail/settings", s.requireAuth(s.dashboardMailSettings))
	mux.HandleFunc("POST /dashboard/mail/settings", s.requireAuth(s.updateMailSettingsHandler))
	mux.HandleFunc("POST /dashboard/mail/verify-dns", s.requireAuth(s.verifyDNSHandler))
	mux.HandleFunc("POST /dashboard/mail/cloudflare/deploy-worker", s.requireAuth(s.deployCloudflareWorkerHandler))
	mux.HandleFunc("GET /dashboard/mail/cloudflare/verify-token", s.requireAuth(s.verifyCloudflareTokenHandler))
	mux.HandleFunc("POST /dashboard/mail/ai/draft", s.requireAuth(s.mailAIDraftHandler))
	mux.HandleFunc("POST /dashboard/mail/ai/summary", s.requireAuth(s.mailAISummaryHandler))

	mux.HandleFunc("GET /dashboard/settings", s.requireAuth(s.dashboardSettings))
	mux.HandleFunc("POST /dashboard/settings", s.requireAuth(s.updateSettings))
	mux.HandleFunc("GET /dashboard/custom-domain", s.requireAuth(s.dashboardCustomDomain))
	mux.HandleFunc("POST /dashboard/custom-domain", s.requireAuth(s.updateCustomDomain))
	mux.HandleFunc("GET /dashboard/themes", s.requireAuth(s.dashboardThemes))
	mux.HandleFunc("POST /dashboard/themes", s.requireAuth(s.updateTheme))
	mux.HandleFunc("POST /dashboard/settings/test-models", s.requireAuth(s.testAIModels))
	mux.HandleFunc("GET /dashboard/settings/ai-logs", s.requireAuth(s.apiGetAILogs))
	mux.HandleFunc("POST /dashboard/settings/ai-logs/clear", s.requireAuth(s.apiClearAILogs))
	mux.HandleFunc("GET /dashboard/settings/backup/export", s.requireAuth(s.exportBackup))
	mux.HandleFunc("POST /dashboard/settings/custom-homepage/toggle", s.requireAuth(s.customHomepageToggle))
	mux.HandleFunc("GET /dashboard/settings/custom-homepage/template", s.requireAuth(s.customHomepageTemplate))
	mux.HandleFunc("POST /dashboard/settings/custom-homepage/upload", s.requireAuth(s.customHomepageUpload))
	mux.HandleFunc("POST /dashboard/themes/custom-homepage/toggle", s.requireAuth(s.customHomepageToggle))
	mux.HandleFunc("POST /dashboard/themes/custom-homepage/upload", s.requireAuth(s.customHomepageUpload))
	mux.HandleFunc("POST /dashboard/settings/backup/import", s.requireAuth(s.importBackup))
	mux.HandleFunc("POST /dashboard/api-keys", s.requireAuth(s.createAPIKey))
	mux.HandleFunc("POST /dashboard/api-keys/{id}/revoke", s.requireAuth(s.revokeAPIKey))
	mux.HandleFunc("POST /dashboard/api-keys/{id}/delete", s.requireAuth(s.deleteAPIKey))
	mux.HandleFunc("GET /dashboard/users", s.requireAuth(s.dashboardWriters))
	mux.HandleFunc("POST /dashboard/users", s.requireAuth(s.createWriter))
	mux.HandleFunc("POST /dashboard/users/{id}/delete", s.requireAuth(s.deleteWriter))
	mux.HandleFunc("GET /dashboard/app-users", s.requireAuth(s.dashboardAppUsers))
	mux.HandleFunc("POST /dashboard/app-users", s.requireAuth(s.createAppUser))
	mux.HandleFunc("POST /dashboard/app-users/{id}/delete", s.requireAuth(s.deleteAppUser))
	mux.HandleFunc("GET /dashboard/categories/new", s.requireAuth(s.newCategoryForm))
	mux.HandleFunc("GET /dashboard/categories/{id}/edit", s.requireAuth(s.editCategoryForm))
	mux.HandleFunc("POST /dashboard/categories/{id}", s.requireAuth(s.updateCategory))
	mux.HandleFunc("POST /dashboard/categories/{id}/delete", s.requireAuth(s.deleteCategory))
	mux.HandleFunc("GET /dashboard/articles/new", s.requireAuth(s.newArticleForm))
	mux.HandleFunc("GET /dashboard/articles/wizard", s.requireAuth(s.dashboardArticleWizard))
	mux.HandleFunc("POST /dashboard/articles/wizard/fetch", s.requireAuth(s.wizardFetch))
	mux.HandleFunc("POST /dashboard/articles/wizard/rewrite", s.requireAuth(s.wizardRewrite))
	mux.HandleFunc("GET /dashboard/prompts", s.requireAuth(s.dashboardPrompts))
	mux.HandleFunc("POST /dashboard/prompts", s.requireAuth(s.savePrompt))
	mux.HandleFunc("POST /dashboard/prompts/delete", s.requireAuth(s.deletePrompt))
	mux.HandleFunc("GET /dashboard/prompts/list", s.requireAuth(s.apiListPrompts))
	mux.HandleFunc("GET /dashboard/articles/wizard/models", s.requireAuth(s.wizardModels))
	mux.HandleFunc("POST /dashboard/articles", s.requireAuth(s.createArticle))
	mux.HandleFunc("GET /dashboard/articles/{id}/edit", s.requireAuth(s.editArticleForm))
	mux.HandleFunc("POST /dashboard/articles/{id}", s.requireAuth(s.updateArticle))
	mux.HandleFunc("POST /dashboard/articles/{id}/delete", s.requireAuth(s.deleteArticle))
	mux.HandleFunc("POST /dashboard/articles/delete-bulk", s.requireAuth(s.deleteArticlesBulk))
	mux.HandleFunc("POST /dashboard/articles/bulk-bing", s.requireAuth(s.bulkBingThumbnail))
	mux.HandleFunc("POST /dashboard/articles/bulk-bing-all", s.requireAuth(s.bulkBingThumbnailAllMissing))
	mux.HandleFunc("GET /dashboard/duplicates", s.requireAuth(s.dashboardDuplicates))
	mux.HandleFunc("GET /api/dashboard/stats/chart", s.requireAuth(s.apiDashboardChartStats))

	mux.HandleFunc("GET /dashboard/cron", s.requireAuth(s.dashboardCron))
	mux.HandleFunc("POST /dashboard/cron/settings", s.requireAuth(s.cronSaveSettings))
	mux.HandleFunc("POST /dashboard/cron/fetch", s.requireAuth(s.cronFetch))
	mux.HandleFunc("POST /dashboard/cron/import", s.requireAuth(s.cronManualImport))
	mux.HandleFunc("GET /dashboard/blacklist", s.requireAuth(s.dashboardBlacklist))
	mux.HandleFunc("POST /dashboard/blacklist", s.requireAuth(s.blacklistAdd))
	mux.HandleFunc("POST /dashboard/blacklist/delete", s.requireAuth(s.blacklistDelete))
	mux.HandleFunc("POST /dashboard/blacklist/clear", s.requireAuth(s.blacklistClear))
	mux.HandleFunc("GET /dashboard/cron/logs", s.requireAuth(s.cronGetLogs))
	mux.HandleFunc("POST /dashboard/cron/run", s.requireAuth(s.cronRunManual))
	mux.HandleFunc("POST /dashboard/cron/logs/clear", s.requireAuth(s.cronClearLogs))

	mux.HandleFunc("GET /dashboard/proxies", s.requireAuth(s.dashboardProxies))
	mux.HandleFunc("POST /dashboard/proxies", s.requireAuth(s.proxyCreate))
	mux.HandleFunc("POST /dashboard/proxies/import", s.requireAuth(s.proxyBatchImport))
	mux.HandleFunc("POST /dashboard/proxies/webshare-sync", s.requireAuth(s.proxyWebshareSync))
	mux.HandleFunc("POST /dashboard/proxies/webshare-keys", s.requireAuth(s.webshareKeyAdd))
	mux.HandleFunc("POST /dashboard/proxies/webshare-keys/{id}/delete", s.requireAuth(s.webshareKeyDelete))
	mux.HandleFunc("POST /dashboard/proxies/{id}/delete", s.requireAuth(s.proxyDelete))
	mux.HandleFunc("POST /dashboard/proxies/{id}/check", s.requireAuth(s.proxyCheck))
	mux.HandleFunc("POST /dashboard/proxies/check-all", s.requireAuth(s.proxyCheckAll))
	mux.HandleFunc("POST /dashboard/proxies/scrape-public", s.requireAuth(s.proxyScrapePublic))
	mux.HandleFunc("GET /dashboard/proxies/scraper", s.requireAuth(s.dashboardProxyScraper))
	mux.HandleFunc("POST /dashboard/proxies/tool/test", s.requireAuth(s.proxyToolTest))
	mux.HandleFunc("POST /dashboard/proxies/scraper/import", s.requireAuth(s.proxyScraperImport))
	mux.HandleFunc("POST /dashboard/proxies/scraper/settings", s.requireAuth(s.updateProxyScraperSettings))
	mux.HandleFunc("GET /dashboard/proxies/scraper/logs", s.requireAuth(s.proxyScraperGetLogs))
	mux.HandleFunc("POST /dashboard/proxies/scraper/logs/clear", s.requireAuth(s.proxyScraperClearLogs))

	mux.HandleFunc("GET /dashboard/custom-domains", s.requireAuth(s.dashboardCustomDomains))
	mux.HandleFunc("POST /dashboard/custom-domains/add", s.requireAuth(s.addCustomDomain))
	mux.HandleFunc("POST /dashboard/custom-domains/edit", s.requireAuth(s.editCustomDomain))
	mux.HandleFunc("POST /dashboard/custom-domains/delete", s.requireAuth(s.deleteCustomDomain))
	mux.HandleFunc("POST /dashboard/custom-domains/toggle", s.requireAuth(s.toggleCustomDomain))
	mux.HandleFunc("POST /dashboard/custom-domains/settings", s.requireAuth(s.customDomainsSaveSettings))
	mux.HandleFunc("GET /dashboard/custom-domains/logs", s.requireAuth(s.customDomainsGetLogs))
	mux.HandleFunc("POST /dashboard/custom-domains/run", s.requireAuth(s.customDomainsRunManual))
	mux.HandleFunc("POST /dashboard/custom-domains/logs/clear", s.requireAuth(s.customDomainsClearLogs))


	mux.HandleFunc("GET /dashboard/facebook", s.requireAuth(s.dashboardFacebook))
	mux.HandleFunc("POST /dashboard/facebook/settings", s.requireAuth(s.facebookSaveSettings))
	mux.HandleFunc("POST /dashboard/facebook/detect-pages", s.requireAuth(s.facebookDetectPages))
	mux.HandleFunc("GET /dashboard/facebook/logs", s.requireAuth(s.facebookGetLogs))
	mux.HandleFunc("POST /dashboard/facebook/run", s.requireAuth(s.facebookRunManual))
	mux.HandleFunc("POST /dashboard/facebook/logs/clear", s.requireAuth(s.facebookClearLogs))

	mux.HandleFunc("GET /dashboard/bluesky", s.requireAuth(s.dashboardBluesky))
	mux.HandleFunc("POST /dashboard/bluesky/settings", s.requireAuth(s.blueskySaveSettings))
	mux.HandleFunc("GET /dashboard/bluesky/logs", s.requireAuth(s.blueskyGetLogs))
	mux.HandleFunc("POST /dashboard/bluesky/run", s.requireAuth(s.blueskyRunManual))
	mux.HandleFunc("POST /dashboard/bluesky/logs/clear", s.requireAuth(s.blueskyClearLogs))

	mux.HandleFunc("POST /dashboard/articles/{id}/submit", s.requireAuth(s.submitArticle))
	mux.HandleFunc("POST /dashboard/articles/{id}/approve", s.requireAuth(s.approveArticle))
	mux.HandleFunc("POST /dashboard/articles/{id}/request-revision", s.requireAuth(s.requestRevision))
	mux.HandleFunc("POST /dashboard/articles/{id}/archive", s.requireAuth(s.archiveArticle))
	mux.HandleFunc("POST /dashboard/media/upload", s.requireAuth(s.uploadMedia))
	mux.HandleFunc("GET /api/v1/articles", s.requireAPIKey(s.apiListArticles))
	mux.HandleFunc("POST /api/v1/articles", s.requireAPIKey(s.apiCreateArticle))
	mux.HandleFunc("POST /api/v1/media/upload", s.requireAPIKey(s.apiUploadMedia))
	mux.HandleFunc("POST /api/v1/media/url", s.requireAPIKey(s.apiCreateMediaURL))
	mux.HandleFunc("POST /api/v1/auth/login", s.apiAppLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.apiAppLogout)
	mux.HandleFunc("GET /api/v1/bookmarks", s.apiGetBookmarks)
	mux.HandleFunc("POST /api/v1/bookmarks", s.apiAddBookmark)
	mux.HandleFunc("DELETE /api/v1/bookmarks/{id}", s.apiDeleteBookmark)
	mux.HandleFunc("GET /api/v1/proxy-image", s.apiProxyImage)
	mux.HandleFunc("POST /api/v1/mail/inbound", s.apiInboundMailWebhook)

	mux.HandleFunc("GET /cdn/image/{id}", s.seoProxyImage)
	mux.HandleFunc("GET /api/v1/info", s.apiGetSiteInfo)
	mux.HandleFunc("GET /docs", s.swaggerDocs)
	mux.HandleFunc("GET /docs/openapi.json", s.openAPISpec)
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.cfg.UploadDir))))
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)

	pm := &permalinkMiddleware{server: s}
	var handler http.Handler = mux
	handler = pm.RewriteHTML(handler)
	handler = s.autoDetectHost(handler)
	handler = securityHeaders(handler)
	handler = recoverPanic(s.log, handler)
	handler = accessLog(s.log, handler)
	handler = cors(handler)
	return handler
}

func (s *Server) autoDetectHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/uploads/") {
			next.ServeHTTP(w, r)
			return
		}

		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}

		detectedHost := r.Host
		if detectedHost != "" {
			detectedURL := scheme + "://" + detectedHost

			go func(url string) {
				time.Sleep(1 * time.Second)
				
				settings := s.store.GetSettings()
				currentURL := settings["site_url"]

				needsUpdate := false
				if currentURL == "" {
					needsUpdate = true
				} else {
					isLocalCurrent := strings.Contains(currentURL, "localhost") || strings.Contains(currentURL, "127.0.0.1")
					if isLocalCurrent && currentURL != url {
						needsUpdate = true
					}
				}

				if needsUpdate {
					systemUser, err := s.store.GetSystemUser()
					if err == nil && systemUser != nil {
						settings["site_url"] = url
						err = s.store.UpdateSettings(systemUser, settings)
						if err != nil {
							s.log.Error("failed to auto-update site_url", "error", err)
						} else {
							s.log.Info("auto-detected and updated site_url", "old", currentURL, "new", url)
						}
					}
				}
			}(detectedURL)
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "environment": s.cfg.Environment, "time": time.Now().UTC().Format(time.RFC3339)})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) renderHTML(w http.ResponseWriter, name string) {
	s.renderTemplate(w, name, nil)
}

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 1. Dapatkan nama tema aktif dari database
	activeTheme := "default"
	if s.store != nil {
		settings := s.store.GetSettings()
		if theme, ok := settings["active_theme"]; ok && theme != "" {
			activeTheme = theme
		}
	}

	// 2. Ambil pool template untuk tema aktif
	tmplPool := s.templates
	if pool, ok := s.themes[activeTheme]; ok {
		tmplPool = pool
	}

	if err := tmplPool.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("render template", "template", name, "error", err, "theme", activeTheme)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (s *Server) renderPage(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="id"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>body{font-family:Arial,sans-serif;margin:0;color:#111;background:#f7f7f7}.top{background:#111;color:#fff;padding:14px 20px}.top a{color:#fff;margin-right:12px}.wrap{max-width:1100px;margin:24px auto;padding:0 16px}.panel,.article,.card{background:#fff;padding:22px;margin-bottom:16px;border:1px solid #e5e5e5}.btn,button{background:#e63329;color:#fff;border:0;padding:9px 14px;text-decoration:none;cursor:pointer}input,textarea,select{width:100%%;padding:10px;margin:6px 0 14px;border:1px solid #ddd}textarea{min-height:180px}table{width:100%%;border-collapse:collapse;background:#fff}th,td{border:1px solid #ddd;padding:9px;text-align:left}.inline{display:inline}.error{background:#ffe9e9;color:#9b1c1c;padding:10px}.tag{color:#e63329;text-transform:uppercase;font-weight:bold}.dek{font-size:20px;color:#555}.hero{height:280px;background:linear-gradient(135deg,#37474f,#e63329);margin:20px 0}.hero img{width:100%%;height:100%%;object-fit:cover}.content p{font-size:18px;line-height:1.8}</style></head><body><div class="top"><a href="/">Home</a><a href="/artikel">Artikel</a><a href="/dashboard">Dashboard</a></div><main class="wrap">%s</main></body></html>`, esc(title), body)
}

func esc(value string) string {
	return template.HTMLEscapeString(value)
}

func statusFromError(err error) int {
	switch {
	case errors.Is(err, cms.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, cms.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, cms.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, cms.ErrValidation), errors.Is(err, cms.ErrConflict):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func safeNextPath(value string) string {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.HasPrefix(value, "/login") {
		return "/dashboard"
	}
	return value
}

func (s *Server) loadCustomHomepageTemplate() {
	s.customHomepageTmplMu.Lock()
	defer s.customHomepageTmplMu.Unlock()

	s.customHomepageTmpls = make(map[string]*template.Template)

	s.initializeDefaultCustomHomepage()

	baseDir := filepath.Join(s.cfg.UploadDir, "custom_homepage")
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return
	}

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".html") {
			rel, err := filepath.Rel(baseDir, path)
			if err == nil {
				// Normalize key to slash separators
				key := filepath.ToSlash(rel)
				htmlContent, err := os.ReadFile(path)
				if err == nil {
					tmpl, err := web.ParseCustomTemplate("custom_"+key, string(htmlContent))
					if err == nil {
						s.customHomepageTmpls[key] = tmpl
						s.log.Info("custom homepage template compiled successfully", "path", key)
					} else {
						s.log.Error("failed to compile custom homepage template", "path", key, "error", err)
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		s.log.Error("failed to walk custom homepage directory", "error", err)
	}
}

