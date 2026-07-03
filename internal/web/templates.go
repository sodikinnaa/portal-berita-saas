package web

import (
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"porta-berita/internal/cms"
)

//go:embed templates
var templateFS embed.FS

//go:embed assets/*
var assetFS embed.FS

func dict(values ...interface{}) (map[string]interface{}, error) {
	if len(values)%2 != 0 {
		return nil, errors.New("invalid dict call")
	}
	dict := make(map[string]interface{}, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, errors.New("dict keys must be strings")
		}
		dict[key] = values[i+1]
	}
	return dict, nil
}

func ParseTemplates() (*template.Template, error) {
	funcs := template.FuncMap{
		"articleHTML": articleHTML,
		"formatDate":  formatDate,
		"paragraphs":  paragraphs,
		"readTime":    readTime,
		"statusLabel": statusLabel,
		"hasPrefix":   strings.HasPrefix,
		"isoDate":     isoDate,
		"isoDateTime": isoDateTime,
		"dict":        dict,
		"articleURL":  articleURL,
		"safeHTML":    safeHTML,
		"proxyURL":    proxyURL,
	}
	return template.New("").Funcs(funcs).ParseFS(templateFS, "templates/common/*.html", "templates/themes/default/*.html")
}

// ParseAllThemes secara otomatis menemukan dan mem-parsing semua tema di bawah templates/themes/
func ParseAllThemes() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"articleHTML": articleHTML,
		"formatDate":  formatDate,
		"paragraphs":  paragraphs,
		"readTime":    readTime,
		"statusLabel": statusLabel,
		"hasPrefix":   strings.HasPrefix,
		"isoDate":     isoDate,
		"isoDateTime": isoDateTime,
		"dict":        dict,
		"articleURL":  articleURL,
		"safeHTML":    safeHTML,
		"proxyURL":    proxyURL,
	}

	themes := make(map[string]*template.Template)

	entries, err := templateFS.ReadDir("templates/themes")
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			themeName := entry.Name()
			pattern := "templates/themes/" + themeName + "/*.html"
			tmpl, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/common/*.html", pattern)
			if err != nil {
				return nil, err
			}
			themes[themeName] = tmpl
		}
	}

	return themes, nil
}

// ParseCustomTemplate mem-parsing teks HTML kustom sebagai template dinamis dengan fungsi bawaan
func ParseCustomTemplate(name, text string) (*template.Template, error) {
	funcs := template.FuncMap{
		"articleHTML": articleHTML,
		"formatDate":  formatDate,
		"paragraphs":  paragraphs,
		"readTime":    readTime,
		"statusLabel": statusLabel,
		"hasPrefix":   strings.HasPrefix,
		"isoDate":     isoDate,
		"isoDateTime": isoDateTime,
		"dict":        dict,
		"articleURL":  articleURL,
		"safeHTML":    safeHTML,
	}
	return template.New(name).Funcs(funcs).Parse(text)
}


func Assets() http.FileSystem {
	assets, err := fs.Sub(assetFS, "assets")
	if err != nil {
		panic(err)
	}
	return http.FS(assets)
}

func formatDate(value *time.Time) string {
	if value == nil {
		return "Belum publish"
	}
	return value.Format("02 Jan 2006")
}

func isoDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func isoDateTime(value time.Time) string {
	return value.Format(time.RFC3339)
}

func paragraphs(value string) []string {
	parts := strings.Split(strings.TrimSpace(value), "\n\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

var (
	dangerousHTMLTagPattern  = regexp.MustCompile(`(?is)<\s*(script|style|iframe|object|embed|form|input|button|textarea|select|option|meta|link)[^>]*>.*?<\s*/\s*(script|style|iframe|object|embed|form|input|button|textarea|select|option|meta|link)\s*>`)
	dangerousVoidTagPattern  = regexp.MustCompile(`(?is)<\s*(script|style|iframe|object|embed|form|input|button|textarea|select|option|meta|link)[^>]*?/?>`)
	dangerousAttrPattern     = regexp.MustCompile(`(?is)\s+on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	dangerousProtocolPattern = regexp.MustCompile(`(?is)\s+(href|src)\s*=\s*("\s*javascript:[^"]*"|'\s*javascript:[^']*'|javascript:[^\s>]+)`)
)

func articleHTML(value string) template.HTML {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "<") || !strings.Contains(value, ">") {
		paragraphs := paragraphs(value)
		out := strings.Builder{}
		for index, paragraph := range paragraphs {
			class := ""
			if index == 0 {
				class = ` class="lead"`
			}
			out.WriteString("<p")
			out.WriteString(class)
			out.WriteString(">")
			out.WriteString(template.HTMLEscapeString(paragraph))
			out.WriteString("</p>")
		}
		return template.HTML(out.String())
	}
	return template.HTML(sanitizeCMSHTML(value))
}

func sanitizeCMSHTML(value string) string {
	value = dangerousHTMLTagPattern.ReplaceAllString(value, "")
	value = dangerousVoidTagPattern.ReplaceAllString(value, "")
	value = dangerousAttrPattern.ReplaceAllString(value, "")
	value = dangerousProtocolPattern.ReplaceAllString(value, "")
	return value
}

func readTime(value string) string {
	words := len(strings.Fields(value))
	minutes := int(math.Ceil(float64(words) / 200.0))
	if minutes < 1 {
		minutes = 1
	}
	return strconv.Itoa(minutes) + " menit baca"
}

func statusLabel(status string) string {
	return strings.ReplaceAll(status, "_", " ")
}

func articleURL(art interface{}, settings map[string]string) string {
	var slug, id string
	var createdAt time.Time

	switch v := art.(type) {
	case cms.Article:
		slug = v.Slug
		id = v.ID
		createdAt = v.CreatedAt
	case *cms.Article:
		if v != nil {
			slug = v.Slug
			id = v.ID
			createdAt = v.CreatedAt
		}
	}

	structure := settings["permalink_structure"]
	if structure == "" {
		structure = "post_name"
	}

	return ResolvePermalink(slug, id, createdAt, structure)
}

func ResolvePermalink(slug string, id string, createdAt time.Time, structure string) string {
	if structure == "" {
		structure = "post_name"
	}

	if structure == "plain" {
		return fmt.Sprintf("/?p=%s", id)
	}

	switch structure {
	case "day_and_name":
		structure = "/%year%/%monthnum%/%day%/%postname%"
	case "month_and_name":
		structure = "/%year%/%monthnum%/%postname%"
	case "numeric":
		structure = "/arsip/%post_id%"
	case "post_name":
		structure = "/%postname%"
	}

	res := structure
	res = strings.ReplaceAll(res, "%year%", createdAt.Format("2006"))
	res = strings.ReplaceAll(res, "%monthnum%", createdAt.Format("01"))
	res = strings.ReplaceAll(res, "%day%", createdAt.Format("02"))
	res = strings.ReplaceAll(res, "%post_id%", id)
	res = strings.ReplaceAll(res, "%postname%", slug)

	if !strings.HasPrefix(res, "/") {
		res = "/" + res
	}
	return res
}

func safeHTML(s string) template.HTML {
	return template.HTML(s)
}

func proxyURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}
	if !strings.HasPrefix(urlStr, "http") {
		return urlStr
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(urlStr))
	return "/cdn/image/" + encoded + ".jpg"
}
