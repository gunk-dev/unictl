package events

import (
	"reflect"
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad time %q: %v", s, err)
	}
	return v
}

func TestFold(t *testing.T) {
	now := mustParse(t, "2026-05-10T12:00:00Z")
	cases := []struct {
		name string
		log  []Event
		want []BlockedClient
	}{
		{
			name: "empty log",
			log:  nil,
			want: []BlockedClient{},
		},
		{
			name: "single open-ended block",
			log: []Event{
				{At: mustParse(t, "2026-01-01T00:00:00Z"), Type: TypeBlock, MAC: "AA:BB:CC:DD:EE:01", Reason: "iot"},
			},
			want: []BlockedClient{
				{MAC: "aa:bb:cc:dd:ee:01", Reason: "iot"},
			},
		},
		{
			name: "block then unblock cancels out",
			log: []Event{
				{At: mustParse(t, "2026-01-01T00:00:00Z"), Type: TypeBlock, MAC: "aa:bb:cc:dd:ee:02"},
				{At: mustParse(t, "2026-01-02T00:00:00Z"), Type: TypeUnblock, MAC: "aa:bb:cc:dd:ee:02"},
			},
			want: []BlockedClient{},
		},
		{
			name: "block with past expiry has lapsed",
			log: []Event{
				{At: mustParse(t, "2026-01-01T00:00:00Z"), Type: TypeBlock, MAC: "aa:bb:cc:dd:ee:03", ExpiresAt: mustParse(t, "2026-01-02T00:00:00Z")},
			},
			want: []BlockedClient{},
		},
		{
			name: "block with future expiry is still active",
			log: []Event{
				{At: mustParse(t, "2026-01-01T00:00:00Z"), Type: TypeBlock, MAC: "aa:bb:cc:dd:ee:04", ExpiresAt: mustParse(t, "2027-01-01T00:00:00Z"), Reason: "long cooldown"},
			},
			want: []BlockedClient{
				{MAC: "aa:bb:cc:dd:ee:04", Reason: "long cooldown", ExpiresAt: mustParse(t, "2027-01-01T00:00:00Z")},
			},
		},
		{
			name: "out-of-order timestamps fold deterministically",
			log: []Event{
				{At: mustParse(t, "2026-01-03T00:00:00Z"), Type: TypeUnblock, MAC: "aa:bb:cc:dd:ee:05"},
				{At: mustParse(t, "2026-01-01T00:00:00Z"), Type: TypeBlock, MAC: "aa:bb:cc:dd:ee:05"},
				{At: mustParse(t, "2026-01-02T00:00:00Z"), Type: TypeBlock, MAC: "aa:bb:cc:dd:ee:06"},
			},
			want: []BlockedClient{
				{MAC: "aa:bb:cc:dd:ee:06"},
			},
		},
		{
			name: "idempotent counter-events: double unblock is fine",
			log: []Event{
				{At: mustParse(t, "2026-01-01T00:00:00Z"), Type: TypeBlock, MAC: "aa:bb:cc:dd:ee:07"},
				{At: mustParse(t, "2026-01-02T00:00:00Z"), Type: TypeUnblock, MAC: "aa:bb:cc:dd:ee:07"},
				{At: mustParse(t, "2026-01-03T00:00:00Z"), Type: TypeUnblock, MAC: "aa:bb:cc:dd:ee:07"},
			},
			want: []BlockedClient{},
		},
		{
			name: "future events are ignored",
			log: []Event{
				{At: mustParse(t, "2026-01-01T00:00:00Z"), Type: TypeBlock, MAC: "aa:bb:cc:dd:ee:08"},
				{At: mustParse(t, "2099-01-01T00:00:00Z"), Type: TypeUnblock, MAC: "aa:bb:cc:dd:ee:08"},
			},
			want: []BlockedClient{
				{MAC: "aa:bb:cc:dd:ee:08"},
			},
		},
		{
			name: "MAC case is normalized for comparison",
			log: []Event{
				{At: mustParse(t, "2026-01-01T00:00:00Z"), Type: TypeBlock, MAC: "AA:BB:CC:DD:EE:09"},
				{At: mustParse(t, "2026-01-02T00:00:00Z"), Type: TypeUnblock, MAC: "aa:bb:cc:dd:ee:09"},
			},
			want: []BlockedClient{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Fold(tc.log, now)
			if err != nil {
				t.Fatalf("Fold: %v", err)
			}
			if !reflect.DeepEqual(got.Blocked, tc.want) {
				t.Fatalf("Fold mismatch\n got: %#v\nwant: %#v", got.Blocked, tc.want)
			}
		})
	}
}

func TestFoldUnknownType(t *testing.T) {
	now := mustParse(t, "2026-05-10T12:00:00Z")
	_, err := Fold([]Event{{At: now, Type: "nope", MAC: "aa:bb:cc:dd:ee:ff"}}, now)
	if err == nil {
		t.Fatal("expected error for unknown event type")
	}
}
