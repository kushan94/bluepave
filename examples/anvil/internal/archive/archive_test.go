package archive

import (
	"encoding/json"
	"testing"

	"bluepave.dev/examples/anvil/internal/jobs"
)

func TestFromEnvDisabledWithoutEndpoint(t *testing.T) {
	t.Setenv("AZURE_STORAGE_BLOB_ENDPOINT", "")
	a, err := FromEnv()
	if err != nil || a != nil {
		t.Fatalf("FromEnv() = %v, %v; want nil, nil", a, err)
	}
}

func TestFromEnvIncomplete(t *testing.T) {
	t.Setenv("AZURE_STORAGE_BLOB_ENDPOINT", "https://example.blob.core.windows.net/")
	t.Setenv("AZURE_STORAGE_CONTAINER", "")
	t.Setenv("AZURE_STORAGE_CLIENT_ID", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() with an endpoint but no container or client ID: want an error")
	}
}

func TestEncode(t *testing.T) {
	job := jobs.Job{ID: 42, Kind: "reverse", Input: "abc", Status: jobs.StatusDone, Result: "cba"}
	if got := BlobName(job); got != "jobs/42.json" {
		t.Errorf("BlobName = %q", got)
	}
	body, err := Encode(job)
	if err != nil {
		t.Fatal(err)
	}
	var back jobs.Job
	if err := json.Unmarshal(body, &back); err != nil || back.ID != 42 || back.Result != "cba" {
		t.Errorf("round trip = %+v, %v", back, err)
	}
}
