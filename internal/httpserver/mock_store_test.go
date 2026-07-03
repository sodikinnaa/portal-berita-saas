package httpserver

import (
	"context"
	"time"

	core "porta-berita/internal/cms"
)

type mockStore struct {
	articles   []core.Article
	categories []core.Category
	settings   map[string]string
	blacklist  []string
	emails     []core.Email
	users      map[string]*core.User
}

func (m *mockStore) Authenticate(email, password string) (*core.User, error) { return nil, nil }
func (m *mockStore) CreateSession(userID string, ttl time.Duration) (core.Session, error) {
	return core.Session{}, nil
}
func (m *mockStore) UserBySession(sessionID string) (*core.User, error) { return nil, nil }
func (m *mockStore) DeleteSession(sessionID string) error               { return nil }

func (m *mockStore) ListArticles(user *core.User) []core.Article { return m.articles }
func (m *mockStore) ListPublishedArticles(limit int) []core.Article {
	if limit > 0 && len(m.articles) > limit {
		return m.articles[:limit]
	}
	return m.articles
}
func (m *mockStore) ListRandomPublishedArticles(limit int, excludeID string) []core.Article {
	var filtered []core.Article
	for _, a := range m.articles {
		if a.ID != excludeID {
			filtered = append(filtered, a)
		}
	}
	if limit > 0 && len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}
func (m *mockStore) CountPublishedArticles() int { return len(m.articles) }
func (m *mockStore) ListPublishedArticlesPaginated(offset, limit int) []core.Article {
	if offset >= len(m.articles) {
		return nil
	}
	end := offset + limit
	if end > len(m.articles) {
		end = len(m.articles)
	}
	return m.articles[offset:end]
}
func (m *mockStore) ListPublishedArticlesFiltered(category, query string, offset, limit int) []core.Article {
	var filtered []core.Article
	for _, a := range m.articles {
		if category != "" && a.Category != category {
			continue
		}
		if query != "" && a.Title != query { // Simplification
			continue
		}
		filtered = append(filtered, a)
	}

	if offset >= len(filtered) {
		return nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end]
}
func (m *mockStore) CountPublishedArticlesFiltered(category, query string) int {
	var count int
	for _, a := range m.articles {
		if category != "" && a.Category != category {
			continue
		}
		if query != "" && a.Title != query { // Simplification
			continue
		}
		count++
	}
	return count
}
func (m *mockStore) ArticleBySlug(slug string, includeUnpublished bool) (*core.Article, error) {
	for _, a := range m.articles {
		if a.Slug == slug {
			return &a, nil
		}
	}
	return nil, nil
}
func (m *mockStore) ArticleByID(id string) (*core.Article, error) { return nil, nil }
func (m *mockStore) UserName(userID string) string                { return "" }

func (m *mockStore) ListCategories() []core.Category                { return m.categories }
func (m *mockStore) CategoryByID(id string) (*core.Category, error) { return nil, nil }
func (m *mockStore) CreateCategory(user *core.User, input core.CategoryInput) (*core.Category, error) {
	return nil, nil
}
func (m *mockStore) UpdateCategory(user *core.User, id string, input core.CategoryInput) (*core.Category, error) {
	return nil, nil
}
func (m *mockStore) DeleteCategory(user *core.User, id string) error { return nil }

func (m *mockStore) CreateArticle(user *core.User, input core.ArticleInput) (*core.Article, error) {
	art := &core.Article{
		ID:           "art-" + input.Title,
		AuthorID:     user.ID,
		Title:        input.Title,
		Content:      input.Content,
		Excerpt:      input.Excerpt,
		Category:     input.Category,
		HeroImageURL: input.HeroImageURL,
		Status:       input.Status,
		SourceURL:    input.SourceURL,
		ImageSource:  input.ImageSource,
	}
	m.articles = append(m.articles, *art)
	return art, nil
}
func (m *mockStore) UpdateArticle(user *core.User, id string, input core.ArticleInput) (*core.Article, error) {
	return nil, nil
}
func (m *mockStore) DeleteArticle(user *core.User, id string) error                  { return nil }
func (m *mockStore) SubmitArticle(user *core.User, id string) (*core.Article, error) { return nil, nil }
func (m *mockStore) ApproveArticle(user *core.User, id string) (*core.Article, error) {
	return nil, nil
}
func (m *mockStore) RequestRevision(user *core.User, id, note string) (*core.Article, error) {
	return nil, nil
}
func (m *mockStore) ArchiveArticle(user *core.User, id string) (*core.Article, error) {
	return nil, nil
}

