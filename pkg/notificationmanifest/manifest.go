// Package notificationmanifest declares what the notification module registers
// with the application gateway: routes derived from the embedded OpenAPI
// document, the API permissions, the CASL abilities the shell derives from
// them and the navigation entries (contracts/manifest.md). The gRPC surface is
// service-to-service and not proxied. Public so contract tests and the gateway
// can validate it.
package notificationmanifest

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/go-tangra/go-tangra-notification/v4/api/openapi"
	"github.com/go-tangra/go-tangra-portal/sdk/v4/pkg/gatewayclient"
)

// Module is the registered module name.
const (
	Module      = "notification"
	DisplayName = "Notifications"
	Version     = "1.1.0"
	// RemotePrefix is where the module serves its federated remote assets.
	RemotePrefix = "/ui"
)

// OpenAPI operation extensions read by the builder.
const (
	PermissionExtension    = "x-freya-permission"
	PublicExtension        = "x-freya-public"
	BodyLimitExtension     = "x-freya-max-body-bytes"
	ClientAddressExtension = "x-freya-client-address"
	TimeoutExtension       = "x-freya-timeout-seconds"
)

// Permissions the module registers (granted to built-in roles by the auth service).
var Permissions = []gatewayclient.Permission{
	{Resource: "channels", Action: "read", Description: "List and read notification channels the caller is granted (settings redacted)"},
	{Resource: "channels", Action: "manage", Description: "Create, change, test and delete channels the caller is granted"},
	{Resource: "templates", Action: "read", Description: "List, read and preview templates the caller is granted"},
	{Resource: "templates", Action: "manage", Description: "Create, change and delete templates the caller is granted"},
	{Resource: "notifications", Action: "send", Description: "Send notifications through templates and channels the caller may use"},
	{Resource: "notifications", Action: "read", Description: "Read the notification log (own sends without stats:read)"},
	{Resource: "messages", Action: "read", Description: "List message categories and messages"},
	{Resource: "messages", Action: "manage", Description: "Create, send, revoke and archive internal messages; manage categories"},
	{Resource: "inbox", Action: "read", Description: "Read and manage the caller's own inbox and live stream"},
	{Resource: "events", Action: "publish", Description: "Publish live events to users of the tenant (modules)"},
	{Resource: "permissions", Action: "manage", Description: "Grant and revoke access on channels and templates the caller may share"},
	{Resource: "backup", Action: "manage", Description: "Export and import tenant backups (bulk disclosure with credentials on request)"},
	{Resource: "stats", Action: "read", Description: "Read statistics, health and the audit trail"},
}

// Grants maps built-in role slugs to the permission references they hold
// (research R12); the auth service seeds them on permission registration.
var Grants = map[string][]string{
	"owner":    PermissionRefs(),
	"admin":    PermissionRefs(),
	"member":   {"channels:read", "templates:read", "notifications:send", "notifications:read", "messages:read", "inbox:read"},
	"auditor":  {"stats:read", "notifications:read"},
	"operator": {"stats:read"},
}

// Methods proxied by the gateway: none (notification.v1 is called service to service).
var Methods []gatewayclient.Method

// Abilities are the CASL rules bound to the permissions.
var Abilities = []gatewayclient.Ability{
	{Action: []string{"read", "create", "update", "delete", "share", "use"}, Subject: []string{"Channel"}, Requires: "channels:read"},
	{Action: []string{"read", "create", "update", "delete", "share", "use"}, Subject: []string{"Template"}, Requires: "templates:read"},
	{Action: []string{"send"}, Subject: []string{"Notification"}, Requires: "notifications:send"},
	{Action: []string{"read"}, Subject: []string{"NotificationLog"}, Requires: "notifications:read"},
	{Action: []string{"manage"}, Subject: []string{"Message", "MessageCategory"}, Requires: "messages:manage"},
	{Action: []string{"read"}, Subject: []string{"Inbox"}, Requires: "inbox:read"},
	{Action: []string{"manage"}, Subject: []string{"NotificationGrant"}, Requires: "permissions:manage"},
	{Action: []string{"manage"}, Subject: []string{"NotificationBackup"}, Requires: "backup:manage"},
	{Action: []string{"read"}, Subject: []string{"NotificationStats", "NotificationAudit"}, Requires: "stats:read"},
}

