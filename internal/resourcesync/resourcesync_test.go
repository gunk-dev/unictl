package resourcesync

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gunk-dev/unictl/internal/resources"
	"github.com/gunk-dev/unictl/internal/unifi"
)

// fakeController is an in-memory stand-in for *unifi.Client. Tests
// assert against the recorded create/update/delete calls rather than
// spinning up an HTTP server, since the controller protocol is already
// exercised by internal/unifi tests.
type fakeController struct {
	networks      []unifi.Network
	wlans         []unifi.WlanConf
	firewallRules []unifi.FirewallRule
	portForwards  []unifi.PortForward

	createdNets []unifi.Network
	updatedNets []unifi.Network
	deletedNets []string

	createdWlans []unifi.WlanConf
	updatedWlans []unifi.WlanConf
	deletedWlans []string

	createdFW []unifi.FirewallRule
	updatedFW []unifi.FirewallRule
	deletedFW []string

	createdPF []unifi.PortForward
	updatedPF []unifi.PortForward
	deletedPF []string
}

func (f *fakeController) Site() string { return "default" }

func (f *fakeController) ListNetworks(ctx context.Context) ([]unifi.Network, error) {
	return f.networks, nil
}
func (f *fakeController) CreateNetwork(ctx context.Context, n unifi.Network) (unifi.Network, error) {
	f.createdNets = append(f.createdNets, n)
	return n, nil
}
func (f *fakeController) UpdateNetwork(ctx context.Context, n unifi.Network) error {
	f.updatedNets = append(f.updatedNets, n)
	return nil
}
func (f *fakeController) DeleteNetwork(ctx context.Context, id string) error {
	f.deletedNets = append(f.deletedNets, id)
	return nil
}

func (f *fakeController) ListWlans(ctx context.Context) ([]unifi.WlanConf, error) {
	return f.wlans, nil
}
func (f *fakeController) CreateWlan(ctx context.Context, w unifi.WlanConf) (unifi.WlanConf, error) {
	f.createdWlans = append(f.createdWlans, w)
	return w, nil
}
func (f *fakeController) UpdateWlan(ctx context.Context, w unifi.WlanConf) error {
	f.updatedWlans = append(f.updatedWlans, w)
	return nil
}
func (f *fakeController) DeleteWlan(ctx context.Context, id string) error {
	f.deletedWlans = append(f.deletedWlans, id)
	return nil
}

func (f *fakeController) ListFirewallRules(ctx context.Context) ([]unifi.FirewallRule, error) {
	return f.firewallRules, nil
}
func (f *fakeController) CreateFirewallRule(ctx context.Context, r unifi.FirewallRule) (unifi.FirewallRule, error) {
	f.createdFW = append(f.createdFW, r)
	return r, nil
}
func (f *fakeController) UpdateFirewallRule(ctx context.Context, r unifi.FirewallRule) error {
	f.updatedFW = append(f.updatedFW, r)
	return nil
}
func (f *fakeController) DeleteFirewallRule(ctx context.Context, id string) error {
	f.deletedFW = append(f.deletedFW, id)
	return nil
}

func (f *fakeController) ListPortForwards(ctx context.Context) ([]unifi.PortForward, error) {
	return f.portForwards, nil
}
func (f *fakeController) CreatePortForward(ctx context.Context, p unifi.PortForward) (unifi.PortForward, error) {
	f.createdPF = append(f.createdPF, p)
	return p, nil
}
func (f *fakeController) UpdatePortForward(ctx context.Context, p unifi.PortForward) error {
	f.updatedPF = append(f.updatedPF, p)
	return nil
}
func (f *fakeController) DeletePortForward(ctx context.Context, id string) error {
	f.deletedPF = append(f.deletedPF, id)
	return nil
}

