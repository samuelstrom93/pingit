package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samuelstrom93/pingit/internal/auth"
)

type magicLinkRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func (s *Server) handleMagicLink(w http.ResponseWriter, r *http.Request) {
	var req magicLinkRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}

	token, err := auth.NewMagicToken()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	now := nowMS()
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO magic_link_tokens (token, email, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		token, strings.ToLower(req.Email), now+int64(auth.MagicLinkTTL/time.Millisecond), now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to persist token")
		return
	}

	link := fmt.Sprintf("%s/api/auth/callback?token=%s", strings.TrimSuffix(s.cfg.BaseURL, "/"), url.QueryEscape(token))
	if err := s.email.SendMagicLink(r.Context(), req.Email, link); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to send email")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		s.writeError(w, http.StatusBadRequest, "missing token")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to open transaction")
		return
	}
	defer tx.Rollback()

	var email string
	var expiresAt int64
	var usedAt sql.NullInt64
	err = tx.QueryRowContext(r.Context(), `SELECT email, expires_at, used_at FROM magic_link_tokens WHERE token = ?`, token).Scan(&email, &expiresAt, &usedAt)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid token")
		return
	}
	if usedAt.Valid || expiresAt <= nowMS() {
		s.writeError(w, http.StatusBadRequest, "expired token")
		return
	}

	user, err := s.findOrCreateUserTx(r.Context(), tx, email)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	sessionID := auth.NewID()
	now := nowMS()
	if _, err = tx.ExecContext(r.Context(), `UPDATE magic_link_tokens SET used_at = ? WHERE token = ?`, now, token); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update token")
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO sessions (id, user_id, expires_at, created_at, user_agent, ip) VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, user.ID, now+int64(auth.SessionTTL/time.Millisecond), now, r.UserAgent(), clientIP(r))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.IsProd(),
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ck, err := r.Cookie(auth.SessionCookieName)
	if err == nil && ck.Value != "" {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, ck.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cfg.IsProd()})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, userFromContext(r.Context()))
}
