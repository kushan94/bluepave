//go:build !windows

package run

import (
	"context"
	"testing"
	"time"
)

// A timed-out command returns even when a child it started still holds its output open, as az's
// Python does when the az shell script is killed.
func TestExecTimeoutWithChildHoldingOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Exec{}.Run(ctx, "sh", "-c", "sleep 60 & wait")
	if err == nil {
		t.Fatal("want an error from the timed-out command")
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("Run returned after %s: it waited for the child", d)
	}
}
