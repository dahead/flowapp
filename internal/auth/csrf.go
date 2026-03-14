package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	csrfFieldName = "csrf_token"
	csrfTTL       = 4 * time.Hour
)

// GenerateCSRFToken creates a session-bound CSRF token valid for csrfTTL.
// Format: base64(userID:timestamp:hmac) — no server-side state required.
func GenerateCSRFToken(userID string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	payload := userID + ":" + ts
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	raw := payload + ":" + sig
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// ValidateCSRFToken checks the token from the form against the session user.
// Returns an error if the token is missing, tampered with, expired, or belongs to a different user.
func ValidateCSRFToken(token, userID string) error {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return fmt.Errorf("invalid csrf token encoding")
	}
	parts := strings.SplitN(string(raw), ":", 3)
	if len(parts) != 3 {
		return fmt.Errorf("malformed csrf token")
	}
	tokenUserID, ts, sig := parts[0], parts[1], parts[2]

	if tokenUserID != userID {
		return fmt.Errorf("csrf token user mismatch")
	}

	// verify signature
	payload := tokenUserID + ":" + ts
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return fmt.Errorf("invalid csrf token signature")
	}

	// check expiry
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid csrf token timestamp")
	}
	if time.Since(time.Unix(tsInt, 0)) > csrfTTL {
		return fmt.Errorf("csrf token expired")
	}
	return nil
}

// CSRFTokenFromRequest extracts the CSRF token from the form body.
func CSRFTokenFromRequest(r *http.Request) string {
	return r.FormValue(csrfFieldName)
}

// CSRFFieldName returns the HTML form field name for the CSRF token.
func CSRFFieldName() string {
	return csrfFieldName
}
