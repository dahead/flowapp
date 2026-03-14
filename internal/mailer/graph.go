package mailer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// GraphMailer sends mail via the Microsoft Graph API (v1.0) using OAuth2 client credentials.
// Requires Mail.Send permission on the Azure application registration.
type GraphMailer struct {
	TenantID     string // Azure AD tenant ID
	ClientID     string // Azure app client ID
	ClientSecret string // Azure app client secret
	SenderUPN    string // UPN of the sending mailbox, e.g. "workflow@example.com"

	// cached OAuth2 token to avoid a round-trip on every send
	cachedToken    string
	tokenExpiresAt time.Time
}

// NewGraphMailer constructs a GraphMailer with the given Azure AD credentials.
func NewGraphMailer(tenantID, clientID, clientSecret, senderUPN string) *GraphMailer {
	return &GraphMailer{
		TenantID:     tenantID,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		SenderUPN:    senderUPN,
	}
}

// Send acquires an access token (from cache if still valid) and delivers the message
// via the Graph sendMail endpoint.
func (g *GraphMailer) Send(msg Message) error {
	log.Printf("[mailer/graph] sending email — subject: %q to: %v", msg.Subject, msg.To)
	token, err := g.getToken()
	if err != nil {
		log.Printf("[mailer/graph] failed to get token: %v", err)
		return fmt.Errorf("get token: %w", err)
	}
	if err := g.sendMail(token, msg); err != nil {
		log.Printf("[mailer/graph] send failed — subject: %q to: %v: %v", msg.Subject, msg.To, err)
		return err
	}
	log.Printf("[mailer/graph] email sent successfully — subject: %q to: %v", msg.Subject, msg.To)
	return nil
}

// getToken returns a valid OAuth2 access token, re-fetching from Azure AD only when the
// cached token has expired. Tokens are cached for 55 minutes (Graph tokens are valid for 1 hour).
func (g *GraphMailer) getToken() (string, error) {
	if g.cachedToken != "" && time.Now().Before(g.tokenExpiresAt) {
		return g.cachedToken, nil
	}
	log.Printf("[mailer/graph] fetching OAuth2 token for tenant %s", g.TenantID)
	url := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", g.TenantID)
	body := fmt.Sprintf(
		"grant_type=client_credentials&client_id=%s&client_secret=%s&scope=https://graph.microsoft.com/.default",
		g.ClientID, g.ClientSecret,
	)
	resp, err := http.Post(url, "application/x-www-form-urlencoded", bytes.NewBufferString(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != "" {
		log.Printf("[mailer/graph] token error: %s: %s", result.Error, result.Description)
		return "", fmt.Errorf("%s: %s", result.Error, result.Description)
	}
	g.cachedToken = result.AccessToken
	g.tokenExpiresAt = time.Now().Add(55 * time.Minute)
	log.Printf("[mailer/graph] token acquired successfully")
	return g.cachedToken, nil
}

// Graph API JSON payload types

type graphMessage struct {
	Message         graphMail `json:"message"`
	SaveToSentItems bool      `json:"saveToSentItems"`
}

type graphMail struct {
	Subject      string           `json:"subject"`
	Body         graphBody        `json:"body"`
	ToRecipients []graphRecipient `json:"toRecipients"`
	CcRecipients []graphRecipient `json:"ccRecipients,omitempty"`
	Attachments  []graphAttach    `json:"attachments,omitempty"`
}

type graphBody struct {
	ContentType string `json:"contentType"` // "HTML" or "Text"
	Content     string `json:"content"`
}

type graphRecipient struct {
	EmailAddress graphEmail `json:"emailAddress"`
}

type graphEmail struct {
	Address string `json:"address"`
}

type graphAttach struct {
	OdataType    string `json:"@odata.type"`
	Name         string `json:"name"`
	ContentType  string `json:"contentType"`
	ContentBytes string `json:"contentBytes"` // base64-encoded file contents
}

// toRecipients converts a slice of email address strings to Graph API recipient objects.
func toRecipients(addrs []string) []graphRecipient {
	out := make([]graphRecipient, len(addrs))
	for i, a := range addrs {
		out[i] = graphRecipient{EmailAddress: graphEmail{Address: a}}
	}
	return out
}

// sendMail serialises the message as JSON and POSTs it to the Graph sendMail endpoint.
func (g *GraphMailer) sendMail(token string, msg Message) error {
	// prefer HTML body; fall back to plain text
	bodyContent := msg.HTMLBody
	bodyType := "HTML"
	if bodyContent == "" {
		bodyContent = msg.PlainBody
		bodyType = "Text"
	}

	payload := graphMessage{
		SaveToSentItems: true,
		Message: graphMail{
			Subject:      msg.Subject,
			Body:         graphBody{ContentType: bodyType, Content: bodyContent},
			ToRecipients: toRecipients(msg.To),
			CcRecipients: toRecipients(msg.CC),
		},
	}

	// encode any file attachments as base64
	for _, path := range msg.Attachments {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read attachment %s: %w", path, err)
		}
		payload.Message.Attachments = append(payload.Message.Attachments, graphAttach{
			OdataType:    "#microsoft.graph.fileAttachment",
			Name:         filepath.Base(path),
			ContentType:  "application/octet-stream",
			ContentBytes: base64.StdEncoding.EncodeToString(data),
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s/sendMail", g.SenderUPN)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("graph api %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
