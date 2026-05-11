package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyncE2E runs the real unictl binary against an httptest server
// that fakes the controller's CRUD endpoints. The point is to exercise
// the full path: directory classification, schema validation, plan
// build, JSON envelope. This is the kind of test that catches wiring
// bugs unit tests miss.
func TestSyncE2E(t *testing.T) {
	// Stand-in controller: returns empty lists on every GET and
	// echoes back the body on every POST. Enough for a dry-run plan
	// to reach the JSON envelope.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	// Build the binary.
	bin := filepath.Join(t.TempDir(), "unictl")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// Drop a directory containing both events and resources.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events.cue"), []byte(`
events: [
	{at: "2026-01-01T00:00:00Z", type: "block", mac: "aa:bb:cc:dd:ee:01", reason: "iot"},
]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources.cue"), []byte(`
networks: [{name: "iot", purpose: "corporate", vlan: 42}]
firewall_rules: [{name: "drop-iot", action: "drop", ruleset: "LAN_IN"}]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	run := exec.Command(bin, "sync", dir)
	run.Env = append(os.Environ(),
		"UNIFI_HOST="+srv.URL,
		"UNIFI_API_KEY=test-key",
		"UNIFI_INSECURE=1",
	)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("sync failed: %v\n%s", err, out)
	}

	var env struct {
		Schema string `json:"$schema"`
		Data   struct {
			Events    map[string]any `json:"events"`
			Resources map[string]any `json:"resources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if env.Schema != "unifi-v1" {
		t.Fatalf("schema=%q", env.Schema)
	}
	if env.Data.Events == nil {
		t.Fatalf("expected events sub-envelope, got %s", out)
	}
	if env.Data.Resources == nil {
		t.Fatalf("expected resources sub-envelope, got %s", out)
	}
	// Resources plan should propose two creates (network + firewall rule).
	rsPlan, _ := env.Data.Resources["plan"].([]any)
	if len(rsPlan) != 2 {
		t.Fatalf("expected 2 resource ops, got %d: %s", len(rsPlan), out)
	}
	if !strings.Contains(string(out), "create_network") || !strings.Contains(string(out), "create_firewall_rule") {
		t.Fatalf("expected create_network and create_firewall_rule ops, got %s", out)
	}
}
