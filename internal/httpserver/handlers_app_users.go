package httpserver

import (
	"net/http"

	"porta-berita/internal/cms"
)

type appUsersViewData struct {
	User     *cms.User
	AppUsers []cms.AppUser
	Error    string
}

func (s *Server) dashboardAppUsers(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	s.renderTemplate(w, "app_users.html", appUsersViewData{User: user, AppUsers: s.store.ListAppUsers(user)})
}

func (s *Server) createAppUser(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	input, err := parseAppUserForm(r)
	if err != nil {
		s.renderTemplate(w, "app_users.html", appUsersViewData{User: user, AppUsers: s.store.ListAppUsers(user), Error: "Form tidak valid"})
		return
	}
	if _, err := s.store.CreateAppUser(user, input); err != nil {
		s.renderTemplate(w, "app_users.html", appUsersViewData{User: user, AppUsers: s.store.ListAppUsers(user), Error: err.Error()})
		return
	}
	http.Redirect(w, r, "/dashboard/app-users", http.StatusFound)
}

func (s *Server) deleteAppUser(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAppUser(userFromRequest(r), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), statusFromError(err))
		return
	}
	http.Redirect(w, r, "/dashboard/app-users", http.StatusFound)
}

func parseAppUserForm(r *http.Request) (cms.AppUserInput, error) {
	if err := r.ParseForm(); err != nil {
		return cms.AppUserInput{}, err
	}
	return cms.AppUserInput{
		Name:     r.FormValue("name"),
		Email:    r.FormValue("email"),
		Password: r.FormValue("password"),
		Status:   r.FormValue("status"),
	}, nil
}
