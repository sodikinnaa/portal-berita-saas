package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"

	"porta-berita/internal/cms"
)

type apiBookmarkRequest struct {
	ArticleID string `json:"article_id"`
}

// GET /api/v1/bookmarks
func (s *Server) apiGetBookmarks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	user, err := s.authenticateRequestAppUser(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "Unauthorized"})
		return
	}

	bookmarks, err := s.store.GetAppBookmarks(user.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "Gagal mengambil bookmark"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "success",
		"bookmarks": bookmarks,
	})
}

// POST /api/v1/bookmarks
func (s *Server) apiAddBookmark(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	user, err := s.authenticateRequestAppUser(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "Unauthorized"})
		return
	}

	var req apiBookmarkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ArticleID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "Request tidak valid"})
		return
	}

	err = s.store.AddAppBookmark(user.ID, req.ArticleID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "Gagal menyimpan bookmark"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// DELETE /api/v1/bookmarks/{id}
func (s *Server) apiDeleteBookmark(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	user, err := s.authenticateRequestAppUser(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "Unauthorized"})
		return
	}

	articleID := r.PathValue("id")
	if articleID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "ID artikel tidak valid"})
		return
	}

	err = s.store.DeleteAppBookmark(user.ID, articleID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "Gagal menghapus bookmark"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) authenticateRequestAppUser(r *http.Request) (*cms.AppUser, error) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		if cookie, err := r.Cookie(appSessionCookieName); err == nil {
			token = cookie.Value
		}
	}
	if token == "" {
		return nil, cms.ErrUnauthorized
	}
	user, err := s.store.AppUserBySession(token)
	if err != nil {
		return nil, cms.ErrUnauthorized
	}
	return user, nil
}
