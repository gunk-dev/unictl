// Package unifi is a minimal HTTP client for the UniFi Network controller.
//
// It supports local API-key authentication introduced in UniFi Network 8.x.
// Auth header is `X-API-KEY: <key>`. TLS verification is on by default and
// must be opted out explicitly; most home UDMs serve a self-signed cert,
// so an `--insecure` escape hatch is required in practice.
//
// Endpoint provenance (verified against current documentation):
//
//   - GET /proxy/network/integration/v1/sites
//     GET /proxy/network/integration/v1/sites/{siteId}/clients
//     Official Integration API, Ubiquiti Help Center
//     "Getting Started with the Official UniFi API" (May 2026).
//
//   - GET /proxy/network/api/s/{siteName}/rest/user
//     POST /proxy/network/api/s/{siteName}/cmd/stamgr
//     Legacy/classic controller API, still authoritative for block/unblock
//     because the official Integration API does not yet expose these
//     operations. Documented across the Ubiquiti community wiki
//     (ubntwiki.com) and the long-running Art-of-WiFi PHP client
//     (github.com/Art-of-WiFi/UniFi-API-client). Reached via the same
//     /proxy/network prefix when running against UniFi OS consoles, with
//     X-API-KEY auth.
//
// We deliberately ship only the endpoints we need for `unictl sync`.
package unifi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Common errors. Callers can use errors.Is to discriminate.
var (
	ErrAuth       = errors.New("unifi: authentication failed")
	ErrNetwork    = errors.New("unifi: controller unreachable")
	ErrController = errors.New("unifi: controller returned an error")
)

// Config configures a Client.
type Config struct {
	// Host is the controller base URL, e.g. "https://10.0.0.1".
	Host string
	// APIKey is the UniFi local API key (X-API-KEY header).
	APIKey string
	// Site is the legacy short site name (e.g. "default"). The Integration
	// API uses UUIDs, but the legacy /api/s/{site}/... paths require the
	// short name. unictl currently assumes a single site; default "default".
	Site string
	// Insecure disables TLS verification. Required for the typical home UDM
	// shipping a self-signed cert. Default false.
	Insecure bool
	// Timeout is the per-request timeout. Default 15s.
	Timeout time.Duration
}

// Client talks to a UniFi Network controller.
type Client struct {
	cfg  Config
	http *http.Client
}

// New constructs a Client. It validates required configuration up-front so
// downstream call sites can rely on a usable Client or a clear error.
func New(cfg Config) (*Client, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("unifi: host is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("unifi: api key is required")
	}
	if _, err := url.Parse(cfg.Host); err != nil {
		return nil, fmt.Errorf("unifi: invalid host %q: %w", cfg.Host, err)
	}
	if cfg.Site == "" {
		cfg.Site = "default"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure},
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Transport: tr,
			Timeout:   cfg.Timeout,
		},
	}, nil
}

// BaseURL returns the configured controller base URL. Useful for tests.
func (c *Client) BaseURL() string { return strings.TrimRight(c.cfg.Host, "/") }

// Site returns the configured site short-name.
func (c *Client) Site() string { return c.cfg.Site }

// User represents a single record from the legacy /rest/user endpoint. Only
// the fields unictl needs are decoded; extra fields are ignored.
type User struct {
	ID       string `json:"_id"`
	MAC      string `json:"mac"`
	Hostname string `json:"hostname,omitempty"`
	Name     string `json:"name,omitempty"`
	Blocked  bool   `json:"blocked"`
	Note     string `json:"note,omitempty"`
}

// ListBlocked returns every user record on the site with blocked=true.
// Uses the legacy /rest/user endpoint, which is the only path that surfaces
// blocked clients regardless of online state.
func (c *Client) ListBlocked(ctx context.Context) ([]User, error) {
	path := fmt.Sprintf("/proxy/network/api/s/%s/rest/user", url.PathEscape(c.cfg.Site))
	var resp struct {
		Data []User `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := resp.Data[:0]
	for _, u := range resp.Data {
		if u.Blocked {
			out = append(out, u)
		}
	}
	return out, nil
}

// Block instructs the controller to block traffic to/from `mac`.
func (c *Client) Block(ctx context.Context, mac string) error {
	return c.stamgr(ctx, "block-sta", mac)
}

// Unblock lifts a previously-applied block for `mac`.
func (c *Client) Unblock(ctx context.Context, mac string) error {
	return c.stamgr(ctx, "unblock-sta", mac)
}

func (c *Client) stamgr(ctx context.Context, cmd, mac string) error {
	path := fmt.Sprintf("/proxy/network/api/s/%s/cmd/stamgr", url.PathEscape(c.cfg.Site))
	body := map[string]string{"cmd": cmd, "mac": strings.ToLower(strings.TrimSpace(mac))}
	return c.do(ctx, http.MethodPost, path, body, nil)
}

// do sends an authenticated request and decodes a JSON response into `out`
// when non-nil. Network failures, auth failures, and controller-level
// errors are mapped to the sentinel errors so callers can categorize
// failures without parsing strings.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("unifi: marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	endpoint := c.BaseURL() + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return fmt.Errorf("unifi: build request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: %s", ErrAuth, resp.Status)
	}
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%w: %s: %s", ErrController, resp.Status, strings.TrimSpace(string(snippet)))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("unifi: decode response: %w", err)
		}
	}
	return nil
}
