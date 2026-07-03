package postgresstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	dbassets "porta-berita/database"
	appcms "porta-berita/internal/application/cms"
	"porta-berita/internal/cms"

	_ "github.com/jackc/pgx/v5/stdlib"
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
type Proxy = cms.Proxy
type ProxyInput = cms.ProxyInput
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

	APIKeyStatusActive  = cms.APIKeyStatusActive
	APIKeyStatusRevoked = cms.APIKeyStatusRevoked

	ScopeArticlesCreate = cms.ScopeArticlesCreate
	ScopeMediaUpload    = cms.ScopeMediaUpload
	ScopeMediaURL       = cms.ScopeMediaURL

	MediaSourceDashboardUpload = cms.MediaSourceDashboardUpload
	MediaSourceAPIUpload       = cms.MediaSourceAPIUpload
	MediaSourceExternalURL     = cms.MediaSourceExternalURL
)

const userColumns = `id, name, email, password_hash, role, status, bio, phone, avatar_url, deleted_at, created_at, updated_at`
const articleColumns = `id, author_id, title, slug, excerpt, content, category, hero_image_url, status, review_note, reviewed_by, reviewed_at, published_at, created_by_api_key_id, api_actor_admin_id, source_url, image_source, is_premium, created_at, updated_at`

type PostgresStore struct {
	db *sql.DB
}

func OpenPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	store := &PostgresStore{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.seedIfEmpty(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);`); err != nil {
		return err
	}

	files, err := dbassets.MigrationFiles()
	if err != nil {
		return err
	}
	for _, file := range files {
		version := strings.TrimSuffix(path.Base(file), ".sql")
		applied, err := s.migrationApplied(ctx, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := s.applyMigration(ctx, version, file); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) migrationApplied(ctx context.Context, version string) (bool, error) {
	var applied bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied)
	return applied, err
}

func (s *PostgresStore) applyMigration(ctx context.Context, version, file string) error {
	body, err := dbassets.ReadFile(file)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) seedIfEmpty(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	files, err := dbassets.SeederFiles()
	if err != nil {
		return err
	}
	for _, file := range files {
		body, err := dbassets.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("run seeder %s: %w", path.Base(file), err)
		}
	}
	return nil
}

func (s *PostgresStore) Authenticate(email, password string) (*User, error) {
	user, err := s.userByEmail(context.Background(), strings.TrimSpace(email))
	if err != nil {
		return nil, ErrUnauthorized
	}
	if user.Status != StatusActive || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, ErrUnauthorized
	}
	return user, nil
}

func (s *PostgresStore) CreateSession(userID string, ttl time.Duration) (Session, error) {
	now := time.Now().UTC()
	session := Session{ID: randomID("sess"), UserID: userID, ExpiresAt: now.Add(ttl), CreatedAt: now}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO sessions (id, user_id, expires_at, created_at) VALUES ($1, $2, $3, $4)`, session.ID, session.UserID, session.ExpiresAt, session.CreatedAt)
	return session, err
}

func (s *PostgresStore) UserBySession(sessionID string) (*User, error) {
	row := s.db.QueryRowContext(context.Background(), `
	SELECT u.id, u.name, u.email, u.password_hash, u.role, u.status, u.bio, u.phone, u.avatar_url, u.deleted_at, u.created_at, u.updated_at
	FROM sessions sess
	JOIN users u ON u.id = sess.user_id
	WHERE sess.id = $1 AND sess.expires_at > now() AND u.deleted_at IS NULL`, sessionID)
	user, err := scanUser(row)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrUnauthorized
	}
	return user, err
}

func (s *PostgresStore) DeleteSession(sessionID string) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM sessions WHERE id = $1`, sessionID)
	return err
}

func (s *PostgresStore) ListArticles(user *User) []Article {
	if user == nil {
		return nil
	}
	query := `SELECT ` + articleColumns + ` FROM articles`
	args := []any{}
	if user.Role != RoleAdmin && user.Role != RoleEditor {
		query += ` WHERE author_id = $1`
		args = append(args, user.ID)
	}
	query += ` ORDER BY updated_at DESC`
	articles, err := s.queryArticles(context.Background(), query, args...)
	if err != nil {
		return nil
	}
	return articles
}


func (s *PostgresStore) ListRandomPublishedArticles(limit int, excludeID string) []Article {
	query := `SELECT ` + articleColumns + ` FROM articles WHERE status = 'published' AND is_premium = false AND id != $1 ORDER BY RANDOM() LIMIT $2`
	articles, err := s.queryArticles(context.Background(), query, excludeID, limit)
	if err != nil {
		return nil
	}
	return articles
}

func (s *PostgresStore) ListPublishedArticles(limit int) []Article {
	query := `SELECT ` + articleColumns + ` FROM articles WHERE status = 'published' AND is_premium = false ORDER BY created_at DESC`
	args := []any{}
	if limit > 0 {
		query += ` LIMIT $1`
		args = append(args, limit)
	}
	articles, err := s.queryArticles(context.Background(), query, args...)
	if err != nil {
		return nil
	}
	return articles
}

func (s *PostgresStore) CountPublishedArticles() int {
	var count int
	err := s.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM articles WHERE status = 'published' AND is_premium = false`).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (s *PostgresStore) ListPublishedArticlesPaginated(offset, limit int) []Article {
	query := `SELECT ` + articleColumns + ` FROM articles WHERE status = 'published' AND is_premium = false ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	articles, err := s.queryArticles(context.Background(), query, limit, offset)
	if err != nil {
		return nil
	}
	return articles
}

func (s *PostgresStore) ListPublishedArticlesFiltered(category, query string, offset, limit int) []Article {
	sql := `SELECT ` + articleColumns + ` FROM articles WHERE status = 'published' AND is_premium = false`
	args := []any{}
	argIndex := 1

	if category != "" {
		sql += ` AND LOWER(category) = LOWER($` + fmt.Sprintf("%d", argIndex) + `)`
		args = append(args, category)
		argIndex++
	}

	if query != "" {
		sql += ` AND (title ILIKE $` + fmt.Sprintf("%d", argIndex) + ` OR excerpt ILIKE $` + fmt.Sprintf("%d", argIndex) + ` OR content ILIKE $` + fmt.Sprintf("%d", argIndex) + `)`
		args = append(args, "%"+query+"%")
		argIndex++
	}

	sql += ` ORDER BY created_at DESC LIMIT $` + fmt.Sprintf("%d", argIndex) + ` OFFSET $` + fmt.Sprintf("%d", argIndex+1)
	args = append(args, limit, offset)

	articles, err := s.queryArticles(context.Background(), sql, args...)
	if err != nil {
		return nil
	}
	return articles
}

func (s *PostgresStore) CountPublishedArticlesFiltered(category, query string) int {
	sql := `SELECT COUNT(*) FROM articles WHERE status = 'published' AND is_premium = false`
	args := []any{}
	argIndex := 1

	if category != "" {
		sql += ` AND LOWER(category) = LOWER($` + fmt.Sprintf("%d", argIndex) + `)`
		args = append(args, category)
		argIndex++
	}

	if query != "" {
		sql += ` AND (title ILIKE $` + fmt.Sprintf("%d", argIndex) + ` OR excerpt ILIKE $` + fmt.Sprintf("%d", argIndex) + ` OR content ILIKE $` + fmt.Sprintf("%d", argIndex) + `)`
		args = append(args, "%"+query+"%")
	}

	var count int
	err := s.db.QueryRowContext(context.Background(), sql, args...).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (s *PostgresStore) ArticleBySlug(slug string, includeUnpublished bool) (*Article, error) {
	query := `SELECT ` + articleColumns + ` FROM articles WHERE slug = $1`
	args := []any{slug}
	if !includeUnpublished {
		query += ` AND status = 'published' AND is_premium = false`
	}
	return scanArticle(s.db.QueryRowContext(context.Background(), query, args...))
}

func (s *PostgresStore) ArticleByID(id string) (*Article, error) {
	return scanArticle(s.db.QueryRowContext(context.Background(), `SELECT `+articleColumns+` FROM articles WHERE id = $1`, id))
}

func (s *PostgresStore) UserName(userID string) string {
	var name string
	if err := s.db.QueryRowContext(context.Background(), `SELECT name FROM users WHERE id = $1`, userID).Scan(&name); err != nil {
		return "Redaksi NewsPaper"
	}
	return name
}

func (s *PostgresStore) ListCategories() []Category {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, name, slug, show_in_navbar, created_at, updated_at FROM categories ORDER BY name ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	categories := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.ShowInNavbar, &c.CreatedAt, &c.UpdatedAt); err == nil {
			categories = append(categories, c)
		}
	}
	return categories
}

func (s *PostgresStore) ListNavbarCategories() []Category {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, name, slug, show_in_navbar, created_at, updated_at FROM categories WHERE show_in_navbar = true ORDER BY name ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	categories := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.ShowInNavbar, &c.CreatedAt, &c.UpdatedAt); err == nil {
			categories = append(categories, c)
		}
	}
	return categories
}

func (s *PostgresStore) CategoryByID(id string) (*Category, error) {
	var c Category
	err := s.db.QueryRowContext(context.Background(), `SELECT id, name, slug, show_in_navbar, created_at, updated_at FROM categories WHERE id = $1`, id).Scan(&c.ID, &c.Name, &c.Slug, &c.ShowInNavbar, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (s *PostgresStore) CreateCategory(user *User, input CategoryInput) (*Category, error) {
	if !canManageCategories(user) {
		return nil, ErrForbidden
	}
	name, slug, err := validateCategoryInput(input)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	category := Category{ID: randomID("cat"), Name: name, Slug: slug, ShowInNavbar: input.ShowInNavbar, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO categories (id, name, slug, show_in_navbar, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`, category.ID, category.Name, category.Slug, category.ShowInNavbar, category.CreatedAt, category.UpdatedAt)
	if err != nil {
		return nil, conflictErr(err, "category already exists")
	}
	return &category, nil
}

