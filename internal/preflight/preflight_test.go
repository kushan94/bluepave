package preflight

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/kushan94/bluepave/internal/run"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func trial(t *testing.T) *Profile {
	t.Helper()
	p, err := LoadProfile("../..", "trial")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func check(t *testing.T, skus, usage string, clusters int) *Result {
	t.Helper()
	rec := &run.Recorder{Responses: map[string]string{
		"az rest --method get --url https://management.azure.com/subscriptions/s/providers/Microsoft.Compute/skus": fixture(t, skus),
		"az vm list-usage": fixture(t, usage),
		"az rest --method get --url https://management.azure.com/subscriptions/s/providers/Microsoft.DBforPostgreSQL/locations": fixture(t, "postgres.json"),
	}}
	res, err := Check(context.Background(), rec, Input{SubscriptionID: "s", Region: "eastasia", Profile: trial(t), Clusters: clusters, Postgres: true})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestTrialFits(t *testing.T) {
	res := check(t, "skus-ok.json", "usage-free.json", 1)
	if len(res.Problems) > 0 {
		t.Errorf("problems: %v", res.Problems)
	}
	// trial: system + apps (2 x 2 vCPUs regular), spot (1 x 2): exactly the Free Trial's quota.
	notes := strings.Join(res.Notes, "\n")
	for _, want := range []string{"Total Regional vCPUs in eastasia: 4 of 4 free, 4 needed", "Low-priority vCPUs in eastasia: 3 of 3 free, 2 needed"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
}

// The real failure: the size isn't offered to a Free Trial subscription in the region.
func TestSizeNotAvailableForSubscription(t *testing.T) {
	res := check(t, "skus-restricted.json", "usage-free.json", 1)
	if len(res.Problems) != 3 || !strings.Contains(res.Problems[0], "isn't available to this subscription") {
		t.Errorf("want one problem per pool, got %v", res.Problems)
	}
}

func TestZoneRestricted(t *testing.T) {
	res := check(t, "skus-zone-restricted.json", "usage-free.json", 1)
	if len(res.Problems) == 0 || !strings.Contains(res.Problems[0], "zone(s) 2") {
		t.Errorf("want a zone problem, got %v", res.Problems)
	}
}

func TestQuotaTaken(t *testing.T) {
	res := check(t, "skus-ok.json", "usage-taken.json", 1)
	if len(res.Problems) == 0 || !strings.Contains(strings.Join(res.Problems, "\n"), "needs 4 vCPUs, 0 of 4 are free") {
		t.Errorf("want a quota problem, got %v", res.Problems)
	}
}

// Re-running up on an existing cluster doesn't need new quota.
func TestQuotaSkippedForExistingClusters(t *testing.T) {
	res := check(t, "skus-ok.json", "usage-taken.json", 0)
	if len(res.Problems) > 0 {
		t.Errorf("problems: %v", res.Problems)
	}
}
