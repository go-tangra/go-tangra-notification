package app

import (
	"context"

	"github.com/go-tangra/go-tangra-notification/v4/internal/notify"
)

// SeedSystemEmail prepares central email delivery at start (feature 017),
// after the migrations and before anything is served: the managed platform
// channel from platform_email. Failures are logged, never fatal: the
// module keeps serving its other functions and system sends report
// email_not_configured until the next start fixes it. The relay password
// never reaches a log line.
func (a *App) SeedSystemEmail(ctx context.Context) {
	tenant := a.Cfg.PlatformTenantID
	var pe *notify.PlatformEmail
	if p := a.Cfg.PlatformEmail; p != nil {
		pw, err := p.ReadPassword()
		if err != nil {
			a.Log.Error("platform email disabled: password file", "err", err)
			return
		}
		pe = &notify.PlatformEmail{Host: p.Host, Port: p.Port, TLS: p.Mode(), Username: p.Username, Password: pw, From: p.From, ReplyTo: p.ReplyTo}
	}
	res, err := a.Channels.EnsurePlatformChannel(ctx, tenant, pe)
	switch {
	case err != nil:
		a.Log.Error("platform email channel", "tenant", tenant, "err", err)
	case pe == nil:
		a.Log.Warn("platform email disabled: no platform_email configured; system email is sent only for tenants with their own default email channel", "result", string(res))
	default:
		a.Log.Info("platform email channel ready", "tenant", tenant, "host", pe.Host, "port", pe.Port, "tls", pe.TLS, "result", string(res))
	}
}
