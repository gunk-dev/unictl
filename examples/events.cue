// Example event log for unictl.
//
// Try it out:
//
//   unictl sync examples/events.cue --dry-run
//
// The fold sees one open-ended block (a known-bad IoT camera), one block
// with an expiry that has likely lapsed by the time you read this (so the
// fold treats it as not-currently-blocked), and one block + unblock pair
// that cancels itself out.
events: [
	{
		at:     "2026-01-01T00:00:00Z"
		type:   "block"
		mac:    "aa:bb:cc:dd:ee:01"
		reason: "Unpatched IoT camera, isolate until firmware update lands"
	},
	{
		at:         "2026-01-02T00:00:00Z"
		type:       "block"
		mac:        "aa:bb:cc:dd:ee:02"
		reason:     "Temporary guest cooldown"
		expires_at: "2026-01-03T00:00:00Z"
	},
	{
		at:     "2026-01-05T00:00:00Z"
		type:   "block"
		mac:    "aa:bb:cc:dd:ee:03"
		reason: "Investigating odd egress traffic"
	},
	{
		at:     "2026-01-06T00:00:00Z"
		type:   "unblock"
		mac:    "aa:bb:cc:dd:ee:03"
		reason: "False alarm — was a scheduled backup"
	},
]
