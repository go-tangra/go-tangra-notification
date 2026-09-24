package httpapi

import (
	"net/http"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
)

// RegisterGrants mounts the grant and access routes (contracts §permissions).
func (s *Server) RegisterGrants(d GrantDeps) {
	s.MustHandle("GET", Prefix+"/grants", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		out, err := d.Authz.ListGrants(r.Context(), subj, q.Get("resource_type"), q.Get("resource_id"))
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": out})
	})
	s.MustHandle("POST", Prefix+"/grants", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in struct {
			ResourceType string     `json:"resource_type"`
			ResourceID   string     `json:"resource_id"`
			SubjectType  string     `json:"subject_type"`
			SubjectID    string     `json:"subject_id"`
			Relation     string     `json:"relation"`
			ExpiresAt    *time.Time `json:"expires_at"`
		}
		if err := DecodeJSON(r, &in, 0); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Authz.Grant(r.Context(), subj, authz.GrantInput{ResourceType: in.ResourceType, ResourceID: in.ResourceID, SubjectType: in.SubjectType, SubjectID: in.SubjectID, Relation: in.Relation, ExpiresAt: in.ExpiresAt})
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusCreated, out)
	})
	s.MustHandle("POST", Prefix+"/grants/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		if err := d.Authz.Revoke(r.Context(), subj, r.PathValue("id")); err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	s.MustHandle("GET", Prefix+"/access/check", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		d, err := d.Authz.Check(r.Context(), subj, q.Get("resource_type"), q.Get("resource_id"), q.Get("action"))
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"allowed": d.Allowed, "relation": d.Relation, "permissions": d.Permissions})
	})
	s.MustHandle("GET", Prefix+"/access/effective", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		var dec authz.Decision
		if q.Get("subject_type") != "" || q.Get("subject_id") != "" {
			dec, err = d.Authz.EffectiveFor(r.Context(), subj, q.Get("resource_type"), q.Get("resource_id"), q.Get("subject_type"), q.Get("subject_id"))
		} else {
			dec, err = d.Authz.Effective(r.Context(), subj, q.Get("resource_type"), q.Get("resource_id"))
		}
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"relation": dec.Relation, "permissions": dec.Permissions, "grants": dec.Sources})
	})
}
