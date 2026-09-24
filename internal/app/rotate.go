package app

import (
	"context"
	"fmt"

	"github.com/go-tangra/go-tangra-notification/v4/internal/sealed"
)

// RotateKEK re-seals every channel's settings with the key in newKEKPath,
// opening them with the current envelope. Tenants are enumerated through the
// system scope; each channel is rewritten in its own tenant transaction.
func RotateKEK(ctx context.Context, a *App, newKEKPath string) (int, error) {
	kek, err := sealed.LoadKEK("file", newKEKPath, "")
	if err != nil {
		return 0, err
	}
	next, err := sealed.NewEnvelope(kek)
	if err != nil {
		return 0, err
	}
	channels, err := a.Store.AllChannelsSystem(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range channels {
		clear, err := a.Envelope.Open(c.SettingsSealed, sealed.AD(c.ID))
		if err != nil {
			return n, fmt.Errorf("channel %s: %w", c.ID, err)
		}
		blob, err := next.Seal(clear, sealed.AD(c.ID))
		if err != nil {
			return n, err
		}
		c.SettingsSealed = blob
		if err := a.Repo.UpdateChannel(ctx, c); err != nil {
			return n, fmt.Errorf("channel %s: %w", c.ID, err)
		}
		n++
	}
	return n, nil
}
