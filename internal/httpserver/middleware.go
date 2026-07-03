package httpserver

import (
	"bytes"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"porta-berita/internal/web"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(body)
	r.size += n
	return n, err
}

func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}

		log.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"bytes", recorder.size,
			"duration", time.Since(started).String(),
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
	})
}

func recoverPanic(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Error("panic recovered", "error", err, "stack", string(debug.Stack()))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func cacheHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
		next.ServeHTTP(w, r)
	})
}

type slugInfo struct {
	id        string
	createdAt time.Time
}

type permalinkMiddleware struct {
	server *Server
	cache  sync.Map // map[string]slugInfo
}

func (pm *permalinkMiddleware) RewriteHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/dashboard") || strings.HasPrefix(r.URL.Path, "/assets") {
			next.ServeHTTP(w, r)
			return
		}

		settings := pm.server.store.GetSettings()
		structure := settings["permalink_structure"]
		catBase := settings["category_permalink_base"]

		// 1. INCOMING ROUTING FOR CATEGORIES
		// Map /{catBase}/{slug} to /artikel?category={slug}
		if catBase != "" && r.URL.Path != "/" {
			trimmedPath := strings.TrimPrefix(r.URL.Path, "/")
			if strings.HasPrefix(trimmedPath, catBase+"/") {
				catSlug := strings.TrimPrefix(trimmedPath, catBase+"/")
				if catSlug != "" && !strings.Contains(catSlug, "/") {
					// Rewrite the internal request path so the router uses the article handler
					r.URL.Path = "/artikel"
					q := r.URL.Query()
					q.Set("category", catSlug)
					r.URL.RawQuery = q.Encode()
				}
			}
		}

		if (structure == "" || structure == "post_name") && catBase == "" {
			next.ServeHTTP(w, r)
			return
		}

		rec := &responseRecorder{
			ResponseWriter: w,
			body:           &bytes.Buffer{},
		}

		next.ServeHTTP(rec, r)

		bodyBytes := rec.body.Bytes()
		contentType := rec.Header().Get("Content-Type")
		if strings.Contains(contentType, "text/html") {
			// A. Rewrite Article Links
			if structure != "" && structure != "post_name" {
				re := regexp.MustCompile(`/artikel/([a-zA-Z0-9_-]+)`)
				bodyBytes = re.ReplaceAllFunc(bodyBytes, func(match []byte) []byte {
					slug := string(match[9:])
					var info slugInfo
					if val, ok := pm.cache.Load(slug); ok {
						info = val.(slugInfo)
					} else {
						art, err := pm.server.store.ArticleBySlug(slug, false)
						if err != nil {
							return match
						}
						info = slugInfo{id: art.ID, createdAt: art.CreatedAt}
						pm.cache.Store(slug, info)
					}
					
					newURL := web.ResolvePermalink(slug, info.id, info.createdAt, structure)
					return []byte(newURL)
				})
			}

			// B. Rewrite Category Links
			if catBase != "" {
				reCat := regexp.MustCompile(`/artikel\?category=([^"&\s]+)(?:(?:&amp;|&)([^"\s]*))?`)
				bodyBytes = reCat.ReplaceAllFunc(bodyBytes, func(match []byte) []byte {
					groups := reCat.FindSubmatch(match)
					if len(groups) >= 2 {
						catSlug := string(groups[1])
						newLink := "/" + catBase + "/" + catSlug
						if len(groups) >= 3 && len(groups[2]) > 0 {
							newLink += "?" + string(groups[2])
						}
						return []byte(newLink)
					}
					return match
				})
			}
		}

		rec.writeHeaderToOriginal()
		w.Write(bodyBytes)
	})
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
	headerSent bool
}

func (rr *responseRecorder) WriteHeader(code int) {
	if !rr.headerSent {
		rr.statusCode = code
		rr.headerSent = true
	}
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	return rr.body.Write(b)
}

func (rr *responseRecorder) writeHeaderToOriginal() {
	if rr.statusCode == 0 {
		rr.statusCode = http.StatusOK
	}
	rr.ResponseWriter.WriteHeader(rr.statusCode)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-App-Session")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
