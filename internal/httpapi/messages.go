package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/inbox"
	"github.com/go-tangra/go-tangra-notification/v4/internal/messages"
)

// MessageDeps are the services behind the category, message and inbox routes.
type MessageDeps struct {
	Messages *messages.Service
	Inbox    *inbox.Service
	Perms    PermissionChecker
}

// MaxMessageBody bounds message bodies (64 KiB content + recipients + slack).
const MaxMessageBody = 600 << 10

func messageError(err error) error {
	var ve *messages.ValidationError
	switch {
	case errors.As(err, &ve):
		return &DetailError{Err: ErrValidation, Detail: ve.Detail}
	case errors.Is(err, messages.ErrState):
		return &DetailError{Err: ErrConflict, Detail: map[string]any{"message": "not allowed in this status"}}
	case errors.Is(err, messages.ErrInUse):
		return &DetailError{Err: ErrConflict, Detail: map[string]any{"messages": err.Error()}}
	case errors.Is(err, inbox.ErrInput):
		return ErrValidation
	}
	return domainError(err)
}

// RegisterMessages mounts categories, messages and inbox (contracts §categories, §messages, §inbox).
func (s *Server) RegisterMessages(d MessageDeps) {
	manage := func(r *http.Request) bool { return hasPermission(r, d.Perms, "messages:manage") }
	// ---- categories
	s.MustHandle("GET", Prefix+"/categories", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Messages.ListCategories(r.Context(), subj)
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": out})
	})
	s.MustHandle("POST", Prefix+"/categories", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Sort        int    `json:"sort"`
		}
		if err := DecodeJSON(r, &in, 0); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Messages.CreateCategory(r.Context(), subj, messages.CategoryInput{Name: in.Name, Description: in.Description, Sort: in.Sort})
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusCreated, out)
	})
	s.MustHandle("PUT", Prefix+"/categories/{id}", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Sort        int    `json:"sort"`
		}
		if err := DecodeJSON(r, &in, 0); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Messages.UpdateCategory(r.Context(), subj, r.PathValue("id"), messages.CategoryInput{Name: in.Name, Description: in.Description, Sort: in.Sort})
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("POST", Prefix+"/categories/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		if err := d.Messages.DeleteCategory(r.Context(), subj, r.PathValue("id")); err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// ---- messages
	type messageBody struct {
		Title       string              `json:"title"`
		Content     string              `json:"content"`
		Type        string              `json:"type"`
		CategoryID  *string             `json:"category_id"`
		Recipients  messages.Recipients `json:"recipients"`
		ScheduledAt *time.Time          `json:"scheduled_at"`
	}
	toInput := func(b messageBody) messages.MessageInput {
		in := messages.MessageInput{Title: b.Title, Content: b.Content, Type: b.Type, Recipients: b.Recipients, ScheduledAt: b.ScheduledAt}
		if b.CategoryID != nil {
			in.CategoryID = *b.CategoryID
		}
		return in
	}
	s.MustHandle("GET", Prefix+"/messages", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		items, next, err := d.Messages.List(r.Context(), subj, messages.ListFilter{Status: q.Get("status"), CategoryID: q.Get("category_id"), Q: q.Get("q"), Cursor: q.Get("cursor"), Limit: limitParam(r)}, manage(r))
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	})
	s.MustHandle("POST", Prefix+"/messages", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in messageBody
		if err := DecodeJSON(r, &in, MaxMessageBody); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Messages.Create(r.Context(), subj, toInput(in))
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusCreated, out)
	})
	s.MustHandle("GET", Prefix+"/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Messages.Get(r.Context(), subj, r.PathValue("id"), manage(r))
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("PUT", Prefix+"/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in messageBody
		if err := DecodeJSON(r, &in, MaxMessageBody); err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Messages.Update(r.Context(), subj, r.PathValue("id"), toInput(in), manage(r))
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("POST", Prefix+"/messages/{id}/send", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Messages.Send(r.Context(), subj, r.PathValue("id"), manage(r))
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	transition := func(path string, fn func(r *http.Request, subj subjectsT, id string) (any, error)) {
		s.MustHandle("POST", Prefix+"/messages/{id}/"+path, func(w http.ResponseWriter, r *http.Request) {
			subj, err := subjects(r)
			if err != nil {
				Fail(w, r, nil, err)
				return
			}
			out, err := fn(r, subj, r.PathValue("id"))
			if err != nil {
				s.fail(w, r, messageError(err))
				return
			}
			if out == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			WriteJSON(w, http.StatusOK, out)
		})
	}
	transition("cancel", func(r *http.Request, subj subjectsT, id string) (any, error) {
		return d.Messages.Cancel(r.Context(), subj, id, manage(r))
	})
	transition("revoke", func(r *http.Request, subj subjectsT, id string) (any, error) {
		return d.Messages.Revoke(r.Context(), subj, id, manage(r))
	})
	transition("archive", func(r *http.Request, subj subjectsT, id string) (any, error) {
		return d.Messages.Archive(r.Context(), subj, id, manage(r))
	})
	transition("remove", func(r *http.Request, subj subjectsT, id string) (any, error) {
		return nil, d.Messages.Delete(r.Context(), subj, id, manage(r))
	})
	s.MustHandle("GET", Prefix+"/messages/{id}/recipients", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		items, next, err := d.Messages.Recipients(r.Context(), subj, r.PathValue("id"), q.Get("status"), q.Get("cursor"), limitParam(r), manage(r))
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	})
	// ---- inbox
	s.MustHandle("GET", Prefix+"/inbox", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		page, err := d.Inbox.List(r.Context(), subj, q.Get("status"), q.Get("cursor"), limitParam(r))
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	s.MustHandle("GET", Prefix+"/inbox/unread", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		n, err := d.Inbox.Unread(r.Context(), subj)
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]int64{"unread": n})
	})
	s.MustHandle("GET", Prefix+"/inbox/{id}", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		out, err := d.Inbox.Read(r.Context(), subj, r.PathValue("id"))
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	s.MustHandle("POST", Prefix+"/inbox/status", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in struct {
			IDs    []string `json:"ids"`
			Status string   `json:"status"`
		}
		if err := DecodeJSON(r, &in, 0); err != nil {
			Fail(w, r, nil, err)
			return
		}
		updated, unread, err := d.Inbox.SetStatus(r.Context(), subj, in.IDs, in.Status)
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]int64{"updated": updated, "unread": unread})
	})
	s.MustHandle("POST", Prefix+"/inbox/remove", func(w http.ResponseWriter, r *http.Request) {
		subj, err := subjects(r)
		if err != nil {
			Fail(w, r, nil, err)
			return
		}
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := DecodeJSON(r, &in, 0); err != nil {
			Fail(w, r, nil, err)
			return
		}
		updated, unread, err := d.Inbox.Remove(r.Context(), subj, in.IDs)
		if err != nil {
			s.fail(w, r, messageError(err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]int64{"updated": updated, "unread": unread})
	})
}
