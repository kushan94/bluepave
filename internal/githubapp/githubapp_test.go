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
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kushan94/bluepave/internal/run"
)

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}))
}

func TestJWTIsSignedByTheAppKey(t *testing.T) {
	k, keyPEM := testKey(t)
	now := time.Unix(1_800_000_000, 0)
	tok, err := JWT(42, keyPEM, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token = %q", tok)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&k.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("signature: %v", err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	_ = json.Unmarshal(raw, &claims)
	if claims["iss"] != "42" || claims["exp"].(float64) <= float64(now.Unix()) || claims["exp"].(float64) > float64(now.Add(10*time.Minute).Unix()) {
		t.Errorf("claims = %v", claims)
	}
}

func TestManifestAsksOnlyForWhatThePlatformNeeds(t *testing.T) {
	m := NewManifest("acme-platform", "https://github.com/acme/acme-platform", "http://127.0.0.1:1/callback")
	if m.Public || m.HookAttributes["active"] != false {
		t.Errorf("manifest = %+v", m)
	}
	if len(m.DefaultPermissions) != 5 || m.DefaultPermissions["contents"] != "write" || m.DefaultPermissions["administration"] != "" {
		t.Errorf("permissions = %v", m.DefaultPermissions)
	}
}

func TestFlowServesTheManifestAndReturnsTheCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan string, 1)
	f := Flow{Owner: "acme", AppName: "acme-platform", Homepage: "https://github.com/acme/acme-platform", Open: func(u string) { started <- u }}
	codes := make(chan string, 1)
	go func() {
		code, err := f.Code(ctx)
		if err != nil {
			t.Error(err)
		}
		codes <- code
	}()
	start := <-started
	resp, err := http.Get(start)
	if err != nil {
		t.Fatal(err)
	}
	body := new(strings.Builder)
	_, _ = io.Copy(body, resp.Body)
	if !strings.Contains(body.String(), `action="https://github.com/settings/apps/new?state=`) {
		t.Fatalf("page:\n%s", body)
	}
	state := strings.SplitN(strings.SplitN(body.String(), "?state=", 2)[1], `"`, 2)[0]
	// A callback without the right state is refused.
	if r, _ := http.Get(start + "callback?code=evil&state=wrong"); r.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong state accepted: %d", r.StatusCode)
	}
	if _, err := http.Get(start + "callback?code=abc&state=" + url.QueryEscape(state)); err != nil {
		t.Fatal(err)
	}
	if code := <-codes; code != "abc" {
		t.Errorf("code = %q", code)
	}
}

func TestConvert(t *testing.T) {
	rec := &run.Recorder{Responses: map[string]string{
		"gh api --method POST /app-manifests/abc/conversions": `{"id": 7, "slug": "acme-platform", "client_id": "Iv1", "pem": "KEY", "owner": {"login": "acme"}}`,
	}}
	app, err := Convert(context.Background(), rec, "abc")
	if err != nil || app.ID != 7 || app.ClientID != "Iv1" || app.PrivateKey != "KEY" {
		t.Errorf("app = %+v, err = %v", app, err)
	}
}

func TestInstallationID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Path != "/app/installations" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`[{"id": 1, "account": {"login": "someone"}}, {"id": 99, "account": {"login": "ACME"}}]`))
	}))
	defer srv.Close()
	id, err := InstallationID(context.Background(), srv.Client(), srv.URL, "tok", "acme")
	if err != nil || id != 99 {
		t.Errorf("id = %d, err = %v", id, err)
	}
}
