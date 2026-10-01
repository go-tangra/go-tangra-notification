package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-tangra/go-tangra/v4/listquery"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// The browser list endpoints answer the list contract (go-tangra
// specs/032-server-side-tables, contracts/http-list.md): page, page_size,
// sort and order in, {items,total,page,page_size,sort,order} out. A request
// with only the old cursor / limit parameters keeps the old shape
// ({items,next_cursor}) plus total for one release; mixing both styles is
// validation_failed on "cursor". Invalid parameters answer 422
// validation_failed with the parameter name only.

// legacyPage is the pre-032 response of a cursor list.
type legacyPage struct {
	Items      any    `json:"items"`
	NextCursor string `json:"next_cursor"`
	Total      int    `json:"total"`
}

// serveList answers one list request. legacy runs the old cursor path and
// returns its items and next cursor; paged returns one page on the list
// contract (also used, with one row, to count for the legacy shape, so the
// legacy total applies the same filters and visibility). mapErr maps service
// errors to refusals.
func serveList[T any](s *Server, w http.ResponseWriter, r *http.Request, spec listquery.Spec, mapErr func(error) error,
	legacy func() ([]T, string, error), paged func(listquery.Request) (listquery.Page[T], error)) {
	q := r.URL.Query()
	if legacy != nil && listquery.Legacy(q) {
		items, next, err := legacy()
		if err != nil {
			s.fail(w, r, mapErr(err))
			return
		}
		count, err := paged(store.ListRequest(listquery.Request{PageSize: 1}, spec))
		if err != nil {
			s.fail(w, r, mapErr(err))
			return
		}
		if items == nil {
			items = []T{}
		}
		WriteJSON(w, http.StatusOK, legacyPage{Items: items, NextCursor: next, Total: count.Total})
		return
	}
	req, err := listquery.Parse(q, spec)
	if err != nil {
		failParam(w, err)
		return
	}
	pg, err := paged(req)
	if err != nil {
		s.fail(w, r, mapErr(err))
		return
	}
	WriteJSON(w, http.StatusOK, pg)
}

// failParam answers a *listquery.Error as 422 validation_failed {param}.
func failParam(w http.ResponseWriter, err error) {
	param := "page"
	var le *listquery.Error
	if errors.As(err, &le) {
		param = le.Param
	}
	WriteDetail(w, ErrValidation, map[string]any{"param": param})
}

// timeParam reads an optional RFC 3339 query parameter; a malformed value is
// a *listquery.Error naming it.
func timeParam(q url.Values, name string) (time.Time, error) {
	v := q.Get(name)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, &listquery.Error{Param: name}
	}
	return t, nil
}

// window reads the optional from/to parameters of the log and audit lists.
func window(q url.Values) (from, to time.Time, err error) {
	if from, err = timeParam(q, "from"); err != nil {
		return
	}
	to, err = timeParam(q, "to")
	return
}
