package auth

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	SessionCookieName = "pingit_session"
	SessionTTL        = 30 * 24 * time.Hour
	MagicLinkTTL      = 15 * time.Minute
)

func NewID() string {
	return ulid.Make().String()
}

func NewMagicToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func DisplayNameFromEmail(email string) string {
	local := email
	if idx := strings.IndexByte(email, '@'); idx > 0 {
		local = email[:idx]
	}
	local = strings.ReplaceAll(local, ".", " ")
	local = strings.ReplaceAll(local, "_", " ")
	parts := strings.Fields(local)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	if len(parts) == 0 {
		return "Player"
	}
	return strings.Join(parts, " ")
}
