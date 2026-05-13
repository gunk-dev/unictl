// Package resourcesync diffs a declarative NetworkConfig against live
// controller state and produces a plan of create/update/delete ops for
// networks, WLANs, firewall rules, and port forwards.
//
// This package lives alongside (not inside) internal/sync because the
// resource axis is shaped differently from the event-log axis: it
// matches by user-stable name, tracks an `app=unictl` marker for
// pruning, and emits structured per-resource ops rather than a single
// block/unblock action. The event-log path is unchanged.
//
// Pruning posture: the planner is non-destructive by default.
// Controller-side resources marked managed (note contains `app=unictl`)
// but absent from the declared config are only proposed for deletion
// when Prune=true. Unmanaged resources (no marker) are never deleted.
package resourcesync

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/gunk-dev/unictl/internal/resources"
	"github.com/gunk-dev/unictl/internal/unifi"
)

// Op kinds. Stable; only add, never rename.
const (
	OpCreateNetwork     = "create_network"
	OpUpdateNetwork     = "update_network"
	OpDeleteNetwork     = "delete_network"
	OpCreateWlan        = "create_wlan"
	OpUpdateWlan        = "update_wlan"
	OpDeleteWlan        = "delete_wlan"
	OpCreateFirewall    = "create_firewall_rule"
	OpUpdateFirewall    = "update_firewall_rule"
	OpDeleteFirewall    = "delete_firewall_rule"
	OpCreatePortForward = "create_port_forward"
	OpUpdatePortForward = "update_port_forward"
	OpDeletePortForward = "delete_port_forward"
)

// ResourceKind groups ops by the resource type they touch. Used by the
// CLI to bucket the plan output for readability.
const (
	KindNetwork     = "network"
	KindWlan        = "wlan"
	KindFirewall    = "firewall_rule"
	KindPortForward = "port_forward"
)

// Op is a single create/update/delete operation in a plan. The Diff
// field carries the structured payload — fields that differ for an
// update, or the desired shape for a create. For a delete, Diff is the
// nil/zero map (the controller record alone is sufficient).
type Op struct {
	Op       string         `json:"op"`
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	Diff     map[string]any `json:"diff,omitempty"`
	Status   string         `json:"status"`
	Error    string         `json:"error,omitempty"`
	// SecretRefs lists env vars this op needs at apply time. Populated
	// for WLAN creates/updates so the dry-run plan exposes what's
	// required without actually reading the secret.
	SecretRefs []string `json:"secret_refs,omitempty"`
}

// Plan is the structured diff produced by Build.
type Plan struct {
	Plan    []Op   `json:"plan"`
	DryRun  bool   `json:"dry_run"`
	Applied bool   `json:"applied"`
	Site    string `json:"site"`
	Prune   bool   `json:"prune"`
}

// HasFailures returns true if any op failed during Apply.
func (p Plan) HasFailures() bool {
	for _, op := range p.Plan {
		if op.Status == "failed" {
			return true
		}
	}
	return false
}

// HasSuccesses returns true if any op was applied successfully.
func (p Plan) HasSuccesses() bool {
	for _, op := range p.Plan {
		if op.Status == "applied" {
			return true
		}
	}
	return false
}

// Controller is the subset of *unifi.Client this engine needs. Extracted
// as an interface so tests can swap in fakes.
type Controller interface {
	Site() string

	ListNetworks(ctx context.Context) ([]unifi.Network, error)
	CreateNetwork(ctx context.Context, n unifi.Network) (unifi.Network, error)
	UpdateNetwork(ctx context.Context, n unifi.Network) error
	DeleteNetwork(ctx context.Context, id string) error

	ListWlans(ctx context.Context) ([]unifi.WlanConf, error)
	CreateWlan(ctx context.Context, w unifi.WlanConf) (unifi.WlanConf, error)
	UpdateWlan(ctx context.Context, w unifi.WlanConf) error
	DeleteWlan(ctx context.Context, id string) error

	ListFirewallRules(ctx context.Context) ([]unifi.FirewallRule, error)
	CreateFirewallRule(ctx context.Context, r unifi.FirewallRule) (unifi.FirewallRule, error)
	UpdateFirewallRule(ctx context.Context, r unifi.FirewallRule) error
	DeleteFirewallRule(ctx context.Context, id string) error

	ListPortForwards(ctx context.Context) ([]unifi.PortForward, error)
	CreatePortForward(ctx context.Context, p unifi.PortForward) (unifi.PortForward, error)
	UpdatePortForward(ctx context.Context, p unifi.PortForward) error
	DeletePortForward(ctx context.Context, id string) error
}