// Nav lists the navigation contributions (the shell groups them under the module).
var Nav = []gatewayclient.NavEntry{
	{Title: "Inbox", Path: "/notification/inbox", Icon: "mdi-inbox-outline", Order: 200, Requires: "inbox:read"},
	{Title: "Channels", Path: "/notification/channels", Icon: "mdi-radio-tower", Order: 210, Requires: "channels:read"},
	{Title: "Templates", Path: "/notification/templates", Icon: "mdi-file-document-edit-outline", Order: 220, Requires: "templates:read"},
	{Title: "Log", Path: "/notification/log", Icon: "mdi-history", Order: 230, Requires: "notifications:read"},
	{Title: "Messages", Path: "/notification/messages", Icon: "mdi-message-text-outline", Order: 240, Requires: "messages:read"},
	{Title: "Categories", Path: "/notification/categories", Icon: "mdi-tag-multiple-outline", Order: 250, Requires: "messages:manage"},
	{Title: "Permissions", Path: "/notification/permissions", Icon: "mdi-shield-account-outline", Order: 260, Requires: "permissions:manage"},
}

// PermissionRefs lists "resource:action" for every declared permission.
func PermissionRefs() []string {
	out := make([]string, 0, len(Permissions))
	for _, p := range Permissions {
		out = append(out, p.Resource+":"+p.Action)
	}
	return out
}

// Routes derives the gateway routes from the OpenAPI document: every
// operation carries x-freya-permission (one of Permissions); there is no
// public route. x-freya-max-body-bytes and x-freya-timeout-seconds map to
// the route fields. Undeclared permissions are an error.
func Routes(doc *openapi3.T) ([]gatewayclient.Route, error) {
	known := map[string]bool{}
	for _, p := range PermissionRefs() {
		known[p] = true
	}
	var routes []gatewayclient.Route
	for p, item := range doc.Paths.Map() {
		for m, op := range item.Operations() {
			r := gatewayclient.Route{Method: strings.ToUpper(m), Path: p}
			perm, _ := op.Extensions[PermissionExtension].(string)
			public, _ := op.Extensions[PublicExtension].(bool)
			switch {
			case public:
				return nil, fmt.Errorf("notificationmanifest: %s %s must not be public", r.Method, p)
			case perm == "":
				return nil, fmt.Errorf("notificationmanifest: %s %s declares no permission", r.Method, p)
			case !known[perm]:
				return nil, fmt.Errorf("notificationmanifest: %s %s uses undeclared permission %q", r.Method, p, perm)
			default:
				r.Permission = perm
			}
			if v, ok := op.Extensions[BodyLimitExtension]; ok {
				n, ok := v.(float64)
				if !ok || n <= 0 {
					return nil, fmt.Errorf("notificationmanifest: %s %s has a bad body limit", r.Method, p)
				}
				r.MaxBodyBytes = uint64(n)
			}
			if v, ok := op.Extensions[ClientAddressExtension].(bool); ok && v {
				r.ClientAddress = true
			}
			if v, ok := op.Extensions[TimeoutExtension]; ok {
				n, ok := v.(float64)
				if !ok || n <= 0 || n > 600 {
					return nil, fmt.Errorf("notificationmanifest: %s %s has a bad timeout", r.Method, p)
				}
				r.Timeout = time.Duration(n) * time.Second
			}
			routes = append(routes, r)
		}
	}
	// The federated remote is reached through the gateway's per-module relay
	// (/m/notification/… -> the module's /ui/…), so the module does not own the
	// shared /ui prefix — declaring it would conflict with any other UI module
	// (e.g. warden) that also serves a remote.
	sort.Slice(routes, func(i, j int) bool { return routes[i].Method+" "+routes[i].Path < routes[j].Method+" "+routes[j].Path })
	return routes, nil
}

// Load parses the embedded document.
func Load() (*openapi3.T, error) {
	return openapi3.NewLoader().LoadFromData(openapi.Notification)
}

// Manifest builds the gateway manifest from the embedded OpenAPI document.
func Manifest() (gatewayclient.Manifest, error) {
	doc, err := Load()
	if err != nil {
		return gatewayclient.Manifest{}, err
	}
	routes, err := Routes(doc)
	if err != nil {
		return gatewayclient.Manifest{}, err
	}
	return gatewayclient.Manifest{
		Module: Module, DisplayName: DisplayName, Version: Version,
		Prefixes:    []string{"/api/notification"},
		Routes:      routes,
		Methods:     Methods,
		Permissions: Permissions,
		Abilities:   Abilities,
		Exposes:     []string{"./routes", "./nav", "./header"},
		Nav:         Nav,
	}, nil
}
