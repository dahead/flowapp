package mailer

import (
	"bytes"
	"encoding/base64"
	"flowapp/internal/logger"
	"fmt"
	"mime"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
)

var smtpLog = logger.New("mailer/smtp")

// SMTPMailer sends mail via a standard SMTP relay.
// Leave Username and Password empty for unauthenticated relay (e.g. internal mail servers).
type SMTPMailer struct {
	Host     string // SMTP host, e.g. "mailrelay.internal"
	Port     int    // SMTP port, e.g. 25 or 587
	Username string // optional: SMTP auth username
	Password string // optional: SMTP auth password
}

// NewSMTPMailer constructs an SMTPMailer with the given connection parameters.
func NewSMTPMailer(host string, port int, username, password string) *SMTPMailer {
	return &SMTPMailer{Host: host, Port: port, Username: username, Password: password}
}

// Send builds a MIME message and delivers it via SMTP.
// Plain auth is used when Username is non-empty; otherwise the connection is unauthenticated.
func (s *SMTPMailer) Send(msg Message) error {
	smtpLog.Info("sending email — subject: %q to: %v", msg.Subject, msg.To)
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}

	raw, err := buildMIME(msg)
	if err != nil {
		smtpLog.Error("build mime failed: %v", err)
		return fmt.Errorf("build mime: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	all := append(msg.To, append(msg.CC, msg.BCC...)...)
	if err := smtp.SendMail(addr, auth, msg.From, all, raw); err != nil {
		smtpLog.Error("send failed to %v via %s: %v", all, addr, err)
		return err
	}
	smtpLog.Info("email sent successfully — subject: %q to: %v", msg.Subject, msg.To)
	return nil
}

// buildMIME constructs a complete RFC 2045 MIME message from a Message struct.
// Without attachments: multipart/alternative (plain + HTML).
// With attachments: multipart/mixed wrapping multipart/alternative + file parts.
func buildMIME(msg Message) ([]byte, error) {
	var buf bytes.Buffer

	// standard headers
	buf.WriteString("From: " + msg.From + "\r\n")
	buf.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	if len(msg.CC) > 0 {
		buf.WriteString("Cc: " + strings.Join(msg.CC, ", ") + "\r\n")
	}
	buf.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject) + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(msg.Attachments) == 0 {
		// no attachments: use multipart/alternative directly
		alt := multipart.NewWriter(&buf)
		buf.WriteString("Content-Type: multipart/alternative; boundary=" + alt.Boundary() + "\r\n\r\n")
		if err := writeAlternativeParts(alt, msg); err != nil {
			return nil, err
		}
		alt.Close()
		return buf.Bytes(), nil
	}

	// with attachments: multipart/mixed outer wrapper
	mixed := multipart.NewWriter(&buf)
	buf.WriteString("Content-Type: multipart/mixed; boundary=" + mixed.Boundary() + "\r\n\r\n")

	// inner multipart/alternative body part
	var altBuf bytes.Buffer
	alt := multipart.NewWriter(&altBuf)
	if err := writeAlternativeParts(alt, msg); err != nil {
		return nil, err
	}
	alt.Close()

	altHeader := textproto.MIMEHeader{}
	altHeader.Set("Content-Type", "multipart/alternative; boundary="+alt.Boundary())
	altPart, err := mixed.CreatePart(altHeader)
	if err != nil {
		return nil, err
	}
	altPart.Write(altBuf.Bytes())

	// attachment parts
	for _, path := range msg.Attachments {
		if err := writeAttachment(mixed, path); err != nil {
			return nil, fmt.Errorf("attachment %s: %w", path, err)
		}
	}
	mixed.Close()
	return buf.Bytes(), nil
}

// writeAlternativeParts writes the plain-text and HTML body parts into a multipart/alternative writer.
func writeAlternativeParts(w *multipart.Writer, msg Message) error {
	// plain text part
	ph := textproto.MIMEHeader{}
	ph.Set("Content-Type", "text/plain; charset=utf-8")
	ph.Set("Content-Transfer-Encoding", "quoted-printable")
	pp, err := w.CreatePart(ph)
	if err != nil {
		return err
	}
	pp.Write([]byte(msg.PlainBody))

	// HTML part
	hh := textproto.MIMEHeader{}
	hh.Set("Content-Type", "text/html; charset=utf-8")
	hh.Set("Content-Transfer-Encoding", "quoted-printable")
	hp, err := w.CreatePart(hh)
	if err != nil {
		return err
	}
	hp.Write([]byte(msg.HTMLBody))
	return nil
}

// writeAttachment reads a file from disk and appends it as a base64-encoded MIME part.
func writeAttachment(w *multipart.Writer, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	filename := filepath.Base(path)
	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	h := textproto.MIMEHeader{}
	h.Set("Content-Type", mimeType+"; name=\""+filename+"\"")
	h.Set("Content-Transfer-Encoding", "base64")
	h.Set("Content-Disposition", "attachment; filename=\""+filename+"\"")

	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	// write base64 in 76-character lines per RFC 2045
	encoded := base64.StdEncoding.EncodeToString(data)
	for len(encoded) > 76 {
		part.Write([]byte(encoded[:76] + "\r\n"))
		encoded = encoded[76:]
	}
	part.Write([]byte(encoded + "\r\n"))
	return nil
}
