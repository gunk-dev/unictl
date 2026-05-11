package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gunk-dev/unictl/internal/events"
	"github.com/gunk-dev/unictl/internal/unifi"
)

type fakeController struct {
	blocked   []unifi.User
	blocks    []string
	unblocks  []string
	failBlock map[string]error
}

func (f *fakeController) ListBlocked(ctx context.Context) ([]unifi.User, error) {
	return f.blocked, nil
}

func (f *fakeController) Block(ctx context.Context, mac string) error {
	if err := f.failBlock[mac]; err != nil {
		return err
	}
	f.blocks = append(f.blocks, mac)
	return nil
}

func (f *fakeController) Unblock(ctx context.Context, mac string) error {
	f.unblocks = append(f.unblocks, mac)
	return nil
}

func (f *fakeController) Site() string { return "default" }

func TestBuildEmitsBlockAndUnblockOps(t *testing.T) {
	fc := &fakeController{
		blocked: []unifi.User{
			{MAC: "aa:bb:cc:dd:ee:99"}, // currently blocked on the controller
		},
	}
	// Mark the controller-blocked user as blocked=true so ListBlocked filtering
	// in real code would have surfaced it.
	for i := range fc.blocked {
		fc.blocked[i].Blocked = true
	}
	desired := events.DesiredState{
		Blocked: []events.BlockedClient{
			{MAC: "aa:bb:cc:dd:ee:01", Reason: "iot"},
		},
	}
	plan, err := Build(context.Background(), fc, desired)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Plan) != 2 {
		t.Fatalf("want 2 ops, got %d (%+v)", len(plan.Plan), plan.Plan)
	}
	// Deterministic ordering: block before unblock.
	if plan.Plan[0].Op != "block" || plan.Plan[0].MAC != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("unexpected first op: %+v", plan.Plan[0])
	}
	if plan.Plan[1].Op != "unblock" || plan.Plan[1].MAC != "aa:bb:cc:dd:ee:99" {
		t.Fatalf("unexpected second op: %+v", plan.Plan[1])
	}
	if !plan.DryRun || plan.Applied {
		t.Fatalf("expected dry-run plan, got %+v", plan)
	}
}

func TestBuildNoOpWhenStateMatches(t *testing.T) {
	fc := &fakeController{blocked: []unifi.User{{MAC: "aa:bb:cc:dd:ee:01", Blocked: true}}}
	desired := events.DesiredState{Blocked: []events.BlockedClient{{MAC: "AA:BB:CC:DD:EE:01"}}}
	plan, err := Build(context.Background(), fc, desired)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(plan.Plan) != 0 {
		t.Fatalf("want empty plan, got %+v", plan.Plan)
	}
}

func TestApplyMarksPartialFailure(t *testing.T) {
	fc := &fakeController{
		failBlock: map[string]error{"aa:bb:cc:dd:ee:01": errors.New("boom")},
	}
	desired := events.DesiredState{
		Blocked: []events.BlockedClient{
			{MAC: "aa:bb:cc:dd:ee:01"},
			{MAC: "aa:bb:cc:dd:ee:02"},
		},
	}
	plan, err := Build(context.Background(), fc, desired)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	Apply(context.Background(), fc, &plan)
	if !plan.HasFailures() || !plan.HasSuccesses() {
		t.Fatalf("expected partial success, got %+v", plan)
	}
}

func TestRoundTripFromEventsFold(t *testing.T) {
	// Drives the fold → build pipeline end-to-end against the in-memory
	// controller. If a future PR adds new event types, this is the first
	// place a regression will surface.
	fc := &fakeController{blocked: []unifi.User{{MAC: "aa:bb:cc:dd:ee:99", Blocked: true}}}
	log := []events.Event{
		{Type: events.TypeBlock, MAC: "aa:bb:cc:dd:ee:01", Reason: "iot"},
	}
	desired, err := events.Fold(log, parseRFC("2099-01-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Build(context.Background(), fc, desired)
	if err != nil {
		t.Fatal(err)
	}
	Apply(context.Background(), fc, &plan)
	if len(fc.blocks) != 1 || fc.blocks[0] != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("expected exactly one block call for the IoT MAC, got %+v", fc.blocks)
	}
	if len(fc.unblocks) != 1 || fc.unblocks[0] != "aa:bb:cc:dd:ee:99" {
		t.Fatalf("expected one unblock for the stale MAC, got %+v", fc.unblocks)
	}
}

func parseRFC(s string) (t time.Time) {
	t, _ = time.Parse(time.RFC3339, s)
	return
}
