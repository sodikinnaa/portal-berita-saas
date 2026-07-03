package jsonstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	appcms "porta-berita/internal/application/cms"
	"porta-berita/internal/cms"

	"golang.org/x/crypto/bcrypt"
)

type User = cms.User
type Article = cms.Article
type Category = cms.Category
type CategoryInput = cms.CategoryInput
type Media = cms.Media
type Session = cms.Session
type ArticleInput = cms.ArticleInput
type APIKey = cms.APIKey
type APIKeyInput = cms.APIKeyInput
type APIKeyWithSecret = cms.APIKeyWithSecret
type APIPrincipal = cms.APIPrincipal
type ProfileInput = cms.ProfileInput
type WriterInput = cms.WriterInput
type ExternalMediaInput = cms.ExternalMediaInput
type AppUser = cms.AppUser
type AppUserInput = cms.AppUserInput

var (
	ErrNotFound     = cms.ErrNotFound
	ErrUnauthorized = cms.ErrUnauthorized
	ErrForbidden    = cms.ErrForbidden
	ErrValidation   = cms.ErrValidation
	ErrConflict     = cms.ErrConflict
)

const (
	RoleAdmin  = cms.RoleAdmin
	RoleEditor = cms.RoleEditor
	RoleWriter = cms.RoleWriter

	StatusActive   = cms.StatusActive
	StatusInactive = cms.StatusInactive

	ArticleDraft         = cms.ArticleDraft
	ArticleSubmitted     = cms.ArticleSubmitted
	ArticleNeedsRevision = cms.ArticleNeedsRevision
	ArticlePublished     = cms.ArticlePublished
	ArticleArchived      = cms.ArticleArchived
)

type Store struct {
	mu   sync.RWMutex
	path string
	data dbData
}

type dbData struct {
	Users      []User     `json:"users"`
	Articles   []Article  `json:"articles"`
	Categories []Category `json:"categories"`
	Media      []Media    `json:"media"`
	Sessions   []Session  `json:"sessions"`
	Blacklist  []string   `json:"blacklist"`
}

func OpenStore(path string) (*Store, error) {
	store := &Store{path: path}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := store.seed(); err != nil {
			return nil, err
		}
		if err := store.saveLocked(); err != nil {
			return nil, err
		}
		return store, nil
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		if err := store.seed(); err != nil {
			return nil, err
		}
		return store, store.saveLocked()
	}
	if err := json.Unmarshal(body, &store.data); err != nil {
		return nil, err
	}
	if store.ensureDefaultCategoriesLocked() {
		if err := store.saveLocked(); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *Store) seed() error {
	now := time.Now().UTC()
	adminHash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	writerHash, err := bcrypt.GenerateFromPassword([]byte("writer123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	published := now.Add(-24 * time.Hour)
	s.data = dbData{
		Users: []User{
			{ID: "user-admin", Name: "Admin Portal", Email: "admin@portal.test", PasswordHash: string(adminHash), Role: RoleAdmin, Status: StatusActive, CreatedAt: now, UpdatedAt: now},
			{ID: "user-writer", Name: "Maria Sari", Email: "writer@portal.test", PasswordHash: string(writerHash), Role: RoleWriter, Status: StatusActive, CreatedAt: now, UpdatedAt: now},
		},
		Categories: defaultCategories(now),
		Articles: []Article{
			{
				ID:           "article-seed",
				AuthorID:     "user-writer",
				Title:        "WordPress News Magazine Meraih Penghargaan Majalah Paling Bergaya di New York Tahun 2026",
				Slug:         "wordpress-news-magazine-meraih-penghargaan",
				Excerpt:      "Ajang tahunan industri media digital kembali menyorot transformasi desain, pengalaman membaca, dan strategi editorial.",
				Content:      defaultArticleContent(),
				Category:     "Teknologi",
				HeroImageURL: "",
				Status:       ArticlePublished,
				PublishedAt:  &published,
				CreatedAt:    published,
				UpdatedAt:    now,
			},
		},
	}
	return nil
}

func defaultArticleContent() string {
	return strings.Join([]string{
		"NewsPaper edisi digital berhasil meraih penghargaan sebagai majalah berita paling bergaya dalam ajang New York Digital Publishing Awards 2026.",
		"Panel juri menyebut pendekatan desain NewsPaper sebagai contoh bagaimana portal berita modern dapat tetap terasa premium tanpa mengorbankan performa.",
		"Dalam beberapa tahun terakhir, konsumsi berita semakin cepat dan mobile-first. Karena itu, halaman artikel tidak lagi cukup hanya menampilkan teks panjang.",
		"Tim redaksi NewsPaper menjelaskan bahwa proses redesign dimulai dari riset perilaku pembaca dan pola klik pada artikel populer.",
	}, "\n\n")
}

func defaultCategories(now time.Time) []Category {
	return []Category{
		{ID: "cat-teknologi", Name: "Teknologi", Slug: "teknologi", CreatedAt: now, UpdatedAt: now},
		{ID: "cat-politik", Name: "Politik", Slug: "politik", CreatedAt: now, UpdatedAt: now},
		{ID: "cat-olahraga", Name: "Olahraga", Slug: "olahraga", CreatedAt: now, UpdatedAt: now},
		{ID: "cat-bisnis", Name: "Bisnis", Slug: "bisnis", CreatedAt: now, UpdatedAt: now},
		{ID: "cat-hiburan", Name: "Hiburan", Slug: "hiburan", CreatedAt: now, UpdatedAt: now},
	}
}

func (s *Store) ensureDefaultCategoriesLocked() bool {
	if len(s.data.Categories) > 0 {
		return false
	}
	s.data.Categories = defaultCategories(time.Now().UTC())
	return true
}

func (s *Store) saveLocked() error {
	body, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, body, 0o644)
}

func (s *Store) Authenticate(email, password string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, user := range s.data.Users {
		if strings.EqualFold(user.Email, strings.TrimSpace(email)) && user.Status == StatusActive {
			if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
				return nil, ErrUnauthorized
			}
			copy := user
			return &copy, nil
		}
	}
	return nil, ErrUnauthorized
}

