package mailer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// GraphMailer sends mail via Microsoft Graph API (v1.0).
// Requires an OAuth2 access token with Mail.Send permission.
type GraphMailer struct {
	// TenantID, ClientID, ClientSecret for client credentials flow
	TenantID     string
	ClientID     string
	ClientSecret string
	// SenderUPN is the UPN/email of the mailbox to send from (e.g. "workflow@example.com")
	SenderUPN string
}

func NewGraphMailer(tenantID, clientID, clientSecret, senderUPN string) *GraphMailer {
	return &GraphMailer{
		TenantID:     tenantID,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		SenderUPN:    senderUPN,
	}
}

func (g *GraphMailer) Send(msg Message) error {
	token, err := g.getToken()
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}
	return g.sendMail(token, msg)
}

// getToken fetches an access token via client credentials grant.
func (g *GraphMailer) getToken() (string, error) {
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
		return "", fmt.Errorf("%s: %s", result.Error, result.Description)
	}
	return result.AccessToken, nil
}

// Graph API message payload types
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
	ContentBytes string `json:"contentBytes"` // base64
}

func toRecipients(addrs []string) []graphRecipient {
	out := make([]graphRecipient, len(addrs))
	for i, a := range addrs {
		out[i] = graphRecipient{EmailAddress: graphEmail{Address: a}}
	}
	return out
}

func (g *GraphMailer) sendMail(token string, msg Message) error {
	// Prefer HTML body; fall back to plain
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

	// Attachments
	for _, path := range msg.Attachments {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read attachment %s: %w", path, err)
		}
		mimeType := "application/octet-stream"
		payload.Message.Attachments = append(payload.Message.Attachments, graphAttach{
			OdataType:    "#microsoft.graph.fileAttachment",
			Name:         filepath.Base(path),
			ContentType:  mimeType,
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