// IsManaged reports whether a controller record carries the unictl
// marker. Only managed records are eligible for delete-pruning.
func IsManaged(note string) bool {
	return strings.Contains(note, unifi.UniCtlMarker)
}

// stampMarker appends the unictl marker to a note value if not already
// present. Preserving operator-written prefixes makes the marker
// non-destructive when adopted on a resource later.
func stampMarker(note string) string {
	if IsManaged(note) {
		return note
	}
	if note == "" {
		return unifi.UniCtlMarker
	}
	return note + " " + unifi.UniCtlMarker
}

// Build computes a plan that converges live controller state onto the
// declared config. With prune=false the plan never proposes deletions
// even for managed-but-undeclared resources.
func Build(ctx context.Context, c Controller, cfg resources.NetworkConfig, prune bool) (Plan, error) {
	var ops []Op

	netOps, err := buildNetworks(ctx, c, cfg.Networks, prune)
	if err != nil {
		return Plan{}, err
	}
	ops = append(ops, netOps...)

	wlanOps, err := buildWlans(ctx, c, cfg.Wlans, prune)
	if err != nil {
		return Plan{}, err
	}
	ops = append(ops, wlanOps...)

	fwOps, err := buildFirewallRules(ctx, c, cfg.FirewallRules, prune)
	if err != nil {
		return Plan{}, err
	}
	ops = append(ops, fwOps...)

	pfOps, err := buildPortForwards(ctx, c, cfg.PortForwards, prune)
	if err != nil {
		return Plan{}, err
	}
	ops = append(ops, pfOps...)

	sortOps(ops)
	return Plan{Plan: ops, DryRun: true, Site: c.Site(), Prune: prune}, nil
}

// Apply executes each planned op. Failures are recorded per-op so the
// caller sees the full picture; the loop never stops on a single error.
func Apply(ctx context.Context, c Controller, p *Plan) {
	for i := range p.Plan {
		op := &p.Plan[i]
		if err := applyOne(ctx, c, op); err != nil {
			op.Status = "failed"
			op.Error = err.Error()
			continue
		}
		op.Status = "applied"
	}
	p.DryRun = false
	p.Applied = true
}

func applyOne(ctx context.Context, c Controller, op *Op) error {
	switch op.Op {
	case OpCreateNetwork:
		n, err := networkFromDiff(op)
		if err != nil {
			return err
		}
		_, err = c.CreateNetwork(ctx, n)
		return err
	case OpUpdateNetwork:
		n, err := networkFromDiff(op)
		if err != nil {
			return err
		}
		return c.UpdateNetwork(ctx, n)
	case OpDeleteNetwork:
		return c.DeleteNetwork(ctx, idFromDiff(op))

	case OpCreateWlan:
		w, err := wlanFromDiff(op)
		if err != nil {
			return err
		}
		if err := resolveWlanSecret(op, &w); err != nil {
			return err
		}
		_, err = c.CreateWlan(ctx, w)
		return err
	case OpUpdateWlan:
		w, err := wlanFromDiff(op)
		if err != nil {
			return err
		}
		if err := resolveWlanSecret(op, &w); err != nil {
			return err
		}
		return c.UpdateWlan(ctx, w)
	case OpDeleteWlan:
		return c.DeleteWlan(ctx, idFromDiff(op))

	case OpCreateFirewall:
		r, err := firewallFromDiff(op)
		if err != nil {
			return err
		}
		_, err = c.CreateFirewallRule(ctx, r)
		return err
	case OpUpdateFirewall:
		r, err := firewallFromDiff(op)
		if err != nil {
			return err
		}
		return c.UpdateFirewallRule(ctx, r)
	case OpDeleteFirewall:
		return c.DeleteFirewallRule(ctx, idFromDiff(op))

	case OpCreatePortForward:
		pf, err := portForwardFromDiff(op)
		if err != nil {
			return err
		}
		_, err = c.CreatePortForward(ctx, pf)
		return err
	case OpUpdatePortForward:
		pf, err := portForwardFromDiff(op)
		if err != nil {
			return err
		}
		return c.UpdatePortForward(ctx, pf)
	case OpDeletePortForward:
		return c.DeletePortForward(ctx, idFromDiff(op))
	}
	return errors.New("unknown op")
}

