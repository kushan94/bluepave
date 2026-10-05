// Package githubapp creates the platform's GitHub App with GitHub's manifest flow, the only way to
// create an App without clicking through its settings by hand: a local page posts the App's
// manifest to GitHub, the user confirms in the browser, and GitHub redirects back with a one-time
// code that converts into the App's ID, client ID and private key.
//
// Argo CD and Kargo mint short-lived installation tokens from the key (no personal tokens, no
// deploy keys); the portal reads the catalog and opens pull requests with it.
package githubapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kushan94/bluepave/internal/run"
)

// Permissions the platform needs, and nothing else: read and push app repositories (Kargo's
// promotions, the portal's pull requests, Argo CD's reads), edit workflows (templates create
// them), read commit statuses (the portal's CI view).
var Permissions = map[string]string{
	"contents":      "write",
	"pull_requests": "write",
	"workflows":     "write",
	"metadata":      "read",
	"statuses":      "read",
}

// Manifest is the App definition posted to GitHub.
type Manifest struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	RedirectURL        string            `json:"redirect_url"`
	Public             bool              `json:"public"`
	HookAttributes     map[string]any    `json:"hook_attributes"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	DefaultEvents      []string          `json:"default_events"`
}

// NewManifest describes the platform's App. Webhooks are off: nothing listens for them.
func NewManifest(name, homepage, redirect string) Manifest {
	return Manifest{
		Name:               name,
		URL:                homepage,
		RedirectURL:        redirect,
		Public:             false,
		HookAttributes:     map[string]any{"url": homepage, "active": false},
		DefaultPermissions: Permissions,
		DefaultEvents:      []string{},
	}
}

// App is a created App's credentials. PrivateKey and the secrets are secrets: store them in Key
// Vault, never print or commit them.
type App struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	PrivateKey    string `json:"pem"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// NewURL is where the manifest is posted: the owner's organization settings, or the user's.
func NewURL(owner string, organization bool) string {
	if organization {
		return "https://github.com/organizations/" + owner + "/settings/apps/new"
	}
	return "https://github.com/settings/apps/new"
}

var page = template.Must(template.New("post").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Create the platform's GitHub App</title></head>
<body onload="document.forms[0].submit()">
<p>Sending the App's manifest to GitHub. If nothing happens, press the button.</p>
<form method="post" action="{{.Action}}?state={{.State}}">
<input type="hidden" name="manifest" value="{{.Manifest}}">
<button type="submit">Continue to GitHub</button>
</form></body></html>`))

// Flow runs the manifest flow's local side: a page that posts the manifest, and the callback
// that receives the code. Open prints the start URL (and opens a browser if it can).
type Flow struct {
	Owner        string
	Organization bool
	AppName      string
	Homepage     string
	Open         func(url string)
}

// Code serves the flow on 127.0.0.1 and returns the one-time code GitHub redirects back with.
func (f Flow) Code(ctx context.Context) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	base := "http://" + ln.Addr().String()
	state, err := randomState()
	if err != nil {
		return "", err
	}
	manifest, _ := json.Marshal(NewManifest(f.AppName, f.Homepage, base+"/callback"))
	codes := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_ = page.Execute(w, map[string]string{"Action": NewURL(f.Owner, f.Organization), "State": state, "Manifest": string(manifest)})
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// The state ties the callback to this run: another site can't feed us a code.
		if r.URL.Query().Get("state") != state || r.URL.Query().Get("code") == "" {
			http.Error(w, "unexpected callback", http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "The GitHub App was created. You can close this tab and return to the terminal.")
		select {
		case codes <- r.URL.Query().Get("code"):
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	f.Open(base + "/")
	select {
	case code := <-codes:
		return code, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Convert exchanges the one-time code for the App's credentials (POST /app-manifests/{code}/conversions).
func Convert(ctx context.Context, r run.Runner, code string) (*App, error) {
	out, err := r.Run(ctx, "gh", "api", "--method", "POST", "/app-manifests/"+code+"/conversions")
	if err != nil {
		return nil, err
	}
	var app App
	if err := json.Unmarshal(out, &app); err != nil {
		return nil, fmt.Errorf("reading the created App: %w", err)
	}
	if app.ID == 0 || app.PrivateKey == "" {
		return nil, errors.New("GitHub returned no App ID or private key")
	}
	return &app, nil
}

// JWT returns a short-lived token that authenticates as the App (to list its installations).
func JWT(appID int64, privateKeyPEM string, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", errors.New("the App's private key isn't PEM")
	}
	key, err := parseRSA(block.Bytes)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(), // GitHub allows some clock skew
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": fmt.Sprint(appID),
	})
	signing := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

func parseRSA(der []byte) (*rsa.PrivateKey, error) {
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("the App's private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the App's private key isn't RSA")
	}
	return rk, nil
}

// InstallationID returns the App's installation on the owner's account, or 0 if it isn't
// installed. It calls the API directly, so the App's token never appears on a command line.
func InstallationID(ctx context.Context, client *http.Client, apiBase, jwt, owner string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(apiBase, "/")+"/app/installations", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("listing the App's installations: %s", resp.Status)
	}
	var installs []struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&installs); err != nil {
		return 0, fmt.Errorf("listing installations: %w", err)
	}
	for _, in := range installs {
		if strings.EqualFold(in.Account.Login, owner) {
			return in.ID, nil
		}
	}
	return 0, nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Delete deletes the App itself (DELETE /app, authenticated as the App). Its installations go with it.
func Delete(ctx context.Context, client *http.Client, apiBase, jwt string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimSuffix(apiBase, "/")+"/app", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("deleting the GitHub App: %s", resp.Status)
	}
	return nil
}
