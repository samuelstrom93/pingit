package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestMagicLinkFlow(t *testing.T) {
	h := newHarness(t)

	// 1. Anonymous /api/me → 401
	resp, _ := h.request(http.MethodGet, "/api/me", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}

	// 2. Request magic link
	resp, _ = h.request(http.MethodPost, "/api/auth/magic-link", "", map[string]string{"email": "alice@example.com"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("magic-link want 204, got %d", resp.StatusCode)
	}
	link := h.emails.lastLink()
	if !strings.Contains(link, "/api/auth/callback?token=") {
		t.Fatalf("unexpected link: %s", link)
	}

	// 3. Hit callback, verify Set-Cookie + redirect
	cookie := h.login(t, "alice@example.com")
	if cookie == "" {
		t.Fatal("no cookie")
	}

	// 4. /api/me with cookie returns user
	resp, body := h.request(http.MethodGet, "/api/me", cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me want 200, got %d (body=%s)", resp.StatusCode, body)
	}
	user := decode[map[string]any](t, body)
	if user["email"] != "alice@example.com" {
		t.Fatalf("unexpected user: %v", user)
	}
	if user["display_name"] == "" {
		t.Fatalf("display_name not set: %v", user)
	}

	// 5. Token cannot be reused
	tokenIdx := strings.Index(link, "token=")
	tok := link[tokenIdx+len("token="):]
	resp, _ = h.request(http.MethodGet, "/api/auth/callback?token="+tok, "", nil)
	if resp.StatusCode == http.StatusOK || (resp.StatusCode >= 300 && resp.StatusCode < 400) {
		// Defensive: depends on impl, but token should be marked used.
		// Subsequent /api/me with NEW cookie from this 2nd response should still work — but we
		// expect the server to reject reuse. Accept any non-2xx/3xx as proof.
		// If impl does set-cookie + redirect on reuse, that's a bug — fail.
		for _, c := range resp.Cookies() {
			if c.Name == "pingit_session" {
				t.Fatalf("magic link token reused: got new session cookie %s on second use", c.Value)
			}
		}
	}

	// 6. Logout clears cookie
	resp, _ = h.request(http.MethodPost, "/api/auth/logout", cookie, nil)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("logout want 2xx, got %d", resp.StatusCode)
	}
	resp, _ = h.request(http.MethodGet, "/api/me", cookie, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout /api/me want 401, got %d", resp.StatusCode)
	}
}

func TestMagicLinkInvalidEmail(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.request(http.MethodPost, "/api/auth/magic-link", "", map[string]string{"email": "not-an-email"})
	if resp.StatusCode == http.StatusNoContent {
		t.Fatalf("want validation error, got 204")
	}
}
