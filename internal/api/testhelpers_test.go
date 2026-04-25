package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	pingit "github.com/samuelstrom93/pingit"
	"github.com/samuelstrom93/pingit/internal/config"
	"github.com/samuelstrom93/pingit/internal/db"
	"github.com/samuelstrom93/pingit/internal/ws"
)

type capturedEmail struct {
	Kind  string
	Email string
	Link  string
}

type captureSender struct {
	mu     sync.Mutex
	sent   []capturedEmail
}

func (c *captureSender) SendMagicLink(_ context.Context, addr, link string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, capturedEmail{Kind: "magic-link", Email: addr, Link: link})
	return nil
}

func (c *captureSender) SendInvitation(_ context.Context, addr, link string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, capturedEmail{Kind: "invitation", Email: addr, Link: link})
	return nil
}

func (c *captureSender) lastLink() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		return ""
	}
	return c.sent[len(c.sent)-1].Link
}

type testHarness struct {
	t      *testing.T
	server *httptest.Server
	conn   *sql.DB
	emails *captureSender
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	conn, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	if err := db.Migrate(conn, pingit.MigrationsFS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := config.Config{Env: "dev", BaseURL: "http://test.local"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	emails := &captureSender{}
	srv := NewServer(cfg, conn, logger, emails, ws.NewHub())
	ts := httptest.NewServer(srv.Router(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})))
	t.Cleanup(func() {
		ts.Close()
		_ = conn.Close()
	})
	return &testHarness{t: t, server: ts, conn: conn, emails: emails}
}

func (h *testHarness) URL(path string) string { return h.server.URL + path }

func (h *testHarness) request(method, path, cookie string, body any) (*http.Response, []byte) {
	h.t.Helper()
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal: %v", err)
		}
		rdr = strings.NewReader(string(buf))
	}
	req, err := http.NewRequest(method, h.URL(path), rdr)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", "pingit_session="+cookie)
	}
	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	return resp, out
}

func (h *testHarness) login(t *testing.T, addr string) string {
	t.Helper()
	resp, _ := h.request(http.MethodPost, "/api/auth/magic-link", "", map[string]string{"email": addr})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("magic-link want 204, got %d", resp.StatusCode)
	}
	link := h.emails.lastLink()
	if link == "" {
		t.Fatal("no magic link captured")
	}
	idx := strings.Index(link, "token=")
	if idx < 0 {
		t.Fatalf("link missing token: %s", link)
	}
	token := link[idx+len("token="):]

	req, _ := http.NewRequest(http.MethodGet, h.URL("/api/auth/callback?token="+token), nil)
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	r, err := client.Do(req)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer r.Body.Close()
	for _, c := range r.Cookies() {
		if c.Name == "pingit_session" {
			return c.Value
		}
	}
	t.Fatal("no session cookie set on callback")
	return ""
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, string(body))
	}
	return v
}