func (s *Store) CreateSession(userID string, ttl time.Duration) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	session := Session{ID: randomID("sess"), UserID: userID, ExpiresAt: now.Add(ttl), CreatedAt: now}
	s.data.Sessions = append(s.data.Sessions, session)
	return session, s.saveLocked()
}

func (s *Store) UserBySession(sessionID string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now().UTC()
	for _, session := range s.data.Sessions {
		if session.ID == sessionID && session.ExpiresAt.After(now) {
			return s.userByIDLocked(session.UserID)
		}
	}
	return nil, ErrUnauthorized
}

func (s *Store) DeleteSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, session := range s.data.Sessions {
		if session.ID == sessionID {
			s.data.Sessions = append(s.data.Sessions[:i], s.data.Sessions[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *Store) userByIDLocked(id string) (*User, error) {
	for _, user := range s.data.Users {
		if user.ID == id {
			copy := user
			return &copy, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) ListArticles(user *User) []Article {
	s.mu.RLock()
	defer s.mu.RUnlock()

	articles := make([]Article, 0, len(s.data.Articles))
	for _, article := range s.data.Articles {
		if user.Role == RoleAdmin || user.Role == RoleEditor || article.AuthorID == user.ID {
			articles = append(articles, article)
		}
	}
	sort.Slice(articles, func(i, j int) bool { return articles[i].UpdatedAt.After(articles[j].UpdatedAt) })
	return articles
}

func (s *Store) ListRandomPublishedArticles(limit int, excludeID string) []Article {
	s.mu.RLock()
	defer s.mu.RUnlock()

	articles := make([]Article, 0)
	for _, article := range s.data.Articles {
		if article.Status == ArticlePublished && article.ID != excludeID && !article.IsPremium {
			articles = append(articles, article)
		}
	}
	mathrand.Shuffle(len(articles), func(i, j int) { articles[i], articles[j] = articles[j], articles[i] })
	if len(articles) > limit {
		return articles[:limit]
	}
	return articles
}

func (s *Store) ListPublishedArticles(limit int) []Article {
	s.mu.RLock()
	defer s.mu.RUnlock()

	articles := make([]Article, 0)
	for _, article := range s.data.Articles {
		if article.Status == ArticlePublished && !article.IsPremium {
			articles = append(articles, article)
		}
	}
	sort.Slice(articles, func(i, j int) bool { return articles[i].CreatedAt.After(articles[j].CreatedAt) })
	if limit > 0 && len(articles) > limit {
		return articles[:limit]
	}
	return articles
}

func (s *Store) CountPublishedArticles() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, article := range s.data.Articles {
		if article.Status == ArticlePublished && !article.IsPremium {
			count++
		}
	}
	return count
}

func (s *Store) ListPublishedArticlesPaginated(offset, limit int) []Article {
	s.mu.RLock()
	defer s.mu.RUnlock()

	articles := make([]Article, 0)
	for _, article := range s.data.Articles {
		if article.Status == ArticlePublished && !article.IsPremium {
			articles = append(articles, article)
		}
	}
	sort.Slice(articles, func(i, j int) bool { return articles[i].CreatedAt.After(articles[j].CreatedAt) })
	if offset >= len(articles) {
		return nil
	}
	articles = articles[offset:]
	if limit > 0 && len(articles) > limit {
		return articles[:limit]
	}
	return articles
}

func (s *Store) ListPublishedArticlesFiltered(category, query string, offset, limit int) []Article {
	s.mu.RLock()
	defer s.mu.RUnlock()

	category = strings.ToLower(strings.TrimSpace(category))
	query = strings.ToLower(strings.TrimSpace(query))
	articles := make([]Article, 0)
	for _, article := range s.data.Articles {
		if article.Status != ArticlePublished || article.IsPremium {
			continue
		}
		if category != "" && strings.ToLower(article.Category) != category {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(article.Title + " " + article.Excerpt + " " + article.Content)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		articles = append(articles, article)
	}
	sort.Slice(articles, func(i, j int) bool { return articles[i].CreatedAt.After(articles[j].CreatedAt) })
	if offset >= len(articles) {
		return nil
	}
	articles = articles[offset:]
	if limit > 0 && len(articles) > limit {
		return articles[:limit]
	}
	return articles
}

func (s *Store) CountPublishedArticlesFiltered(category, query string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	category = strings.ToLower(strings.TrimSpace(category))
	query = strings.ToLower(strings.TrimSpace(query))
	count := 0
	for _, article := range s.data.Articles {
		if article.Status != ArticlePublished || article.IsPremium {
			continue
		}
		if category != "" && strings.ToLower(article.Category) != category {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(article.Title + " " + article.Excerpt + " " + article.Content)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		count++
	}
	return count
}

func (s *Store) ArticleBySlug(slug string, includeUnpublished bool) (*Article, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, article := range s.data.Articles {
		if article.Slug == slug && (includeUnpublished || (article.Status == ArticlePublished && !article.IsPremium)) {
			copy := article
			return &copy, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) ArticleByID(id string) (*Article, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, article := range s.data.Articles {
		if article.ID == id {
			copy := article
			return &copy, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) UserName(userID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, err := s.userByIDLocked(userID)
	if err != nil {
		return "Redaksi NewsPaper"
	}
	return user.Name
}

func (s *Store) ListCategories() []Category {
	s.mu.RLock()
	defer s.mu.RUnlock()

	categories := append([]Category(nil), s.data.Categories...)
	sort.Slice(categories, func(i, j int) bool { return categories[i].Name < categories[j].Name })
	return categories
}

func (s *Store) CategoryByID(id string) (*Category, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, category := range s.data.Categories {
		if category.ID == id {
			copy := category
			return &copy, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) CreateCategory(user *User, input CategoryInput) (*Category, error) {
	if !canManageCategories(user) {
		return nil, ErrForbidden
	}
	name, slug, err := validateCategoryInput(input)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.categoryNameExistsLocked(name, "") || s.categorySlugExistsLocked(slug, "") {
		return nil, fmt.Errorf("%w: category already exists", ErrConflict)
	}
	now := time.Now().UTC()
	category := Category{ID: randomID("cat"), Name: name, Slug: slug, ShowInNavbar: input.ShowInNavbar, CreatedAt: now, UpdatedAt: now}
	s.data.Categories = append(s.data.Categories, category)
	return &category, s.saveLocked()
}

func (s *Store) UpdateCategory(user *User, id string, input CategoryInput) (*Category, error) {
	if !canManageCategories(user) {
		return nil, ErrForbidden
	}
	name, slug, err := validateCategoryInput(input)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Categories {
		category := &s.data.Categories[i]
		if category.ID != id {
			continue
		}
		if s.categoryNameExistsLocked(name, id) || s.categorySlugExistsLocked(slug, id) {
			return nil, fmt.Errorf("%w: category already exists", ErrConflict)
		}
		oldName := category.Name
		category.Name = name
		category.Slug = slug
		category.ShowInNavbar = input.ShowInNavbar
		category.UpdatedAt = time.Now().UTC()
		for j := range s.data.Articles {
			if strings.EqualFold(s.data.Articles[j].Category, oldName) {
				s.data.Articles[j].Category = name
				s.data.Articles[j].UpdatedAt = category.UpdatedAt
			}
		}
		copy := *category
		return &copy, s.saveLocked()
	}
	return nil, ErrNotFound
}

func (s *Store) DeleteCategory(user *User, id string) error {
	if !canManageCategories(user) {
		return ErrForbidden
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, category := range s.data.Categories {
		if category.ID != id {
			continue
		}
		for _, article := range s.data.Articles {
			if strings.EqualFold(article.Category, category.Name) {
				return fmt.Errorf("%w: category is used by articles", ErrConflict)
			}
		}
		s.data.Categories = append(s.data.Categories[:i], s.data.Categories[i+1:]...)
		return s.saveLocked()
	}
	return ErrNotFound
}

func (s *Store) CreateArticle(user *User, input ArticleInput) (*Article, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	if err := validateArticleInput(input, input.Status == ArticlePublished || input.Status == ArticleSubmitted); err != nil {
		return nil, err
	}
	if err := validateArticleStatusForRole(user, input.Status); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	slug := normalizeSlug(input.Slug)
	if slug == "" {
		slug = slugify(input.Title)
	}
	if s.slugExistsLocked(slug, "") {
		return nil, fmt.Errorf("%w: slug already exists", ErrConflict)
	}
	category := strings.TrimSpace(input.Category)
	if category != "" && !s.categoryNameExistsLocked(category, "") {
		return nil, fmt.Errorf("%w: category does not exist", ErrValidation)
	}
	status := input.Status
	if status == "" {
		status = ArticleDraft
	}
	article := Article{
		ID:           randomID("art"),
		AuthorID:     user.ID,
		Title:        strings.TrimSpace(input.Title),
		Slug:         slug,
		Excerpt:      strings.TrimSpace(input.Excerpt),
		Content:      strings.TrimSpace(input.Content),
		Category:     category,
		HeroImageURL: strings.TrimSpace(input.HeroImageURL),
		Status:       status,
		SourceURL:    strings.TrimSpace(input.SourceURL),
		ImageSource:  strings.TrimSpace(input.ImageSource),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if status == ArticlePublished {
		article.PublishedAt = &now
	}
	s.data.Articles = append(s.data.Articles, article)
	return &article, s.saveLocked()
}

func (s *Store) UpdateArticle(user *User, id string, input ArticleInput) (*Article, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	if err := validateArticleInput(input, input.Status == ArticlePublished || input.Status == ArticleSubmitted); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Articles {
		article := &s.data.Articles[i]
		if article.ID != id {
			continue
		}
		if !canEdit(user, *article) {
			return nil, ErrForbidden
		}
		if input.Status != "" && input.Status != article.Status {
			if err := validateArticleStatusForRole(user, input.Status); err != nil {
				return nil, err
			}
		}
		slug := normalizeSlug(input.Slug)
		if slug == "" {
			slug = slugify(input.Title)
		}
		if s.slugExistsLocked(slug, id) {
			return nil, fmt.Errorf("%w: slug already exists", ErrConflict)
		}
		category := strings.TrimSpace(input.Category)
		if category != "" && !s.categoryNameExistsLocked(category, "") {
			return nil, fmt.Errorf("%w: category does not exist", ErrValidation)
		}
		article.Title = strings.TrimSpace(input.Title)
		article.Slug = slug
		article.Excerpt = strings.TrimSpace(input.Excerpt)
		article.Content = strings.TrimSpace(input.Content)
		article.Category = category
		article.HeroImageURL = strings.TrimSpace(input.HeroImageURL)
		article.SourceURL = strings.TrimSpace(input.SourceURL)
		article.ImageSource = strings.TrimSpace(input.ImageSource)
		if input.Status != "" {
			article.Status = input.Status
		}
		now := time.Now().UTC()
		article.UpdatedAt = now
		if article.Status == ArticlePublished && article.PublishedAt == nil {
			article.PublishedAt = &now
		}
		copy := *article
		return &copy, s.saveLocked()
	}
	return nil, ErrNotFound
}

func (s *Store) DeleteArticle(user *User, id string) error {
	if user == nil {
		return ErrUnauthorized
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, article := range s.data.Articles {
		if article.ID != id {
			continue
		}
		if !canDelete(user, article) {
			return ErrForbidden
		}
		s.data.Articles = append(s.data.Articles[:i], s.data.Articles[i+1:]...)
		return s.saveLocked()
	}
	return ErrNotFound
}

func (s *Store) SubmitArticle(user *User, id string) (*Article, error) {
	return s.transitionArticle(user, id, ArticleSubmitted, "")
}

func (s *Store) ApproveArticle(user *User, id string) (*Article, error) {
	if user == nil || (user.Role != RoleAdmin && user.Role != RoleEditor) {
		return nil, ErrForbidden
	}
	return s.transitionArticle(user, id, ArticlePublished, "")
}

func (s *Store) RequestRevision(user *User, id, note string) (*Article, error) {
	if user == nil || (user.Role != RoleAdmin && user.Role != RoleEditor) {
		return nil, ErrForbidden
	}
	if strings.TrimSpace(note) == "" {
		return nil, fmt.Errorf("%w: review note required", ErrValidation)
	}
	return s.transitionArticle(user, id, ArticleNeedsRevision, note)
}

func (s *Store) ArchiveArticle(user *User, id string) (*Article, error) {
	if user == nil || (user.Role != RoleAdmin && user.Role != RoleEditor) {
		return nil, ErrForbidden
	}
	return s.transitionArticle(user, id, ArticleArchived, "")
}

func (s *Store) transitionArticle(user *User, id, status, note string) (*Article, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Articles {
		article := &s.data.Articles[i]
		if article.ID != id {
			continue
		}
		if status == ArticleSubmitted && !canEdit(user, *article) {
			return nil, ErrForbidden
		}
		now := time.Now().UTC()
		article.Status = status
		article.ReviewNote = strings.TrimSpace(note)
		if status == ArticlePublished || status == ArticleNeedsRevision || status == ArticleArchived {
			article.ReviewedBy = user.ID
			article.ReviewedAt = &now
		}
		if status == ArticlePublished {
			article.PublishedAt = &now
		}
		article.UpdatedAt = now
		copy := *article
		return &copy, s.saveLocked()
	}
	return nil, ErrNotFound
}

func (s *Store) CreateMedia(user *User, filename, originalName, mimeType, url string, size int64) (Media, error) {
	if user == nil {
		return Media{}, ErrUnauthorized
	}
	media := Media{ID: randomID("media"), OwnerID: user.ID, Filename: filename, OriginalName: originalName, MIMEType: mimeType, SizeBytes: size, URL: url, Source: cms.MediaSourceDashboardUpload, CreatedAt: time.Now().UTC()}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Media = append(s.data.Media, media)
	return media, s.saveLocked()
}

func (s *Store) CreateMediaFromAPI(principal APIPrincipal, filename, originalName, mimeType, url, source string, size int64) (Media, error) {
	return Media{}, ErrForbidden
}

func (s *Store) CreateExternalMediaURL(principal APIPrincipal, input ExternalMediaInput) (Media, error) {
	return Media{}, ErrForbidden
}

func (s *Store) ListAPIKeys(user *User) []APIKey {
	return nil
}

func (s *Store) CreateAPIKey(user *User, input APIKeyInput) (*APIKeyWithSecret, error) {
	return nil, ErrForbidden
}

func (s *Store) RevokeAPIKey(user *User, id string) error {
	return ErrForbidden
}

func (s *Store) DeleteAPIKey(user *User, id string) error {
	return ErrForbidden
}

func (s *Store) AuthenticateAPIKey(secret string) (*APIPrincipal, error) {
	return nil, ErrUnauthorized
}

func (s *Store) UpdateProfile(user *User, input ProfileInput) (*User, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	valid, err := cms.ValidateProfileInput(input)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Users {
		if s.data.Users[i].ID == user.ID {
			s.data.Users[i].Name = valid.Name
			s.data.Users[i].Bio = valid.Bio
			s.data.Users[i].Phone = valid.Phone
			s.data.Users[i].AvatarURL = valid.AvatarURL
			s.data.Users[i].UpdatedAt = time.Now().UTC()
			copy := s.data.Users[i]
			return &copy, s.saveLocked()
		}
	}
	return nil, ErrNotFound
}

func (s *Store) ListWriters(user *User) []User {
	return nil
}

func (s *Store) CreateWriter(user *User, input WriterInput) (*User, error) {
	return nil, ErrForbidden
}

func (s *Store) DeleteWriter(user *User, id string) error {
	return ErrForbidden
}

func (s *Store) ListAppUsers(user *User) []AppUser {
	return nil
}

func (s *Store) CreateAppUser(user *User, input AppUserInput) (*AppUser, error) {
	return nil, ErrForbidden
}

func (s *Store) DeleteAppUser(user *User, id string) error {
	return ErrForbidden
}

func (s *Store) AuthenticateAppUser(email, password string) (*AppUser, error) {
	return nil, ErrUnauthorized
}

func (s *Store) CreateAppSession(userID string, ttl time.Duration) (Session, error) {
	return Session{}, ErrForbidden
}

func (s *Store) AppUserBySession(sessionID string) (*AppUser, error) {
	return nil, ErrUnauthorized
}

func (s *Store) DeleteAppSession(sessionID string) error {
	return ErrForbidden
}

func (s *Store) GetAppBookmarks(userID string) ([]Article, error) {
	return nil, ErrForbidden
}

func (s *Store) AddAppBookmark(userID string, articleID string) error {
	return ErrForbidden
}

func (s *Store) DeleteAppBookmark(userID string, articleID string) error {
	return ErrForbidden
}

func (s *Store) CreateArticleFromAPI(principal APIPrincipal, input ArticleInput) (*Article, error) {
	return nil, ErrForbidden
}

func (s *Store) ListArticlesFromAPI(principal APIPrincipal) ([]Article, error) {
	return nil, ErrForbidden
}

func validateCategoryInput(input CategoryInput) (string, string, error) {
	name := strings.TrimSpace(input.Name)
	if len(name) < 3 {
		return "", "", fmt.Errorf("%w: category name must be at least 3 characters", ErrValidation)
	}
	slug := normalizeSlug(input.Slug)
	if slug == "" {
		slug = slugify(name)
	}
	if slug == "" {
		return "", "", fmt.Errorf("%w: category slug is required", ErrValidation)
	}
	return name, slug, nil
}

func validateArticleInput(input ArticleInput, strict bool) error {
	if len(strings.TrimSpace(input.Title)) < 5 {
		return fmt.Errorf("%w: title must be at least 5 characters", ErrValidation)
	}
	if strict && len(visibleArticleText(input.Content)) < 1 {
		return fmt.Errorf("%w: content is required", ErrValidation)
	}
	if input.Status != "" {
		switch input.Status {
		case ArticleDraft, ArticleSubmitted, ArticleNeedsRevision, ArticlePublished, ArticleArchived:
		default:
			return fmt.Errorf("%w: invalid status", ErrValidation)
		}
	}
	return nil
}

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func visibleArticleText(value string) string {
	value = strings.ReplaceAll(value, "&nbsp;", " ")
	value = html.UnescapeString(value)
	value = htmlTagPattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func validateArticleStatusForRole(user *User, status string) error {
	if status == "" || status == ArticleDraft || status == ArticleSubmitted {
		return nil
	}
	if user.Role == RoleAdmin || user.Role == RoleEditor {
		return nil
	}
	return ErrForbidden
}

func (s *Store) categoryNameExistsLocked(name, excludeID string) bool {
	for _, category := range s.data.Categories {
		if strings.EqualFold(category.Name, strings.TrimSpace(name)) && category.ID != excludeID {
			return true
		}
	}
	return false
}

func (s *Store) categorySlugExistsLocked(slug, excludeID string) bool {
	slug = normalizeSlug(slug)
	for _, category := range s.data.Categories {
		if category.Slug == slug && category.ID != excludeID {
			return true
		}
	}
	return false
}

func (s *Store) slugExistsLocked(slug, excludeID string) bool {
	for _, article := range s.data.Articles {
		if article.Slug == slug && article.ID != excludeID {
			return true
		}
	}
	return false
}

func canEdit(user *User, article Article) bool {
	return user.Role == RoleAdmin || user.Role == RoleEditor || article.AuthorID == user.ID
}

func canDelete(user *User, article Article) bool {
	return user.Role == RoleAdmin || article.AuthorID == user.ID
}

func canManageCategories(user *User) bool {
	return user != nil && (user.Role == RoleAdmin || user.Role == RoleEditor)
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return normalizeSlug(b.String())
}

func normalizeSlug(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), "-")
}

func randomID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

func (s *Store) GetSettings() map[string]string {
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

func (s *Store) UpdateSettings(user *User, settings map[string]string) error {
	return nil
}

func (s *Store) ArticleExistsBySourceURL(sourceURL string) (bool, error) {
	if sourceURL == "" {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, art := range s.data.Articles {
		if art.SourceURL == sourceURL {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) ArticleExistsByTitleOrSlug(title, slug string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normTitle := strings.ToLower(strings.TrimSpace(title))
	for _, art := range s.data.Articles {
		if strings.ToLower(strings.TrimSpace(art.Title)) == normTitle || art.Slug == slug {
			return true, nil
		}
	}
	return false, nil
}


func (s *Store) ArticleSlugBySourceURL(sourceURL string) (string, error) {
	if sourceURL == "" {
		return "", nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, art := range s.data.Articles {
		if art.SourceURL == sourceURL {
			return art.Slug, nil
		}
	}
	return "", nil
}

func (s *Store) GetSystemUser() (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, u := range s.data.Users {
		if u.ID == "user-admin" {
			return &u, nil
		}
	}
	for _, u := range s.data.Users {
		if u.Role == RoleAdmin {
			return &u, nil
		}
	}
	if len(s.data.Users) > 0 {
		return &s.data.Users[0], nil
	}
	return nil, fmt.Errorf("no users found in jsonstore")
}

func (s *Store) IsDomainBlacklisted(domain string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, d := range s.data.Blacklist {
		if d == domain {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) AddDomainToBlacklist(domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range s.data.Blacklist {
		if d == domain {
			return nil
		}
	}
	s.data.Blacklist = append(s.data.Blacklist, domain)
	return s.saveLocked()
}

func (s *Store) RemoveDomainFromBlacklist(domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var newList []string
	for _, d := range s.data.Blacklist {
		if d != domain {
			newList = append(newList, d)
		}
	}
	s.data.Blacklist = newList
	return s.saveLocked()
}

func (s *Store) ListBlacklistedDomains() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.data.Blacklist == nil {
		return []string{}, nil
	}
	return s.data.Blacklist, nil
}

func (s *Store) ClearBlacklistedDomains() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Blacklist = []string{}
	return s.saveLocked()
}

func (s *Store) ExportBackup() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	backup := cms.DatabaseBackup{
		Version:      1,
		Timestamp:    time.Now().UTC(),
		Users:        s.data.Users,
		Categories:   s.data.Categories,
		Articles:     s.data.Articles,
		Blacklist:    s.data.Blacklist,
		Media:        s.data.Media,
		SiteSettings: s.GetSettings(),
	}

	return json.MarshalIndent(backup, "", "  ")
}

func (s *Store) ImportBackup(backupData []byte) error {
	var backup cms.DatabaseBackup
	if err := json.Unmarshal(backupData, &backup); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Users = backup.Users
	s.data.Categories = backup.Categories
	s.data.Articles = backup.Articles
	s.data.Blacklist = backup.Blacklist
	s.data.Media = backup.Media
	s.data.Sessions = nil

	return s.saveLocked()
}

func (s *Store) IsArticlePostedToFB(articleID string) (bool, error) {
	return false, nil
}

func (s *Store) MarkArticleAsPostedToFB(articleID string, fbPostID string) error {
	return nil
}

func (s *Store) IsArticlePostedToBSky(articleID string) (bool, error) {
	return false, nil
}

func (s *Store) MarkArticleAsPostedToBSky(articleID string, bskyPostURI string) error {
	return nil
}

func (s *Store) LockArticleForBSky(articleID string) (bool, error) {
	return true, nil
}

func (s *Store) UnmarkArticleAsPostedToBSky(articleID string) error {
	return nil
}

func (s *Store) ListProxies() []cms.Proxy {
	return nil
}

func (s *Store) CreateProxy(user *cms.User, input cms.ProxyInput) (*cms.Proxy, error) {
	return nil, nil
}

func (s *Store) DeleteProxy(user *cms.User, id string) error {
	return nil
}

func (s *Store) UpdateProxyStatus(id string, status string, latency int) error {
	return nil
}

func (s *Store) UpdateProxyLastUsed(id string, lastUsed time.Time) error {
	return nil
}

func (s *Store) AddProxyBandwidth(id string, sent int64, received int64) error {
	return nil
}

func (s *Store) GetProxyByID(id string) (*cms.Proxy, error) {
	return nil, cms.ErrNotFound
}

func (s *Store) ListActiveProxies() []cms.Proxy {
	return nil
}

func (s *Store) ListWebshareKeys() ([]cms.WebshareKey, error) {
	return nil, nil
}

func (s *Store) AddWebshareKey(user *cms.User, apiKey, label string) (*cms.WebshareKey, error) {
	return nil, nil
}

func (s *Store) DeleteWebshareKey(user *cms.User, id string) error {
	return nil
}

func (s *Store) UpdateWebshareKeyBandwidth(id string, bytesUsed int64) error {
	return nil
}

func (s *Store) GetArticleChartStats(ctx context.Context, user *cms.User, filter string) ([]cms.ChartDataPoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pts []cms.ChartDataPoint
	counts := make(map[string]int)

	now := time.Now()
	var cutoff time.Time
	var formatFunc func(time.Time) string

	switch filter {
	case "day":
		cutoff = now.AddDate(0, 0, -7)
		formatFunc = func(t time.Time) string { return t.Format("2006-01-02") }
	case "week":
		cutoff = now.AddDate(0, 0, -56) // ~8 weeks
		formatFunc = func(t time.Time) string {
			year, week := t.ISOWeek()
			return fmt.Sprintf("%04d-W%02d", year, week)
		}
	case "month":
		cutoff = now.AddDate(0, -12, 0)
		formatFunc = func(t time.Time) string { return t.Format("2006-01") }
	case "year":
		cutoff = now.AddDate(-5, 0, 0)
		formatFunc = func(t time.Time) string { return t.Format("2006") }
	default:
		cutoff = now.AddDate(0, 0, -7)
		formatFunc = func(t time.Time) string { return t.Format("2006-01-02") }
	}

	for _, a := range s.data.Articles {
		if a.CreatedAt.Before(cutoff) {
			continue
		}
		if user != nil && user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor && a.AuthorID != user.ID {
			continue
		}
		lbl := formatFunc(a.CreatedAt)
		counts[lbl]++
	}
	
	for lbl, val := range counts {
		pts = append(pts, cms.ChartDataPoint{Label: lbl, Value: val})
	}
	
	return pts, nil
}

func (s *Store) ToggleArticlePremium(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.data.Articles {
		if a.ID == id {
			s.data.Articles[i].IsPremium = !s.data.Articles[i].IsPremium
			return s.saveLocked()
		}
	}
	return cms.ErrNotFound
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*cms.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.data.Users {
		if strings.EqualFold(u.Email, email) {
			return &u, nil
		}
	}
	return nil, cms.ErrNotFound
}

func (s *Store) InsertEmail(ctx context.Context, email *cms.Email) error {
	return nil
}

func (s *Store) GetEmailByID(ctx context.Context, id string) (*cms.Email, error) {
	return nil, cms.ErrNotFound
}

func (s *Store) ListEmails(ctx context.Context, userID string, direction string, limit, offset int) ([]cms.Email, error) {
	return nil, nil
}

func (s *Store) MarkEmailAsRead(ctx context.Context, id string) error {
	return nil
}

var _ appcms.ContentStore = (*Store)(nil)