func (s *PostgresStore) UpdateCategory(user *User, id string, input CategoryInput) (*Category, error) {
	if !canManageCategories(user) {
		return nil, ErrForbidden
	}
	name, slug, err := validateCategoryInput(input)
	if err != nil {
		return nil, err
	}
	old, err := s.CategoryByID(id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(context.Background(), `UPDATE categories SET name = $1, slug = $2, show_in_navbar = $3, updated_at = $4 WHERE id = $5`, name, slug, input.ShowInNavbar, now, id)
	if err != nil {
		return nil, conflictErr(err, "category already exists")
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	_, err = tx.ExecContext(context.Background(), `UPDATE articles SET category = $1, updated_at = $2 WHERE lower(category) = lower($3)`, name, now, old.Name)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Category{ID: id, Name: name, Slug: slug, ShowInNavbar: input.ShowInNavbar, CreatedAt: old.CreatedAt, UpdatedAt: now}, nil
}

func (s *PostgresStore) DeleteCategory(user *User, id string) error {
	if !canManageCategories(user) {
		return ErrForbidden
	}
	category, err := s.CategoryByID(id)
	if err != nil {
		return err
	}
	var used int
	if err := s.db.QueryRowContext(context.Background(), `SELECT count(*) FROM articles WHERE lower(category) = lower($1)`, category.Name).Scan(&used); err != nil {
		return err
	}
	if used > 0 {
		return fmt.Errorf("%w: category is used by articles", ErrConflict)
	}
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM categories WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CreateArticle(user *User, input ArticleInput) (*Article, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	if err := validateArticleInput(input, input.Status == ArticlePublished || input.Status == ArticleSubmitted); err != nil {
		return nil, err
	}
	if err := validateArticleStatusForRole(user, input.Status); err != nil {
		return nil, err
	}
	category := strings.TrimSpace(input.Category)
	if category != "" && !s.categoryNameExists(category) {
		return nil, fmt.Errorf("%w: category does not exist", ErrValidation)
	}
	now := time.Now().UTC()
	status := input.Status
	if status == "" {
		status = ArticleDraft
	}
	slug := normalizeSlug(input.Slug)
	if slug == "" {
		slug = slugify(input.Title)
	}
	article := Article{ID: randomID("art"), AuthorID: user.ID, Title: strings.TrimSpace(input.Title), Slug: slug, Excerpt: strings.TrimSpace(input.Excerpt), Content: strings.TrimSpace(input.Content), Category: category, HeroImageURL: strings.TrimSpace(input.HeroImageURL), Status: status, SourceURL: strings.TrimSpace(input.SourceURL), ImageSource: strings.TrimSpace(input.ImageSource), CreatedAt: now, UpdatedAt: now}
	if status == ArticlePublished {
		article.PublishedAt = &now
	}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO articles (id, author_id, title, slug, excerpt, content, category, hero_image_url, status, review_note, reviewed_by, reviewed_at, published_at, created_by_api_key_id, api_actor_admin_id, source_url, image_source, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'','',NULL,$10,NULL,NULL,$11,$12,$13,$14)`, article.ID, article.AuthorID, article.Title, article.Slug, article.Excerpt, article.Content, article.Category, article.HeroImageURL, article.Status, article.PublishedAt, article.SourceURL, article.ImageSource, article.CreatedAt, article.UpdatedAt)
	if err != nil {
		return nil, conflictErr(err, "slug already exists")
	}
	return &article, nil
}

func (s *PostgresStore) createArticleForUser(user *User, input ArticleInput, apiKeyID, apiActorAdminID string) (*Article, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	if err := validateArticleInput(input, input.Status == ArticlePublished || input.Status == ArticleSubmitted); err != nil {
		return nil, err
	}
	if err := validateArticleStatusForRole(user, input.Status); err != nil {
		return nil, err
	}
	category := strings.TrimSpace(input.Category)
	if category != "" && !s.categoryNameExists(category) {
		return nil, fmt.Errorf("%w: category does not exist", ErrValidation)
	}
	now := time.Now().UTC()
	status := input.Status
	if status == "" {
		status = ArticleDraft
	}
	slug := normalizeSlug(input.Slug)
	if slug == "" {
		slug = slugify(input.Title)
	}
	article := Article{ID: randomID("art"), AuthorID: user.ID, Title: strings.TrimSpace(input.Title), Slug: slug, Excerpt: strings.TrimSpace(input.Excerpt), Content: strings.TrimSpace(input.Content), Category: category, HeroImageURL: strings.TrimSpace(input.HeroImageURL), Status: status, CreatedByAPIKeyID: apiKeyID, APIActorAdminID: apiActorAdminID, SourceURL: strings.TrimSpace(input.SourceURL), ImageSource: strings.TrimSpace(input.ImageSource), CreatedAt: now, UpdatedAt: now}
	if status == ArticlePublished {
		article.PublishedAt = &now
	}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO articles (id, author_id, title, slug, excerpt, content, category, hero_image_url, status, review_note, reviewed_by, reviewed_at, published_at, created_by_api_key_id, api_actor_admin_id, source_url, image_source, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'','',NULL,$10,$11,$12,$13,$14,$15,$16)`, article.ID, article.AuthorID, article.Title, article.Slug, article.Excerpt, article.Content, article.Category, article.HeroImageURL, article.Status, article.PublishedAt, article.CreatedByAPIKeyID, article.APIActorAdminID, article.SourceURL, article.ImageSource, article.CreatedAt, article.UpdatedAt)
	if err != nil {
		return nil, conflictErr(err, "slug already exists")
	}
	return &article, nil
}

func (s *PostgresStore) UpdateArticle(user *User, id string, input ArticleInput) (*Article, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	if err := validateArticleInput(input, input.Status == ArticlePublished || input.Status == ArticleSubmitted); err != nil {
		return nil, err
	}
	article, err := s.ArticleByID(id)
	if err != nil {
		return nil, err
	}
	if !canEdit(user, *article) {
		return nil, ErrForbidden
	}
	if input.Status != "" && input.Status != article.Status {
		if err := validateArticleStatusForRole(user, input.Status); err != nil {
			return nil, err
		}
	}
	category := strings.TrimSpace(input.Category)
	if category != "" && !s.categoryNameExists(category) {
		return nil, fmt.Errorf("%w: category does not exist", ErrValidation)
	}
	slug := normalizeSlug(input.Slug)
	if slug == "" {
		slug = slugify(input.Title)
	}
	now := time.Now().UTC()
	status := article.Status
	if input.Status != "" {
		status = input.Status
	}
	publishedAt := article.PublishedAt
	if status == ArticlePublished && publishedAt == nil {
		publishedAt = &now
	}
	updated := Article{ID: article.ID, AuthorID: article.AuthorID, Title: strings.TrimSpace(input.Title), Slug: slug, Excerpt: strings.TrimSpace(input.Excerpt), Content: strings.TrimSpace(input.Content), Category: category, HeroImageURL: strings.TrimSpace(input.HeroImageURL), Status: status, ReviewNote: article.ReviewNote, ReviewedBy: article.ReviewedBy, ReviewedAt: article.ReviewedAt, PublishedAt: publishedAt, SourceURL: strings.TrimSpace(input.SourceURL), ImageSource: strings.TrimSpace(input.ImageSource), CreatedAt: article.CreatedAt, UpdatedAt: now}
	res, err := s.db.ExecContext(context.Background(), `UPDATE articles SET title=$1, slug=$2, excerpt=$3, content=$4, category=$5, hero_image_url=$6, status=$7, published_at=$8, source_url=$9, image_source=$10, updated_at=$11 WHERE id=$12`, updated.Title, updated.Slug, updated.Excerpt, updated.Content, updated.Category, updated.HeroImageURL, updated.Status, updated.PublishedAt, updated.SourceURL, updated.ImageSource, updated.UpdatedAt, id)
	if err != nil {
		return nil, conflictErr(err, "slug already exists")
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return &updated, nil
}

func (s *PostgresStore) DeleteArticle(user *User, id string) error {
	if user == nil {
		return ErrUnauthorized
	}
	article, err := s.ArticleByID(id)
	if err != nil {
		return err
	}
	if !canDelete(user, *article) {
		return ErrForbidden
	}
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM articles WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) SubmitArticle(user *User, id string) (*Article, error) {
	return s.transitionArticle(user, id, ArticleSubmitted, "")
}

func (s *PostgresStore) ApproveArticle(user *User, id string) (*Article, error) {
	if user == nil || (user.Role != RoleAdmin && user.Role != RoleEditor) {
		return nil, ErrForbidden
	}
	return s.transitionArticle(user, id, ArticlePublished, "")
}

func (s *PostgresStore) RequestRevision(user *User, id, note string) (*Article, error) {
	if user == nil || (user.Role != RoleAdmin && user.Role != RoleEditor) {
		return nil, ErrForbidden
	}
	if strings.TrimSpace(note) == "" {
		return nil, fmt.Errorf("%w: review note required", ErrValidation)
	}
	return s.transitionArticle(user, id, ArticleNeedsRevision, note)
}

func (s *PostgresStore) ArchiveArticle(user *User, id string) (*Article, error) {
	if user == nil || (user.Role != RoleAdmin && user.Role != RoleEditor) {
		return nil, ErrForbidden
	}
	return s.transitionArticle(user, id, ArticleArchived, "")
}

func (s *PostgresStore) transitionArticle(user *User, id, status, note string) (*Article, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	article, err := s.ArticleByID(id)
	if err != nil {
		return nil, err
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
	_, err = s.db.ExecContext(context.Background(), `UPDATE articles SET status=$1, review_note=$2, reviewed_by=$3, reviewed_at=$4, published_at=$5, updated_at=$6 WHERE id=$7`, article.Status, article.ReviewNote, article.ReviewedBy, article.ReviewedAt, article.PublishedAt, article.UpdatedAt, id)
	return article, err
}

func (s *PostgresStore) CreateMedia(user *User, filename, originalName, mimeType, url string, size int64) (Media, error) {
	if user == nil {
		return Media{}, ErrUnauthorized
	}
	media := Media{ID: randomID("media"), OwnerID: user.ID, Filename: filename, OriginalName: originalName, MIMEType: mimeType, SizeBytes: size, URL: url, Source: MediaSourceDashboardUpload, CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO media (id, owner_id, filename, original_name, mime_type, size_bytes, url, source, created_by_api_key_id, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULL,$9)`, media.ID, media.OwnerID, media.Filename, media.OriginalName, media.MIMEType, media.SizeBytes, media.URL, media.Source, media.CreatedAt)
	return media, err
}

func (s *PostgresStore) CreateMediaFromAPI(principal APIPrincipal, filename, originalName, mimeType, url, source string, size int64) (Media, error) {
	if !cms.HasScope(principal.Key.Scopes, ScopeMediaUpload) {
		return Media{}, ErrForbidden
	}
	if source == "" {
		source = MediaSourceAPIUpload
	}
	media := Media{ID: randomID("media"), OwnerID: principal.Admin.ID, Filename: filename, OriginalName: originalName, MIMEType: mimeType, SizeBytes: size, URL: url, Source: source, CreatedByAPIKeyID: principal.Key.ID, CreatedAt: time.Now().UTC()}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO media (id, owner_id, filename, original_name, mime_type, size_bytes, url, source, created_by_api_key_id, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, media.ID, media.OwnerID, media.Filename, media.OriginalName, media.MIMEType, media.SizeBytes, media.URL, media.Source, media.CreatedByAPIKeyID, media.CreatedAt)
	if err == nil {
		_ = s.touchAPIKey(principal.Key.ID)
	}
	return media, err
}

func (s *PostgresStore) CreateExternalMediaURL(principal APIPrincipal, input ExternalMediaInput) (Media, error) {
	if !cms.HasScope(principal.Key.Scopes, ScopeMediaURL) {
		return Media{}, ErrForbidden
	}
	mediaURL, err := cms.ValidateExternalMediaURL(input.URL)
	if err != nil {
		return Media{}, err
	}
	originalName := strings.TrimSpace(input.OriginalName)
	if originalName == "" {
		originalName = path.Base(mediaURL)
	}
	media := Media{ID: randomID("media"), OwnerID: principal.Admin.ID, Filename: originalName, OriginalName: originalName, MIMEType: "image/remote", URL: mediaURL, Source: MediaSourceExternalURL, CreatedByAPIKeyID: principal.Key.ID, CreatedAt: time.Now().UTC()}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO media (id, owner_id, filename, original_name, mime_type, size_bytes, url, source, created_by_api_key_id, created_at) VALUES ($1,$2,$3,$4,$5,0,$6,$7,$8,$9)`, media.ID, media.OwnerID, media.Filename, media.OriginalName, media.MIMEType, media.URL, media.Source, media.CreatedByAPIKeyID, media.CreatedAt)
	if err == nil {
		_ = s.touchAPIKey(principal.Key.ID)
	}
	return media, err
}

func (s *PostgresStore) ListAPIKeys(user *User) []APIKey {
	if !cms.CanManageAPIKeys(user) {
		return nil
	}
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, admin_id, name, key_prefix, key_hash, key_secret, scopes, status, last_used_at, expires_at, created_at, updated_at, revoked_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	keys := []APIKey{}
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}

func (s *PostgresStore) CreateAPIKey(user *User, input APIKeyInput) (*APIKeyWithSecret, error) {
	if !cms.CanManageAPIKeys(user) {
		return nil, ErrForbidden
	}
	valid, err := cms.ValidateAPIKeyInput(input)
	if err != nil {
		return nil, err
	}
	secret := strings.ReplaceAll(randomID("portal"), "-", "_") + strings.ReplaceAll(randomID("key"), "-", "_")
	keyPrefix := secret
	if len(keyPrefix) > 18 {
		keyPrefix = keyPrefix[:18]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	key := APIKey{ID: randomID("api"), AdminID: user.ID, Name: valid.Name, KeyPrefix: keyPrefix, Secret: secret, Scopes: valid.Scopes, Status: APIKeyStatusActive, ExpiresAt: valid.ExpiresAt, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO api_keys (id, admin_id, name, key_prefix, key_hash, key_secret, scopes, status, expires_at, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, key.ID, key.AdminID, key.Name, key.KeyPrefix, string(hash), key.Secret, scopesString(key.Scopes), key.Status, key.ExpiresAt, key.CreatedAt, key.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &APIKeyWithSecret{APIKey: key, Secret: secret}, nil
}

func (s *PostgresStore) RevokeAPIKey(user *User, id string) error {
	if !cms.CanManageAPIKeys(user) {
		return ErrForbidden
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(context.Background(), `UPDATE api_keys SET status=$1, revoked_at=$2, updated_at=$2 WHERE id=$3`, APIKeyStatusRevoked, now, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteAPIKey(user *User, id string) error {
	if !cms.CanManageAPIKeys(user) {
		return ErrForbidden
	}
	res, err := s.db.ExecContext(context.Background(), `DELETE FROM api_keys WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) AuthenticateAPIKey(secret string) (*APIPrincipal, error) {
	secret = strings.TrimSpace(strings.TrimPrefix(secret, "Bearer "))
	if secret == "" {
		return nil, ErrUnauthorized
	}
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, admin_id, name, key_prefix, key_hash, key_secret, scopes, status, last_used_at, expires_at, created_at, updated_at, revoked_at FROM api_keys WHERE status=$1`, APIKeyStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		key, hash, err := scanAPIKeyWithHash(rows)
		if err != nil {
			continue
		}
		if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now().UTC()) {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret)) != nil {
			continue
		}
		admin, err := s.userByID(context.Background(), key.AdminID)
		if err != nil || admin.Role != RoleAdmin || admin.Status != StatusActive {
			return nil, ErrUnauthorized
		}
		return &APIPrincipal{Key: key, Admin: *admin}, nil
	}
	return nil, ErrUnauthorized
}

func (s *PostgresStore) UpdateProfile(user *User, input ProfileInput) (*User, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	valid, err := cms.ValidateProfileInput(input)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(context.Background(), `UPDATE users SET name=$1, bio=$2, phone=$3, avatar_url=$4, updated_at=$5 WHERE id=$6 AND deleted_at IS NULL`, valid.Name, valid.Bio, valid.Phone, valid.AvatarURL, now, user.ID)
	if err != nil {
		return nil, err
	}
	return s.userByID(context.Background(), user.ID)
}

func (s *PostgresStore) ListWriters(user *User) []User {
	if !cms.CanManageWriters(user) {
		return nil
	}
	rows, err := s.db.QueryContext(context.Background(), `SELECT `+userColumns+` FROM users WHERE role=$1 AND deleted_at IS NULL ORDER BY created_at DESC`, RoleWriter)
	if err != nil {
		return nil
	}
	defer rows.Close()
	writers := []User{}
	for rows.Next() {
		writer, err := scanUser(rows)
		if err == nil {
			writers = append(writers, *writer)
		}
	}
	return writers
}

func (s *PostgresStore) CreateWriter(user *User, input WriterInput) (*User, error) {
	if !cms.CanManageWriters(user) {
		return nil, ErrForbidden
	}
	valid, err := cms.ValidateWriterInput(input, true)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(valid.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	writer := User{ID: randomID("user"), Name: valid.Name, Email: valid.Email, PasswordHash: string(hash), Role: RoleWriter, Status: valid.Status, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO users (id, name, email, password_hash, role, status, bio, phone, avatar_url, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,'','','',$7,$8)`, writer.ID, writer.Name, writer.Email, writer.PasswordHash, writer.Role, writer.Status, writer.CreatedAt, writer.UpdatedAt)
	if err != nil {
		return nil, conflictErr(err, "email already exists")
	}
	return &writer, nil
}

func (s *PostgresStore) DeleteWriter(user *User, id string) error {
	if !cms.CanManageWriters(user) {
		return ErrForbidden
	}
	if user.ID == id {
		return fmt.Errorf("%w: admin cannot delete own account", ErrValidation)
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(context.Background(), `UPDATE users SET status=$1, deleted_at=$2, updated_at=$2 WHERE id=$3 AND role=$4`, StatusInactive, now, id, RoleWriter)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListAppUsers(user *User) []AppUser {
	if !cms.CanManageAppUsers(user) {
		return nil
	}
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, name, email, password_hash, status, created_at, updated_at, deleted_at FROM app_users WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	appUsers := []AppUser{}
	for rows.Next() {
		appUser, err := scanAppUser(rows)
		if err == nil {
			appUsers = append(appUsers, *appUser)
		}
	}
	return appUsers
}

func (s *PostgresStore) CreateAppUser(user *User, input AppUserInput) (*AppUser, error) {
	if !cms.CanManageAppUsers(user) {
		return nil, ErrForbidden
	}
	valid, err := cms.ValidateAppUserInput(input, true)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(valid.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	appUser := AppUser{ID: randomID("uapp"), Name: valid.Name, Email: valid.Email, PasswordHash: string(hash), Status: valid.Status, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO app_users (id, name, email, password_hash, status, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, appUser.ID, appUser.Name, appUser.Email, appUser.PasswordHash, appUser.Status, appUser.CreatedAt, appUser.UpdatedAt)
	if err != nil {
		return nil, conflictErr(err, "email already exists")
	}
	return &appUser, nil
}

func (s *PostgresStore) DeleteAppUser(user *User, id string) error {
	if !cms.CanManageAppUsers(user) {
		return ErrForbidden
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(context.Background(), `UPDATE app_users SET status=$1, deleted_at=$2, updated_at=$2 WHERE id=$3`, StatusInactive, now, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) AuthenticateAppUser(email, password string) (*AppUser, error) {
	row := s.db.QueryRowContext(context.Background(), `SELECT id, name, email, password_hash, status, created_at, updated_at FROM app_users WHERE lower(email) = lower($1) AND deleted_at IS NULL`, strings.TrimSpace(email))
	var user AppUser
	err := row.Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if user.Status != StatusActive || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, ErrUnauthorized
	}
	return &user, nil
}

func (s *PostgresStore) CreateAppSession(userID string, ttl time.Duration) (Session, error) {
	now := time.Now().UTC()
	session := Session{ID: randomID("sess_app"), UserID: userID, ExpiresAt: now.Add(ttl), CreatedAt: now}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO app_sessions (id, user_id, expires_at, created_at) VALUES ($1, $2, $3, $4)`, session.ID, session.UserID, session.ExpiresAt, session.CreatedAt)
	return session, err
}

func (s *PostgresStore) AppUserBySession(sessionID string) (*AppUser, error) {
	row := s.db.QueryRowContext(context.Background(), `
		SELECT u.id, u.name, u.email, u.password_hash, u.status, u.created_at, u.updated_at
		FROM app_sessions s
		JOIN app_users u ON s.user_id = u.id
		WHERE s.id = $1 AND s.expires_at > now() AND u.deleted_at IS NULL`, sessionID)
	var user AppUser
	err := row.Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, ErrUnauthorized
	}
	return &user, nil
}

func (s *PostgresStore) DeleteAppSession(sessionID string) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM app_sessions WHERE id = $1`, sessionID)
	return err
}

func (s *PostgresStore) GetAppBookmarks(userID string) ([]Article, error) {
	query := `
		SELECT a.id, a.author_id, a.title, a.slug, a.excerpt, a.content, a.category, a.hero_image_url, a.status, a.review_note, a.reviewed_by, a.reviewed_at, a.published_at, a.created_by_api_key_id, a.api_actor_admin_id, a.source_url, a.image_source, a.is_premium, a.created_at, a.updated_at
		FROM articles a
		JOIN app_bookmarks b ON a.id = b.article_id
		WHERE b.app_user_id = $1 AND a.status = 'published'
		ORDER BY b.created_at DESC`
	return s.queryArticles(context.Background(), query, userID)
}

func (s *PostgresStore) AddAppBookmark(userID string, articleID string) error {
	query := `INSERT INTO app_bookmarks (app_user_id, article_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := s.db.ExecContext(context.Background(), query, userID, articleID)
	return err
}

func (s *PostgresStore) DeleteAppBookmark(userID string, articleID string) error {
	query := `DELETE FROM app_bookmarks WHERE app_user_id = $1 AND article_id = $2`
	_, err := s.db.ExecContext(context.Background(), query, userID, articleID)
	return err
}

func scanAppUser(row scanner) (*AppUser, error) {
	var user AppUser
	var deletedAt sql.NullTime
	err := row.Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if deletedAt.Valid {
		user.DeletedAt = &deletedAt.Time
	}
	return &user, err
}

func (s *PostgresStore) CreateArticleFromAPI(principal APIPrincipal, input ArticleInput) (*Article, error) {
	if !cms.HasScope(principal.Key.Scopes, ScopeArticlesCreate) {
		return nil, ErrForbidden
	}
	if input.Status == "" {
		input.Status = ArticleDraft
	}
	article, err := s.createArticleForUser(&principal.Admin, input, principal.Key.ID, principal.Admin.ID)
	if err == nil {
		_ = s.touchAPIKey(principal.Key.ID)
	}
	return article, err
}

func (s *PostgresStore) ListArticlesFromAPI(principal APIPrincipal) ([]Article, error) {
	if !cms.HasScope(principal.Key.Scopes, "articles:read") {
		return nil, ErrForbidden
	}
	query := `SELECT ` + articleColumns + ` FROM articles ORDER BY created_at DESC`
	articles, err := s.queryArticles(context.Background(), query)
	if err != nil {
		return nil, err
	}
	_ = s.touchAPIKey(principal.Key.ID)
	return articles, nil
}

func (s *PostgresStore) userByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1) AND deleted_at IS NULL`, email))
}

func (s *PostgresStore) userByID(ctx context.Context, id string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1 AND deleted_at IS NULL`, id))
}

func (s *PostgresStore) touchAPIKey(id string) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE api_keys SET last_used_at=now(), updated_at=now() WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) categoryNameExists(name string) bool {
	var exists bool
	_ = s.db.QueryRowContext(context.Background(), `SELECT EXISTS (SELECT 1 FROM categories WHERE lower(name) = lower($1))`, strings.TrimSpace(name)).Scan(&exists)
	return exists
}

func (s *PostgresStore) queryArticles(ctx context.Context, query string, args ...any) ([]Article, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	articles := []Article{}
	for rows.Next() {
		article, err := scanArticleRows(rows)
		if err != nil {
			return nil, err
		}
		articles = append(articles, article)
	}
	return articles, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (*User, error) {
	var user User
	var deletedAt sql.NullTime
	err := row.Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash, &user.Role, &user.Status, &user.Bio, &user.Phone, &user.AvatarURL, &deletedAt, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if deletedAt.Valid {
		user.DeletedAt = &deletedAt.Time
	}
	return &user, err
}

func scanArticle(row scanner) (*Article, error) {
	article, err := scanArticleInto(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &article, err
}

func scanArticleRows(rows *sql.Rows) (Article, error) {
	return scanArticleInto(rows)
}

func scanArticleInto(row scanner) (Article, error) {
	var article Article
	var reviewedAt, publishedAt sql.NullTime
	var apiKeyID, apiActorAdminID sql.NullString
	err := row.Scan(&article.ID, &article.AuthorID, &article.Title, &article.Slug, &article.Excerpt, &article.Content, &article.Category, &article.HeroImageURL, &article.Status, &article.ReviewNote, &article.ReviewedBy, &reviewedAt, &publishedAt, &apiKeyID, &apiActorAdminID, &article.SourceURL, &article.ImageSource, &article.IsPremium, &article.CreatedAt, &article.UpdatedAt)
	if reviewedAt.Valid {
		article.ReviewedAt = &reviewedAt.Time
	}
	if publishedAt.Valid {
		article.PublishedAt = &publishedAt.Time
	}
	if apiKeyID.Valid {
		article.CreatedByAPIKeyID = apiKeyID.String
	}
	if apiActorAdminID.Valid {
		article.APIActorAdminID = apiActorAdminID.String
	}
	return article, err
}

func scanAPIKey(row scanner) (APIKey, error) {
	key, _, err := scanAPIKeyWithHash(row)
	return key, err
}

func scanAPIKeyWithHash(row scanner) (APIKey, string, error) {
	var key APIKey
	var hash string
	var secret sql.NullString
	var scopes string
	var lastUsedAt, expiresAt, revokedAt sql.NullTime
	err := row.Scan(&key.ID, &key.AdminID, &key.Name, &key.KeyPrefix, &hash, &secret, &scopes, &key.Status, &lastUsedAt, &expiresAt, &key.CreatedAt, &key.UpdatedAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, "", ErrNotFound
	}
	if secret.Valid {
		key.Secret = secret.String
	}
	if lastUsedAt.Valid {
		key.LastUsedAt = &lastUsedAt.Time
	}
	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.Time
	}
	if revokedAt.Valid {
		key.RevokedAt = &revokedAt.Time
	}
	key.Scopes = splitScopes(scopes)
	return key, hash, err
}

func scopesString(scopes []string) string {
	return strings.Join(scopes, ",")
}

func splitScopes(value string) []string {
	parts := strings.Split(value, ",")
	scopes := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			scopes = append(scopes, part)
		}
	}
	return scopes
}

func conflictErr(err error, message string) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
		return fmt.Errorf("%w: %s", ErrConflict, message)
	}
	return err
}