// --- networks --------------------------------------------------------------

func buildNetworks(ctx context.Context, c Controller, desired []resources.Network, prune bool) ([]Op, error) {
	live, err := c.ListNetworks(ctx)
	if err != nil {
		return nil, err
	}
	liveByName := map[string]unifi.Network{}
	for _, n := range live {
		liveByName[n.Name] = n
	}
	declared := map[string]struct{}{}
	var ops []Op
	for _, d := range desired {
		declared[d.Name] = struct{}{}
		want := networkToWire(d)
		if cur, ok := liveByName[d.Name]; ok {
			want.ID = cur.ID
			if diff := diffNetwork(cur, want); diff != nil {
				ops = append(ops, Op{
					Op:     OpUpdateNetwork,
					Kind:   KindNetwork,
					Name:   d.Name,
					Diff:   diff,
					Status: "planned",
				})
			}
			continue
		}
		ops = append(ops, Op{
			Op:     OpCreateNetwork,
			Kind:   KindNetwork,
			Name:   d.Name,
			Diff:   networkDiff(want),
			Status: "planned",
		})
	}
	if prune {
		for _, cur := range live {
			if _, ok := declared[cur.Name]; ok {
				continue
			}
			if !IsManaged(cur.Note) {
				continue
			}
			ops = append(ops, Op{
				Op:     OpDeleteNetwork,
				Kind:   KindNetwork,
				Name:   cur.Name,
				Diff:   map[string]any{"_id": cur.ID},
				Status: "planned",
			})
		}
	}
	return ops, nil
}

func networkToWire(n resources.Network) unifi.Network {
	out := unifi.Network{
		Name:    n.Name,
		Purpose: n.Purpose,
		Subnet:  n.Subnet,
		VLAN:    n.VLAN,
		Enabled: n.Enabled,
	}
	if n.DHCPEnabled != nil {
		out.DHCPDEnable = n.DHCPEnabled
	}
	if n.DHCPRange != nil {
		out.DHCPDStart = n.DHCPRange.Start
		out.DHCPDStop = n.DHCPRange.End
	}
	return out
}

func diffNetwork(cur, want unifi.Network) map[string]any {
	diff := map[string]any{}
	// purpose is required in the schema; never propose clearing it.
	if cur.Purpose != want.Purpose && want.Purpose != "" {
		diff["purpose"] = want.Purpose
	}
	// Optional fields below: a missing CUE value should clear the
	// controller-side value too, per the declarative promise.
	if cur.Subnet != want.Subnet {
		diff["ip_subnet"] = want.Subnet
	}
	if cur.VLAN != want.VLAN {
		diff["vlan"] = want.VLAN
	}
	if cur.DHCPDStart != want.DHCPDStart {
		diff["dhcpd_start"] = want.DHCPDStart
	}
	if cur.DHCPDStop != want.DHCPDStop {
		diff["dhcpd_stop"] = want.DHCPDStop
	}
	if want.DHCPDEnable != nil && !boolEq(cur.DHCPDEnable, want.DHCPDEnable) {
		diff["dhcpd_enabled"] = *want.DHCPDEnable
	}
	if want.Enabled != nil && !boolEq(cur.Enabled, want.Enabled) {
		diff["enabled"] = *want.Enabled
	}
	if len(diff) == 0 {
		return nil
	}
	diff["_id"] = cur.ID
	// Carry the note on every update so PUT preserves operator-written
	// content and stamps the marker if missing. Without this, the PUT's
	// "note" field would be omitted (or worse, sent empty) and the
	// controller could clobber an operator's existing note.
	diff["note"] = stampMarker(cur.Note)
	return diff
}

