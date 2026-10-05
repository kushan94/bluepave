// Package cache wraps Valkey / Redis for two optional jobs: caching finished jobs, and waking
// workers up when a job is queued. The app works without it (slower), so every error here is
// logged by callers and otherwise ignored.
package cache

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

// redisScope is the Entra resource for Azure Managed Redis.
const redisScope = "https://redis.azure.com/.default"

// ErrMiss is returned when a key is not cached.
var ErrMiss = errors.New("cache miss")

// Cache is the subset of Redis the app uses.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Publish(ctx context.Context, channel, message string) error
	Subscribe(ctx context.Context, channel string) <-chan struct{}
	Ping(ctx context.Context) error
	Close() error
}

// FromEnv connects to the cache the platform's AppCache API describes (its ConfigMap
// <name>-cache): CACHE_HOST and CACHE_PORT. Without CACHE_HOST it returns a no-op cache.
//
//	CACHE_AUTH=none   (default) no authentication: Valkey in the app's namespace, reachable only
//	                  from that namespace (NetworkPolicy)
//	CACHE_AUTH=entra  Azure Managed Redis: username = the identity's object ID (CACHE_USERNAME),
//	                  password = an Entra token for CACHE_CLIENT_ID (Workload Identity)
//
// CACHE_TLS=true turns on TLS (Azure Managed Redis).
func FromEnv() (Cache, error) {
	host := os.Getenv("CACHE_HOST")
	if host == "" {
		return Noop{}, nil
	}
	port := os.Getenv("CACHE_PORT")
	if port == "" {
		port = "6379"
	}
	opts := &redis.Options{
		Addr:         net.JoinHostPort(host, port),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	}
	if os.Getenv("CACHE_TLS") == "true" {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	}
	if os.Getenv("CACHE_AUTH") == "entra" {
		cred, err := azidentity.NewWorkloadIdentityCredential(&azidentity.WorkloadIdentityCredentialOptions{
			ClientID: os.Getenv("CACHE_CLIENT_ID"),
		})
		if err != nil {
			return nil, fmt.Errorf("workload identity: %w", err)
		}
		username := os.Getenv("CACHE_USERNAME")
		opts.CredentialsProviderContext = func(ctx context.Context) (string, string, error) {
			tok, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{redisScope}})
			if err != nil {
				return "", "", fmt.Errorf("get Entra token for Redis: %w", err)
			}
			return username, tok.Token, nil
		}
	}
	client := redis.NewClient(opts)
	// A span per command, as children of the request or job that sent it.
	if err := redisotel.InstrumentTracing(client); err != nil {
		return nil, fmt.Errorf("instrument cache client: %w", err)
	}
	return &Redis{client: client}, nil
}

// Redis is a Cache backed by Valkey or Redis.
type Redis struct {
	client *redis.Client
}

func (r *Redis) Get(ctx context.Context, key string) ([]byte, error) {
	b, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrMiss
	}
	return b, err
}

func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *Redis) Publish(ctx context.Context, channel, message string) error {
	return r.client.Publish(ctx, channel, message).Err()
}

// Subscribe returns a channel that receives a signal for every message on channel, until ctx
// is cancelled. go-redis reconnects by itself if the server restarts.
func (r *Redis) Subscribe(ctx context.Context, channel string) <-chan struct{} {
	out := make(chan struct{}, 1)
	sub := r.client.Subscribe(ctx, channel)
	go func() {
		defer close(out)
		defer sub.Close()
		messages := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-messages:
				if !ok {
					return
				}
				select {
				case out <- struct{}{}:
				default: // a wake-up is already pending
				}
			}
		}
	}()
	return out
}

func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }
func (r *Redis) Close() error                   { return r.client.Close() }

// Noop is a Cache that stores nothing; used when no cache is configured.
type Noop struct{}

func (Noop) Get(context.Context, string) ([]byte, error)              { return nil, ErrMiss }
func (Noop) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (Noop) Publish(context.Context, string, string) error            { return nil }
func (Noop) Subscribe(context.Context, string) <-chan struct{}        { return nil }
func (Noop) Ping(context.Context) error                               { return nil }
func (Noop) Close() error                                             { return nil }
