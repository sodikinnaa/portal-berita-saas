package jsonstore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "portal.json"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return store
}

func loginWriter(t *testing.T, store *Store) *User {
	t.Helper()
	user, err := store.Authenticate("writer@portal.test", "writer123")
	if err != nil {
		t.Fatalf("Authenticate writer: %v", err)
	}
	return user
}

func loginAdmin(t *testing.T, store *Store) *User {
	t.Helper()
	user, err := store.Authenticate("admin@portal.test", "admin123")
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	return user
}

func TestFeature01AuthLoginPenulis(t *testing.T) {
	store := newTestStore(t)

	user, err := store.Authenticate("writer@portal.test", "writer123")
	if err != nil {
		t.Fatalf("valid writer login failed: %v", err)
	}
	if user.Role != RoleWriter {
		t.Fatalf("expected writer role, got %s", user.Role)
	}

	if _, err := store.Authenticate("writer@portal.test", "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for wrong password, got %v", err)
	}

	session, err := store.CreateSession(user.ID, 24*60*60*1_000_000_000)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fromSession, err := store.UserBySession(session.ID)
	if err != nil {
		t.Fatalf("UserBySession: %v", err)
	}
	if fromSession.ID != user.ID {
		t.Fatalf("expected session user %s, got %s", user.ID, fromSession.ID)
	}

	if err := store.DeleteSession(session.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := store.UserBySession(session.ID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected deleted session unauthorized, got %v", err)
	}
}

func TestFeature02ManajemenArtikel(t *testing.T) {
	store := newTestStore(t)
	writer := loginWriter(t, store)

	article, err := store.CreateArticle(writer, ArticleInput{
		Title:    "Artikel Baru Portal",
		Content:  "Konten draft pendek",
		Category: "Teknologi",
		Status:   ArticleDraft,
	})
	if err != nil {
		t.Fatalf("CreateArticle draft: %v", err)
	}
	if article.Slug != "artikel-baru-portal" {
		t.Fatalf("unexpected slug %s", article.Slug)
	}

	admin := loginAdmin(t, store)
	updated, err := store.UpdateArticle(admin, article.ID, ArticleInput{
		Title:    "Artikel Baru Portal Update",
		Content:  strings.Repeat("Konten publish aman. ", 5),
		Category: "Bisnis",
		Status:   ArticlePublished,
	})
	if err != nil {
		t.Fatalf("UpdateArticle publish: %v", err)
	}
	if updated.Status != ArticlePublished || updated.PublishedAt == nil {
		t.Fatalf("expected published article with published_at, got %#v", updated)
	}

	articles := store.ListArticles(writer)
	if len(articles) < 2 {
		t.Fatalf("expected writer articles, got %d", len(articles))
	}

	if err := store.DeleteArticle(writer, article.ID); err != nil {
		t.Fatalf("DeleteArticle: %v", err)
	}
	if _, err := store.ArticleByID(article.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleted article not found, got %v", err)
	}
}

func TestFeature03PublikasiArtikel(t *testing.T) {
	store := newTestStore(t)
	writer := loginWriter(t, store)

	draft, err := store.CreateArticle(writer, ArticleInput{Title: "Draft Rahasia", Content: "draft", Status: ArticleDraft})
	if err != nil {
		t.Fatalf("CreateArticle draft: %v", err)
	}
	if _, err := store.ArticleBySlug(draft.Slug, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft should not be public, got %v", err)
	}

	admin := loginAdmin(t, store)
	published, err := store.CreateArticle(admin, ArticleInput{Title: "Artikel Publik Database", Content: strings.Repeat("Konten lengkap. ", 8), Category: "Teknologi", Status: ArticlePublished})
	if err != nil {
		t.Fatalf("CreateArticle published: %v", err)
	}
	found, err := store.ArticleBySlug(published.Slug, false)
	if err != nil {
		t.Fatalf("ArticleBySlug published: %v", err)
	}
	if found.ID != published.ID {
		t.Fatalf("expected %s, got %s", published.ID, found.ID)
	}
}

