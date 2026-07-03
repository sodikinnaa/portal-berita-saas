package httpserver

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// GET /api/v1/proxy-image?url=...
func (s *Server) apiProxyImage(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for this endpoint
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	targetURLStr := r.URL.Query().Get("url")
	if targetURLStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Missing url parameter"))
		return
	}

	targetURL, err := url.Parse(targetURLStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Invalid url parameter"))
		return
	}

	// Validate scheme
	if targetURL.Scheme != "http" && targetURL.Scheme != "https" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("URL scheme must be http or https"))
		return
	}

	// Create request
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURLStr, nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Set user agent to resemble a browser so target servers don't block us
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("Failed to fetch target image"))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	// Forward response headers
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", "image/jpeg") // fallback
	}

	// Cache control for 1 day
	w.Header().Set("Cache-Control", "public, max-age=86400")

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

// GET /cdn/image/{id}
func (s *Server) seoProxyImage(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for this endpoint
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	serveFallback := func() {
		fallbackSVG := `<svg xmlns="http://www.w3.org/2000/svg" width="800" height="400" viewBox="0 0 800 400"><rect width="800" height="400" fill="#f3f4f6"/><text x="50%" y="50%" font-family="sans-serif" font-size="24" fill="#9ca3af" text-anchor="middle" dominant-baseline="middle">Gambar Tidak Tersedia</text></svg>`
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=3600") // Cache fallback for 1 hour
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(fallbackSVG))
	}

	id := r.PathValue("id")
	if id == "" {
		serveFallback()
		return
	}

	// Remove standard image extensions used for SEO
	exts := []string{".jpg", ".jpeg", ".png", ".webp", ".gif"}
	for _, ext := range exts {
		if strings.HasSuffix(id, ext) {
			id = strings.TrimSuffix(id, ext)
			break
		}
	}

	decodedBytes, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		serveFallback()
		return
	}

	targetURLStr := string(decodedBytes)
	targetURL, err := url.Parse(targetURLStr)
	if err != nil {
		serveFallback()
		return
	}

	// Validate scheme
	if targetURL.Scheme != "http" && targetURL.Scheme != "https" {
		serveFallback()
		return
	}

	// Create request
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURLStr, nil)
	if err != nil {
		serveFallback()
		return
	}

	// Set user agent to resemble a browser so target servers don't block us
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		serveFallback()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		serveFallback()
		return
	}

	// Forward response headers
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", "image/jpeg") // fallback
	}

	// Cache control for 1 day
	w.Header().Set("Cache-Control", "public, max-age=86400")

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

