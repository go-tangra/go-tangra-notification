package data

import (
	"context"
	"os"

	"entgo.io/ent/dialect/sql"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/lib/pq"

	entCrud "github.com/tx7do/go-crud/entgo"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	entBootstrap "github.com/tx7do/kratos-bootstrap/database/ent"

	"github.com/go-tangra/go-tangra-notification/internal/data/ent"
	"github.com/go-tangra/go-tangra-notification/internal/data/ent/migrate"

	_ "github.com/go-tangra/go-tangra-notification/internal/data/ent/runtime"
)

// NewEntClient creates an Ent ORM database client
func NewEntClient(ctx *bootstrap.Context) (*entCrud.EntClient[*ent.Client], func(), error) {
	l := ctx.NewLoggerHelper("ent/data/notification-service")

	cfg := ctx.GetConfig()
	if cfg == nil || cfg.Data == nil {
		l.Fatalf("failed getting config")
		return nil, func() {}, nil
	}

	cli := entBootstrap.NewEntClient(cfg, func(drv *sql.Driver) *ent.Client {
		opts := []ent.Option{ent.Driver(drv)}
		// M3: Only enable SQL query logging when explicitly requested
		// to prevent accidental exposure of secrets in query parameters.
		if os.Getenv("DEBUG_SQL") == "true" {
			opts = append(opts, ent.Log(func(a ...any) {
				l.Debug(a...)
			}))
		}
		client := ent.NewClient(opts...)
		if client == nil {
			l.Fatalf("failed creating ent client")
			return nil
		}

		if cfg.Data.Database.GetMigrate() {
			if err := client.Schema.Create(context.Background(), migrate.WithForeignKeys(true)); err != nil {
				l.Fatalf("failed creating schema resources: %v", err)
			}
		}

		return client
	})

	return cli, func() {
		if err := cli.Close(); err != nil {
			l.Error(err)
		}
	}, nil
}
