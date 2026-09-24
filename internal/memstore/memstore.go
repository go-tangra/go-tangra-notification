// Package memstore is the in-memory repo.Store double for unit tests. It
// applies the same tenant scoping, uniqueness and paging rules as the SQL
// repositories and supports failure injection per operation.
package memstore

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/repo"
	"github.com/go-tangra/go-tangra-notification/v4/internal/store"
)

// Store holds every table; exported maps ease assertions in tests.
type Store struct {
	mu         sync.Mutex
	Channels   map[string]store.Channel
	Templates  map[string]store.Template
	Logs       map[string]store.LogRow
	Grants     map[string]store.Grant
	Categories map[string]store.Category
	Messages   map[string]store.Message
	Inbox      map[string]store.InboxRow
	Audit      []store.AuditRow
	Fail       map[string]error // operation name → error to return
	Now        func() time.Time
}

// New returns an empty store.
func New() *Store {
	return &Store{Channels: map[string]store.Channel{}, Templates: map[string]store.Template{}, Logs: map[string]store.LogRow{}, Grants: map[string]store.Grant{},
		Categories: map[string]store.Category{}, Messages: map[string]store.Message{}, Inbox: map[string]store.InboxRow{}, Fail: map[string]error{}, Now: time.Now}
}

// FailOn makes op return err until cleared (err nil).
func (m *Store) FailOn(op string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		delete(m.Fail, op)
		return
	}
	m.Fail[op] = err
}

func (m *Store) fail(op string) error { return m.Fail[op] }

type snapshot struct {
	channels   map[string]store.Channel
	templates  map[string]store.Template
	logs       map[string]store.LogRow
	grants     map[string]store.Grant
	categories map[string]store.Category
	messages   map[string]store.Message
	inbox      map[string]store.InboxRow
	audit      []store.AuditRow
}

