package cms

import (
	"context"
	"time"

	core "porta-berita/internal/cms"
)

// ContentStore is the storage contract used by the HTTP layer.
// The runtime application uses the PostgreSQL implementation (*PostgresStore).
type ContentStore interface {
	Authenticate(email, password string) (*core.User, error)
	CreateSession(userID string, ttl time.Duration) (core.Session, error)
	UserBySession(sessionID string) (*core.User, error)
	DeleteSession(sessionID string) error

	ListArticles(user *core.User) []core.Article
	ListPublishedArticles(limit int) []core.Article
	ListRandomPublishedArticles(limit int, excludeID string) []core.Article
	CountPublishedArticles() int
	ListPublishedArticlesPaginated(offset, limit int) []core.Article
	ListPublishedArticlesFiltered(category, query string, offset, limit int) []core.Article
	CountPublishedArticlesFiltered(category, query string) int
	ArticleBySlug(slug string, includeUnpublished bool) (*core.Article, error)
	ArticleByID(id string) (*core.Article, error)
	UserName(userID string) string

	ListCategories() []core.Category
	CategoryByID(id string) (*core.Category, error)
	CreateCategory(user *core.User, input core.CategoryInput) (*core.Category, error)
	UpdateCategory(user *core.User, id string, input core.CategoryInput) (*core.Category, error)
	DeleteCategory(user *core.User, id string) error

	CreateArticle(user *core.User, input core.ArticleInput) (*core.Article, error)
	UpdateArticle(user *core.User, id string, input core.ArticleInput) (*core.Article, error)
	DeleteArticle(user *core.User, id string) error
	SubmitArticle(user *core.User, id string) (*core.Article, error)
	ApproveArticle(user *core.User, id string) (*core.Article, error)
	RequestRevision(user *core.User, id, note string) (*core.Article, error)
	ArchiveArticle(user *core.User, id string) (*core.Article, error)

	CreateMedia(user *core.User, filename, originalName, mimeType, url string, size int64) (core.Media, error)
	CreateMediaFromAPI(principal core.APIPrincipal, filename, originalName, mimeType, url, source string, size int64) (core.Media, error)
	CreateExternalMediaURL(principal core.APIPrincipal, input core.ExternalMediaInput) (core.Media, error)

	ListAPIKeys(user *core.User) []core.APIKey
	CreateAPIKey(user *core.User, input core.APIKeyInput) (*core.APIKeyWithSecret, error)
	RevokeAPIKey(user *core.User, id string) error
	DeleteAPIKey(user *core.User, id string) error
	AuthenticateAPIKey(secret string) (*core.APIPrincipal, error)

	UpdateProfile(user *core.User, input core.ProfileInput) (*core.User, error)
	ListWriters(user *core.User) []core.User
	CreateWriter(user *core.User, input core.WriterInput) (*core.User, error)
	DeleteWriter(user *core.User, id string) error
	ListAppUsers(user *core.User) []core.AppUser
	CreateAppUser(user *core.User, input core.AppUserInput) (*core.AppUser, error)
	DeleteAppUser(user *core.User, id string) error
	AuthenticateAppUser(email, password string) (*core.AppUser, error)
	CreateAppSession(userID string, ttl time.Duration) (core.Session, error)
	AppUserBySession(sessionID string) (*core.AppUser, error)
	DeleteAppSession(sessionID string) error
	GetAppBookmarks(userID string) ([]core.Article, error)
	AddAppBookmark(userID string, articleID string) error
	DeleteAppBookmark(userID string, articleID string) error
	CreateArticleFromAPI(principal core.APIPrincipal, input core.ArticleInput) (*core.Article, error)
	ListArticlesFromAPI(principal core.APIPrincipal) ([]core.Article, error)
	ToggleArticlePremium(id string) error

	GetSettings() map[string]string
	UpdateSettings(user *core.User, settings map[string]string) error
	ArticleExistsBySourceURL(sourceURL string) (bool, error)
	ArticleExistsByTitleOrSlug(title, slug string) (bool, error)
	ArticleSlugBySourceURL(sourceURL string) (string, error)
	GetSystemUser() (*core.User, error)
	IsDomainBlacklisted(domain string) (bool, error)
	AddDomainToBlacklist(domain string) error
	RemoveDomainFromBlacklist(domain string) error
	ClearBlacklistedDomains() error
	ListBlacklistedDomains() ([]string, error)
	IsArticlePostedToFB(articleID string) (bool, error)
	MarkArticleAsPostedToFB(articleID string, fbPostID string) error
	IsArticlePostedToBSky(articleID string) (bool, error)
	MarkArticleAsPostedToBSky(articleID string, bskyPostURI string) error
	LockArticleForBSky(articleID string) (bool, error)
	UnmarkArticleAsPostedToBSky(articleID string) error
	ExportBackup() ([]byte, error)
	ImportBackup(backupData []byte) error

	ListProxies() []core.Proxy
	CreateProxy(user *core.User, input core.ProxyInput) (*core.Proxy, error)
	DeleteProxy(user *core.User, id string) error
	UpdateProxyStatus(id string, status string, latency int) error
	UpdateProxyLastUsed(id string, lastUsed time.Time) error
	AddProxyBandwidth(id string, sent int64, received int64) error
	GetProxyByID(id string) (*core.Proxy, error)
	ListActiveProxies() []core.Proxy

	ListWebshareKeys() ([]core.WebshareKey, error)
	AddWebshareKey(user *core.User, apiKey, label string) (*core.WebshareKey, error)
	DeleteWebshareKey(user *core.User, id string) error
	UpdateWebshareKeyBandwidth(id string, bytesUsed int64) error

	// Dashboard Stats
	GetArticleChartStats(ctx context.Context, user *core.User, filter string) ([]core.ChartDataPoint, error)

	// Custom Email Services
	GetUserByEmail(ctx context.Context, email string) (*core.User, error)
	InsertEmail(ctx context.Context, email *core.Email) error
	GetEmailByID(ctx context.Context, id string) (*core.Email, error)
	ListEmails(ctx context.Context, userID string, direction string, limit, offset int) ([]core.Email, error)
	MarkEmailAsRead(ctx context.Context, id string) error
}