func validateCategoryInput(input CategoryInput) (string, string, error) {
	return cms.ValidateCategoryInput(input)
}

func validateArticleInput(input ArticleInput, strict bool) error {
	return cms.ValidateArticleInput(input, strict)
}

func validateArticleStatusForRole(user *User, status string) error {
	return cms.ValidateArticleStatusForRole(user, status)
}

func canEdit(user *User, article Article) bool {
	return cms.CanEdit(user, article)
}

func canDelete(user *User, article Article) bool {
	return cms.CanDelete(user, article)
}

func canManageCategories(user *User) bool {
	return cms.CanManageCategories(user)
}

func slugify(value string) string {
	return cms.Slugify(value)
}

func normalizeSlug(value string) string {
	return cms.NormalizeSlug(value)
}

func randomID(prefix string) string {
	return cms.RandomID(prefix)
}

func (s *PostgresStore) GetSettings() map[string]string {
	rows, err := s.db.QueryContext(context.Background(), `SELECT key, value FROM site_settings`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err == nil {
			settings[key] = value
		}
	}
	return settings
}

func (s *PostgresStore) UpdateSettings(user *User, settings map[string]string) error {
	if user == nil || (user.Role != RoleAdmin && user.Role != RoleEditor) {
		return ErrUnauthorized
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for k, v := range settings {
		_, err := tx.ExecContext(context.Background(), `
			INSERT INTO site_settings (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
		`, k, v)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) ArticleExistsBySourceURL(sourceURL string) (bool, error) {
	if sourceURL == "" {
		return false, nil
	}
	var exists bool
	err := s.db.QueryRowContext(context.Background(), `SELECT EXISTS(SELECT 1 FROM articles WHERE source_url = $1)`, sourceURL).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (s *PostgresStore) ArticleExistsByTitleOrSlug(title, slug string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(
		SELECT 1 FROM articles 
		WHERE LOWER(TRIM(title)) = LOWER(TRIM($1)) 
		OR slug = $2
	)`
	err := s.db.QueryRowContext(context.Background(), query, title, slug).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}


func (s *PostgresStore) ArticleSlugBySourceURL(sourceURL string) (string, error) {
	if sourceURL == "" {
		return "", nil
	}
	var slug string
	err := s.db.QueryRowContext(context.Background(), `SELECT slug FROM articles WHERE source_url = $1 LIMIT 1`, sourceURL).Scan(&slug)
	if err != nil {
		return "", nil
	}
	return slug, nil
}

func (s *PostgresStore) GetSystemUser() (*User, error) {
	u, err := s.userByID(context.Background(), "user-admin")
	if err == nil {
		return u, nil
	}
	row := s.db.QueryRowContext(context.Background(), `SELECT `+userColumns+` FROM users WHERE deleted_at IS NULL ORDER BY role = 'admin' DESC, created_at ASC LIMIT 1`)
	u, err = scanUser(row)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *PostgresStore) IsDomainBlacklisted(domain string) (bool, error) {
	if domain == "" {
		return false, nil
	}
	var exists bool
	err := s.db.QueryRowContext(context.Background(), `SELECT EXISTS (SELECT 1 FROM domain_blacklist WHERE domain = $1)`, domain).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) AddDomainToBlacklist(domain string) error {
	if domain == "" {
		return nil
	}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO domain_blacklist (domain) VALUES ($1) ON CONFLICT (domain) DO NOTHING`, domain)
	return err
}

func (s *PostgresStore) RemoveDomainFromBlacklist(domain string) error {
	if domain == "" {
		return nil
	}
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM domain_blacklist WHERE domain = $1`, domain)
	return err
}

func (s *PostgresStore) ClearBlacklistedDomains() error {
	_, err := s.db.ExecContext(context.Background(), `TRUNCATE TABLE domain_blacklist`)
	return err
}

func (s *PostgresStore) ListBlacklistedDomains() ([]string, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT domain FROM domain_blacklist ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	if domains == nil {
		domains = []string{}
	}
	return domains, nil
}

func (s *PostgresStore) ExportBackup() ([]byte, error) {
	ctx := context.Background()

	// 1. Users
	rowsUsers, err := s.db.QueryContext(ctx, "SELECT "+userColumns+" FROM users")
	if err != nil {
		return nil, fmt.Errorf("export users: %w", err)
	}
	defer rowsUsers.Close()
	var users []User
	for rowsUsers.Next() {
		user, err := scanUser(rowsUsers)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *user)
	}

	// 2. Categories
	rowsCategories, err := s.db.QueryContext(ctx, "SELECT id, name, slug, show_in_navbar, created_at, updated_at FROM categories")
	if err != nil {
		return nil, fmt.Errorf("export categories: %w", err)
	}
	defer rowsCategories.Close()
	var categories []Category
	for rowsCategories.Next() {
		var cat Category
		if err := rowsCategories.Scan(&cat.ID, &cat.Name, &cat.Slug, &cat.ShowInNavbar, &cat.CreatedAt, &cat.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		categories = append(categories, cat)
	}

	// 3. Articles
	rowsArticles, err := s.db.QueryContext(ctx, "SELECT "+articleColumns+" FROM articles")
	if err != nil {
		return nil, fmt.Errorf("export articles: %w", err)
	}
	defer rowsArticles.Close()
	var articles []Article
	for rowsArticles.Next() {
		art, err := scanArticle(rowsArticles)
		if err != nil {
			return nil, fmt.Errorf("scan article: %w", err)
		}
		articles = append(articles, *art)
	}

	// 4. API Keys
	rowsKeys, err := s.db.QueryContext(ctx, "SELECT id, admin_id, name, key_prefix, key_hash, key_secret, scopes, status, last_used_at, expires_at, created_at, updated_at, revoked_at FROM api_keys")
	if err != nil {
		return nil, fmt.Errorf("export api keys: %w", err)
	}
	defer rowsKeys.Close()
	var apiKeys []cms.APIKeyBackup
	for rowsKeys.Next() {
		var key cms.APIKeyBackup
		var scopes string
		var lastUsedAt, expiresAt, revokedAt sql.NullTime
		if err := rowsKeys.Scan(&key.ID, &key.AdminID, &key.Name, &key.KeyPrefix, &key.KeyHash, &key.KeySecret, &scopes, &key.Status, &lastUsedAt, &expiresAt, &key.CreatedAt, &key.UpdatedAt, &revokedAt); err != nil {
			return nil, fmt.Errorf("scan api key backup: %w", err)
		}
		key.Scopes = strings.Split(scopes, ",")
		if lastUsedAt.Valid {
			key.LastUsedAt = &lastUsedAt.Time
		}
		if expiresAt.Valid {
			key.ExpiresAt = &expiresAt.Time
		}
		if revokedAt.Valid {
			key.RevokedAt = &revokedAt.Time
		}
		apiKeys = append(apiKeys, key)
	}

	// 5. Site Settings
	rowsSettings, err := s.db.QueryContext(ctx, "SELECT key, value FROM site_settings")
	if err != nil {
		return nil, fmt.Errorf("export site settings: %w", err)
	}
	defer rowsSettings.Close()
	siteSettings := make(map[string]string)
	for rowsSettings.Next() {
		var k, v string
		if err := rowsSettings.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		siteSettings[k] = v
	}

	// 6. Blacklist
	blacklist, err := s.ListBlacklistedDomains()
	if err != nil {
		return nil, fmt.Errorf("export blacklist: %w", err)
	}

	// 7. Media
	rowsMedia, err := s.db.QueryContext(ctx, "SELECT id, owner_id, filename, original_name, mime_type, size_bytes, url, source, created_by_api_key_id, created_at FROM media")
	if err != nil {
		return nil, fmt.Errorf("export media: %w", err)
	}
	defer rowsMedia.Close()
	var media []Media
	for rowsMedia.Next() {
		var m Media
		var apiKeyID sql.NullString
		if err := rowsMedia.Scan(&m.ID, &m.OwnerID, &m.Filename, &m.OriginalName, &m.MIMEType, &m.SizeBytes, &m.URL, &m.Source, &apiKeyID, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan media: %w", err)
		}
		if apiKeyID.Valid {
			m.CreatedByAPIKeyID = apiKeyID.String
		}
		media = append(media, m)
	}

	backup := cms.DatabaseBackup{
		Version:      1,
		Timestamp:    time.Now().UTC(),
		Users:        users,
		Categories:   categories,
		Articles:     articles,
		APIKeys:      apiKeys,
		SiteSettings: siteSettings,
		Blacklist:    blacklist,
		Media:        media,
	}

	return json.MarshalIndent(backup, "", "  ")
}

func (s *PostgresStore) ImportBackup(backupData []byte) error {
	var backup cms.DatabaseBackup
	if err := json.Unmarshal(backupData, &backup); err != nil {
		return fmt.Errorf("unmarshal backup: %w", err)
	}

	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Delete all existing data to prepare for clean restore
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions"); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM articles"); err != nil {
		return fmt.Errorf("delete articles: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM media"); err != nil {
		return fmt.Errorf("delete media: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM api_keys"); err != nil {
		return fmt.Errorf("delete api_keys: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM categories"); err != nil {
		return fmt.Errorf("delete categories: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM users"); err != nil {
		return fmt.Errorf("delete users: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM domain_blacklist"); err != nil {
		return fmt.Errorf("delete domain_blacklist: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM site_settings"); err != nil {
		return fmt.Errorf("delete site_settings: %w", err)
	}

	// 2. Insert Users
	for _, u := range backup.Users {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO users (id, name, email, password_hash, role, status, bio, phone, avatar_url, deleted_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`, u.ID, u.Name, u.Email, u.PasswordHash, u.Role, u.Status, u.Bio, u.Phone, u.AvatarURL, u.DeletedAt, u.CreatedAt, u.UpdatedAt)
		if err != nil {
			return fmt.Errorf("import user %s: %w", u.Email, err)
		}
	}

	// 3. Insert Categories
	for _, c := range backup.Categories {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO categories (id, name, slug, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5)
		`, c.ID, c.Name, c.Slug, c.CreatedAt, c.UpdatedAt)
		if err != nil {
			return fmt.Errorf("import category %s: %w", c.Name, err)
		}
	}

	// 4. Insert API Keys
	for _, k := range backup.APIKeys {
		scopesJoined := strings.Join(k.Scopes, ",")
		_, err := tx.ExecContext(ctx, `
			INSERT INTO api_keys (id, admin_id, name, key_prefix, key_hash, key_secret, scopes, status, last_used_at, expires_at, created_at, updated_at, revoked_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		`, k.ID, k.AdminID, k.Name, k.KeyPrefix, k.KeyHash, k.KeySecret, scopesJoined, k.Status, k.LastUsedAt, k.ExpiresAt, k.CreatedAt, k.UpdatedAt, k.RevokedAt)
		if err != nil {
			return fmt.Errorf("import api key %s: %w", k.Name, err)
		}
	}

	// 5. Insert Media
	for _, m := range backup.Media {
		var apiKeyID *string
		if m.CreatedByAPIKeyID != "" {
			apiKeyID = &m.CreatedByAPIKeyID
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO media (id, owner_id, filename, original_name, mime_type, size_bytes, url, source, created_by_api_key_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, m.ID, m.OwnerID, m.Filename, m.OriginalName, m.MIMEType, m.SizeBytes, m.URL, m.Source, apiKeyID, m.CreatedAt)
		if err != nil {
			return fmt.Errorf("import media %s: %w", m.OriginalName, err)
		}
	}

	// 6. Insert Articles
	for _, a := range backup.Articles {
		var apiKeyID, apiActorAdminID *string
		if a.CreatedByAPIKeyID != "" {
			apiKeyID = &a.CreatedByAPIKeyID
		}
		if a.APIActorAdminID != "" {
			apiActorAdminID = &a.APIActorAdminID
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO articles (id, author_id, title, slug, excerpt, content, category, hero_image_url, status, review_note, reviewed_by, reviewed_at, published_at, created_by_api_key_id, api_actor_admin_id, source_url, image_source, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		`, a.ID, a.AuthorID, a.Title, a.Slug, a.Excerpt, a.Content, a.Category, a.HeroImageURL, a.Status, a.ReviewNote, a.ReviewedBy, a.ReviewedAt, a.PublishedAt, apiKeyID, apiActorAdminID, a.SourceURL, a.ImageSource, a.CreatedAt, a.UpdatedAt)
		if err != nil {
			return fmt.Errorf("import article %s: %w", a.Title, err)
		}
	}

	// 7. Insert Site Settings
	for k, v := range backup.SiteSettings {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO site_settings (key, value)
			VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
		`, k, v)
		if err != nil {
			return fmt.Errorf("import setting %s: %w", k, err)
		}
	}

	// 8. Insert Blacklist
	for _, d := range backup.Blacklist {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO domain_blacklist (domain, created_at)
			VALUES ($1, $2)
			ON CONFLICT (domain) DO NOTHING
		`, d, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("import blacklist domain %s: %w", d, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backup transaction: %w", err)
	}

	return nil
}

func (s *PostgresStore) IsArticlePostedToFB(articleID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(context.Background(), `SELECT EXISTS (SELECT 1 FROM facebook_posted_articles WHERE article_id = $1)`, articleID).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) MarkArticleAsPostedToFB(articleID string, fbPostID string) error {
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO facebook_posted_articles (article_id, fb_post_id, posted_at) VALUES ($1, $2, NOW()) ON CONFLICT (article_id) DO NOTHING`, articleID, fbPostID)
	return err
}

func (s *PostgresStore) IsArticlePostedToBSky(articleID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(context.Background(), `SELECT EXISTS (SELECT 1 FROM bluesky_posted_articles WHERE article_id = $1)`, articleID).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) MarkArticleAsPostedToBSky(articleID string, bskyPostURI string) error {
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO bluesky_posted_articles (article_id, bsky_post_uri, posted_at) VALUES ($1, $2, NOW()) ON CONFLICT (article_id) DO UPDATE SET bsky_post_uri = EXCLUDED.bsky_post_uri, posted_at = NOW()`, articleID, bskyPostURI)
	return err
}

