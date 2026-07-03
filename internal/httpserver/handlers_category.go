package httpserver

import (
	"net/http"

	"porta-berita/internal/cms"
)

type dashboardCategoriesViewData struct {
	User       *cms.User
	Categories []cms.Category
	Error      string
}

type categoryFormViewData struct {
	User     *cms.User
	Title    string
	Action   string
	Category cms.Category
	Error    string
}

func (s *Server) dashboardCategories(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	s.renderTemplate(w, "dashboard_categories.html", dashboardCategoriesViewData{User: user, Categories: s.store.ListCategories()})
}

func (s *Server) newCategoryForm(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "category_form.html", categoryFormViewData{User: userFromRequest(r), Title: "Tambah Kategori", Action: "/dashboard/categories"})
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	input, err := parseCategoryForm(r)
	if err != nil {
		s.renderTemplate(w, "category_form.html", categoryFormViewData{User: user, Title: "Tambah Kategori", Action: "/dashboard/categories", Error: "Form tidak valid"})
		return
	}
	if _, err := s.store.CreateCategory(user, input); err != nil {
		s.renderTemplate(w, "category_form.html", categoryFormViewData{User: user, Title: "Tambah Kategori", Action: "/dashboard/categories", Category: categoryFromInput(input), Error: err.Error()})
		return
	}
	http.Redirect(w, r, "/dashboard/categories", http.StatusFound)
}

func (s *Server) editCategoryForm(w http.ResponseWriter, r *http.Request) {
	category, err := s.store.CategoryByID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.renderTemplate(w, "category_form.html", categoryFormViewData{User: userFromRequest(r), Title: "Edit Kategori", Action: "/dashboard/categories/" + category.ID, Category: *category})
}

func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	input, err := parseCategoryForm(r)
	if err != nil {
		category, _ := s.store.CategoryByID(r.PathValue("id"))
		s.renderTemplate(w, "category_form.html", categoryFormViewData{User: user, Title: "Edit Kategori", Action: "/dashboard/categories/" + r.PathValue("id"), Category: categoryValueOrEmpty(category), Error: "Form tidak valid"})
		return
	}
	if _, err := s.store.UpdateCategory(user, r.PathValue("id"), input); err != nil {
		category := categoryFromInput(input)
		category.ID = r.PathValue("id")
		s.renderTemplate(w, "category_form.html", categoryFormViewData{User: user, Title: "Edit Kategori", Action: "/dashboard/categories/" + r.PathValue("id"), Category: category, Error: err.Error()})
		return
	}
	http.Redirect(w, r, "/dashboard/categories", http.StatusFound)
}

func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCategory(userFromRequest(r), r.PathValue("id")); err != nil {
		s.renderTemplate(w, "dashboard_categories.html", dashboardCategoriesViewData{User: userFromRequest(r), Categories: s.store.ListCategories(), Error: err.Error()})
		return
	}
	http.Redirect(w, r, "/dashboard/categories", http.StatusFound)
}

func parseCategoryForm(r *http.Request) (cms.CategoryInput, error) {
	if err := r.ParseForm(); err != nil {
		return cms.CategoryInput{}, err
	}
	showInNavbar := r.FormValue("show_in_navbar") == "true" || r.FormValue("show_in_navbar") == "on"
	return cms.CategoryInput{
		Name:         r.FormValue("name"),
		Slug:         r.FormValue("slug"),
		ShowInNavbar: showInNavbar,
	}, nil
}

func categoryFromInput(input cms.CategoryInput) cms.Category {
	return cms.Category{
		Name:         input.Name,
		Slug:         input.Slug,
		ShowInNavbar: input.ShowInNavbar,
	}
}

func categoryValueOrEmpty(category *cms.Category) cms.Category {
	if category == nil {
		return cms.Category{}
	}
	return *category
}

func (s *Server) dashboardCategoriesNavbar(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	s.renderTemplate(w, "dashboard_categories_navbar.html", dashboardCategoriesViewData{
		User:       user,
		Categories: s.store.ListCategories(),
	})
}

func (s *Server) updateCategoriesNavbar(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	if err := r.ParseForm(); err != nil {
		s.renderTemplate(w, "dashboard_categories_navbar.html", dashboardCategoriesViewData{
			User:       user,
			Categories: s.store.ListCategories(),
			Error:      "Form tidak valid",
		})
		return
	}

	categories := s.store.ListCategories()
	selectedIDs := r.Form["navbar_categories"]

	selectedMap := make(map[string]bool)
	for _, id := range selectedIDs {
		selectedMap[id] = true
	}

	for _, cat := range categories {
		shouldShow := selectedMap[cat.ID]
		if cat.ShowInNavbar != shouldShow {
			input := cms.CategoryInput{
				Name:         cat.Name,
				Slug:         cat.Slug,
				ShowInNavbar: shouldShow,
			}
			if _, err := s.store.UpdateCategory(user, cat.ID, input); err != nil {
				s.renderTemplate(w, "dashboard_categories_navbar.html", dashboardCategoriesViewData{
					User:       user,
					Categories: s.store.ListCategories(),
					Error:      "Gagal memperbarui kategori: " + err.Error(),
				})
				return
			}
		}
	}

	http.Redirect(w, r, "/dashboard/categories", http.StatusFound)
}