func networkDiff(n unifi.Network) map[string]any {
	d := map[string]any{"name": n.Name, "purpose": n.Purpose}
	if n.Subnet != "" {
		d["ip_subnet"] = n.Subnet
	}
	if n.VLAN != 0 {
		d["vlan"] = n.VLAN
	}
	if n.DHCPDEnable != nil {
		d["dhcpd_enabled"] = *n.DHCPDEnable
	}
	if n.DHCPDStart != "" {
		d["dhcpd_start"] = n.DHCPDStart
	}
	if n.DHCPDStop != "" {
		d["dhcpd_stop"] = n.DHCPDStop
	}
	if n.Enabled != nil {
		d["enabled"] = *n.Enabled
	}
	d["note"] = stampMarker(n.Note)
	return d
}

// --- WLANs -----------------------------------------------------------------

func buildWlans(ctx context.Context, c Controller, desired []resources.Wlan, prune bool) ([]Op, error) {
	live, err := c.ListWlans(ctx)
	if err != nil {
		return nil, err
	}
	liveByName := map[string]unifi.WlanConf{}
	for _, w := range live {
		liveByName[w.Name] = w
	}
	declared := map[string]struct{}{}
	var ops []Op
	for _, d := range desired {
		declared[d.Name] = struct{}{}
		want := wlanToWire(d)
		if cur, ok := liveByName[d.Name]; ok {
			want.ID = cur.ID
			if diff := diffWlan(cur, want, d); diff != nil {
				ops = append(ops, Op{
					Op:         OpUpdateWlan,
					Kind:       KindWlan,
					Name:       d.Name,
					Diff:       diff,
					Status:     "planned",
					SecretRefs: secretRefs(d),
				})
			}
			continue
		}
		ops = append(ops, Op{
			Op:         OpCreateWlan,
			Kind:       KindWlan,
			Name:       d.Name,
			Diff:       wlanDiff(want, d),
			Status:     "planned",
			SecretRefs: secretRefs(d),
		})
	}
	if prune {
		for _, cur := range live {
			if _, ok := declared[cur.Name]; ok {
				continue
			}
			if !IsManaged(cur.Note) {
				continue
			}
			ops = append(ops, Op{
				Op:     OpDeleteWlan,
				Kind:   KindWlan,
				Name:   cur.Name,
				Diff:   map[string]any{"_id": cur.ID},
				Status: "planned",
			})
		}
	}
	return ops, nil
}

func wlanToWire(w resources.Wlan) unifi.WlanConf {
	return unifi.WlanConf{
		Name:     w.Name,
		Security: w.Security,
		Band:     w.Band,
		HideSSID: w.HideSSID,
		Enabled:  w.Enabled,
	}
}

