// Package sync diffs a desired-state snapshot (produced by folding an event
// log) against the live controller state and produces a plan of block/
// unblock operations. With Apply=true it executes the plan and records
// per-op outcomes.
package sync

import (
	"context"
	"errors"
	"sort"

	"github.com/gunk-dev/unictl/internal/events"
	"github.com/gunk-dev/unictl/internal/unifi"
)

// Op is a single block or unblock operation in a plan.
type Op struct {
	Op     string `json:"op"`     // "block" | "unblock"
	MAC    string `json:"mac"`
	Reason string `json:"reason,omitempty"`
	// Before / After describe the controller-side blocked status before
	// and after this op would run, expressed as "blocked" / "unblocked".
	Before string `json:"before"`
	After  string `json:"after"`
	// Status reflects execution outcome: "planned" before apply,
	// "applied" on success, or "failed" with Error populated.
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Plan is the structured diff produced by Build.
type Plan struct {
	Plan    []Op   `json:"plan"`
	DryRun  bool   `json:"dry_run"`
	Applied bool   `json:"applied"`
	Site    string `json:"site"`
}

// Controller is the subset of *unifi.Client the sync engine needs.
// Extracted as an interface so tests can swap in fakes without spinning up
// an HTTP server when they're focused on the planner.
type Controller interface {
	ListBlocked(ctx context.Context) ([]unifi.User, error)
	Block(ctx context.Context, mac string) error
	Unblock(ctx context.Context, mac string) error
	Site() string
}

// Build returns the plan needed to converge the controller's blocked set
// onto `desired`. It does not mutate the controller.
func Build(ctx context.Context, c Controller, desired events.DesiredState) (Plan, error) {
	live, err := c.ListBlocked(ctx)
	if err != nil {
		return Plan{}, err
	}
	liveSet := map[string]struct{}{}
	for _, u := range live {
		liveSet[events.Normalize(u.MAC)] = struct{}{}
	}
	desiredSet := map[string]events.BlockedClient{}
	for _, b := range desired.Blocked {
		desiredSet[events.Normalize(b.MAC)] = b
	}

	var ops []Op
	// Blocks: in desired but not live.
	for mac, b := range desiredSet {
		if _, ok := liveSet[mac]; ok {
			continue
		}
		ops = append(ops, Op{
			Op:     "block",
			MAC:    mac,
			Reason: b.Reason,
			Before: "unblocked",
			After:  "blocked",
			Status: "planned",
		})
	}
	// Unblocks: in live but not desired.
	for mac := range liveSet {
		if _, ok := desiredSet[mac]; ok {
			continue
		}
		ops = append(ops, Op{
			Op:     "unblock",
			MAC:    mac,
			Before: "blocked",
			After:  "unblocked",
			Status: "planned",
		})
	}
	sortOps(ops)
	return Plan{Plan: ops, DryRun: true, Site: c.Site()}, nil
}

// Apply executes each planned op against the controller. Successful ops
// are marked "applied"; failures are marked "failed" with Error populated
// and the loop continues so the caller sees the full picture.
//
// HasFailures reports whether any op failed; callers map that to exit
// code 3 (partial success) when at least one op succeeded, or exit code 2
// when none did.
func Apply(ctx context.Context, c Controller, p *Plan) {
	for i := range p.Plan {
		op := &p.Plan[i]
		var err error
		switch op.Op {
		case "block":
			err = c.Block(ctx, op.MAC)
		case "unblock":
			err = c.Unblock(ctx, op.MAC)
		default:
			err = errors.New("unknown op")
		}
		if err != nil {
			op.Status = "failed"
			op.Error = err.Error()
			continue
		}
		op.Status = "applied"
	}
	p.DryRun = false
	p.Applied = true
}

// HasFailures returns true if any op in the plan failed during Apply.
func (p Plan) HasFailures() bool {
	for _, op := range p.Plan {
		if op.Status == "failed" {
			return true
		}
	}
	return false
}

// HasSuccesses returns true if any op in the plan was applied successfully.
func (p Plan) HasSuccesses() bool {
	for _, op := range p.Plan {
		if op.Status == "applied" {
			return true
		}
	}
	return false
}

func sortOps(ops []Op) {
	// Deterministic ordering: blocks first, then unblocks, MAC ascending.
	// Avoids spurious diffs in test fixtures and CI output.
	sort.Slice(ops, func(i, j int) bool {
		return less(ops[i], ops[j])
	})
}

func less(a, b Op) bool {
	if a.Op != b.Op {
		return a.Op < b.Op
	}
	return a.MAC < b.MAC
}
