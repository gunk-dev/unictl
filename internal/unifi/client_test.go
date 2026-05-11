package unifi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeController spins up an httptest server that responds to the small
// subset of UniFi paths unictl uses. It validates the X-API-KEY header and
// records the last block/unblock command for assertions.
type fakeController struct {
	srv      *httptest.Server
	users    []User
	lastCmd  string
	lastMAC  string
	authFail bool
}

func newFakeController(t *testing.T, users []User) *fakeController {
	t.Helper()
	fc := &fakeController{users: users}
	fc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fc.authFail && r.Header.Get("X-API-KEY") != "test-key" {
			http.Error(w, "missing key", http.StatusForbidden)
			return
		}
		if fc.authFail {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/rest/user"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": fc.users})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cmd/stamgr"):
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Cmd string `json:"cmd"`
				MAC string `json:"mac"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fc.lastCmd = req.Cmd
			fc.lastMAC = req.MAC
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fc.srv.Close)
	return fc
}

func newTestClient(t *testing.T, fc *fakeController) *Client {
	t.Helper()
	c, err := New(Config{
		Host:   fc.srv.URL,
		APIKey: "test-key",
		Site:   "default",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestListBlockedFiltersUnblockedUsers(t *testing.T) {
	fc := newFakeController(t, []User{
		{MAC: "aa:bb:cc:dd:ee:01", Blocked: true, Note: "iot"},
		{MAC: "aa:bb:cc:dd:ee:02", Blocked: false},
		{MAC: "aa:bb:cc:dd:ee:03", Blocked: true},
	})
	c := newTestClient(t, fc)
	got, err := c.ListBlocked(context.Background())
	if err != nil {
		t.Fatalf("ListBlocked: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 blocked, got %d (%+v)", len(got), got)
	}
}

func TestBlockUnblockSendsStamgrCommand(t *testing.T) {
	fc := newFakeController(t, nil)
	c := newTestClient(t, fc)
	if err := c.Block(context.Background(), "AA:BB:CC:DD:EE:01"); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if fc.lastCmd != "block-sta" || fc.lastMAC != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("unexpected block payload: cmd=%q mac=%q", fc.lastCmd, fc.lastMAC)
	}
	if err := c.Unblock(context.Background(), "aa:bb:cc:dd:ee:01"); err != nil {
		t.Fatalf("Unblock: %v", err)
	}
	if fc.lastCmd != "unblock-sta" {
		t.Fatalf("expected unblock-sta, got %q", fc.lastCmd)
	}
}

func TestAuthFailureReturnsErrAuth(t *testing.T) {
	fc := newFakeController(t, nil)
	fc.authFail = true
	c := newTestClient(t, fc)
	_, err := c.ListBlocked(context.Background())
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("expected ErrAuth, got %v", err)
	}
}

func TestNewRejectsMissingConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := New(Config{Host: "https://x"}); err == nil {
		t.Fatal("expected error for missing api key")
	}
}
