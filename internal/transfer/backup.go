// Package transfer exports and imports tenant backups: channels (credentials
// only on request), templates and categories, validated whole before any
// write, applied per entity with skip or overwrite (research R9).
package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/go-freya/freya/services/notification/api/schema"
	"github.com/go-freya/freya/services/notification/internal/audit"
	"github.com/go-freya/freya/services/notification/internal/authz"
	"github.com/go-freya/freya/services/notification/internal/messages"
	"github.com/go-freya/freya/services/notification/internal/notify"
	"github.com/go-freya/freya/services/notification/internal/repo"
	"github.com/go-freya/freya/services/notification/internal/sealed"
	"github.com/go-freya/freya/services/notification/internal/store"
)

// Limits.
const (
	MaxBytes = 16 << 20
	MaxItems = 10000
	MaxDepth = 8
)

// Errors.
var (
	ErrTooLarge = errors.New("transfer: document too large")
	ErrInvalid  = errors.New("transfer: invalid document")
	ErrMode     = errors.New("transfer: mode must be skip or overwrite")
)

// Document is the backup (contracts/backup.schema.json).
type Document struct {
	Version             int        `json:"version"`
	ExportedAt          time.Time  `json:"exported_at"`
	Tenant              string     `json:"tenant,omitempty"`
	IncludesCredentials bool       `json:"includes_credentials"`
	Channels            []Channel  `json:"channels"`
	Templates           []Template `json:"templates"`
	Categories          []Category `json:"categories"`
}

// Channel is an exported channel.
type Channel struct {
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Settings  sealed.Settings `json:"settings"`
	Enabled   bool            `json:"enabled"`
	IsDefault bool            `json:"is_default"`
}

// Template is an exported template.
type Template struct {
	Name        string   `json:"name"`
	Channel     string   `json:"channel,omitempty"`
	ChannelType string   `json:"channel_type,omitempty"`
	Subject     string   `json:"subject"`
	Body        string   `json:"body"`
	Variables   []string `json:"variables"`
	IsDefault   bool     `json:"is_default"`
}

// Category is an exported category.
type Category struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Sort        int    `json:"sort"`
}

// EntityReport counts one entity type's outcome.
type EntityReport struct {
	Created     int `json:"created"`
	Skipped     int `json:"skipped"`
	Overwritten int `json:"overwritten"`
	Failed      int `json:"failed"`
}

// Report is the import outcome.
type Report struct {
	Channels   EntityReport `json:"channels"`
	Templates  EntityReport `json:"templates"`
	Categories EntityReport `json:"categories"`
	Warnings   []string     `json:"warnings"`
}

var backupSchema = func() *jsonschema.Schema {
	c := jsonschema.NewCompiler()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.Backup))
	if err != nil {
		panic(err)
	}
	if err := c.AddResource("backup.schema.json", doc); err != nil {
		panic(err)
	}
	return c.MustCompile("backup.schema.json")
}()

// Service exports and imports.
type Service struct {
	st        repo.Store
	channels  *notify.Channels
	templates *notify.Templates
	msgs      *messages.Service
	audit     *audit.Writer
	now       func() time.Time
}

// New wires the service.
func New(st repo.Store, ch *notify.Channels, tp *notify.Templates, msgs *messages.Service, aw *audit.Writer) *Service {
	return &Service{st: st, channels: ch, templates: tp, msgs: msgs, audit: aw, now: time.Now}
}

