package httpserver

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"porta-berita/internal/cms"
)

type dashboardBlacklistViewData struct {
	User       *cms.User
	Domains    []string
	Error      string
	Success    string
	Settings   map[string]string
	Page       int
	TotalPages int
	TotalItems int
	HasPrev    bool
	HasNext    bool
	PrevPage   int
	NextPage   int
	Pages      []int
}

func (s *Server) renderBlacklist(w http.ResponseWriter, r *http.Request, user *cms.User, successMsg, errorMsg string) {
	domains, err := s.store.ListBlacklistedDomains()
	if err != nil {
		domains = []string{}
	}

	// Parse page
	page := 1
	pageStr := r.URL.Query().Get("page")
	if pageStr == "" {
		pageStr = r.FormValue("page")
	}
	if pageStr != "" {
		_, _ = fmt.Sscanf(pageStr, "%d", &page)
	}
	if page < 1 {
		page = 1
	}

	pageSize := 10
	total := len(domains)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}

	paginated := []string{}
	if start < total {
		paginated = domains[start:end]
	}

	pages := getPaginationRange(page, totalPages)

	settings := s.store.GetSettings()

	s.renderTemplate(w, "blacklist.html", dashboardBlacklistViewData{
		User:       user,
		Domains:    paginated,
		Settings:   settings,
		Success:    successMsg,
		Error:      errorMsg,
		Page:       page,
		TotalPages: totalPages,
		TotalItems: total,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
		PrevPage:   page - 1,
		NextPage:   page + 1,
		Pages:      pages,
	})
}

func getPaginationRange(page, totalPages int) []int {
	if totalPages <= 7 {
		pages := make([]int, totalPages)
		for i := 0; i < totalPages; i++ {
			pages[i] = i + 1
		}
		return pages
	}

	var pages []int
	if page <= 4 {
		pages = []int{1, 2, 3, 4, 5, -1, totalPages}
	} else if page >= totalPages-3 {
		pages = []int{1, -1, totalPages - 4, totalPages - 3, totalPages - 2, totalPages - 1, totalPages}
	} else {
		pages = []int{1, -1, page - 1, page, page + 1, -1, totalPages}
	}
	return pages
}

func (s *Server) dashboardBlacklist(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user == nil || user.Role != cms.RoleAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")
	s.renderBlacklist(w, r, user, successMsg, errorMsg)
}

func (s *Server) blacklistAdd(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user == nil || user.Role != cms.RoleAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_ = r.ParseForm()
	domain := strings.TrimSpace(r.FormValue("domain"))
	page := r.FormValue("page")
	if page == "" {
		page = "1"
	}

	// Normalize domain (remove protocol, path if entered)
	if strings.Contains(domain, "://") {
		parsed, err := url.Parse(domain)
		if err == nil {
			domain = parsed.Hostname()
		}
	} else if strings.Contains(domain, "/") {
		parts := strings.Split(domain, "/")
		domain = parts[0]
	}
	domain = strings.TrimSpace(domain)

	if domain == "" {
		http.Redirect(w, r, "/dashboard/blacklist?page="+page+"&error="+url.QueryEscape("Domain tidak boleh kosong"), http.StatusSeeOther)
		return
	}

	err := s.store.AddDomainToBlacklist(domain)
	if err != nil {
		http.Redirect(w, r, "/dashboard/blacklist?page="+page+"&error="+url.QueryEscape("Gagal menambahkan domain ke blacklist: "+err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard/blacklist?page="+page+"&success="+url.QueryEscape(fmt.Sprintf("Domain %s berhasil ditambahkan ke blacklist", domain)), http.StatusSeeOther)
}

func (s *Server) blacklistDelete(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user == nil || user.Role != cms.RoleAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_ = r.ParseForm()
	domain := strings.TrimSpace(r.FormValue("domain"))
	page := r.FormValue("page")
	if page == "" {
		page = "1"
	}

	err := s.store.RemoveDomainFromBlacklist(domain)
	if err != nil {
		http.Redirect(w, r, "/dashboard/blacklist?page="+page+"&error="+url.QueryEscape("Gagal menghapus domain dari blacklist: "+err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard/blacklist?page="+page+"&success="+url.QueryEscape(fmt.Sprintf("Domain %s berhasil dihapus dari blacklist", domain)), http.StatusSeeOther)
}

func (s *Server) blacklistClear(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if user == nil || user.Role != cms.RoleAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	err := s.store.ClearBlacklistedDomains()
	if err != nil {
		http.Redirect(w, r, "/dashboard/blacklist?error="+url.QueryEscape("Gagal mengosongkan blacklist: "+err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard/blacklist?success="+url.QueryEscape("Daftar blacklist berhasil dikosongkan"), http.StatusSeeOther)
}

func (s *Server) blacklistURLHost(targetURL string) {
	if targetURL == "" {
		return
	}
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return
	}
	host := parsed.Hostname()
	if host != "" {
		_ = s.store.AddDomainToBlacklist(host)
		s.log.Info("Automatically blacklisted domain", "domain", host)
	}
}