func diffWlan(cur, want unifi.WlanConf, d resources.Wlan) map[string]any {
	diff := map[string]any{}
	// security and band are required in the schema; never propose clearing.
	if cur.Security != want.Security && want.Security != "" {
		diff["security"] = want.Security
	}
	if cur.Band != want.Band && want.Band != "" {
		diff["wlan_band"] = want.Band
	}
	if want.HideSSID != nil && !boolEq(cur.HideSSID, want.HideSSID) {
		diff["hide_ssid"] = *want.HideSSID
	}
	if want.Enabled != nil && !boolEq(cur.Enabled, want.Enabled) {
		diff["enabled"] = *want.Enabled
	}
	if d.PasswordEnv != "" {
		// We cannot diff the actual PSK from the live config (the
		// controller doesn't echo passwords back), so updates that name
		// a password_env always carry the placeholder. The reconciler
		// applies the latest env-var value at apply time.
		diff["x_passphrase"] = resources.Placeholder(d.PasswordEnv)
	}
	if len(diff) == 0 {
		return nil
	}
	diff["_id"] = cur.ID
	diff["note"] = stampMarker(cur.Note)
	return diff
}

func wlanDiff(want unifi.WlanConf, d resources.Wlan) map[string]any {
	out := map[string]any{
		"name":     want.Name,
		"security": want.Security,
		"network":  d.Network,
	}
	if want.Band != "" {
		out["wlan_band"] = want.Band
	}
	if want.HideSSID != nil {
		out["hide_ssid"] = *want.HideSSID
	}
	if want.Enabled != nil {
		out["enabled"] = *want.Enabled
	}
	if d.PasswordEnv != "" {
		out["x_passphrase"] = resources.Placeholder(d.PasswordEnv)
	}
	out["note"] = stampMarker(want.Note)
	return out
}

func secretRefs(w resources.Wlan) []string {
	if w.PasswordEnv == "" {
		return nil
	}
	return []string{w.PasswordEnv}
}

func resolveWlanSecret(op *Op, w *unifi.WlanConf) error {
	if len(op.SecretRefs) == 0 {
		return nil
	}
	envVar := op.SecretRefs[0]
	psk, err := resources.ResolvePSK(stringFromDiff(op, "security"), envVar)
	if err != nil {
		return err
	}
	w.XPassphrase = psk
	return nil
}

// --- firewall rules --------------------------------------------------------

func buildFirewallRules(ctx context.Context, c Controller, desired []resources.FirewallRule, prune bool) ([]Op, error) {
	live, err := c.ListFirewallRules(ctx)
	if err != nil {
		return nil, err
	}
	liveByName := map[string]unifi.FirewallRule{}
	for _, r := range live {
		liveByName[r.Name] = r
	}
	declared := map[string]struct{}{}
	var ops []Op
	for _, d := range desired {
		declared[d.Name] = struct{}{}
		want := firewallToWire(d)
		if cur, ok := liveByName[d.Name]; ok {
			want.ID = cur.ID
			if diff := diffFirewall(cur, want); diff != nil {
				ops = append(ops, Op{
					Op:     OpUpdateFirewall,
					Kind:   KindFirewall,
					Name:   d.Name,
					Diff:   diff,
					Status: "planned",
				})
			}
			continue
		}
		ops = append(ops, Op{
			Op:     OpCreateFirewall,
			Kind:   KindFirewall,
			Name:   d.Name,
			Diff:   firewallDiff(want),
			Status: "planned",
		})
	}
	if prune {
		for _, cur := range live {
			if _, ok := declared[cur.Name]; ok {
				continue
			}
			if !IsManaged(cur.Note) {
				continue
			}
			ops = append(ops, Op{
				Op:     OpDeleteFirewall,
				Kind:   KindFirewall,
				Name:   cur.Name,
				Diff:   map[string]any{"_id": cur.ID},
				Status: "planned",
			})
		}
	}
	return ops, nil
}

func firewallToWire(r resources.FirewallRule) unifi.FirewallRule {
	return unifi.FirewallRule{
		Name:       r.Name,
		Action:     r.Action,
		Ruleset:    r.Ruleset,
		Protocol:   r.Protocol,
		SrcAddress: r.SrcAddress,
		SrcPort:    r.SrcPort,
		DstAddress: r.DstAddress,
		DstPort:    r.DstPort,
		Enabled:    r.Enabled,
	}
}

