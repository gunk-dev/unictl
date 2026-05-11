package events

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

func TestLoadPathListShape(t *testing.T) {
	body := `events: [
		{at: "2026-01-01T00:00:00Z", type: "block", mac: "aa:bb:cc:dd:ee:01", reason: "iot"},
		{at: "2026-01-02T00:00:00Z", type: "unblock", mac: "aa:bb:cc:dd:ee:02"},
	]
	`
	p := writeTemp(t, "log.cue", body)
	got, err := LoadPath(p)
	if err != nil {
		t.Fatalf("LoadPath: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d", len(got))
	}
	if got[0].Type != TypeBlock || got[1].Type != TypeUnblock {
		t.Fatalf("unexpected event types: %+v", got)
	}
	if got[0].Reason != "iot" {
		t.Fatalf("reason not parsed: %q", got[0].Reason)
	}
}

func TestLoadPathRejectsBadTimestamp(t *testing.T) {
	body := `events: [{at: "not-a-time", type: "block", mac: "aa:bb:cc:dd:ee:01"}]`
	p := writeTemp(t, "log.cue", body)
	if _, err := LoadPath(p); err == nil {
		t.Fatal("expected error for bad timestamp, got nil")
	}
}

func TestLoadPathRejectsBadMAC(t *testing.T) {
	body := `events: [{at: "2026-01-01T00:00:00Z", type: "block", mac: "not-a-mac"}]`
	p := writeTemp(t, "log.cue", body)
	if _, err := LoadPath(p); err == nil {
		t.Fatal("expected error for bad MAC, got nil")
	}
}

func TestLoadPathMissingFile(t *testing.T) {
	if _, err := LoadPath("/does/not/exist.cue"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadPathExampleFixture(t *testing.T) {
	// Use the real example shipped in the repo as an end-to-end fixture.
	// If this breaks, the example or the loader changed shape — that's worth
	// noticing in CI.
	wd, _ := os.Getwd()
	example := filepath.Join(wd, "..", "..", "examples", "events.cue")
	got, err := LoadPath(example)
	if err != nil {
		t.Fatalf("LoadPath example: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 events in example, got %d", len(got))
	}
}
