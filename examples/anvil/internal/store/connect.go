package store

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// postgresScope is the Entra resource for Azure Database for PostgreSQL.
const postgresScope = "https://ossrdbms-aad.database.windows.net/.default"

// Connect opens a connection pool from the standard libpq environment variables (PGHOST,
// PGUSER, PGDATABASE, PGSSLMODE, ...), which the platform's AppDatabase API provides (its
// ConfigMap <name>-database).
//
// With DATABASE_AUTH=entra (the default) every new connection gets a fresh Entra access token as
// its password, for the identity DATABASE_CLIENT_ID (Workload Identity), so no password exists
// anywhere. DATABASE_AUTH=password uses PGPASSWORD instead, for a local PostgreSQL.
func Connect(ctx context.Context) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL settings: %w", err)
	}
	cfg.MaxConns = 5
	// A span per query, as children of the request or job that ran it.
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	// Entra tokens live about an hour; recycle connections well before that.
	cfg.MaxConnLifetime = 30 * time.Minute

	if os.Getenv("DATABASE_AUTH") != "password" {
		cred, err := azidentity.NewWorkloadIdentityCredential(&azidentity.WorkloadIdentityCredentialOptions{
			ClientID: os.Getenv("DATABASE_CLIENT_ID"),
		})
		if err != nil {
			return nil, fmt.Errorf("workload identity: %w", err)
		}
		cfg.BeforeConnect = entraPassword(cred)
	}

	return pgxpool.NewWithConfig(ctx, cfg)
}

// entraPassword sets an Entra token as the password of each new connection. azidentity caches
// the token and refreshes it before it expires, so this is cheap.
func entraPassword(cred azcore.TokenCredential) func(context.Context, *pgx.ConnConfig) error {
	return func(ctx context.Context, cc *pgx.ConnConfig) error {
		tok, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{postgresScope}})
		if err != nil {
			return fmt.Errorf("get Entra token for PostgreSQL: %w", err)
		}
		cc.Password = tok.Token
		return nil
	}
}