func TestBuildEmitsCreatesForAllKinds(t *testing.T) {
	fc := &fakeController{}
	cfg := resources.NetworkConfig{
		Networks:      []resources.Network{{Name: "iot", Purpose: "corporate", VLAN: 42, Subnet: "10.0.42.0/24"}},
		Wlans:         []resources.Wlan{{Name: "home-iot", Network: "iot", Security: "wpapsk", PasswordEnv: "WLAN_HOME_PSK", Band: "both"}},
		FirewallRules: []resources.FirewallRule{{Name: "drop-iot", Action: "drop", Ruleset: "LAN_IN"}},
		PortForwards:  []resources.PortForward{{Name: "ha", SrcPort: "8123", DstAddress: "10.0.42.10", DstPort: "8123", Protocol: "tcp"}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Plan) != 4 {
		t.Fatalf("want 4 ops, got %d (%+v)", len(plan.Plan), plan.Plan)
	}
	// Deterministic ordering: network, wlan, firewall, port_forward.
	wantKinds := []string{KindNetwork, KindWlan, KindFirewall, KindPortForward}
	for i, k := range wantKinds {
		if plan.Plan[i].Kind != k {
			t.Fatalf("op[%d].Kind=%q, want %q", i, plan.Plan[i].Kind, k)
		}
	}
	// WLAN create exposes the env-var dependency for dry-run inspection.
	wlanOp := plan.Plan[1]
	if len(wlanOp.SecretRefs) != 1 || wlanOp.SecretRefs[0] != "WLAN_HOME_PSK" {
		t.Fatalf("expected WLAN op to declare secret ref, got %+v", wlanOp)
	}
	if wlanOp.Diff["x_passphrase"] != "@env:WLAN_HOME_PSK" {
		t.Fatalf("expected placeholder for PSK, got %v", wlanOp.Diff["x_passphrase"])
	}
}

func TestBuildNoOpWhenInSync(t *testing.T) {
	fc := &fakeController{
		networks: []unifi.Network{{ID: "n1", Name: "iot", Purpose: "corporate", VLAN: 42, Subnet: "10.0.42.0/24"}},
	}
	cfg := resources.NetworkConfig{
		Networks: []resources.Network{{Name: "iot", Purpose: "corporate", VLAN: 42, Subnet: "10.0.42.0/24"}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Plan) != 0 {
		t.Fatalf("expected empty plan, got %+v", plan.Plan)
	}
}

func TestBuildEmitsUpdateOnFieldDrift(t *testing.T) {
	fc := &fakeController{
		networks: []unifi.Network{{ID: "n1", Name: "iot", Purpose: "corporate", VLAN: 42, Subnet: "10.0.42.0/24"}},
	}
	cfg := resources.NetworkConfig{
		Networks: []resources.Network{{Name: "iot", Purpose: "corporate", VLAN: 42, Subnet: "10.0.99.0/24"}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Plan) != 1 || plan.Plan[0].Op != OpUpdateNetwork {
		t.Fatalf("expected one update_network op, got %+v", plan.Plan)
	}
	if plan.Plan[0].Diff["ip_subnet"] != "10.0.99.0/24" {
		t.Fatalf("diff missing subnet change: %+v", plan.Plan[0].Diff)
	}
}

func TestBuildPrunesOnlyManagedAndOnlyWithFlag(t *testing.T) {
	fc := &fakeController{
		networks: []unifi.Network{
			{ID: "n1", Name: "iot", Purpose: "corporate", Note: "app=unictl"},
			{ID: "n2", Name: "operator-made", Purpose: "corporate", Note: "hand-rolled"},
		},
	}
	cfg := resources.NetworkConfig{} // declare nothing

	// Default: prune=false. Even managed/undeclared resources stay.
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Plan) != 0 {
		t.Fatalf("expected non-destructive default plan, got %+v", plan.Plan)
	}

	// With prune=true: only the managed network is queued for deletion.
	plan, err = Build(context.Background(), fc, cfg, true)
	if err != nil {
		t.Fatalf("Build prune: %v", err)
	}
	if len(plan.Plan) != 1 || plan.Plan[0].Op != OpDeleteNetwork || plan.Plan[0].Name != "iot" {
		t.Fatalf("expected single delete_network for managed-only, got %+v", plan.Plan)
	}
}

func TestApplyExecutesAndStampsMarker(t *testing.T) {
	fc := &fakeController{}
	cfg := resources.NetworkConfig{
		Networks: []resources.Network{{Name: "iot", Purpose: "corporate", VLAN: 42}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	Apply(context.Background(), fc, &plan)
	if plan.DryRun || !plan.Applied {
		t.Fatalf("expected applied plan, got %+v", plan)
	}
	if len(fc.createdNets) != 1 {
		t.Fatalf("expected one CreateNetwork call, got %+v", fc.createdNets)
	}
	if !IsManaged(fc.createdNets[0].Note) {
		t.Fatalf("expected unictl marker on created network, got note=%q", fc.createdNets[0].Note)
	}
}

func TestApplyResolvesWlanPSKFromEnv(t *testing.T) {
	t.Setenv("UNICTL_WLAN_TEST", "hunter2")
	fc := &fakeController{}
	cfg := resources.NetworkConfig{
		Wlans: []resources.Wlan{{Name: "iot", Network: "iot", Security: "wpapsk", PasswordEnv: "UNICTL_WLAN_TEST", Band: "both"}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	Apply(context.Background(), fc, &plan)
	if plan.HasFailures() {
		t.Fatalf("apply failed: %+v", plan)
	}
	if len(fc.createdWlans) != 1 || fc.createdWlans[0].XPassphrase != "hunter2" {
		t.Fatalf("expected resolved PSK on created WLAN, got %+v", fc.createdWlans)
	}
}

func TestApplyFailsLoudWhenPSKUnset(t *testing.T) {
	fc := &fakeController{}
	cfg := resources.NetworkConfig{
		Wlans: []resources.Wlan{{Name: "iot", Network: "iot", Security: "wpapsk", PasswordEnv: "UNICTL_WLAN_NEVER_SET", Band: "both"}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	Apply(context.Background(), fc, &plan)
	if !plan.HasFailures() {
		t.Fatalf("expected apply to fail when PSK env unset, got %+v", plan)
	}
	if len(fc.createdWlans) != 0 {
		t.Fatalf("controller should not have been called: %+v", fc.createdWlans)
	}
}

func TestStampMarkerPreservesExistingNote(t *testing.T) {
	got := stampMarker("operator: keep me")
	if !IsManaged(got) {
		t.Fatalf("expected marker to be appended: %q", got)
	}
	if got == unifi.UniCtlMarker {
		t.Fatalf("expected operator note preserved, got just the marker")
	}
}

// TestUpdatePreservesOperatorNote guards the regression where an update
// would PUT just the marker, wiping any operator-written note prefix.
func TestUpdatePreservesOperatorNote(t *testing.T) {
	fc := &fakeController{
		networks: []unifi.Network{{
			ID: "n1", Name: "iot", Purpose: "corporate", VLAN: 42,
			Subnet: "10.0.42.0/24", Note: "operator: keep me app=unictl",
		}},
	}
	cfg := resources.NetworkConfig{
		Networks: []resources.Network{{Name: "iot", Purpose: "corporate", VLAN: 42, Subnet: "10.0.99.0/24"}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	Apply(context.Background(), fc, &plan)
	if len(fc.updatedNets) != 1 {
		t.Fatalf("want one UpdateNetwork call, got %+v", fc.updatedNets)
	}
	if !strings.Contains(fc.updatedNets[0].Note, "operator: keep me") {
		t.Fatalf("operator note clobbered, got note=%q", fc.updatedNets[0].Note)
	}
	if !IsManaged(fc.updatedNets[0].Note) {
		t.Fatalf("marker missing after update, got note=%q", fc.updatedNets[0].Note)
	}
}

// TestUpdateAdoptsUnmarkedResource guards the case where a declared
// resource matches an unmanaged controller record but has drift: the
// update must stamp the marker (so future `--prune` sees it as managed)
// without dropping the operator's note.
func TestUpdateAdoptsUnmarkedResource(t *testing.T) {
	fc := &fakeController{
		networks: []unifi.Network{{
			ID: "n1", Name: "iot", Purpose: "corporate", VLAN: 42,
			Subnet: "10.0.42.0/24", Note: "hand-rolled",
		}},
	}
	cfg := resources.NetworkConfig{
		Networks: []resources.Network{{Name: "iot", Purpose: "corporate", VLAN: 99, Subnet: "10.0.42.0/24"}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	Apply(context.Background(), fc, &plan)
	if len(fc.updatedNets) != 1 {
		t.Fatalf("want one UpdateNetwork, got %+v", fc.updatedNets)
	}
	if !IsManaged(fc.updatedNets[0].Note) {
		t.Fatalf("expected adoption to add marker, got note=%q", fc.updatedNets[0].Note)
	}
	if !strings.Contains(fc.updatedNets[0].Note, "hand-rolled") {
		t.Fatalf("operator note dropped during adoption, got note=%q", fc.updatedNets[0].Note)
	}
}

// TestPlanRoundTripsThroughJSON guards against integer fields being
// silently lost when a plan is marshaled to JSON, saved, and replayed
// later — JSON decodes numbers as float64, so the *FromDiff readers
// must accept both int and float64.
func TestPlanRoundTripsThroughJSON(t *testing.T) {
	fc := &fakeController{}
	cfg := resources.NetworkConfig{
		Networks: []resources.Network{{Name: "iot", Purpose: "corporate", VLAN: 42, Subnet: "10.0.42.0/24"}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	blob, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped Plan
	if err := json.Unmarshal(blob, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	Apply(context.Background(), fc, &roundTripped)
	if roundTripped.HasFailures() {
		t.Fatalf("apply failed: %+v", roundTripped)
	}
	if len(fc.createdNets) != 1 || fc.createdNets[0].VLAN != 42 {
		t.Fatalf("VLAN lost across JSON round-trip: %+v", fc.createdNets)
	}
}

// TestSortOrderRespectsDependencies pins the ordering contract: creates
// run in dependency order (Network → Wlan → Firewall → PortForward),
// deletes run in reverse so referenced parents outlive their dependents.
func TestSortOrderRespectsDependencies(t *testing.T) {
	fc := &fakeController{
		networks:      []unifi.Network{{ID: "n-old", Name: "old-net", Note: unifi.UniCtlMarker}},
		wlans:         []unifi.WlanConf{{ID: "w-old", Name: "old-wlan", Note: unifi.UniCtlMarker}},
		firewallRules: []unifi.FirewallRule{{ID: "f-old", Name: "old-fw", Note: unifi.UniCtlMarker}},
		portForwards:  []unifi.PortForward{{ID: "p-old", Name: "old-pf", Note: unifi.UniCtlMarker}},
	}
	cfg := resources.NetworkConfig{
		Networks:      []resources.Network{{Name: "new-net", Purpose: "corporate"}},
		Wlans:         []resources.Wlan{{Name: "new-wlan", Network: "new-net", Security: "open", Band: "both"}},
		FirewallRules: []resources.FirewallRule{{Name: "new-fw", Action: "drop", Ruleset: "LAN_IN"}},
		PortForwards:  []resources.PortForward{{Name: "new-pf", SrcPort: "80", DstAddress: "10.0.0.1", DstPort: "80", Protocol: "tcp"}},
	}
	plan, err := Build(context.Background(), fc, cfg, true)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	wantOps := []string{
		OpCreateNetwork,
		OpCreateWlan,
		OpCreateFirewall,
		OpCreatePortForward,
		OpDeletePortForward,
		OpDeleteFirewall,
		OpDeleteWlan,
		OpDeleteNetwork,
	}
	if len(plan.Plan) != len(wantOps) {
		t.Fatalf("want %d ops, got %d: %+v", len(wantOps), len(plan.Plan), plan.Plan)
	}
	for i, want := range wantOps {
		if plan.Plan[i].Op != want {
			t.Fatalf("op[%d]=%q, want %q (full plan: %+v)", i, plan.Plan[i].Op, want, plan.Plan)
		}
	}
}

// TestDiffClearsRemovedOptionalFields guards the declarative promise
// that removing an optional value from CUE clears it on the controller.
func TestDiffClearsRemovedOptionalFields(t *testing.T) {
	fc := &fakeController{
		firewallRules: []unifi.FirewallRule{{
			ID: "f1", Name: "drop-iot", Action: "drop", Ruleset: "LAN_IN",
			Protocol: "tcp", SrcAddress: "10.0.42.0/24",
		}},
	}
	cfg := resources.NetworkConfig{
		// Drop protocol and src_address from the declaration; the diff
		// should propose clearing them on the controller.
		FirewallRules: []resources.FirewallRule{{Name: "drop-iot", Action: "drop", Ruleset: "LAN_IN"}},
	}
	plan, err := Build(context.Background(), fc, cfg, false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Plan) != 1 || plan.Plan[0].Op != OpUpdateFirewall {
		t.Fatalf("want one update_firewall_rule, got %+v", plan.Plan)
	}
	d := plan.Plan[0].Diff
	if got, ok := d["protocol"].(string); !ok || got != "" {
		t.Fatalf("expected protocol cleared in diff, got %v", d["protocol"])
	}
	if got, ok := d["src_address"].(string); !ok || got != "" {
		t.Fatalf("expected src_address cleared in diff, got %v", d["src_address"])
	}
}