func (s *PostgresStore) LockArticleForBSky(articleID string) (bool, error) {
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO bluesky_posted_articles (article_id, bsky_post_uri, posted_at) VALUES ($1, 'pending', NOW())`, articleID)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "23505") || strings.Contains(errStr, "duplicate key") || strings.Contains(errStr, "unique constraint") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *PostgresStore) UnmarkArticleAsPostedToBSky(articleID string) error {
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM bluesky_posted_articles WHERE article_id = $1`, articleID)
	return err
}

func (s *PostgresStore) ListProxies() []Proxy {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, ip, port, username, password, protocol, status, last_checked, latency_ms, last_used, bytes_sent, bytes_received, created_at, updated_at FROM proxies ORDER BY created_at DESC`)
	if err != nil {
		return []Proxy{}
	}
	defer rows.Close()
	var list []Proxy
	for rows.Next() {
		var p Proxy
		err := rows.Scan(&p.ID, &p.IP, &p.Port, &p.Username, &p.Password, &p.Protocol, &p.Status, &p.LastChecked, &p.LatencyMS, &p.LastUsed, &p.BytesSent, &p.BytesReceived, &p.CreatedAt, &p.UpdatedAt)
		if err == nil {
			list = append(list, p)
		}
	}
	return list
}

func (s *PostgresStore) CreateProxy(user *User, input ProxyInput) (*Proxy, error) {
	if user.Role != RoleAdmin && user.Role != RoleEditor {
		return nil, ErrForbidden
	}
	ip := strings.TrimSpace(input.IP)
	protocol := strings.TrimSpace(input.Protocol)
	if ip == "" || input.Port <= 0 || protocol == "" {
		return nil, fmt.Errorf("IP, Port, and Protocol are required")
	}
	now := time.Now().UTC()
	p := Proxy{
		ID:        randomID("prx"),
		IP:        ip,
		Port:      input.Port,
		Username:  strings.TrimSpace(input.Username),
		Password:  strings.TrimSpace(input.Password),
		Protocol:  protocol,
		Status:    "checking",
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO proxies (id, ip, port, username, password, protocol, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		p.ID, p.IP, p.Port, p.Username, p.Password, p.Protocol, p.Status, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *PostgresStore) DeleteProxy(user *User, id string) error {
	if user.Role != RoleAdmin && user.Role != RoleEditor {
		return ErrForbidden
	}
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM proxies WHERE id = $1`, id)
	return err
}