// Export builds the document; credentials only when asked (audited as a bulk disclosure).
func (s *Service) Export(ctx context.Context, subj authz.Subjects, includeCredentials bool) (Document, error) {
	doc := Document{Version: 1, ExportedAt: s.now(), Tenant: subj.TenantID, IncludesCredentials: includeCredentials, Channels: []Channel{}, Templates: []Template{}, Categories: []Category{}}
	channels, err := s.st.ListChannels(ctx, subj.TenantID, "", "", 1000)
	if err != nil {
		return Document{}, err
	}
	names := map[string]string{}
	for _, c := range channels {
		names[c.ID] = c.Name
		var settings sealed.Settings
		if includeCredentials {
			_, settings, _, err = s.channels.Resolve(ctx, subj.TenantID, c.ID)
			if err != nil {
				return Document{}, err
			}
		} else {
			_, clear, _, err := s.channels.Resolve(ctx, subj.TenantID, c.ID)
			if err != nil {
				return Document{}, err
			}
			settings = sealed.Public(clear, s.channels.SecretFields(c.Type))
		}
		doc.Channels = append(doc.Channels, Channel{Name: c.Name, Type: c.Type, Settings: settings, Enabled: c.Enabled, IsDefault: c.IsDefault})
	}
	templates, err := s.st.AllTemplates(ctx, subj.TenantID, 5000)
	if err != nil {
		return Document{}, err
	}
	for _, t := range templates {
		vars := t.Variables
		if vars == nil {
			vars = []string{}
		}
		tpl := Template{Name: t.Name, ChannelType: t.ChannelType, Subject: t.Subject, Body: t.Body, Variables: vars, IsDefault: t.IsDefault}
		if t.ChannelID != nil {
			tpl.Channel = names[*t.ChannelID]
		}
		doc.Templates = append(doc.Templates, tpl)
	}
	cats, err := s.st.ListCategories(ctx, subj.TenantID)
	if err != nil {
		return Document{}, err
	}
	for _, c := range cats {
		doc.Categories = append(doc.Categories, Category{Name: c.Name, Description: c.Description, Sort: c.Sort})
	}
	ev := audit.BackupExported
	if includeCredentials {
		ev = audit.BackupExportedCredential
	}
	s.emit(audit.Event{Type: ev, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "backup", SubjectID: "tenant", Outcome: "ok",
		Details: map[string]any{"channels": len(doc.Channels), "templates": len(doc.Templates), "categories": len(doc.Categories), "credentials": includeCredentials}})
	return doc, nil
}

