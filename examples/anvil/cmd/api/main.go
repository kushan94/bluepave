// Command api is the Anvil REST API: it validates and stores jobs in PostgreSQL, and
// serves them, caching finished ones in Valkey/Redis.
package main

import (
	"context"
	"os"
	"strconv"
	"time"

	"bluepave.dev/examples/anvil/internal/cache"
	"bluepave.dev/examples/anvil/internal/server"
	"bluepave.dev/examples/anvil/internal/store"
)

func main() {
	log := server.Logger("api")
	ctx, stop := server.SignalContext()
	defer stop()
	defer server.Start(ctx, log, "api")()

	pool, err := store.Connect(ctx)
	if err != nil {
		log.Error("connect to PostgreSQL", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	db := store.New(pool)

	// The API owns the schema. Retry for a while: after a nightly stop the database can come
	// up after the cluster.
	for attempt := 1; ; attempt++ {
		err := db.Migrate(ctx)
		if err == nil {
			break
		}
		if attempt == 30 || ctx.Err() != nil {
			log.Error("migrate", "err", err)
			os.Exit(1)
		}
		log.Warn("migrate failed, retrying", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}

	c, err := cache.FromEnv()
	if err != nil {
		log.Error("configure cache", "err", err)
		os.Exit(1)
	}
	defer c.Close()
	if err := c.Ping(context.WithoutCancel(ctx)); err != nil {
		log.Warn("cache unavailable; continuing without it", "err", err)
	}

	faultRate, err := strconv.ParseFloat(server.Env("FAULT_INJECTION_RATE", "0"), 64)
	if err != nil || faultRate < 0 || faultRate > 1 {
		log.Error("FAULT_INJECTION_RATE must be between 0 and 1", "value", os.Getenv("FAULT_INJECTION_RATE"))
		os.Exit(1)
	}
	if faultRate > 0 {
		log.Warn("fault injection enabled", "rate", faultRate)
	}

	a := &api{store: db, cache: c, log: log, faultRate: faultRate}
	if err := server.Run(ctx, log, ":"+server.Env("PORT", "8080"), a.routes()); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
