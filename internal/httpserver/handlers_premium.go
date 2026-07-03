package httpserver

import (
	"net/http"
	"strconv"
	"strings"

	"porta-berita/internal/cms"
)

type dashboardPremiumViewData struct {
	User         *cms.User
	Articles     []cms.Article
	Total        int
	Page         int
	PageSize     int
	TotalPages   int
	HasPrev      bool
	HasNext      bool
	PrevPage     int
	NextPage     int
	Pages        []int
	SearchQuery  string
	Filter       string
	TotalAll     int
	TotalPremium int
	TotalReguler int
}

func (s *Server) dashboardPremium(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	allArticles := s.store.ListArticles(user)

	searchQuery := r.URL.Query().Get("s")
	filterType := r.URL.Query().Get("filter")

	var searchFiltered []cms.Article
	for _, a := range allArticles {
		if a.Status == "published" {
			if searchQuery != "" {
				titleMatch := strings.Contains(strings.ToLower(a.Title), strings.ToLower(searchQuery))
				contentMatch := strings.Contains(strings.ToLower(a.Content), strings.ToLower(searchQuery))
				categoryMatch := strings.Contains(strings.ToLower(a.Category), strings.ToLower(searchQuery))
				if !titleMatch && !contentMatch && !categoryMatch {
					continue
				}
			}
			searchFiltered = append(searchFiltered, a)
		}
	}

	totalAll := len(searchFiltered)
	totalPremium := 0
	totalReguler := 0
	for _, a := range searchFiltered {
		if a.IsPremium {
			totalPremium++
		} else {
			totalReguler++
		}
	}

	var statusFiltered []cms.Article
	for _, a := range searchFiltered {
		if filterType == "premium" && !a.IsPremium {
			continue
		}
		if filterType == "reguler" && a.IsPremium {
			continue
		}
		statusFiltered = append(statusFiltered, a)
	}

	const pageSize = 10
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	total := len(statusFiltered)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	if page > totalPages {
		page = totalPages
	}

	offset := (page - 1) * pageSize
	limit := offset + pageSize
	if limit > total {
		limit = total
	}

	var paginatedArticles []cms.Article
	if total > 0 && offset < total {
		paginatedArticles = statusFiltered[offset:limit]
	}

	pages := make([]int, 0, totalPages)
	for i := 1; i <= totalPages; i++ {
		pages = append(pages, i)
	}

	data := dashboardPremiumViewData{
		User:         user,
		Articles:     paginatedArticles,
		Total:        total,
		Page:         page,
		PageSize:     pageSize,
		TotalPages:   totalPages,
		HasPrev:      page > 1,
		HasNext:      page < totalPages,
		PrevPage:     page - 1,
		NextPage:     page + 1,
		Pages:        pages,
		SearchQuery:  searchQuery,
		Filter:       filterType,
		TotalAll:     totalAll,
		TotalPremium: totalPremium,
		TotalReguler: totalReguler,
	}

	s.renderTemplate(w, "dashboard_premium.html", data)
}

func (s *Server) togglePremiumStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing article id", http.StatusBadRequest)
		return
	}

	if err := s.store.ToggleArticlePremium(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pageStr := r.URL.Query().Get("page")
	searchQuery := r.URL.Query().Get("s")
	filterType := r.URL.Query().Get("filter")
	redirectURL := "/dashboard/premium"
	
	var params []string
	if pageStr != "" {
		params = append(params, "page="+pageStr)
	}
	if searchQuery != "" {
		params = append(params, "s="+searchQuery)
	}
	if filterType != "" {
		params = append(params, "filter="+filterType)
	}
	
	if len(params) > 0 {
		redirectURL += "?" + strings.Join(params, "&")
	}

	http.Redirect(w, r, redirectURL, http.StatusFound)
}
