package discovered

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	root := t.TempDir()
	ids, err := Load(root)
	if err != nil || len(ids) != 0 {
		t.Fatalf("missing file: ids=%v err=%v", ids, err)
	}
	ids.Set("t-1", "azure", "tenantId")
	ids.SetOutputs("", "dns", map[string]any{"nameServers": []any{"ns1", "ns2"}})
	ids.SetOutputs("dev", "aks", map[string]any{"clusterName": "aks-acme-dev-we"})
	ids.Set("app-1", "environments", "dev", "aks", "extra") // an ID the CLI created stays
	ids.SetOutputs("dev", "aks", map[string]any{"clusterName": "aks-acme-dev-we"})
	if err := ids.Save(root); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, Path))
	if !strings.HasPrefix(string(data), "# IDs that `bluepave up`") || !strings.Contains(string(data), "\ndiscovered:\n") {
		t.Errorf("file:\n%s", data)
	}
	back, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Get("azure", "tenantId"); got != "t-1" {
		t.Errorf("tenantId = %v", got)
	}
	if got := back.Outputs("dev", "aks"); got["clusterName"] != "aks-acme-dev-we" || got["extra"] != "app-1" {
		t.Errorf("aks outputs = %v", got)
	}
	if got := back.Outputs("", "dns")["nameServers"]; len(got.([]any)) != 2 {
		t.Errorf("dns nameServers = %v", got)
	}
}
