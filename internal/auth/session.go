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

type sessionPayload struct {
	UserID    string    `json:"u"`
	ExpiresAt time.Time `json:"e"`
}

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

func ClearSession(w http.ResponseWriter) {
	log.Printf("[session] clearing session")
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

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

func sign(data string) string {
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
