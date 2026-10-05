package cache

import "testing"

// Without the AppCache settings the app runs without a cache.
func TestFromEnvWithoutCache(t *testing.T) {
	t.Setenv("CACHE_HOST", "")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.(Noop); !ok {
		t.Errorf("got %T, want Noop", c)
	}
}

// The keys of the AppCache ConfigMap (in-cluster Valkey) give a client; it connects lazily.
func TestFromEnvAppCache(t *testing.T) {
	t.Setenv("CACHE_HOST", "sessions-cache.anvil-dev.svc.cluster.local")
	t.Setenv("CACHE_PORT", "6379")
	t.Setenv("CACHE_TLS", "false")
	t.Setenv("CACHE_AUTH", "none")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, ok := c.(*Redis)
	if !ok {
		t.Fatalf("got %T, want *Redis", c)
	}
	if got := r.client.Options().Addr; got != "sessions-cache.anvil-dev.svc.cluster.local:6379" {
		t.Errorf("addr = %q", got)
	}
	if r.client.Options().TLSConfig != nil {
		t.Error("TLS on without CACHE_TLS=true")
	}
}
