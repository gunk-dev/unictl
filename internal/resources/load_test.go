package resources

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValidConfig(t *testing.T) {
	body := `
networks: [{name: "iot", purpose: "corporate", subnet: "10.0.42.0/24", vlan: 42}]
wlans: [{name: "home-iot", network: "iot", security: "wpapsk", password_env: "WLAN_HOME_PSK", band: "both"}]
firewall_rules: [{name: "drop-iot", action: "drop", ruleset: "LAN_IN"}]
port_forwards: [{name: "ha", src_port: "8123", dst_address: "10.0.42.10", dst_port: "8123", protocol: "tcp"}]
`
	p := writeTemp(t, "resources.cue", body)
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Networks) != 1 || got.Networks[0].Name != "iot" || got.Networks[0].VLAN != 42 {
		t.Fatalf("networks: %+v", got.Networks)
	}
	if len(got.Wlans) != 1 || got.Wlans[0].PasswordEnv != "WLAN_HOME_PSK" {
		t.Fatalf("wlans: %+v", got.Wlans)
	}
	if len(got.FirewallRules) != 1 || got.FirewallRules[0].Action != "drop" {
		t.Fatalf("firewall_rules: %+v", got.FirewallRules)
	}
	if len(got.PortForwards) != 1 || got.PortForwards[0].Protocol != "tcp" {
		t.Fatalf("port_forwards: %+v", got.PortForwards)
	}
}

func TestLoadRejectsInvalidEnum(t *testing.T) {
	// firewall action is pinned to accept|drop|reject. "yeet" should fail.
	body := `firewall_rules: [{name: "x", action: "yeet", ruleset: "LAN_IN"}]`
	p := writeTemp(t, "resources.cue", body)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for invalid firewall action, got nil")
	}
}

func TestLoadRejectsMissingRequiredField(t *testing.T) {
	// Networks require both name and purpose; this drops purpose.
	body := `networks: [{name: "iot"}]`
	p := writeTemp(t, "resources.cue", body)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for missing network.purpose, got nil")
	}
}

func TestLoadRejectsBadVLAN(t *testing.T) {
	body := `networks: [{name: "iot", purpose: "corporate", vlan: 9999}]`
	p := writeTemp(t, "resources.cue", body)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for out-of-range vlan, got nil")
	}
}

func TestLoadDirectoryOnlyPicksResourceFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "resources.cue"), []byte(
		`networks: [{name: "iot", purpose: "corporate"}]`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	// An unrelated CUE file in the same directory should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "events.cue"), []byte(
		`events: [{at: "2026-01-01T00:00:00Z", type: "block", mac: "aa:bb:cc:dd:ee:01"}]`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load dir: %v", err)
	}
	if len(got.Networks) != 1 {
		t.Fatalf("expected one network, got %+v", got)
	}
}

func TestLoadExampleFixture(t *testing.T) {
	// End-to-end sanity check on the example shipped in the repo.
	wd, _ := os.Getwd()
	example := filepath.Join(wd, "..", "..", "examples", "resources.cue")
	got, err := Load(example)
	if err != nil {
		t.Fatalf("Load example: %v", err)
	}
	if len(got.Networks) == 0 || len(got.Wlans) == 0 || len(got.FirewallRules) == 0 || len(got.PortForwards) == 0 {
		t.Fatalf("example fixture missing a kind: %+v", got)
	}
	if got.Wlans[0].PasswordEnv == "" {
		t.Fatalf("example WLAN should reference password_env, got %+v", got.Wlans[0])
	}
}

func TestResolvePSKUnsetEnvFailsLoud(t *testing.T) {
	// Apply path: missing env var should produce a clear error rather than
	// silently sending an empty password to the controller.
	_, err := ResolvePSK("wpapsk", "WLAN_DEFINITELY_NOT_SET_TEST_VAR")
	if err == nil {
		t.Fatal("expected error for unset env var, got nil")
	}
}

func TestResolvePSKOpenIgnoresEnv(t *testing.T) {
	v, err := ResolvePSK("open", "")
	if err != nil {
		t.Fatalf("open WLAN without env should not error: %v", err)
	}
	if v != "" {
		t.Fatalf("expected empty PSK for open WLAN, got %q", v)
	}
}

func TestResolvePSKReadsEnv(t *testing.T) {
	t.Setenv("UNICTL_TEST_PSK", "hunter2")
	v, err := ResolvePSK("wpapsk", "UNICTL_TEST_PSK")
	if err != nil {
		t.Fatalf("ResolvePSK: %v", err)
	}
	if v != "hunter2" {
		t.Fatalf("expected hunter2, got %q", v)
	}
}