func diffFirewall(cur, want unifi.FirewallRule) map[string]any {
	diff := map[string]any{}
	// action and ruleset are required in the schema; never propose clearing.
	if cur.Action != want.Action && want.Action != "" {
		diff["action"] = want.Action
	}
	if cur.Ruleset != want.Ruleset && want.Ruleset != "" {
		diff["ruleset"] = want.Ruleset
	}
	// Optional fields below: missing CUE values clear the controller-side
	// value to match the declarative source of truth.
	if cur.Protocol != want.Protocol {
		diff["protocol"] = want.Protocol
	}
	if cur.SrcAddress != want.SrcAddress {
		diff["src_address"] = want.SrcAddress
	}
	if cur.SrcPort != want.SrcPort {
		diff["src_port"] = want.SrcPort
	}
	if cur.DstAddress != want.DstAddress {
		diff["dst_address"] = want.DstAddress
	}
	if cur.DstPort != want.DstPort {
		diff["dst_port"] = want.DstPort
	}
	if want.Enabled != nil && !boolEq(cur.Enabled, want.Enabled) {
		diff["enabled"] = *want.Enabled
	}
	if len(diff) == 0 {
		return nil
	}
	diff["_id"] = cur.ID
	diff["note"] = stampMarker(cur.Note)
	return diff
}

func firewallDiff(r unifi.FirewallRule) map[string]any {
	out := map[string]any{
		"name":    r.Name,
		"action":  r.Action,
		"ruleset": r.Ruleset,
	}
	if r.Protocol != "" {
		out["protocol"] = r.Protocol
	}
	if r.SrcAddress != "" {
		out["src_address"] = r.SrcAddress
	}
	if r.SrcPort != "" {
		out["src_port"] = r.SrcPort
	}
	if r.DstAddress != "" {
		out["dst_address"] = r.DstAddress
	}
	if r.DstPort != "" {
		out["dst_port"] = r.DstPort
	}
	if r.Enabled != nil {
		out["enabled"] = *r.Enabled
	}
	out["note"] = stampMarker(r.Note)
	return out
}

// --- port forwards ---------------------------------------------------------

func buildPortForwards(ctx context.Context, c Controller, desired []resources.PortForward, prune bool) ([]Op, error) {
	live, err := c.ListPortForwards(ctx)
	if err != nil {
		return nil, err
	}
	liveByName := map[string]unifi.PortForward{}
	for _, p := range live {
		liveByName[p.Name] = p
	}
	declared := map[string]struct{}{}
	var ops []Op
	for _, d := range desired {
		declared[d.Name] = struct{}{}
		want := portForwardToWire(d)
		if cur, ok := liveByName[d.Name]; ok {
			want.ID = cur.ID
			if diff := diffPortForward(cur, want); diff != nil {
				ops = append(ops, Op{
					Op:     OpUpdatePortForward,
					Kind:   KindPortForward,
					Name:   d.Name,
					Diff:   diff,
					Status: "planned",
				})
			}
			continue
		}
		ops = append(ops, Op{
			Op:     OpCreatePortForward,
			Kind:   KindPortForward,
			Name:   d.Name,
			Diff:   portForwardDiff(want),
			Status: "planned",
		})
	}
	if prune {
		for _, cur := range live {
			if _, ok := declared[cur.Name]; ok {
				continue
			}
			if !IsManaged(cur.Note) {
				continue
			}
			ops = append(ops, Op{
				Op:     OpDeletePortForward,
				Kind:   KindPortForward,
				Name:   cur.Name,
				Diff:   map[string]any{"_id": cur.ID},
				Status: "planned",
			})
		}
	}
	return ops, nil
}

func portForwardToWire(p resources.PortForward) unifi.PortForward {
	src := p.Source
	if src == "" {
		src = "any"
	}
	return unifi.PortForward{
		Name:       p.Name,
		Src:        src,
		SrcPort:    p.SrcPort,
		DstAddress: p.DstAddress,
		DstPort:    p.DstPort,
		Protocol:   p.Protocol,
		Enabled:    p.Enabled,
	}
}

