// Example declarative resource config for unictl.
//
// Try it out (dry-run does not require WLAN_HOME_PSK to be set):
//
//   unictl sync examples/
//
// And to apply for real:
//
//   export WLAN_HOME_PSK="..."
//   unictl sync examples/ --apply
//
// The reconciler matches each resource by `name` against live controller
// state and emits create/update/delete ops. Resources unictl creates
// carry an `app=unictl` marker in their `note` field; `--prune` only
// deletes managed-but-undeclared resources.
networks: [
	{
		name:    "iot"
		purpose: "corporate"
		subnet:  "10.0.42.0/24"
		vlan:    42
		dhcp_enabled: true
		dhcp_range: {start: "10.0.42.100", end: "10.0.42.250"}
	},
]

wlans: [
	{
		name:         "home-iot"
		network:      "iot"
		security:     "wpapsk"
		password_env: "WLAN_HOME_PSK"
		band:         "both"
	},
]

firewall_rules: [
	{
		name:        "drop-iot-to-lan"
		action:      "drop"
		ruleset:     "LAN_IN"
		protocol:    "all"
		src_address: "10.0.42.0/24"
		dst_address: "10.0.0.0/24"
	},
]

port_forwards: [
	{
		name:        "home-assistant"
		source:      "any"
		src_port:    "8123"
		dst_address: "10.0.42.10"
		dst_port:    "8123"
		protocol:    "tcp"
	},
]
