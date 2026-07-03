package dbassets

import (
	"embed"
	"io/fs"
	"sort"
)

// FS contains SQL migration and seeder files embedded into the Go binary.
// This keeps Docker/distroless builds self-contained while preserving a
// Laravel-like database folder structure in the repository.
//
//go:embed migrations/*.sql seeders/*.sql
var FS embed.FS

func MigrationFiles() ([]string, error) {
	return globSorted("migrations/*.sql")
}

func SeederFiles() ([]string, error) {
	return globSorted("seeders/*.sql")
}

func ReadFile(name string) ([]byte, error) {
	return FS.ReadFile(name)
}

func globSorted(pattern string) ([]string, error) {
	matches, err := fs.Glob(FS, pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}