func diffPortForward(cur, want unifi.PortForward) map[string]any {
	diff := map[string]any{}
	// All these fields are required in the schema (or have a defaulting
	// rule applied in portForwardToWire), so plain inequality is enough.
	if cur.Src != want.Src && want.Src != "" {
		diff["src"] = want.Src
	}
	if cur.SrcPort != want.SrcPort && want.SrcPort != "" {
		diff["src_port"] = want.SrcPort
	}
	if cur.DstAddress != want.DstAddress && want.DstAddress != "" {
		diff["fwd"] = want.DstAddress
	}
	if cur.DstPort != want.DstPort && want.DstPort != "" {
		diff["fwd_port"] = want.DstPort
	}
	if cur.Protocol != want.Protocol && want.Protocol != "" {
		diff["proto"] = want.Protocol
	}
	if want.Enabled != nil && !boolEq(cur.Enabled, want.Enabled) {
		diff["enabled"] = *want.Enabled
	}
	if len(diff) == 0 {
		return nil
	}
	diff["_id"] = cur.ID
	diff["note"] = stampMarker(cur.Note)
	return diff
}

func portForwardDiff(p unifi.PortForward) map[string]any {
	out := map[string]any{
		"name":     p.Name,
		"src":      p.Src,
		"src_port": p.SrcPort,
		"fwd":      p.DstAddress,
		"fwd_port": p.DstPort,
		"proto":    p.Protocol,
	}
	if p.Enabled != nil {
		out["enabled"] = *p.Enabled
	}
	out["note"] = stampMarker(p.Note)
	return out
}

// --- shared helpers --------------------------------------------------------

