package httpserver

import (
	"net/http"

	"porta-berita/internal/cms"
)

type writersViewData struct {
	User    *cms.User
	Writers []cms.User
	Error   string
}

func (s *Server) dashboardWriters(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	s.renderTemplate(w, "writers.html", writersViewData{User: user, Writers: s.store.ListWriters(user)})
}

func (s *Server) createWriter(w http.ResponseWriter, r *http.Request) {
	user := userFromRequest(r)
	input, err := parseWriterForm(r)
	if err != nil {
		s.renderTemplate(w, "writers.html", writersViewData{User: user, Writers: s.store.ListWriters(user), Error: "Form tidak valid"})
		return
	}
	if _, err := s.store.CreateWriter(user, input); err != nil {
		s.renderTemplate(w, "writers.html", writersViewData{User: user, Writers: s.store.ListWriters(user), Error: err.Error()})
		return
	}
	http.Redirect(w, r, "/dashboard/users", http.StatusFound)
}

func (s *Server) deleteWriter(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteWriter(userFromRequest(r), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), statusFromError(err))
		return
	}
	http.Redirect(w, r, "/dashboard/users", http.StatusFound)
}

func parseWriterForm(r *http.Request) (cms.WriterInput, error) {
	if err := r.ParseForm(); err != nil {
		return cms.WriterInput{}, err
	}
	return cms.WriterInput{Name: r.FormValue("name"), Email: r.FormValue("email"), Password: r.FormValue("password"), Status: r.FormValue("status")}, nil
}
