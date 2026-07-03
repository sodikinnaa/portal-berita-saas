package httpserver

import (
	"net/http"
	"strings"

	"porta-berita/internal/cms"
	"porta-berita/internal/web"
)

type permalinkSettingsViewData struct {
	User               *cms.User
	PermalinkStructure string
	CategoryBase       string
	Success            string
	Error              string
}

func (s *Server) dashboardPermalinkSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	settings := s.store.GetSettings()
	structure := settings["permalink_structure"]
	if structure == "" {
		structure = "post_name" // default
	}
	
	catBase := settings["category_permalink_base"]
	if catBase == "" {
		catBase = "kategori" // default
	}

	s.renderTemplate(w, "options_permalink.html", permalinkSettingsViewData{
		User:               user,
		PermalinkStructure: structure,
		CategoryBase:       catBase,
	})
}

func (s *Server) updatePermalinkSettings(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user.Role != cms.RoleAdmin && user.Role != cms.RoleEditor {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	structure := r.FormValue("permalink_structure")
	if structure == "custom" {
		structure = r.FormValue("permalink_structure_custom")
	}
	
	catBase := strings.TrimSpace(r.FormValue("category_permalink_base"))
	if catBase == "" {
		catBase = "kategori"
	}

	// Save to settings
	settings := s.store.GetSettings()
	settings["permalink_structure"] = structure
	settings["category_permalink_base"] = catBase

	err := s.store.UpdateSettings(user, settings)
	if err != nil {
		s.renderTemplate(w, "options_permalink.html", permalinkSettingsViewData{
			User:               user,
			PermalinkStructure: structure,
			CategoryBase:       catBase,
			Error:              "Gagal menyimpan pengaturan: " + err.Error(),
		})
		return
	}

	s.renderTemplate(w, "options_permalink.html", permalinkSettingsViewData{
		User:               user,
		PermalinkStructure: structure,
		CategoryBase:       catBase,
		Success:            "Pengaturan permalink berhasil diperbarui.",
	})
}

func (s *Server) articleByID(w http.ResponseWriter, r *http.Request, id string) {
	article, err := s.store.ArticleByID(id)
	if err != nil || article == nil {
		http.NotFound(w, r)
		return
	}

	settings := s.store.GetSettings()
	structure := settings["permalink_structure"]
	if structure == "" {
		structure = "post_name"
	}

	canonicalPath := web.ResolvePermalink(article.Slug, article.ID, article.CreatedAt, structure)
	if structure == "plain" {
		if r.URL.Path != "/" || r.URL.Query().Get("p") != article.ID {
			http.Redirect(w, r, canonicalPath, http.StatusMovedPermanently)
			return
		}
	} else {
		if r.URL.Path != canonicalPath {
			http.Redirect(w, r, canonicalPath, http.StatusMovedPermanently)
			return
		}
	}

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	data := articleViewData{
		Article:        *article,
		AuthorName:     s.store.UserName(article.AuthorID),
		Related:        s.store.ListRandomPublishedArticles(4, article.ID),
		Popular:        limitArticles(s.store.ListPublishedArticles(4), 4),
		EditorPicks:    limitArticles(s.store.ListPublishedArticles(4), 4),
		Categories:     s.store.ListCategories(),
		CategoryFilter: article.Category,
		Settings:       settings,
		BaseURL:        baseURL,
	}
	s.renderTemplate(w, "article.html", data)
}