func boolEq(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func networkFromDiff(op *Op) (unifi.Network, error) {
	n := unifi.Network{Name: op.Name}
	if op.Op == OpUpdateNetwork {
		n.ID = idFromDiff(op)
	}
	n.Purpose = stringFromDiff(op, "purpose")
	n.Subnet = stringFromDiff(op, "ip_subnet")
	n.VLAN = intFromDiff(op, "vlan")
	n.DHCPDStart = stringFromDiff(op, "dhcpd_start")
	n.DHCPDStop = stringFromDiff(op, "dhcpd_stop")
	if v, ok := boolFromDiff(op, "dhcpd_enabled"); ok {
		n.DHCPDEnable = &v
	}
	if v, ok := boolFromDiff(op, "enabled"); ok {
		n.Enabled = &v
	}
	n.Note = stringFromDiff(op, "note")
	return n, nil
}

func wlanFromDiff(op *Op) (unifi.WlanConf, error) {
	w := unifi.WlanConf{Name: op.Name}
	if op.Op == OpUpdateWlan {
		w.ID = idFromDiff(op)
	}
	w.Security = stringFromDiff(op, "security")
	w.Band = stringFromDiff(op, "wlan_band")
	if v, ok := boolFromDiff(op, "hide_ssid"); ok {
		w.HideSSID = &v
	}
	if v, ok := boolFromDiff(op, "enabled"); ok {
		w.Enabled = &v
	}
	w.Note = stringFromDiff(op, "note")
	return w, nil
}

func firewallFromDiff(op *Op) (unifi.FirewallRule, error) {
	r := unifi.FirewallRule{Name: op.Name}
	if op.Op == OpUpdateFirewall {
		r.ID = idFromDiff(op)
	}
	r.Action = stringFromDiff(op, "action")
	r.Ruleset = stringFromDiff(op, "ruleset")
	r.Protocol = stringFromDiff(op, "protocol")
	r.SrcAddress = stringFromDiff(op, "src_address")
	r.SrcPort = stringFromDiff(op, "src_port")
	r.DstAddress = stringFromDiff(op, "dst_address")
	r.DstPort = stringFromDiff(op, "dst_port")
	if v, ok := boolFromDiff(op, "enabled"); ok {
		r.Enabled = &v
	}
	r.Note = stringFromDiff(op, "note")
	return r, nil
}

func portForwardFromDiff(op *Op) (unifi.PortForward, error) {
	p := unifi.PortForward{Name: op.Name}
	if op.Op == OpUpdatePortForward {
		p.ID = idFromDiff(op)
	}
	p.Src = stringFromDiff(op, "src")
	p.SrcPort = stringFromDiff(op, "src_port")
	p.DstAddress = stringFromDiff(op, "fwd")
	p.DstPort = stringFromDiff(op, "fwd_port")
	p.Protocol = stringFromDiff(op, "proto")
	if v, ok := boolFromDiff(op, "enabled"); ok {
		p.Enabled = &v
	}
	p.Note = stringFromDiff(op, "note")
	return p, nil
}

func idFromDiff(op *Op) string {
	if op.Diff == nil {
		return ""
	}
	if v, ok := op.Diff["_id"].(string); ok {
		return v
	}
	return ""
}

func stringFromDiff(op *Op, key string) string {
	if op.Diff == nil {
		return ""
	}
	if v, ok := op.Diff[key].(string); ok {
		return v
	}
	return ""
}

// intFromDiff reads an integer-valued diff field. The Diff map round-trips
// through JSON in tools that save and re-apply plans, where numbers come
// back as float64. Accept both shapes so saved plans behave identically
// to in-process ones.
func intFromDiff(op *Op, key string) int {
	if op.Diff == nil {
		return 0
	}
	switch v := op.Diff[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// boolFromDiff reads a bool-valued diff field. Returns ok=false if the
// key is absent so callers can leave optional `*bool` fields nil.
func boolFromDiff(op *Op, key string) (bool, bool) {
	if op.Diff == nil {
		return false, false
	}
	v, ok := op.Diff[key].(bool)
	return v, ok
}

// kindOrder is the deterministic kind-ordering used by sortOps so the
// plan reads top-down by resource type.
var kindOrder = map[string]int{
	KindNetwork:     0,
	KindWlan:        1,
	KindFirewall:    2,
	KindPortForward: 3,
}

// sortOps orders the plan so dependencies are respected at apply time.
// Creates and updates run first, in dependency order (Network → Wlan →
// Firewall → PortForward) — a WLAN that names a network needs the
// network in place; a firewall rule that names an interface needs that
// interface to exist. Deletes run last, in reverse dependency order, so
// a referenced network isn't deleted while a WLAN still points at it.
// Within an op-class, ties break by name for stable, readable output.
func sortOps(ops []Op) {
	sort.SliceStable(ops, func(i, j int) bool {
		phaseI, phaseJ := opPhase(ops[i].Op), opPhase(ops[j].Op)
		if phaseI != phaseJ {
			return phaseI < phaseJ
		}
		ki, kj := kindOrder[ops[i].Kind], kindOrder[ops[j].Kind]
		if ki != kj {
			if phaseI == phaseDelete {
				return ki > kj
			}
			return ki < kj
		}
		if ops[i].Op != ops[j].Op {
			return opOrder(ops[i].Op) < opOrder(ops[j].Op)
		}
		return ops[i].Name < ops[j].Name
	})
}

// opPhase groups ops into create/update vs delete. We split these so
// deletes can run in reverse dependency order without disrupting the
// natural top-down order of create/update.
const (
	phaseCreateUpdate = 0
	phaseDelete       = 1
)

func opPhase(op string) int {
	if strings.HasPrefix(op, "delete_") {
		return phaseDelete
	}
	return phaseCreateUpdate
}

// opOrder ranks ops within a kind: create < update < delete.
func opOrder(op string) int {
	switch {
	case strings.HasPrefix(op, "create_"):
		return 0
	case strings.HasPrefix(op, "update_"):
		return 1
	case strings.HasPrefix(op, "delete_"):
		return 2
	}
	return 3
}

