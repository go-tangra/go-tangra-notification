package memstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-tangra/go-tangra/v4/listquery"

	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// The Page* doubles apply the same filters, visibility, sort fields (by
// public name) and clamping as the SQL page queries (feature 032).

func window[T any](items []T, req listquery.Request, key func(T, string) any, tie func(T) string) ([]T, int, listquery.Request) {
	listquery.SortSlice(items, req, key, tie)
	page, total, applied := listquery.Window(items, req)
	return page, total, applied
}

func (m *Store) PageChannels(_ context.Context, tid, typ string, vis store.Visible, req listquery.Request) ([]store.Channel, int, listquery.Request, error) {
	if err := m.fail("PageChannels"); err != nil {
		return nil, 0, req, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Channel
	for _, c := range m.Channels {
		if c.TenantID == tid && (typ == "" || c.Type == typ) && vis.Allows(c.ID) {
			c.TemplateCount = m.templateCount(c.ID)
			out = append(out, c)
		}
	}
	page, total, applied := window(out, req, func(c store.Channel, f string) any {
		switch f {
		case "type":
			return c.Type
		case "created_at":
			return c.CreatedAt
		}
		return c.Name
	}, func(c store.Channel) string { return c.ID })
	return page, total, applied, nil
}

func (m *Store) PageTemplates(_ context.Context, tid string, ch *string, q string, vis store.Visible, req listquery.Request) ([]store.Template, int, listquery.Request, error) {
	if err := m.fail("PageTemplates"); err != nil {
		return nil, 0, req, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Template
	for _, t := range m.Templates {
		if t.TenantID != tid || (ch != nil && strp(t.ChannelID) != *ch) || (q != "" && !strings.Contains(lower(t.Name), lower(q))) || !vis.Allows(t.ID) {
			continue
		}
		out = append(out, m.decorate(t))
	}
	page, total, applied := window(out, req, func(t store.Template, f string) any {
		switch f {
		case "channel":
			if t.ChannelID == nil {
				return nil
			}
			return t.ChannelName
		case "updated_at":
			return t.UpdatedAt
		}
		return t.Name
	}, func(t store.Template) string { return t.ID })
	return page, total, applied, nil
}

func (m *Store) PageMessages(_ context.Context, tid string, f store.MessageFilter, req listquery.Request) ([]store.Message, int, listquery.Request, error) {
	if err := m.fail("PageMessages"); err != nil {
		return nil, 0, req, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Message
	for _, msg := range m.Messages {
		if msg.TenantID != tid || (f.Status != "" && msg.Status != f.Status) || (f.CategoryID != "" && strp(msg.CategoryID) != f.CategoryID) ||
			(f.Q != "" && !strings.Contains(lower(msg.Title), lower(f.Q))) || (f.SenderID != "" && strp(msg.SenderID) != f.SenderID) {
			continue
		}
		out = append(out, m.decorateMessage(msg))
	}
	page, total, applied := window(out, req, func(x store.Message, f string) any {
		switch f {
		case "subject":
			return x.Title
		case "status":
			return x.Status
		}
		return x.CreatedAt
	}, func(x store.Message) string { return x.ID })
	return page, total, applied, nil
}

func (m *Store) PageLog(_ context.Context, tid string, f store.LogFilter, req listquery.Request) ([]store.LogRow, int, listquery.Request, error) {
	if err := m.fail("PageLog"); err != nil {
		return nil, 0, req, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.LogRow
	for _, l := range m.Logs {
		if l.TenantID != tid || (f.ChannelID != "" && l.ChannelID != f.ChannelID) || (f.TemplateID != "" && strp(l.TemplateID) != f.TemplateID) ||
			(f.Recipient != "" && !strings.Contains(lower(l.Recipient), lower(f.Recipient))) || (f.Status != "" && l.Status != f.Status) ||
			(f.SenderID != "" && l.SenderID != f.SenderID) || l.CreatedAt.Before(f.From) || l.CreatedAt.After(f.To) {
			continue
		}
		l.RenderedBody = ""
		out = append(out, l)
	}
	page, total, applied := window(out, req, func(l store.LogRow, f string) any {
		switch f {
		case "status":
			return l.Status
		case "channel":
			return l.ChannelType
		}
		return l.CreatedAt
	}, func(l store.LogRow) string { return l.ID })
	return page, total, applied, nil
}

func (m *Store) PageCategories(_ context.Context, tid string, req listquery.Request) ([]store.Category, int, listquery.Request, error) {
	if err := m.fail("PageCategories"); err != nil {
		return nil, 0, req, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Category
	for _, c := range m.Categories {
		if c.TenantID == tid {
			c.MessageCount = m.messageCount(c.ID)
			out = append(out, c)
		}
	}
	page, total, applied := window(out, req, func(c store.Category, f string) any {
		if f == "name" {
			return c.Name
		}
		return c.Sort
	}, func(c store.Category) string { return c.ID })
	return page, total, applied, nil
}

// auditRow pairs an event with its insertion position (the double's
// stand-in for the database's physical row id tie-breaker).
type auditRow struct {
	store.AuditRow
	seq int
}

func (m *Store) PageAudit(_ context.Context, tid, et, actor string, from, to time.Time, req listquery.Request) ([]store.AuditRow, int, listquery.Request, error) {
	if err := m.fail("PageAudit"); err != nil {
		return nil, 0, req, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var rows []auditRow
	for i, r := range m.Audit {
		if r.TenantID != tid || (et != "" && r.EventType != et) || (actor != "" && r.ActorID != actor) || r.TS.Before(from) || r.TS.After(to) {
			continue
		}
		r.SubjectName = m.subjectName(tid, r)
		rows = append(rows, auditRow{AuditRow: r, seq: i})
	}
	page, total, applied := window(rows, req, func(r auditRow, _ string) any { return r.TS },
		func(r auditRow) string { return fmt.Sprintf("%012d", r.seq) })
	out := make([]store.AuditRow, 0, len(page))
	for _, r := range page {
		out = append(out, r.AuditRow)
	}
	return out, total, applied, nil
}

// subjectName resolves a live subject like the SQL query (caller holds mu).
func (m *Store) subjectName(tid string, r store.AuditRow) string {
	switch r.SubjectKind {
	case "channel":
		if c, ok := m.Channels[r.SubjectID]; ok && c.TenantID == tid {
			return c.Name
		}
	case "template":
		if t, ok := m.Templates[r.SubjectID]; ok && t.TenantID == tid {
			return t.Name
		}
	case "message":
		if x, ok := m.Messages[r.SubjectID]; ok && x.TenantID == tid {
			return x.Title
		}
	}
	return ""
}