// DecodeBounded parses a document within the size, depth and item bounds
// and validates it against the schema.
func DecodeBounded(raw []byte) (Document, error) {
	if len(raw) > MaxBytes {
		return Document{}, ErrTooLarge
	}
	if depth(raw) > MaxDepth {
		return Document{}, fmt.Errorf("%w: nesting too deep", ErrInvalid)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return Document{}, fmt.Errorf("%w: %s", ErrInvalid, "not JSON")
	}
	if err := backupSchema.Validate(generic); err != nil {
		return Document{}, fmt.Errorf("%w: %s", ErrInvalid, firstLine(err.Error()))
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	if len(doc.Channels)+len(doc.Templates)+len(doc.Categories) > MaxItems {
		return Document{}, fmt.Errorf("%w: more than %d items", ErrInvalid, MaxItems)
	}
	return doc, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// depth measures JSON nesting without building the tree.
func depth(raw []byte) int {
	d, max := 0, 0
	inStr, esc := false, false
	for _, b := range raw {
		switch {
		case esc:
			esc = false
		case inStr && b == '\\':
			esc = true
		case b == '"':
			inStr = !inStr
		case inStr:
		case b == '{' || b == '[':
			d++
			if d > max {
				max = d
			}
		case b == '}' || b == ']':
			d--
		}
	}
	return max
}

// Import applies a document with mode skip or overwrite (matched by name;
// channels by name and type). Each entity type runs in its own transaction.
func (s *Service) Import(ctx context.Context, subj authz.Subjects, doc Document, mode string) (Report, error) {
	if mode != "skip" && mode != "overwrite" {
		return Report{}, ErrMode
	}
	rep := Report{Warnings: []string{}}
	// Categories.
	existing, err := s.st.ListCategories(ctx, subj.TenantID)
	if err != nil {
		return rep, err
	}
	catByName := map[string]store.Category{}
	for _, c := range existing {
		catByName[strings.ToLower(c.Name)] = c
	}
	for _, c := range doc.Categories {
		in := messages.CategoryInput{Name: c.Name, Description: c.Description, Sort: c.Sort}
		if cur, ok := catByName[strings.ToLower(c.Name)]; ok {
			if mode == "skip" {
				rep.Categories.Skipped++
				continue
			}
			if _, err := s.msgs.UpdateCategory(ctx, subj, cur.ID, in); err != nil {
				rep.Categories.Failed++
				rep.Warnings = append(rep.Warnings, "category "+c.Name+": "+err.Error())
				continue
			}
			rep.Categories.Overwritten++
			continue
		}
		if _, err := s.msgs.CreateCategory(ctx, subj, in); err != nil {
			rep.Categories.Failed++
			rep.Warnings = append(rep.Warnings, "category "+c.Name+": "+err.Error())
			continue
		}
		rep.Categories.Created++
	}
	// Channels.
	chans, err := s.st.ListChannels(ctx, subj.TenantID, "", "", 1000)
	if err != nil {
		return rep, err
	}
	chanByName := map[string]store.Channel{}
	for _, c := range chans {
		chanByName[strings.ToLower(c.Name)] = c
	}
	for _, c := range doc.Channels {
		in := notify.ChannelInput{Name: c.Name, Type: c.Type, Settings: c.Settings, Enabled: c.Enabled, IsDefault: c.IsDefault}
		if in.Settings == nil {
			in.Settings = sealed.Settings{}
		}
		if cur, ok := chanByName[strings.ToLower(c.Name)]; ok {
			if mode == "skip" || cur.Type != c.Type {
				rep.Channels.Skipped++
				if cur.Type != c.Type {
					rep.Warnings = append(rep.Warnings, "channel "+c.Name+": existing channel has another type")
				}
				continue
			}
			if !doc.IncludesCredentials {
				for _, f := range s.channels.SecretFields(c.Type) {
					in.Settings[f] = sealed.Marker // keep the stored credential
				}
			}
			if _, err := s.channels.Update(ctx, subj, cur.ID, in); err != nil {
				rep.Channels.Failed++
				rep.Warnings = append(rep.Warnings, "channel "+c.Name+": "+err.Error())
				continue
			}
			rep.Channels.Overwritten++
			continue
		}
		v, err := s.channels.Create(ctx, subj, in)
		if err != nil {
			rep.Channels.Failed++
			rep.Warnings = append(rep.Warnings, "channel "+c.Name+": "+err.Error())
			continue
		}
		chanByName[strings.ToLower(c.Name)] = store.Channel{ID: v.ID, Name: v.Name, Type: v.Type}
		rep.Channels.Created++
	}
	// Templates.
	tpls, err := s.st.AllTemplates(ctx, subj.TenantID, 5000)
	if err != nil {
		return rep, err
	}
	tplByName := map[string]store.Template{}
	for _, t := range tpls {
		tplByName[strings.ToLower(t.Name)] = t
	}
	for _, t := range doc.Templates {
		ch, ok := chanByName[strings.ToLower(t.Channel)]
		if t.Channel == "" || !ok {
			rep.Templates.Failed++
			rep.Warnings = append(rep.Warnings, "template "+t.Name+": needs a channel ("+t.Channel+" unknown)")
			continue
		}
		in := notify.TemplateInput{Name: t.Name, ChannelID: ch.ID, Subject: t.Subject, Body: t.Body, Variables: t.Variables, IsDefault: t.IsDefault}
		if cur, ok := tplByName[strings.ToLower(t.Name)]; ok {
			if mode == "skip" {
				rep.Templates.Skipped++
				continue
			}
			if _, err := s.templates.Update(ctx, subj, cur.ID, in); err != nil {
				rep.Templates.Failed++
				rep.Warnings = append(rep.Warnings, "template "+t.Name+": "+err.Error())
				continue
			}
			rep.Templates.Overwritten++
			continue
		}
		if _, err := s.templates.Create(ctx, subj, in); err != nil {
			rep.Templates.Failed++
			rep.Warnings = append(rep.Warnings, "template "+t.Name+": "+err.Error())
			continue
		}
		rep.Templates.Created++
	}
	s.emit(audit.Event{Type: audit.BackupImported, TenantID: subj.TenantID, ActorKind: subj.ActorKind(), ActorID: subj.ActorID(), SubjectKind: "backup", SubjectID: "tenant", Outcome: "ok",
		Details: map[string]any{"mode": mode, "channels": rep.Channels, "templates": rep.Templates, "categories": rep.Categories}})
	return rep, nil
}

func (s *Service) emit(e audit.Event) {
	if s.audit != nil {
		_ = s.audit.Emit(e)
	}
}
