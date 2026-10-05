// Package archive copies finished jobs to Azure Blob Storage as JSON, one blob per job. It is
// optional and best-effort: PostgreSQL stays the source of truth, so callers log errors and go on.
//
// The storage comes from the platform's AppStorage API, which writes a ConfigMap with the
// AZURE_STORAGE_* variables read here. The app signs in with Workload Identity as the identity
// AppStorage created (AZURE_STORAGE_CLIENT_ID). The pod's own AZURE_CLIENT_ID stays the identity
// used for PostgreSQL: one service account can be trusted by several identities.
package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"

	"bluepave.dev/examples/anvil/internal/jobs"
)

// Archiver stores finished jobs.
type Archiver interface {
	Put(ctx context.Context, job jobs.Job) error
}

// BlobName is where a job is stored in the container.
func BlobName(job jobs.Job) string {
	return fmt.Sprintf("jobs/%d.json", job.ID)
}

// Encode is the stored form of a job.
func Encode(job jobs.Job) ([]byte, error) {
	return json.MarshalIndent(job, "", "  ")
}

// FromEnv returns a blob archiver when AZURE_STORAGE_BLOB_ENDPOINT is set, and nil otherwise
// (archiving disabled).
func FromEnv() (Archiver, error) {
	endpoint := os.Getenv("AZURE_STORAGE_BLOB_ENDPOINT")
	if endpoint == "" {
		return nil, nil
	}
	container := os.Getenv("AZURE_STORAGE_CONTAINER")
	clientID := os.Getenv("AZURE_STORAGE_CLIENT_ID")
	if container == "" || clientID == "" {
		return nil, fmt.Errorf("AZURE_STORAGE_BLOB_ENDPOINT is set but AZURE_STORAGE_CONTAINER or AZURE_STORAGE_CLIENT_ID is not")
	}
	cred, err := azidentity.NewWorkloadIdentityCredential(&azidentity.WorkloadIdentityCredentialOptions{ClientID: clientID})
	if err != nil {
		return nil, fmt.Errorf("workload identity: %w", err)
	}
	client, err := azblob.NewClient(endpoint, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("blob client: %w", err)
	}
	return &blobArchiver{client: client, container: container}, nil
}

type blobArchiver struct {
	client    *azblob.Client
	container string
}

func (a *blobArchiver) Put(ctx context.Context, job jobs.Job) error {
	body, err := Encode(job)
	if err != nil {
		return err
	}
	_, err = a.client.UploadStream(ctx, a.container, BlobName(job), bytes.NewReader(body), nil)
	return err
}
