package httpapi

import (
	"net/http"

	"github.com/go-freya/freya/services/notification/internal/notify"
)

type templateBody struct {
	Name      string   `json:"name"`
	ChannelID string   `json:"channel_id"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	Variables []string `json:"variables"`
	IsDefault bool     `json:"is_default"`
}

func (b templateBody) input() notify.TemplateInput {
	return notify.TemplateInput{Name: b.Name, ChannelID: b.ChannelID, Subject: b.Subject, Body: b.Body, Variables: b.Variables, IsDefault: b.IsDefault}
}

// MaxTemplateBody bounds template create/update/preview bodies (256 KiB body + slack).
const MaxTemplateBody = 300 << 10

// RegisterTemplates mounts the template routes (contracts §templates).
func (s *Server) RegisterTemplates(d NotifyDeps) {
	s.MustHandle("GET", Prefix+"/templates", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		var channelID *string
		if c := q.Get("channel_id"); c != "" {
			channelID = &c
		}
		items, next, err := d.Templates.List(r.Context(), subj, channelID, q.Get("q"), q.Get("cursor"), limitParam(r))
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	})
	s.MustHandle("POST", Prefix+"/templates", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in templateBody
		if err := DecodeJSON(r, &in, MaxTemplateBody); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Templates.Create(r.Context(), subj, in.input())
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusCreated, out)
	})
	s.MustHandle("GET", Prefix+"/templates/{id}", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Templates.Get(r.Context(), subj, r.PathValue("id"))
		if err != nil {
			s.fail(w, r, readError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("PUT", Prefix+"/templates/{id}", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in templateBody
		if err := DecodeJSON(r, &in, MaxTemplateBody); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Templates.Update(r.Context(), subj, r.PathValue("id"), in.input())
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("POST", Prefix+"/templates/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		if err := d.Templates.Delete(r.Context(), subj, r.PathValue("id")); err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	s.MustHandle("POST", Prefix+"/templates/preview", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in struct {
			TemplateID  string            `json:"template_id"`
			ChannelType string            `json:"channel_type"`
			Subject     string            `json:"subject"`
			Body        string            `json:"body"`
			Variables   []string          `json:"variables"`
			Values      map[string]string `json:"values"`
		}
		if err := DecodeJSON(r, &in, MaxTemplateBody); err != nil {
			Fail(w, r, nil, err)
			return
		}
		subject, body, err := d.Templates.Preview(r.Context(), subj, notify.PreviewInput{TemplateID: in.TemplateID, ChannelType: in.ChannelType, Subject: in.Subject, Body: in.Body, Variables: in.Variables, Values: in.Values})
		if err != nil {
			s.fail(w, r, readError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"rendered_subject": subject, "rendered_body": body})
	})
}