func (s *PostgresStore) UpdateProxyStatus(id string, status string, latency int) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(context.Background(), `UPDATE proxies SET status = $1, latency_ms = $2, last_checked = $3, updated_at = $3 WHERE id = $4`, status, latency, now, id)
	return err
}

func (s *PostgresStore) UpdateProxyLastUsed(id string, lastUsed time.Time) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE proxies SET last_used = $1 WHERE id = $2`, lastUsed, id)
	return err
}

func (s *PostgresStore) GetProxyByID(id string) (*Proxy, error) {
	var p Proxy
	err := s.db.QueryRowContext(context.Background(), `SELECT id, ip, port, username, password, protocol, status, last_checked, latency_ms, last_used, bytes_sent, bytes_received, created_at, updated_at FROM proxies WHERE id = $1`, id).
		Scan(&p.ID, &p.IP, &p.Port, &p.Username, &p.Password, &p.Protocol, &p.Status, &p.LastChecked, &p.LatencyMS, &p.LastUsed, &p.BytesSent, &p.BytesReceived, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (s *PostgresStore) AddProxyBandwidth(id string, sent int64, received int64) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE proxies SET bytes_sent = bytes_sent + $1, bytes_received = bytes_received + $2, updated_at = now() WHERE id = $3`, sent, received, id)
	return err
}

