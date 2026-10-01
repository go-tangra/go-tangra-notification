package httpapi

import (
	"net/http"

	"github.com/go-tangra/go-tangra/v4/listquery"

	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

type channelBody struct {
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Settings  sealed.Settings `json:"settings"`
	Enabled   bool            `json:"enabled"`
	IsDefault bool            `json:"is_default"`
}

func (b channelBody) input() notify.ChannelInput {
	return notify.ChannelInput{Name: b.Name, Type: b.Type, Settings: b.Settings, Enabled: b.Enabled, IsDefault: b.IsDefault}
}

// RegisterChannels mounts the channel routes (contracts §channels).
func (s *Server) RegisterChannels(d NotifyDeps) {
	s.MustHandle("GET", Prefix+"/channels", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		serveList(s, w, r, store.ChannelList, domainError,
			func() ([]notify.ChannelView, string, error) {
				return d.Channels.List(r.Context(), subj, q.Get("type"), q.Get("cursor"), limitParam(r))
			},
			func(req listquery.Request) (listquery.Page[notify.ChannelView], error) {
				return d.Channels.Page(r.Context(), subj, q.Get("type"), req)
			})
	})
	s.MustHandle("POST", Prefix+"/channels", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in channelBody
		if err := DecodeJSON(r, &in, 0); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Channels.Create(r.Context(), subj, in.input())
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusCreated, out)
	})
	s.MustHandle("GET", Prefix+"/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Channels.Get(r.Context(), subj, r.PathValue("id"))
		if err != nil {
			s.fail(w, r, readError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("PUT", Prefix+"/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in channelBody
		if err := DecodeJSON(r, &in, 0); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Channels.Update(r.Context(), subj, r.PathValue("id"), in.input())
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("POST", Prefix+"/channels/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		if err := d.Channels.Delete(r.Context(), subj, r.PathValue("id")); err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	s.MustHandle("POST", Prefix+"/channels/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in struct {
			Recipient string `json:"recipient"`
		}
		if err := DecodeJSON(r, &in, 0); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Sender.SendTest(r.Context(), subj, r.PathValue("id"), in.Recipient, RequestID(r))
		if err != nil {
			s.fail(w, r, domainError(err))
			return
		}
		if out.Status == "failed" && out.Error == "no_provider" {
			s.fail(w, r, ErrNoProvider)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
}