func (m *mockStore) CreateMedia(user *core.User, filename, originalName, mimeType, url string, size int64) (core.Media, error) {
	return core.Media{}, nil
}
func (m *mockStore) CreateMediaFromAPI(principal core.APIPrincipal, filename, originalName, mimeType, url, source string, size int64) (core.Media, error) {
	return core.Media{}, nil
}
func (m *mockStore) CreateExternalMediaURL(principal core.APIPrincipal, input core.ExternalMediaInput) (core.Media, error) {
	return core.Media{}, nil
}

func (m *mockStore) ListAPIKeys(user *core.User) []core.APIKey { return nil }
func (m *mockStore) CreateAPIKey(user *core.User, input core.APIKeyInput) (*core.APIKeyWithSecret, error) {
	return nil, nil
}
func (m *mockStore) RevokeAPIKey(user *core.User, id string) error                { return nil }
func (m *mockStore) DeleteAPIKey(user *core.User, id string) error                { return nil }
func (m *mockStore) AuthenticateAPIKey(secret string) (*core.APIPrincipal, error) { return nil, nil }

func (m *mockStore) UpdateProfile(user *core.User, input core.ProfileInput) (*core.User, error) {
	return nil, nil
}
func (m *mockStore) ListWriters(user *core.User) []core.User { return nil }
func (m *mockStore) CreateWriter(user *core.User, input core.WriterInput) (*core.User, error) {
	return nil, nil
}
func (m *mockStore) DeleteWriter(user *core.User, id string) error { return nil }
func (m *mockStore) ListAppUsers(user *core.User) []core.AppUser { return nil }
func (m *mockStore) CreateAppUser(user *core.User, input core.AppUserInput) (*core.AppUser, error) {
	return nil, nil
}
func (m *mockStore) DeleteAppUser(user *core.User, id string) error { return nil }
func (m *mockStore) CreateArticleFromAPI(principal core.APIPrincipal, input core.ArticleInput) (*core.Article, error) {
	return nil, nil
}

func (m *mockStore) ListArticlesFromAPI(principal core.APIPrincipal) ([]core.Article, error) {
	hasScope := false
	for _, sc := range principal.Key.Scopes {
		if sc == "articles:read" {
			hasScope = true
			break
		}
	}
	if !hasScope {
		return nil, core.ErrForbidden
	}
	return m.articles, nil
}

func (m *mockStore) GetSettings() map[string]string {
	if m.settings != nil {
		return m.settings
	}
	return map[string]string{
		"site_title":             "NewsPaper",
		"site_tagline":           "Berita Terkini & Terpercaya",
		"site_description":       "NewsPaper adalah portal berita terpercaya yang menyajikan informasi terkini, mendalam, dan berimbang dari seluruh penjuru dunia. Kami berkomitmen pada jurnalisme berkualitas tinggi sejak 2005.",
		"social_facebook_url":    "#",
		"social_facebook_count":  "24.8K",
		"social_twitter_url":     "#",
		"social_twitter_count":   "18.1K",
		"social_youtube_url":     "#",
		"social_youtube_count":   "103K",
		"social_instagram_url":   "#",
		"social_instagram_count": "56.4K",
	}
}

func (m *mockStore) UpdateSettings(user *core.User, settings map[string]string) error {
	m.settings = settings
	return nil
}

