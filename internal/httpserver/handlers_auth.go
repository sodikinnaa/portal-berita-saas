package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"

	"porta-berita/internal/cms"
)

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if _, err := s.currentUser(r); err == nil {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	next := safeNextPath(r.URL.Query().Get("next"))
	isProd := s.cfg.Environment == "production"
	s.renderTemplate(w, "login.html", loginViewData{
		Email:        "",
		Next:         next,
		LoggedOut:    r.URL.Query().Get("logged_out") == "1",
		IsProduction: isProd,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	isProd := s.cfg.Environment == "production"
	if err := r.ParseForm(); err != nil {
		s.renderTemplate(w, "login.html", loginViewData{
			Email:        "",
			Error:        "Form tidak valid",
			Next:         "/dashboard",
			IsProduction: isProd,
		})
		return
	}
	email := r.FormValue("email")
	next := safeNextPath(r.FormValue("next"))
	user, err := s.store.Authenticate(email, r.FormValue("password"))
	if err != nil {
		s.renderTemplate(w, "login.html", loginViewData{
			Email:        email,
			Error:        "Email atau password salah",
			Next:         next,
			IsProduction: isProd,
		})
		return
	}
	session, err := s.store.CreateSession(user.ID, s.cfg.SessionTTL)
	if err != nil {
		http.Error(w, "gagal membuat session", http.StatusInternalServerError)
		return
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		Expires:  session.ExpiresAt,
	})
	http.Redirect(w, r, next, http.StatusFound)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = s.store.DeleteSession(cookie.Value)
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
	http.Redirect(w, r, "/login?logged_out=1", http.StatusFound)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	articles := s.store.ListArticles(user)
	data := dashboardViewData{User: user, Recent: limitArticles(articles, 5), Settings: s.store.GetSettings()}
	data.Total, data.Draft, data.Submitted, data.Published, data.Today = articleStats(articles)
	s.renderTemplate(w, "dashboard.html", data)
}

func (s *Server) apiDashboardChartStats(w http.ResponseWriter, r *http.Request) {
	filter := strings.TrimSpace(r.URL.Query().Get("range"))
	if filter == "" {
		filter = "day"
	}

	user := userFromRequest(r)
	pts, err := s.store.GetArticleChartStats(r.Context(), user, filter)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pts)
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	s.renderTemplate(w, "profile.html", profileViewData{User: user, Profile: user, Success: r.URL.Query().Get("saved")})
}

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	input, err := parseProfileForm(r)
	if err != nil {
		s.renderTemplate(w, "profile.html", profileViewData{User: user, Profile: user, Error: "Form tidak valid"})
		return
	}
	updated, err := s.store.UpdateProfile(user, input)
	if err != nil {
		s.renderTemplate(w, "profile.html", profileViewData{User: user, Profile: user, Error: err.Error()})
		return
	}
	s.renderTemplate(w, "profile.html", profileViewData{User: updated, Profile: updated, Success: "Profil berhasil diperbarui"})
}

func (s *Server) updateProfileAvatar(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseMultipartForm(3 << 20); err != nil {
		s.renderTemplate(w, "profile.html", profileViewData{User: user, Profile: user, Error: "Foto terlalu besar atau form invalid"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.renderTemplate(w, "profile.html", profileViewData{User: user, Profile: user, Error: "Foto wajib diisi"})
		return
	}
	defer file.Close()
	media, err := s.saveUpload(user, file, header)
	if err != nil {
		s.renderTemplate(w, "profile.html", profileViewData{User: user, Profile: user, Error: err.Error()})
		return
	}
	updated, err := s.store.UpdateProfile(user, cms.ProfileInput{Name: user.Name, Bio: user.Bio, Phone: user.Phone, AvatarURL: media.URL})
	if err != nil {
		s.renderTemplate(w, "profile.html", profileViewData{User: user, Profile: user, Error: err.Error()})
		return
	}
	s.renderTemplate(w, "profile.html", profileViewData{User: updated, Profile: updated, Success: "Foto profil berhasil diperbarui"})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.currentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login?next="+r.URL.EscapedPath(), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	}
}

func (s *Server) currentUser(r *http.Request) (*cms.User, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, cms.ErrUnauthorized
	}
	return s.store.UserBySession(cookie.Value)
}

func parseProfileForm(r *http.Request) (cms.ProfileInput, error) {
	if err := r.ParseForm(); err != nil {
		return cms.ProfileInput{}, err
	}
	return cms.ProfileInput{Name: r.FormValue("name"), Bio: r.FormValue("bio"), Phone: r.FormValue("phone"), AvatarURL: r.FormValue("avatar_url")}, nil
}