func copyMap[V any](in map[string]V) map[string]V {
	out := make(map[string]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m *Store) snap() snapshot {
	return snapshot{channels: copyMap(m.Channels), templates: copyMap(m.Templates), logs: copyMap(m.Logs), grants: copyMap(m.Grants),
		categories: copyMap(m.Categories), messages: copyMap(m.Messages), inbox: copyMap(m.Inbox), audit: append([]store.AuditRow(nil), m.Audit...)}
}

// Atomic runs fn and rolls every table back when it fails.
func (m *Store) Atomic(_ context.Context, _ string, fn func(repo.Store) error) error {
	if err := m.fail("Atomic"); err != nil {
		return err
	}
	m.mu.Lock()
	s := m.snap()
	m.mu.Unlock()
	if err := fn(m); err != nil {
		m.mu.Lock()
		m.Channels, m.Templates, m.Logs, m.Grants, m.Categories, m.Messages, m.Inbox, m.Audit = s.channels, s.templates, s.logs, s.grants, s.categories, s.messages, s.inbox, s.audit
		m.mu.Unlock()
		return err
	}
	return nil
}

func strp(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func lower(s string) string { return strings.ToLower(s) }

// ---- channels

func (m *Store) templateCount(id string) int {
	n := 0
	for _, t := range m.Templates {
		if strp(t.ChannelID) == id {
			n++
		}
	}
	return n
}

func (m *Store) InsertChannel(_ context.Context, c store.Channel) error {
	if err := m.fail("InsertChannel"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.Channels {
		if x.TenantID == c.TenantID && (lower(x.Name) == lower(c.Name) || (c.IsDefault && x.IsDefault && x.Type == c.Type)) {
			return store.ErrConflict
		}
	}
	now := m.Now()
	c.CreatedAt, c.UpdatedAt = now, now
	if c.SettingsPublic == nil {
		c.SettingsPublic = []byte("{}")
	}
	m.Channels[c.ID] = c
	return nil
}

func (m *Store) GetChannel(_ context.Context, tid, id string) (store.Channel, error) {
	if err := m.fail("GetChannel"); err != nil {
		return store.Channel{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.Channels[id]
	if !ok || c.TenantID != tid {
		return store.Channel{}, store.ErrNotFound
	}
	c.TemplateCount = m.templateCount(id)
	return c, nil
}

func (m *Store) ListChannels(_ context.Context, tid, typ, after string, limit int) ([]store.Channel, error) {
	if err := m.fail("ListChannels"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Channel
	for _, c := range m.Channels {
		if c.TenantID == tid && (typ == "" || c.Type == typ) && lower(c.Name) > lower(after) {
			c.TemplateCount = m.templateCount(c.ID)
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if lower(out[i].Name) != lower(out[j].Name) {
			return lower(out[i].Name) < lower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Store) UpdateChannel(_ context.Context, c store.Channel) error {
	if err := m.fail("UpdateChannel"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.Channels[c.ID]
	if !ok || old.TenantID != c.TenantID {
		return store.ErrNotFound
	}
	for id, x := range m.Channels {
		if id != c.ID && x.TenantID == c.TenantID && (lower(x.Name) == lower(c.Name) || (c.IsDefault && x.IsDefault && x.Type == old.Type)) {
			return store.ErrConflict
		}
	}
	old.Name, old.SettingsSealed, old.SettingsPublic, old.Enabled, old.IsDefault, old.UpdatedBy, old.UpdatedAt = c.Name, c.SettingsSealed, c.SettingsPublic, c.Enabled, c.IsDefault, c.UpdatedBy, m.Now()
	m.Channels[c.ID] = old
	return nil
}

func (m *Store) ClearDefaultChannel(_ context.Context, tid, typ, except string) error {
	if err := m.fail("ClearDefaultChannel"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, c := range m.Channels {
		if c.TenantID == tid && c.Type == typ && c.IsDefault && id != except {
			c.IsDefault = false
			m.Channels[id] = c
		}
	}
	return nil
}

func (m *Store) DeleteChannel(_ context.Context, tid, id string) error {
	if err := m.fail("DeleteChannel"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.Channels[id]
	if !ok || c.TenantID != tid {
		return store.ErrNotFound
	}
	if m.templateCount(id) > 0 {
		return store.ErrConflict
	}
	delete(m.Channels, id)
	return nil
}

// ---- templates

func (m *Store) decorate(t store.Template) store.Template {
	if t.ChannelID != nil {
		if c, ok := m.Channels[*t.ChannelID]; ok {
			t.ChannelName = c.Name
		}
	}
	if t.Variables == nil {
		t.Variables = []string{}
	}
	return t
}

func (m *Store) InsertTemplate(_ context.Context, t store.Template) error {
	if err := m.fail("InsertTemplate"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.ChannelID != nil {
		if c, ok := m.Channels[*t.ChannelID]; !ok || c.TenantID != t.TenantID {
			return store.ErrConflict
		}
	}
	for _, x := range m.Templates {
		if x.TenantID == t.TenantID && (lower(x.Name) == lower(t.Name) || (t.IsDefault && x.IsDefault && t.ChannelID != nil && strp(x.ChannelID) == strp(t.ChannelID))) {
			return store.ErrConflict
		}
	}
	now := m.Now()
	t.CreatedAt, t.UpdatedAt = now, now
	m.Templates[t.ID] = t
	return nil
}

func (m *Store) GetTemplate(_ context.Context, tid, id string) (store.Template, error) {
	if err := m.fail("GetTemplate"); err != nil {
		return store.Template{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.Templates[id]
	if !ok || t.TenantID != tid {
		return store.Template{}, store.ErrNotFound
	}
	return m.decorate(t), nil
}

func sortTemplates(out []store.Template) {
	sort.Slice(out, func(i, j int) bool {
		if lower(out[i].Name) != lower(out[j].Name) {
			return lower(out[i].Name) < lower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
}

func (m *Store) ListTemplates(_ context.Context, tid string, ch *string, q, after string, limit int) ([]store.Template, error) {
	if err := m.fail("ListTemplates"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Template
	for _, t := range m.Templates {
		if t.TenantID != tid || (ch != nil && strp(t.ChannelID) != *ch) || (q != "" && !strings.Contains(lower(t.Name), lower(q))) || lower(t.Name) <= lower(after) {
			continue
		}
		out = append(out, m.decorate(t))
	}
	sortTemplates(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Store) TemplatesByIDs(_ context.Context, tid string, ids []string) ([]store.Template, error) {
	if err := m.fail("TemplatesByIDs"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Template
	for _, id := range ids {
		if t, ok := m.Templates[id]; ok && t.TenantID == tid {
			out = append(out, m.decorate(t))
		}
	}
	sortTemplates(out)
	return out, nil
}

func (m *Store) AllTemplates(_ context.Context, tid string, limit int) ([]store.Template, error) {
	if err := m.fail("AllTemplates"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Template
	for _, t := range m.Templates {
		if t.TenantID == tid {
			out = append(out, m.decorate(t))
		}
	}
	sortTemplates(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Store) UpdateTemplate(_ context.Context, t store.Template) error {
	if err := m.fail("UpdateTemplate"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.Templates[t.ID]
	if !ok || old.TenantID != t.TenantID {
		return store.ErrNotFound
	}
	for id, x := range m.Templates {
		if id != t.ID && x.TenantID == t.TenantID && (lower(x.Name) == lower(t.Name) || (t.IsDefault && x.IsDefault && t.ChannelID != nil && strp(x.ChannelID) == strp(t.ChannelID))) {
			return store.ErrConflict
		}
	}
	old.Name, old.ChannelID, old.ChannelType, old.Subject, old.Body, old.Variables, old.IsDefault, old.UpdatedBy, old.UpdatedAt = t.Name, t.ChannelID, t.ChannelType, t.Subject, t.Body, t.Variables, t.IsDefault, t.UpdatedBy, m.Now()
	m.Templates[t.ID] = old
	return nil
}

func (m *Store) ClearDefaultTemplate(_ context.Context, tid, ch, except string) error {
	if err := m.fail("ClearDefaultTemplate"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, t := range m.Templates {
		if t.TenantID == tid && strp(t.ChannelID) == ch && t.IsDefault && id != except {
			t.IsDefault = false
			m.Templates[id] = t
		}
	}
	return nil
}

func (m *Store) DeleteTemplate(_ context.Context, tid, id string) error {
	if err := m.fail("DeleteTemplate"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.Templates[id]
	if !ok || t.TenantID != tid {
		return store.ErrNotFound
	}
	delete(m.Templates, id)
	return nil
}

// ---- log

func (m *Store) InsertLog(_ context.Context, l store.LogRow) error {
	if err := m.fail("InsertLog"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.CreatedAt.IsZero() {
		l.CreatedAt = m.Now()
	}
	m.Logs[l.ID] = l
	return nil
}

func (m *Store) SetLogOutcome(_ context.Context, tid, id, status, errText, subject, body string, sentAt *time.Time) error {
	if err := m.fail("SetLogOutcome"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.Logs[id]
	if !ok || l.TenantID != tid || l.Status != "pending" {
		return store.ErrNotFound
	}
	l.Status, l.Error, l.RenderedSubject, l.RenderedBody, l.SentAt = status, errText, subject, body, sentAt
	m.Logs[id] = l
	return nil
}

func (m *Store) GetLog(_ context.Context, tid, id string) (store.LogRow, error) {
	if err := m.fail("GetLog"); err != nil {
		return store.LogRow{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.Logs[id]
	if !ok || l.TenantID != tid {
		return store.LogRow{}, store.ErrNotFound
	}
	return l, nil
}

func (m *Store) LogPage(_ context.Context, tid string, f store.LogFilter) ([]store.LogRow, error) {
	if err := m.fail("LogPage"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	to := f.To
	if to.IsZero() {
		to = m.Now().Add(time.Minute)
	}
	var out []store.LogRow
	for _, l := range m.Logs {
		if l.TenantID != tid || (f.ChannelID != "" && l.ChannelID != f.ChannelID) || (f.TemplateID != "" && strp(l.TemplateID) != f.TemplateID) ||
			(f.Recipient != "" && !strings.Contains(lower(l.Recipient), lower(f.Recipient))) || (f.Status != "" && l.Status != f.Status) ||
			(f.SenderID != "" && l.SenderID != f.SenderID) || l.CreatedAt.Before(f.From) || l.CreatedAt.After(to) {
			continue
		}
		if !f.CursorTS.IsZero() && !(l.CreatedAt.Before(f.CursorTS) || (l.CreatedAt.Equal(f.CursorTS) && l.ID < f.CursorID)) {
			continue
		}
		l.RenderedBody = ""
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (m *Store) ExpirePendingLogs(_ context.Context, olderThan time.Time) (int64, error) {
	if err := m.fail("ExpirePendingLogs"); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for id, l := range m.Logs {
		if l.Status == "pending" && l.CreatedAt.Before(olderThan) {
			l.Status, l.Error = "failed", "interrupted"
			m.Logs[id] = l
			n++
		}
	}
	return n, nil
}

// ---- grants

func (m *Store) UpsertGrant(_ context.Context, g store.Grant) (store.Grant, error) {
	if err := m.fail("UpsertGrant"); err != nil {
		return store.Grant{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, x := range m.Grants {
		if x.TenantID == g.TenantID && x.ResourceType == g.ResourceType && x.ResourceID == g.ResourceID && x.SubjectType == g.SubjectType && x.SubjectID == g.SubjectID {
			x.Relation, x.GrantedBy, x.GrantedAt, x.ExpiresAt = g.Relation, g.GrantedBy, m.Now(), g.ExpiresAt
			m.Grants[id] = x
			return x, nil
		}
	}
	g.GrantedAt = m.Now()
	m.Grants[g.ID] = g
	return g, nil
}

func (m *Store) GetGrant(_ context.Context, tid, id string) (store.Grant, error) {
	if err := m.fail("GetGrant"); err != nil {
		return store.Grant{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.Grants[id]
	if !ok || g.TenantID != tid {
		return store.Grant{}, store.ErrNotFound
	}
	return g, nil
}

func (m *Store) DeleteGrant(_ context.Context, tid, id string) error {
	if err := m.fail("DeleteGrant"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.Grants[id]
	if !ok || g.TenantID != tid {
		return store.ErrNotFound
	}
	delete(m.Grants, id)
	return nil
}

func sortGrants(out []store.Grant) {
	sort.Slice(out, func(i, j int) bool {
		if !out[i].GrantedAt.Equal(out[j].GrantedAt) {
			return out[i].GrantedAt.Before(out[j].GrantedAt)
		}
		return out[i].ID < out[j].ID
	})
}

func (m *Store) GrantsOnResource(_ context.Context, tid, rt, rid string) ([]store.Grant, error) {
	if err := m.fail("GrantsOnResource"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Grant
	for _, g := range m.Grants {
		if g.TenantID == tid && g.ResourceType == rt && g.ResourceID == rid {
			out = append(out, g)
		}
	}
	sortGrants(out)
	return out, nil
}

func (m *Store) GrantsForSubjects(_ context.Context, tid, uid string, roles []string, now time.Time) ([]store.Grant, error) {
	if err := m.fail("GrantsForSubjects"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Grant
	for _, g := range m.Grants {
		if g.TenantID != tid || (g.ExpiresAt != nil && !g.ExpiresAt.After(now)) {
			continue
		}
		switch g.SubjectType {
		case "user":
			if uid == "" || g.SubjectID != uid {
				continue
			}
		case "role":
			found := false
			for _, r := range roles {
				if r == g.SubjectID {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, g)
	}
	sortGrants(out)
	return out, nil
}

func (m *Store) DeleteGrantsOfResource(_ context.Context, tid, rt, rid string) error {
	if err := m.fail("DeleteGrantsOfResource"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, g := range m.Grants {
		if g.TenantID == tid && g.ResourceType == rt && g.ResourceID == rid {
			delete(m.Grants, id)
		}
	}
	return nil
}

func (m *Store) DeleteGrantsOfSubject(_ context.Context, tid, rt, rid, st, sid string) error {
	if err := m.fail("DeleteGrantsOfSubject"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, g := range m.Grants {
		if g.TenantID == tid && g.ResourceType == rt && g.ResourceID == rid && g.SubjectType == st && g.SubjectID == sid {
			delete(m.Grants, id)
		}
	}
	return nil
}

// ---- categories

func (m *Store) messageCount(cid string) int {
	n := 0
	for _, x := range m.Messages {
		if strp(x.CategoryID) == cid {
			n++
		}
	}
	return n
}

func (m *Store) InsertCategory(_ context.Context, c store.Category) error {
	if err := m.fail("InsertCategory"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.Categories {
		if x.TenantID == c.TenantID && lower(x.Name) == lower(c.Name) {
			return store.ErrConflict
		}
	}
	now := m.Now()
	c.CreatedAt, c.UpdatedAt = now, now
	m.Categories[c.ID] = c
	return nil
}

func (m *Store) GetCategory(_ context.Context, tid, id string) (store.Category, error) {
	if err := m.fail("GetCategory"); err != nil {
		return store.Category{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.Categories[id]
	if !ok || c.TenantID != tid {
		return store.Category{}, store.ErrNotFound
	}
	c.MessageCount = m.messageCount(id)
	return c, nil
}

func (m *Store) ListCategories(_ context.Context, tid string) ([]store.Category, error) {
	if err := m.fail("ListCategories"); err != nil {
		return nil, err
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
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		if lower(out[i].Name) != lower(out[j].Name) {
			return lower(out[i].Name) < lower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Store) UpdateCategory(_ context.Context, c store.Category) error {
	if err := m.fail("UpdateCategory"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.Categories[c.ID]
	if !ok || old.TenantID != c.TenantID {
		return store.ErrNotFound
	}
	for id, x := range m.Categories {
		if id != c.ID && x.TenantID == c.TenantID && lower(x.Name) == lower(c.Name) {
			return store.ErrConflict
		}
	}
	old.Name, old.Description, old.Sort, old.UpdatedBy, old.UpdatedAt = c.Name, c.Description, c.Sort, c.UpdatedBy, m.Now()
	m.Categories[c.ID] = old
	return nil
}

func (m *Store) DeleteCategory(_ context.Context, tid, id string) error {
	if err := m.fail("DeleteCategory"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.Categories[id]
	if !ok || c.TenantID != tid {
		return store.ErrNotFound
	}
	if m.messageCount(id) > 0 {
		return store.ErrConflict
	}
	delete(m.Categories, id)
	return nil
}

// ---- messages

func (m *Store) decorateMessage(msg store.Message) store.Message {
	if msg.CategoryID != nil {
		if c, ok := m.Categories[*msg.CategoryID]; ok {
			msg.CategoryName = c.Name
		}
	}
	msg.RecipientCount, msg.ReadCount = 0, 0
	for _, i := range m.Inbox {
		if i.MessageID == msg.ID {
			msg.RecipientCount++
			if i.ReadAt != nil {
				msg.ReadCount++
			}
		}
	}
	if len(msg.Recipients) == 0 {
		msg.Recipients = []byte("{}")
	}
	return msg
}

func (m *Store) InsertMessage(_ context.Context, msg store.Message) error {
	if err := m.fail("InsertMessage"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg.CategoryID != nil {
		if c, ok := m.Categories[*msg.CategoryID]; !ok || c.TenantID != msg.TenantID {
			return store.ErrConflict
		}
	}
	now := m.Now()
	msg.CreatedAt, msg.UpdatedAt = now, now
	m.Messages[msg.ID] = msg
	return nil
}

func (m *Store) GetMessage(_ context.Context, tid, id string) (store.Message, error) {
	if err := m.fail("GetMessage"); err != nil {
		return store.Message{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.Messages[id]
	if !ok || msg.TenantID != tid {
		return store.Message{}, store.ErrNotFound
	}
	return m.decorateMessage(msg), nil
}

func (m *Store) ListMessages(_ context.Context, tid string, f store.MessageFilter) ([]store.Message, error) {
	if err := m.fail("ListMessages"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Message
	for _, msg := range m.Messages {
		if msg.TenantID != tid || (f.Status != "" && msg.Status != f.Status) || (f.CategoryID != "" && strp(msg.CategoryID) != f.CategoryID) ||
			(f.Q != "" && !strings.Contains(lower(msg.Title), lower(f.Q))) || (f.SenderID != "" && strp(msg.SenderID) != f.SenderID) {
			continue
		}
		if !f.CursorTS.IsZero() && !(msg.CreatedAt.Before(f.CursorTS) || (msg.CreatedAt.Equal(f.CursorTS) && msg.ID < f.CursorID)) {
			continue
		}
		out = append(out, m.decorateMessage(msg))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (m *Store) UpdateMessage(_ context.Context, msg store.Message) error {
	if err := m.fail("UpdateMessage"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.Messages[msg.ID]
	if !ok || old.TenantID != msg.TenantID {
		return store.ErrNotFound
	}
	if msg.CategoryID != nil {
		if c, ok := m.Categories[*msg.CategoryID]; !ok || c.TenantID != msg.TenantID {
			return store.ErrConflict
		}
	}
	old.Title, old.Content, old.Type, old.CategoryID, old.Recipients, old.ScheduledAt, old.Status, old.UpdatedBy, old.UpdatedAt = msg.Title, msg.Content, msg.Type, msg.CategoryID, msg.Recipients, msg.ScheduledAt, msg.Status, msg.UpdatedBy, m.Now()
	m.Messages[msg.ID] = old
	return nil
}

func (m *Store) SetMessageStatus(_ context.Context, tid, id, status string, at *time.Time) error {
	if err := m.fail("SetMessageStatus"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.Messages[id]
	if !ok || msg.TenantID != tid {
		return store.ErrNotFound
	}
	msg.Status, msg.LeaseUntil, msg.UpdatedAt = status, nil, m.Now()
	if at != nil {
		msg.PublishedAt = at
	}
	m.Messages[id] = msg
	return nil
}

func (m *Store) DeleteMessage(_ context.Context, tid, id string) error {
	if err := m.fail("DeleteMessage"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.Messages[id]
	if !ok || msg.TenantID != tid {
		return store.ErrNotFound
	}
	delete(m.Messages, id)
	for iid, i := range m.Inbox {
		if i.MessageID == id {
			delete(m.Inbox, iid)
		}
	}
	return nil
}

func (m *Store) ClaimDueMessages(_ context.Context, now time.Time, lease time.Duration, limit int) ([]store.Message, error) {
	if err := m.fail("ClaimDueMessages"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Message
	for id, msg := range m.Messages {
		if (msg.Status == "scheduled" || msg.Status == "publishing") && msg.ScheduledAt != nil && !msg.ScheduledAt.After(now) && (msg.LeaseUntil == nil || msg.LeaseUntil.Before(now)) {
			until := now.Add(lease)
			msg.Status, msg.LeaseUntil = "publishing", &until
			m.Messages[id] = msg
			out = append(out, store.Message{ID: msg.ID, TenantID: msg.TenantID})
			if len(out) == limit {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ---- inbox

func (m *Store) InsertInboxBatch(_ context.Context, rows []store.InboxRow) (int64, error) {
	if err := m.fail("InsertInboxBatch"); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, r := range rows {
		dup := false
		for _, x := range m.Inbox {
			if x.MessageID == r.MessageID && x.RecipientID == r.RecipientID {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		now := m.Now()
		r.CreatedAt, r.UpdatedAt = now, now
		m.Inbox[r.ID] = r
		n++
	}
	return n, nil
}

func visible(i store.InboxRow) bool {
	return i.Status != "deleted" && (i.Status != "revoked" || i.ReadAt != nil)
}

func (m *Store) withMessage(i store.InboxRow) store.InboxRow {
	if msg, ok := m.Messages[i.MessageID]; ok {
		d := m.decorateMessage(msg)
		i.Message = &d
	}
	return i
}

func (m *Store) InboxPage(_ context.Context, tid, rid, status string, cts time.Time, cid string, limit int) ([]store.InboxRow, error) {
	if err := m.fail("InboxPage"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.InboxRow
	for _, i := range m.Inbox {
		if i.TenantID != tid || i.RecipientID != rid || !visible(i) {
			continue
		}
		if (status == "unread" && i.Status != "sent" && i.Status != "received") || (status == "read" && i.Status != "read") {
			continue
		}
		if !cts.IsZero() && !(i.CreatedAt.Before(cts) || (i.CreatedAt.Equal(cts) && i.ID < cid)) {
			continue
		}
		out = append(out, m.withMessage(i))
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].CreatedAt.Equal(out[b].CreatedAt) {
			return out[a].CreatedAt.After(out[b].CreatedAt)
		}
		return out[a].ID > out[b].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Store) GetInboxEntry(_ context.Context, tid, rid, id string) (store.InboxRow, error) {
	if err := m.fail("GetInboxEntry"); err != nil {
		return store.InboxRow{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.Inbox[id]
	if !ok || i.TenantID != tid || i.RecipientID != rid || !visible(i) {
		return store.InboxRow{}, store.ErrNotFound
	}
	return m.withMessage(i), nil
}

func (m *Store) InboxUnread(_ context.Context, tid, rid string) (int64, error) {
	if err := m.fail("InboxUnread"); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, i := range m.Inbox {
		if i.TenantID == tid && i.RecipientID == rid && (i.Status == "sent" || i.Status == "received") {
			n++
		}
	}
	return n, nil
}

func (m *Store) SetInboxStatus(_ context.Context, tid, rid string, ids []string, status string) (int64, error) {
	if err := m.fail("SetInboxStatus"); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	now := m.Now()
	for _, id := range ids {
		i, ok := m.Inbox[id]
		if !ok || i.TenantID != tid || i.RecipientID != rid {
			continue
		}
		active := i.Status == "sent" || i.Status == "received" || i.Status == "read"
		switch status {
		case "read":
			if !active {
				continue
			}
			i.Status = "read"
			if i.ReadAt == nil {
				i.ReadAt = &now
			}
		case "unread":
			if !active {
				continue
			}
			i.Status, i.ReadAt = "sent", nil
		case "received":
			if i.Status != "sent" {
				continue
			}
			i.Status = "received"
		case "deleted":
			if i.Status == "deleted" {
				continue
			}
			i.Status = "deleted"
		default:
			return n, store.ErrConflict
		}
		i.UpdatedAt = now
		m.Inbox[id] = i
		n++
	}
	return n, nil
}

func (m *Store) RevokeUnread(_ context.Context, tid, mid string) ([]string, error) {
	if err := m.fail("RevokeUnread"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, i := range m.Inbox {
		if i.TenantID == tid && i.MessageID == mid && (i.Status == "sent" || i.Status == "received" || i.Status == "read") {
			i.Status = "revoked"
			m.Inbox[id] = i
			out = append(out, i.RecipientID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *Store) MessageRecipients(_ context.Context, tid, mid, status, after string, limit int) ([]store.InboxRow, error) {
	if err := m.fail("MessageRecipients"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.InboxRow
	for _, i := range m.Inbox {
		if i.TenantID == tid && i.MessageID == mid && (status == "" || i.Status == status) && i.ID > after {
			out = append(out, i)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---- audit & stats

func (m *Store) InsertAuditRows(_ context.Context, rows []store.AuditRow) error {
	if err := m.fail("InsertAuditRows"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Audit = append(m.Audit, rows...)
	return nil
}

func (m *Store) QueryAudit(_ context.Context, tid, et, actor string, from, to, cursor time.Time, limit int) ([]store.AuditRow, error) {
	if err := m.fail("QueryAudit"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.AuditRow
	for _, r := range m.Audit {
		if r.TenantID != tid || (et != "" && r.EventType != et) || (actor != "" && r.ActorID != actor) || r.TS.Before(from) || r.TS.After(to) || (!cursor.IsZero() && !r.TS.Before(cursor)) {
			continue
		}
		switch r.SubjectKind {
		case "channel":
			if c, ok := m.Channels[r.SubjectID]; ok && c.TenantID == tid {
				r.SubjectName = c.Name
			}
		case "template":
			if t, ok := m.Templates[r.SubjectID]; ok && t.TenantID == tid {
				r.SubjectName = t.Name
			}
		case "message":
			if x, ok := m.Messages[r.SubjectID]; ok && x.TenantID == tid {
				r.SubjectName = x.Title
			}
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.After(out[j].TS) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// AuditEvents returns the events of a type (test helper).
func (m *Store) AuditEvents(tid, et string) []store.AuditRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.AuditRow
	for _, r := range m.Audit {
		if r.TenantID == tid && (et == "" || r.EventType == et) {
			out = append(out, r)
		}
	}
	return out
}

// AuditDetails decodes an event's details (test helper).
func AuditDetails(r store.AuditRow) map[string]any {
	var d map[string]any
	_ = json.Unmarshal(r.Details, &d)
	return d
}

func (m *Store) TenantStats(_ context.Context, tid string, now time.Time) (store.Stats, error) {
	if err := m.fail("TenantStats"); err != nil {
		return store.Stats{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := store.Stats{Notifications: map[string]int64{}, Messages: map[string]int64{}}
	for _, c := range m.Channels {
		if c.TenantID == tid {
			st.Channels++
		}
	}
	for _, t := range m.Templates {
		if t.TenantID == tid {
			st.Templates++
		}
	}
	since := now.Add(-24 * time.Hour)
	for _, l := range m.Logs {
		if l.TenantID == tid && !l.CreatedAt.Before(since) {
			st.Notifications[l.Status]++
		}
	}
	for _, x := range m.Messages {
		if x.TenantID == tid {
			st.Messages[x.Status]++
		}
	}
	for _, r := range m.Audit {
		if r.TenantID == tid && !r.TS.Before(since) {
			st.Operations24h++
		}
	}
	return st, nil
}

var _ repo.Store = (*Store)(nil)
