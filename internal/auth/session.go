package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"flowapp/internal/logger"
)

const cookieName = "flowapp_session"
const cookieTTL = 7 * 24 * time.Hour

var sessionLog = logger.New("session")

// sessionSecret is the HMAC key used to sign session cookies and CSRF tokens.
// Priority: SESSION_SECRET env > data/session-secret file > generate random.
var sessionSecret []byte

// InitSessionSecret loads or generates the session secret.
// Call this from main() with the data directory before starting the server.
// Priority: SESSION_SECRET env var > dataDir/session-secret > generate+persist.
func InitSessionSecret(dataDir string) {
	if secret := os.Getenv("SESSION_SECRET"); secret != "" {
		sessionSecret = []byte(secret)
		sessionLog.Info("using SESSION_SECRET from environment")
		return
	}
	path := filepath.Join(dataDir, "session-secret")
	loadOrGenerate(path)
}

func init() {
	// init() runs before main(); only used if InitSessionSecret is never called
	// (e.g. in tests). Falls back to the legacy ~/.config path.
	if sessionSecret != nil {
		return // already set by InitSessionSecret
	}
	if secret := os.Getenv("SESSION_SECRET"); secret != "" {
		sessionSecret = []byte(secret)
		return
	}
	home, _ := os.UserHomeDir()
	legacyPath := ""
	if home != "" {
		legacyPath = filepath.Join(home, ".config", "flowapp", "session-secret")
	}
	loadOrGenerate(legacyPath)
}

// loadOrGenerate tries to load a persisted secret from path, or generates and persists a new one.
func loadOrGenerate(path string) {
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			decoded, err := hex.DecodeString(strings.TrimSpace(string(data)))
			if err == nil && len(decoded) == 32 {
				sessionSecret = decoded
				sessionLog.Info("loaded session secret from %s", path)
				return
			}
		}
	}
	// generate new
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate session secret: " + err.Error())
	}
	sessionSecret = b
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err == nil {
			_ = os.WriteFile(path, []byte(hex.EncodeToString(b)), 0600)
			sessionLog.Info("generated and persisted session secret to %s", path)
		}
	} else {
		sessionLog.Warn("no path for session secret — sessions will not survive restarts")
	}
}

// secretPath returns the legacy ~/.config path (used by DeleteSessionSecret).
func secretPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "flowapp", "session-secret"), nil
}

// sessionPayload is the data stored inside each session cookie.
type sessionPayload struct {
	UserID    string    `json:"u"`
	ExpiresAt time.Time `json:"e"`
}

// SetSession writes a signed session cookie for the given user ID.
// The cookie is HTTP-only, SameSite=Lax, and expires after cookieTTL.
func SetSession(w http.ResponseWriter, userID string) {
	sessionLog.Debug("set session for user %s", userID)
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
	sessionLog.Debug("clearing session")
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
		sessionLog.Warn("invalid session signature")
		return "", fmt.Errorf("invalid session signature")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		sessionLog.Warn("invalid session encoding: %v", err)
		return "", fmt.Errorf("invalid session encoding")
	}
	var p sessionPayload
	if err := json.Unmarshal(data, &p); err != nil {
		sessionLog.Warn("invalid session data: %v", err)
		return "", fmt.Errorf("invalid session data")
	}
	if time.Now().After(p.ExpiresAt) {
		sessionLog.Debug("session expired for user %s", p.UserID)
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
