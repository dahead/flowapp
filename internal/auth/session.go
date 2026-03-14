package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const cookieName = "flowapp_session"
const cookieTTL = 7 * 24 * time.Hour

// sessionSecret is the HMAC key used to sign session cookies.
// Loaded from SESSION_SECRET env var; falls back to a random ephemeral key on startup.
var sessionSecret []byte

func init() {
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		// generate ephemeral secret — sessions won't survive restarts, fine for dev
		b := make([]byte, 32)
		rand.Read(b)
		sessionSecret = b
		log.Printf("[session] no SESSION_SECRET set — using ephemeral secret (sessions will not survive restarts)")
	} else {
		sessionSecret = []byte(secret)
		log.Printf("[session] using SESSION_SECRET from environment")
	}
}

// sessionPayload is the data stored inside each session cookie.
type sessionPayload struct {
	UserID    string    `json:"u"`
	ExpiresAt time.Time `json:"e"`
}

// SetSession writes a signed session cookie for the given user ID.
// The cookie is HTTP-only, SameSite=Lax, and expires after cookieTTL.
func SetSession(w http.ResponseWriter, userID string) {
	log.Printf("[session] set session for user %s", userID)
	payload := sessionPayload{UserID: userID, ExpiresAt: time.Now().Add(cookieTTL)}
	data, _ := json.Marshal(payload)
	encoded := base64.RawURLEncoding.EncodeToString(data)
	sig := sign(encoded)
	value := encoded + "." + sig
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(cookieTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSession expires the session cookie, effectively logging the user out.
func ClearSession(w http.ResponseWriter) {
	log.Printf("[session] clearing session")
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

// GetSessionUserID reads and validates the session cookie from the request.
// Returns the user ID on success, or an error if the cookie is missing,
// tampered with, or expired.
func GetSessionUserID(r *http.Request) (string, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return "", fmt.Errorf("no session")
	}
	parts := strings.SplitN(cookie.Value, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid session")
	}
	encoded, sig := parts[0], parts[1]
	if sig != sign(encoded) {
		log.Printf("[session] invalid session signature")
		return "", fmt.Errorf("invalid session signature")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		log.Printf("[session] invalid session encoding: %v", err)
		return "", fmt.Errorf("invalid session encoding")
	}
	var p sessionPayload
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("[session] invalid session data: %v", err)
		return "", fmt.Errorf("invalid session data")
	}
	if time.Now().After(p.ExpiresAt) {
		log.Printf("[session] session expired for user %s", p.UserID)
		return "", fmt.Errorf("session expired")
	}
	return p.UserID, nil
}

// sign produces an HMAC-SHA256 signature for the given data string,
// encoded as base64url without padding.
func sign(data string) string {
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