func (s *PostgresStore) ListActiveProxies() []Proxy {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, ip, port, username, password, protocol, status, last_checked, latency_ms, last_used, bytes_sent, bytes_received, created_at, updated_at FROM proxies WHERE status = 'active' ORDER BY latency_ms ASC`)
	if err != nil {
		return []Proxy{}
	}
	defer rows.Close()
	var list []Proxy
	for rows.Next() {
		var p Proxy
		err := rows.Scan(&p.ID, &p.IP, &p.Port, &p.Username, &p.Password, &p.Protocol, &p.Status, &p.LastChecked, &p.LatencyMS, &p.LastUsed, &p.BytesSent, &p.BytesReceived, &p.CreatedAt, &p.UpdatedAt)
		if err == nil {
			list = append(list, p)
		}
	}
	return list
}

func (s *PostgresStore) ListWebshareKeys() ([]cms.WebshareKey, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id, api_key, label, bytes_used, created_at FROM webshare_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []cms.WebshareKey
	for rows.Next() {
		var k cms.WebshareKey
		if err := rows.Scan(&k.ID, &k.APIKey, &k.Label, &k.BytesUsed, &k.CreatedAt); err == nil {
			list = append(list, k)
		}
	}
	return list, nil
}

func (s *PostgresStore) AddWebshareKey(user *User, apiKey, label string) (*cms.WebshareKey, error) {
	if user == nil {
		return nil, ErrUnauthorized
	}
	k := cms.WebshareKey{
		ID:        randomID("wskey"),
		APIKey:    strings.TrimSpace(apiKey),
		Label:     strings.TrimSpace(label),
		BytesUsed: 0,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO webshare_keys (id, api_key, label, bytes_used, created_at) VALUES ($1, $2, $3, $4, $5)`, k.ID, k.APIKey, k.Label, k.BytesUsed, k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (s *PostgresStore) DeleteWebshareKey(user *User, id string) error {
	if user == nil {
		return ErrUnauthorized
	}
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM webshare_keys WHERE id = $1`, id)
	return err
}

func (s *PostgresStore) UpdateWebshareKeyBandwidth(id string, bytesUsed int64) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE webshare_keys SET bytes_used = $1 WHERE id = $2`, bytesUsed, id)
	return err
}

