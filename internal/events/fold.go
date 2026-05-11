// Package events loads append-only event logs and folds them into a
// derived desired state.
//
// The fold is the canonical way unictl turns a stream of declarative events
// into "what should the controller look like right now". Events are sorted
// by timestamp, replayed in order, and converted into a snapshot the
// reconciler can diff against the live controller.
//
// The fold is a pure function: same log + same `now` → same state. No I/O,
// no controller calls.
package events

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event is the in-memory representation of an event after CUE validation.
// The discriminator field is Type; payload fields are populated based on it.
type Event struct {
	At        time.Time `json:"at"`
	Type      string    `json:"type"`
	MAC       string    `json:"mac"`
	Reason    string    `json:"reason,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// Known event types.
const (
	TypeBlock   = "block"
	TypeUnblock = "unblock"
)

// BlockedClient describes a client the desired state says should be blocked.
type BlockedClient struct {
	MAC       string    `json:"mac"`
	Reason    string    `json:"reason,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// DesiredState is the snapshot produced by folding an event log at a given
// point in time. Blocked is sorted by MAC for determinism.
type DesiredState struct {
	Blocked []BlockedClient `json:"blocked"`
}

// Normalize converts a MAC address to canonical lowercase colon form. The
// fold compares MACs by normalized value so case/format inconsistencies in
// the log don't produce phantom plan items.
func Normalize(mac string) string {
	return strings.ToLower(strings.TrimSpace(mac))
}

// Fold replays events in timestamp order and returns the desired state as
// of `now`. Events with At > now are ignored (they're "scheduled for the
// future"). Block events whose ExpiresAt <= now have already lapsed and
// produce no entry.
//
// When two events for the same MAC share an exact timestamp, the input
// order is preserved (stable sort). This is rare in practice but documented
// for determinism.
func Fold(log []Event, now time.Time) (DesiredState, error) {
	sorted := make([]Event, len(log))
	copy(sorted, log)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].At.Before(sorted[j].At)
	})

	current := map[string]BlockedClient{}
	for _, ev := range sorted {
		if ev.At.After(now) {
			continue
		}
		mac := Normalize(ev.MAC)
		switch ev.Type {
		case TypeBlock:
			if !ev.ExpiresAt.IsZero() && !ev.ExpiresAt.After(now) {
				// Block has already lapsed; drop any prior block for this MAC too.
				delete(current, mac)
				continue
			}
			current[mac] = BlockedClient{
				MAC:       mac,
				Reason:    ev.Reason,
				ExpiresAt: ev.ExpiresAt,
			}
		case TypeUnblock:
			delete(current, mac)
		default:
			return DesiredState{}, fmt.Errorf("events: unknown event type %q at %s", ev.Type, ev.At.Format(time.RFC3339))
		}
	}

	out := DesiredState{Blocked: make([]BlockedClient, 0, len(current))}
	for _, c := range current {
		out.Blocked = append(out.Blocked, c)
	}
	sort.Slice(out.Blocked, func(i, j int) bool {
		return out.Blocked[i].MAC < out.Blocked[j].MAC
	})
	return out, nil
}