func (m *mockStore) ArticleExistsBySourceURL(sourceURL string) (bool, error) {
	for _, art := range m.articles {
		if art.SourceURL == sourceURL {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockStore) ArticleExistsByTitleOrSlug(title, slug string) (bool, error) {
	for _, art := range m.articles {
		if art.Title == title || art.Slug == slug {
			return true, nil
		}
	}
	return false, nil
}


func (m *mockStore) ArticleSlugBySourceURL(sourceURL string) (string, error) {
	for _, art := range m.articles {
		if art.SourceURL == sourceURL {
			return art.Slug, nil
		}
	}
	return "", nil
}

func (m *mockStore) GetSystemUser() (*core.User, error) {
	return &core.User{ID: "user-admin", Role: "admin", Name: "Admin Portal", Email: "admin@portal.test", Status: "active"}, nil
}

func (m *mockStore) IsDomainBlacklisted(domain string) (bool, error) {
	for _, d := range m.blacklist {
		if d == domain {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockStore) AddDomainToBlacklist(domain string) error {
	for _, d := range m.blacklist {
		if d == domain {
			return nil
		}
	}
	m.blacklist = append(m.blacklist, domain)
	return nil
}

func (m *mockStore) RemoveDomainFromBlacklist(domain string) error {
	var newList []string
	for _, d := range m.blacklist {
		if d != domain {
			newList = append(newList, d)
		}
	}
	m.blacklist = newList
	return nil
}

func (m *mockStore) ListBlacklistedDomains() ([]string, error) {
	if m.blacklist == nil {
		return []string{}, nil
	}
	return m.blacklist, nil
}

func (m *mockStore) ExportBackup() ([]byte, error) {
	return nil, nil
}

func (m *mockStore) ImportBackup(backupData []byte) error {
	return nil
}

func (m *mockStore) IsArticlePostedToFB(articleID string) (bool, error) {
	return false, nil
}

func (m *mockStore) MarkArticleAsPostedToFB(articleID string, fbPostID string) error {
	return nil
}

func (m *mockStore) IsArticlePostedToBSky(articleID string) (bool, error) {
	return false, nil
}

func (m *mockStore) MarkArticleAsPostedToBSky(articleID string, bskyPostURI string) error {
	return nil
}

func (m *mockStore) LockArticleForBSky(articleID string) (bool, error) {
	return true, nil
}

func (m *mockStore) UnmarkArticleAsPostedToBSky(articleID string) error {
	return nil
}

func (m *mockStore) ListProxies() []core.Proxy {
	return nil
}

func (m *mockStore) CreateProxy(user *core.User, input core.ProxyInput) (*core.Proxy, error) {
	return nil, nil
}

func (m *mockStore) DeleteProxy(user *core.User, id string) error {
	return nil
}

func (m *mockStore) UpdateProxyStatus(id string, status string, latency int) error {
	return nil
}

func (m *mockStore) UpdateProxyLastUsed(id string, lastUsed time.Time) error {
	return nil
}

func (m *mockStore) AddProxyBandwidth(id string, sent int64, received int64) error {
	return nil
}

func (m *mockStore) GetProxyByID(id string) (*core.Proxy, error) {
	return nil, core.ErrNotFound
}

func (m *mockStore) ListActiveProxies() []core.Proxy {
	return nil
}

func (m *mockStore) ListWebshareKeys() ([]core.WebshareKey, error) {
	return nil, nil
}

func (m *mockStore) AddWebshareKey(user *core.User, apiKey, label string) (*core.WebshareKey, error) {
	return nil, nil
}

func (m *mockStore) DeleteWebshareKey(user *core.User, id string) error {
	return nil
}

func (m *mockStore) UpdateWebshareKeyBandwidth(id string, bytesUsed int64) error {
	return nil
}

func (m *mockStore) GetArticleChartStats(ctx context.Context, user *core.User, filter string) ([]core.ChartDataPoint, error) {
	return nil, nil
}

func (m *mockStore) AppUserBySession(sessionID string) (*core.AppUser, error) {
	return nil, nil
}

func (m *mockStore) CreateAppSession(userID string, ttl time.Duration) (core.Session, error) {
	return core.Session{}, nil
}

func (m *mockStore) DeleteAppSession(sessionID string) error {
	return nil
}

func (m *mockStore) AuthenticateAppUser(email, password string) (*core.AppUser, error) {
	return nil, nil
}

func (m *mockStore) GetAppBookmarks(userID string) ([]core.Article, error) {
	return nil, nil
}

func (m *mockStore) AddAppBookmark(userID string, articleID string) error {
	return nil
}

func (m *mockStore) DeleteAppBookmark(userID string, articleID string) error {
	return nil
}

func (m *mockStore) ToggleArticlePremium(id string) error {
	return nil
}

func (m *mockStore) ClearBlacklistedDomains() error {
	return nil
}

func (m *mockStore) GetUserByEmail(ctx context.Context, email string) (*core.User, error) {
	if m.users != nil {
		if u, ok := m.users[email]; ok {
			return u, nil
		}
	}
	return nil, nil
}

func (m *mockStore) InsertEmail(ctx context.Context, email *core.Email) error {
	m.emails = append(m.emails, *email)
	return nil
}

func (m *mockStore) GetEmailByID(ctx context.Context, id string) (*core.Email, error) {
	return nil, nil
}

func (m *mockStore) ListEmails(ctx context.Context, userID string, direction string, limit, offset int) ([]core.Email, error) {
	return nil, nil
}

func (m *mockStore) MarkEmailAsRead(ctx context.Context, id string) error {
	return nil
}





