package resources

import (
	"fmt"
	"os"
)

// ResolvedSecret is the result of resolving a `password_env` reference.
//
// In a dry-run plan, the reconciler emits `Placeholder` as the rendered
// password so plans can be inspected without secrets being set. At
// apply time, `Value` is populated and `Placeholder` left blank.
type ResolvedSecret struct {
	EnvVar      string `json:"env_var,omitempty"`
	Value       string `json:"-"`
	Placeholder string `json:"placeholder,omitempty"`
}

// Placeholder returns the marker string emitted into dry-run plans for
// an env-var reference. Chosen to make it obvious it's an indirection,
// not a real password.
func Placeholder(envVar string) string {
	if envVar == "" {
		return ""
	}
	return "@env:" + envVar
}

// ResolvePSK looks up the env var named by `passwordEnv`. It returns a
// "loud" error if the var is unset — applying a WLAN with an unresolved
// PSK would otherwise silently push an empty password to the controller.
//
// Open WLANs pass an empty passwordEnv and get an empty value back.
func ResolvePSK(security, passwordEnv string) (string, error) {
	if security == "open" {
		return "", nil
	}
	if passwordEnv == "" {
		return "", fmt.Errorf("resources: WLAN with security=%q requires password_env to name an env var", security)
	}
	v, ok := os.LookupEnv(passwordEnv)
	if !ok {
		return "", fmt.Errorf("resources: env var %q (password_env) is not set", passwordEnv)
	}
	return v, nil
}
