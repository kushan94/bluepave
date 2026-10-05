// Package keyvault stores platform secrets in an environment's Key Vault. Values never appear on a
// command line or in output: they're passed to the Azure CLI through a private temporary file.
package keyvault

import (
	"context"
	"os"
	"strings"

	"github.com/kushan94/bluepave/internal/run"
)

// Client wraps `az keyvault secret`.
type Client struct {
	Runner run.Runner
	Vault  string
}

// Has reports whether the secret exists.
func (c Client) Has(ctx context.Context, name string) (bool, error) {
	out, err := c.Runner.Run(ctx, "az", "keyvault", "secret", "list", "--vault-name", c.Vault,
		"--query", "[?name=='"+name+"'] | length(@)", "--output", "tsv")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// Get returns a secret's value. The caller must not print it.
func (c Client) Get(ctx context.Context, name string) (string, error) {
	out, err := c.Runner.Run(ctx, "az", "keyvault", "secret", "show", "--vault-name", c.Vault, "--name", name,
		"--query", "value", "--output", "tsv")
	return strings.TrimRight(string(out), "\r\n"), err
}

// Set stores a secret value.
func (c Client) Set(ctx context.Context, name, value string) error {
	f, err := os.CreateTemp("", "bluepave-secret-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(value); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_, err = c.Runner.Run(ctx, "az", "keyvault", "secret", "set", "--vault-name", c.Vault, "--name", name,
		"--file", f.Name(), "--output", "none")
	return err
}