func TestFeature04ManajemenKategori(t *testing.T) {
	store := newTestStore(t)
	admin := loginAdmin(t, store)
	writer := loginWriter(t, store)

	categories := store.ListCategories()
	if len(categories) == 0 {
		t.Fatal("expected seeded categories")
	}

	category, err := store.CreateCategory(admin, CategoryInput{Name: "Ekonomi Kreatif"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if category.Slug != "ekonomi-kreatif" {
		t.Fatalf("unexpected category slug %s", category.Slug)
	}

	article, err := store.CreateArticle(writer, ArticleInput{Title: "Artikel Kategori Baru", Content: "draft", Category: category.Name, Status: ArticleDraft})
	if err != nil {
		t.Fatalf("CreateArticle with category: %v", err)
	}

	if err := store.DeleteCategory(admin, category.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected conflict deleting used category, got %v", err)
	}

	updated, err := store.UpdateCategory(admin, category.ID, CategoryInput{Name: "Ekonomi Digital", Slug: "ekonomi-digital"})
	if err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	if updated.Name != "Ekonomi Digital" {
		t.Fatalf("unexpected updated category %#v", updated)
	}
	storedArticle, err := store.ArticleByID(article.ID)
	if err != nil {
		t.Fatalf("ArticleByID: %v", err)
	}
	if storedArticle.Category != "Ekonomi Digital" {
		t.Fatalf("expected article category updated, got %s", storedArticle.Category)
	}

	if _, err := store.CreateArticle(writer, ArticleInput{Title: "Kategori Tidak Ada", Content: "draft", Category: "Tidak Ada", Status: ArticleDraft}); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation for unknown category, got %v", err)
	}

	if _, err := store.CreateCategory(writer, CategoryInput{Name: "Penulis"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected writer forbidden, got %v", err)
	}
}

func TestFeature05MediaGambar(t *testing.T) {
	store := newTestStore(t)
	writer := loginWriter(t, store)

	media, err := store.CreateMedia(writer, "hero.webp", "Hero.webp", "image/webp", "/uploads/hero.webp", 1234)
	if err != nil {
		t.Fatalf("CreateMedia: %v", err)
	}
	if media.OwnerID != writer.ID || media.URL != "/uploads/hero.webp" {
		t.Fatalf("unexpected media %#v", media)
	}
}

func TestFeature06AdminReviewRole(t *testing.T) {
	store := newTestStore(t)
	writer := loginWriter(t, store)
	admin := loginAdmin(t, store)

	article, err := store.CreateArticle(writer, ArticleInput{Title: "Artikel Review Editor", Content: strings.Repeat("Konten untuk review. ", 5), Status: ArticleDraft})
	if err != nil {
		t.Fatalf("CreateArticle: %v", err)
	}

	submitted, err := store.SubmitArticle(writer, article.ID)
	if err != nil {
		t.Fatalf("SubmitArticle: %v", err)
	}
	if submitted.Status != ArticleSubmitted {
		t.Fatalf("expected submitted, got %s", submitted.Status)
	}

	revision, err := store.RequestRevision(admin, article.ID, "Perbaiki lead artikel")
	if err != nil {
		t.Fatalf("RequestRevision: %v", err)
	}
	if revision.Status != ArticleNeedsRevision || revision.ReviewNote == "" {
		t.Fatalf("expected needs revision with note, got %#v", revision)
	}

	approved, err := store.ApproveArticle(admin, article.ID)
	if err != nil {
		t.Fatalf("ApproveArticle: %v", err)
	}
	if approved.Status != ArticlePublished || approved.PublishedAt == nil || approved.ReviewedBy != admin.ID {
		t.Fatalf("expected published reviewed article, got %#v", approved)
	}

	if _, err := store.ArchiveArticle(writer, article.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("writer should not archive, got %v", err)
	}
}
