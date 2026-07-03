package postgresstore

import (
	"context"
	"os"
	"testing"

	"porta-berita/internal/cms"
)

func setupTestDB(t *testing.T) *PostgresStore {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://portal:portal123@localhost:5432/portal_berita?sslmode=disable"
	}

	store, err := OpenPostgresStore(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}

	return store
}

func TestStore_ArticlesCountAndPagination(t *testing.T) {
	store := setupTestDB(t)
	defer store.Close()

	// 1. Audit logic CountPublishedArticles
	total := store.CountPublishedArticles()
	if total < 0 {
		t.Fatalf("expected count >= 0, got %d", total)
	}

	// 2. Audit logic ListPublishedArticlesPaginated
	articles := store.ListPublishedArticlesPaginated(0, 10)
	if len(articles) > 10 {
		t.Errorf("expected max 10 articles, got %d", len(articles))
	}

	for _, a := range articles {
		if a.Status != cms.ArticlePublished {
			t.Errorf("expected only published articles, got status %s for article %s", a.Status, a.ID)
		}
	}
}

func TestStore_ArticlesFiltering(t *testing.T) {
	store := setupTestDB(t)
	defer store.Close()

	// Ensure we have some base count to work with
	total := store.CountPublishedArticles()
	if total == 0 {
		t.Skip("skipping filter test: no published articles in db")
	}

	articles := store.ListPublishedArticlesPaginated(0, 1)
	if len(articles) == 0 {
		t.Skip("skipping filter test: no articles available")
	}

	testCat := articles[0].Category

	// Test category filtering
	countCat := store.CountPublishedArticlesFiltered(testCat, "")
	if countCat < 1 {
		t.Errorf("expected at least 1 article in category %s", testCat)
	}

	filtered := store.ListPublishedArticlesFiltered(testCat, "", 0, 10)
	if len(filtered) == 0 {
		t.Errorf("expected filtered articles list to have items")
	}

	for _, a := range filtered {
		if a.Category != testCat {
			t.Errorf("expected category %s, got %s", testCat, a.Category)
		}
	}

	// Test search query
	testTitle := articles[0].Title
	searchCount := store.CountPublishedArticlesFiltered("", testTitle)
	if searchCount < 1 {
		t.Errorf("expected at least 1 article matching title %s", testTitle)
	}

	searchFiltered := store.ListPublishedArticlesFiltered("", testTitle, 0, 10)
	if len(searchFiltered) == 0 {
		t.Errorf("expected search results to have items")
	}
}

func TestStore_ArticleBySlug(t *testing.T) {
	store := setupTestDB(t)
	defer store.Close()

	articles := store.ListPublishedArticlesPaginated(0, 1)
	if len(articles) == 0 {
		t.Skip("no articles in db to test slug retrieval")
	}

	testArticle := articles[0]

	found, err := store.ArticleBySlug(testArticle.Slug, false)
	if err != nil {
		t.Fatalf("unexpected error finding article by slug: %v", err)
	}
	if found == nil {
		t.Fatalf("expected to find article with slug %s", testArticle.Slug)
	}
	if found.ID != testArticle.ID {
		t.Errorf("expected article ID %s, got %s", testArticle.ID, found.ID)
	}
}

func TestStore_ArticleBySlugNotFound(t *testing.T) {
	store := setupTestDB(t)
	defer store.Close()

	found, err := store.ArticleBySlug("this-slug-should-not-exist-12345", false)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
	if err != cms.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if found != nil {
		t.Errorf("expected nil article, got %v", found)
	}
}
