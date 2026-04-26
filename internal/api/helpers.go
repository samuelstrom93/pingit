package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/samuelstrom93/pingit/internal/auth"
	"github.com/samuelstrom93/pingit/internal/db"
)

func (s *Server) decodeAndValidate(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !s.decodeJSON(w, r, dst) {
		return false
	}
	if err := s.validate.Struct(dst); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) findOrCreateUserTx(ctx context.Context, tx *sql.Tx, emailAddr string) (db.User, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, email, display_name, avatar_url, is_super_admin, created_at FROM users WHERE email = ?`, strings.ToLower(emailAddr))
	user, err := scanUser(row)
	if err == nil {
		if s.cfg.SuperAdminEmail != "" && strings.EqualFold(s.cfg.SuperAdminEmail, user.Email) && !user.IsSuperAdmin {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET is_super_admin = 1 WHERE id = ?`, user.ID); err == nil {
				user.IsSuperAdmin = true
			}
		}
		return user, nil
	}
	now := nowMS()
	user = db.User{
		ID:           auth.NewID(),
		Email:        strings.ToLower(emailAddr),
		DisplayName:  auth.DisplayNameFromEmail(emailAddr),
		IsSuperAdmin: s.cfg.SuperAdminEmail != "" && strings.EqualFold(s.cfg.SuperAdminEmail, emailAddr),
		CreatedAt:    now,
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO users (id, email, display_name, is_super_admin, created_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, user.Email, user.DisplayName, boolToInt(user.IsSuperAdmin), user.CreatedAt)
	return user, err
}

func (s *Server) requireMembership(ctx context.Context, spaceID, userID string) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM space_members WHERE space_id = ? AND user_id = ?`, spaceID, userID).Scan(&count)
	if err != nil || count == 0 {
		return errors.New("missing membership")
	}
	return nil
}

func (s *Server) requireAdmin(ctx context.Context, spaceID, userID string) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM space_members WHERE space_id = ? AND user_id = ? AND role = 'admin'`, spaceID, userID).Scan(&count)
	if err != nil || count == 0 {
		return errors.New("missing admin")
	}
	return nil
}

func (s *Server) countAdmins(ctx context.Context, spaceID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM space_members WHERE space_id = ? AND role = 'admin'`, spaceID).Scan(&count)
	return count, err
}

func scanUser(row interface{ Scan(dest ...any) error }) (db.User, error) {
	var user db.User
	var avatar sql.NullString
	var isSuperAdmin int
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &avatar, &isSuperAdmin, &user.CreatedAt)
	if err != nil {
		return user, err
	}
	if avatar.Valid {
		user.AvatarURL = &avatar.String
	}
	user.IsSuperAdmin = isSuperAdmin == 1
	return user, nil
}

func nowMS() int64 {
	return time.Now().UnixMilli()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableStringPtr(value *string) any {
	if value == nil {
		return nil
	}
	return nullableString(*value)
}

func nullableIntPtr(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func randomJoinCode() string {
	id := auth.NewID()
	return strings.ToLower(id[len(id)-8:])
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type sqlExecutor struct {
	db *sql.DB
	tx *sql.Tx
}

func (e sqlExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if e.tx != nil {
		return e.tx.ExecContext(ctx, query, args...)
	}
	return e.db.ExecContext(ctx, query, args...)
}
