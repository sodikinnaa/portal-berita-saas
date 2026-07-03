package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"porta-berita/internal/cms"
)

const appSessionCookieName = "app_session"

type appLoginViewData struct {
	Email     string
	Error     string
	Next      string
	LoggedOut bool
}

// GET /app/login
func (s *Server) appLoginForm(w http.ResponseWriter, r *http.Request) {
	if _, err := s.currentAppUser(r); err == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	next := safeNextPath(r.URL.Query().Get("next"))
	s.renderTemplate(w, "app_login.html", appLoginViewData{
		Email:     "",
		Next:      next,
		LoggedOut: r.URL.Query().Get("logged_out") == "1",
	})
}

// POST /app/login
func (s *Server) appLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderTemplate(w, "app_login.html", appLoginViewData{
			Email: "",
			Error: "Form tidak valid",
			Next:  "/",
		})
		return
	}
	email := r.FormValue("email")
	next := safeNextPath(r.FormValue("next"))
	user, err := s.store.AuthenticateAppUser(email, r.FormValue("password"))
	if err != nil {
		s.renderTemplate(w, "app_login.html", appLoginViewData{
			Email: email,
			Error: "Email atau password salah",
			Next:  next,
		})
		return
	}
	session, err := s.store.CreateAppSession(user.ID, 30*24*time.Hour) // 30 days
	if err != nil {
		http.Error(w, "gagal membuat session", http.StatusInternalServerError)
		return
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     appSessionCookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		Expires:  session.ExpiresAt,
	})
	http.Redirect(w, r, next, http.StatusFound)
}

// POST /app/logout
func (s *Server) appLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(appSessionCookieName); err == nil {
		_ = s.store.DeleteAppSession(cookie.Value)
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     appSessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
	http.Redirect(w, r, "/app/login?logged_out=1", http.StatusFound)
}

type apiLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type apiLoginResponse struct {
	Status string       `json:"status"`
	Token  string       `json:"token,omitempty"`
	User   *cms.AppUser `json:"user,omitempty"`
	Error  string       `json:"error,omitempty"`
}

// POST /api/v1/auth/login
func (s *Server) apiAppLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req apiLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(apiLoginResponse{Status: "error", Error: "Format request tidak valid"})
		return
	}
	user, err := s.store.AuthenticateAppUser(req.Email, req.Password)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(apiLoginResponse{Status: "error", Error: "Email atau password salah"})
		return
	}
	session, err := s.store.CreateAppSession(user.ID, 30*24*time.Hour)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(apiLoginResponse{Status: "error", Error: "Gagal membuat sesi login"})
		return
	}
	_ = json.NewEncoder(w).Encode(apiLoginResponse{
		Status: "success",
		Token:  session.ID,
		User:   user,
	})
}

// POST /api/v1/auth/logout
func (s *Server) apiAppLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		if cookie, err := r.Cookie(appSessionCookieName); err == nil {
			token = cookie.Value
		}
	}
	if token != "" {
		_ = s.store.DeleteAppSession(token)
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) currentAppUser(r *http.Request) (*cms.AppUser, error) {
	cookie, err := r.Cookie(appSessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, cms.ErrUnauthorized
	}
	return s.store.AppUserBySession(cookie.Value)
}

func (s *Server) getLoggedInAppUser(r *http.Request) *cms.AppUser {
	if user, err := s.currentAppUser(r); err == nil {
		return user
	}
	return nil
}
