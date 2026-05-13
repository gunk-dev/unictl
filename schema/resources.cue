// Persistent declarative resource shapes for unictl.
//
// Resources are the second of two persistence models the reconciler
// supports. Events (schema/events.cue) model time-bounded, action-shaped
// state — "block this MAC at this time"; the fold collapses them into a
// derived snapshot. Resources describe long-lived controller objects —
// VLANs, WLANs, firewall rules, port forwards — that the user wants to
// exist in a particular shape. The sync engine diffs the declared
// resources against live controller state and emits create/update/delete
// ops.
//
// Matching: resources are keyed by their user-stable `name`, never by
// controller-internal `_id`. The reconciler maintains an `app=unictl`
// marker on resources it creates so pruning can distinguish managed from
// unmanaged objects without operator bookkeeping.
//
// Secrets: WLAN PSKs are NEVER inline. The schema accepts a
// `password_env` field naming an environment variable; the reconciler
// resolves it at apply time and fails loud if the variable is unset.
// Dry-run plans don't require the secret to be set.
//
// Enum permissiveness: where UniFi accepts a known finite vocabulary
// (firewall action, port-forward protocol) the schema pins the values.
// Where the controller's vocabulary is broader, version-skew-prone, or
// version-dependent (WLAN security strings, firewall ruleset names,
// network purpose) the schema accepts any string and the reconciler
// passes the value through verbatim. This keeps the schema useful as a
// guardrail without locking unictl to a single controller version.
package schema

// #Network declares a VLAN / subnet / LAN segment. Maps to a
// /rest/networkconf object on the controller.
//
// `purpose` is the controller's classification — typical values are
// "corporate", "guest", "vlan-only", "wan", "remote-user-vpn"; verify
// against the controller you're targeting. unictl passes the value
// through unchanged.
#Network: {
	name:          string  // user-stable identifier; matched against live state by name
	purpose:       string  // see comment above
	subnet?:       string  // CIDR, e.g. "10.0.42.0/24"
	vlan?:         int & >=1 & <=4094
	dhcp_enabled?: bool
	dhcp_range?: {
		start: string
		end:   string
	}
	enabled?: bool | *true
}

// #Wlan declares a wireless network. Maps to /rest/wlanconf.
//
// `security` is the controller's security string — typical values are
// "open", "wpapsk" (WPA2-PSK), "wpa3", "wpaeap"; pass through verbatim.
// `band` is the controller's band string — "2g", "5g", "both".
//
// PSKs must come from the environment via `password_env`. unictl rejects
// any inline password.
#Wlan: {
	name:          string  // SSID; doubles as the match key
	network:       string  // references #Network.name
	security:      string
	password_env?: string  // name of env var holding the PSK; required when security != "open"
	band:          string
	hide_ssid?:    bool | *false
	enabled?:      bool | *true
}

// #FirewallRule declares a single rule inside a ruleset. Maps to
// /rest/firewallrule.
//
// `ruleset` is the UniFi ruleset identifier — "WAN_IN", "WAN_OUT",
// "WAN_LOCAL", "LAN_IN", "LAN_OUT", "LAN_LOCAL", "GUEST_IN", "GUEST_OUT",
// "GUEST_LOCAL" are the classic values; modern controllers add more.
// unictl passes the value through verbatim.
#FirewallRule: {
	name:         string
	action:       "accept" | "drop" | "reject"
	ruleset:      string
	protocol?:    string  // "tcp" | "udp" | "tcp_udp" | "icmp" | "all" — verify on your controller
	src_address?: string  // CIDR
	src_port?:    string  // single port or "lo-hi" range
	dst_address?: string  // CIDR
	dst_port?:    string
	enabled?:     bool | *true
}

// #PortForward declares a NAT port forward. Maps to /rest/portforward.
#PortForward: {
	name:        string
	source?:     string  // CIDR or "any"; defaults to any on the controller
	src_port:    string
	dst_address: string  // internal IP
	dst_port:    string
	protocol:    "tcp" | "udp" | "tcp_udp"
	enabled?:    bool | *true
}

// #NetworkConfig is the top-level shape of a resources file. All fields
// are optional so a config can declare e.g. only firewall rules without
// being forced to enumerate networks.
//
// Extension point: future PRs will add per-port profiles, static routes,
// etc. Add new top-level fields here and matching list element shapes
// above.
#NetworkConfig: {
	networks?:       [...#Network]
	wlans?:          [...#Wlan]
	firewall_rules?: [...#FirewallRule]
	port_forwards?:  [...#PortForward]
}
