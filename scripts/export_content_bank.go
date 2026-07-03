package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"porta-berita/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Article struct {
	ID          string
	AuthorID    string
	Title       string
	Slug        string
	Excerpt     string
	Content     string
	Category    string
	Status      string
	SourceURL   string
	ImageSource string
	PublishedAt sql.NullTime
	CreatedAt   time.Time
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}

	rows, err := db.QueryContext(context.Background(), `
		SELECT id, author_id, title, slug, excerpt, content, category, status, source_url, image_source, published_at, created_at 
		FROM articles 
		WHERE source_url != ''
	`)
	if err != nil {
		log.Fatalf("failed to query articles: %v", err)
	}
	defer rows.Close()

	outputDir := "content_bank"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("failed to create output dir: %v", err)
	}

	count := 0
	for rows.Next() {
		var a Article
		err := rows.Scan(
			&a.ID,
			&a.AuthorID,
			&a.Title,
			&a.Slug,
			&a.Excerpt,
			&a.Content,
			&a.Category,
			&a.Status,
			&a.SourceURL,
			&a.ImageSource,
			&a.PublishedAt,
			&a.CreatedAt,
		)
		if err != nil {
			log.Fatalf("failed to scan row: %v", err)
		}

		pubStr := "N/A"
		if a.PublishedAt.Valid {
			pubStr = a.PublishedAt.Time.Format(time.RFC3339)
		}

		// Prepare YAML frontmatter and markdown body
		sb := strings.Builder{}
		sb.WriteString("---\n")
		sb.WriteString(fmt.Sprintf("id: %s\n", a.ID))
		sb.WriteString(fmt.Sprintf("title: %q\n", a.Title))
		sb.WriteString(fmt.Sprintf("slug: %s\n", a.Slug))
		sb.WriteString(fmt.Sprintf("source_url: %s\n", a.SourceURL))
		sb.WriteString(fmt.Sprintf("image_source: %q\n", a.ImageSource))
		sb.WriteString(fmt.Sprintf("category: %s\n", a.Category))
		sb.WriteString(fmt.Sprintf("status: %s\n", a.Status))
		sb.WriteString(fmt.Sprintf("published_at: %s\n", pubStr))
		sb.WriteString(fmt.Sprintf("created_at: %s\n", a.CreatedAt.Format(time.RFC3339)))
		sb.WriteString("---\n\n")
		sb.WriteString(fmt.Sprintf("# %s\n\n", a.Title))
		if a.Excerpt != "" {
			sb.WriteString(fmt.Sprintf("> %s\n\n", a.Excerpt))
		}
		sb.WriteString(a.Content)
		sb.WriteString("\n")

		// Create filename: YYYY-MM-DD-[slug].md
		datePrefix := a.CreatedAt.Format("2006-01-02")
		filename := fmt.Sprintf("%s-%s.md", datePrefix, a.Slug)
		filePath := filepath.Join(outputDir, filename)

		if err := os.WriteFile(filePath, []byte(sb.String()), 0644); err != nil {
			log.Fatalf("failed to write file %s: %v", filePath, err)
		}
		fmt.Printf("Exported: %s -> %s\n", a.Title, filePath)
		count++
	}

	if err := rows.Err(); err != nil {
		log.Fatalf("rows error: %v", err)
	}

	fmt.Printf("\nSuccess: Exported %d rewrite articles to '%s/' content bank.\n", count, outputDir)
}