func (s *PostgresStore) GetArticleChartStats(ctx context.Context, user *cms.User, filter string) ([]cms.ChartDataPoint, error) {
	var query string
	var args []any

	authFilter := ""
	if user != nil && user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		authFilter = ` AND author_id = $1 `
		args = append(args, user.ID)
	}

	switch filter {
	case "day":
		query = `
			SELECT TO_CHAR(created_at, 'YYYY-MM-DD') AS label, COUNT(*) AS val
			FROM articles
			WHERE created_at >= NOW() - INTERVAL '7 days' ` + authFilter + `
			GROUP BY label
			ORDER BY label ASC
		`
	case "week":
		query = `
			SELECT TO_CHAR(created_at, 'YYYY-[W]IW') AS label, COUNT(*) AS val
			FROM articles
			WHERE created_at >= NOW() - INTERVAL '8 weeks' ` + authFilter + `
			GROUP BY label
			ORDER BY label ASC
		`
	case "month":
		query = `
			SELECT TO_CHAR(created_at, 'YYYY-MM') AS label, COUNT(*) AS val
			FROM articles
			WHERE created_at >= NOW() - INTERVAL '12 months' ` + authFilter + `
			GROUP BY label
			ORDER BY label ASC
		`
	case "year":
		query = `
			SELECT TO_CHAR(created_at, 'YYYY') AS label, COUNT(*) AS val
			FROM articles
			WHERE created_at >= NOW() - INTERVAL '5 years' ` + authFilter + `
			GROUP BY label
			ORDER BY label ASC
		`
	default:
		query = `
			SELECT TO_CHAR(created_at, 'YYYY-MM-DD') AS label, COUNT(*) AS val
			FROM articles
			WHERE created_at >= NOW() - INTERVAL '7 days' ` + authFilter + `
			GROUP BY label
			ORDER BY label ASC
		`
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []cms.ChartDataPoint
	for rows.Next() {
		var pt cms.ChartDataPoint
		if err := rows.Scan(&pt.Label, &pt.Value); err != nil {
			return nil, err
		}
		list = append(list, pt)
	}

	if len(list) == 0 {
		list = []cms.ChartDataPoint{
			{Label: "No Data", Value: 0},
		}
	}

	return list, nil
}

func (s *PostgresStore) ToggleArticlePremium(id string) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE articles SET is_premium = NOT is_premium, updated_at = NOW() WHERE id = $1`, id)
	return err
}

var _ appcms.ContentStore = (*PostgresStore)(nil)



