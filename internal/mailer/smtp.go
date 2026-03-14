package mailer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
)

// SMTPMailer sends mail via an SMTP relay.
type SMTPMailer struct {
	Host string // e.g. "mailrelay.internal"
	Port int    // e.g. 25 or 587
	// Optional: leave empty for unauthenticated relay
	Username string
	Password string
}

func NewSMTPMailer(host string, port int, username, password string) *SMTPMailer {
	return &SMTPMailer{Host: host, Port: port, Username: username, Password: password}
}

func (s *SMTPMailer) Send(msg Message) error {
	log.Printf("[mailer/smtp] sending email — subject: %q to: %v", msg.Subject, msg.To)
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}

	raw, err := buildMIME(msg)
	if err != nil {
		log.Printf("[mailer/smtp] build mime failed: %v", err)
		return fmt.Errorf("build mime: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	all := append(msg.To, append(msg.CC, msg.BCC...)...)
	if err := smtp.SendMail(addr, auth, msg.From, all, raw); err != nil {
		log.Printf("[mailer/smtp] send failed to %v via %s: %v", all, addr, err)
		return err
	}
	log.Printf("[mailer/smtp] email sent successfully — subject: %q to: %v", msg.Subject, msg.To)
	return nil
}

// buildMIME constructs the full MIME message as a byte slice.
func buildMIME(msg Message) ([]byte, error) {
	var buf bytes.Buffer

	// Headers
	buf.WriteString("From: " + msg.From + "\r\n")
	buf.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	if len(msg.CC) > 0 {
		buf.WriteString("Cc: " + strings.Join(msg.CC, ", ") + "\r\n")
	}
	buf.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject) + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")

	mixed := multipart.NewWriter(&buf)

	if len(msg.Attachments) > 0 {
		buf.WriteString("Content-Type: multipart/mixed; boundary=" + mixed.Boundary() + "\r\n\r\n")
	} else {
		// No attachments: use multipart/alternative directly
		alt := multipart.NewWriter(&buf)
		buf.WriteString("Content-Type: multipart/alternative; boundary=" + alt.Boundary() + "\r\n\r\n")
		if err := writeAlternativeParts(alt, msg); err != nil {
			return nil, err
		}
		alt.Close()
		return buf.Bytes(), nil
	}

	// multipart/mixed wrapper
	buf.WriteString("Content-Type: multipart/mixed; boundary=" + mixed.Boundary() + "\r\n\r\n")

	// Body part: multipart/alternative inside mixed
	altHeader := textproto.MIMEHeader{}
	altWriter := multipart.NewWriter(nil) // just to get a boundary
	altWriter = multipart.NewWriter(&bytes.Buffer{})
	altHeader.Set("Content-Type", "multipart/alternative; boundary="+altWriter.Boundary())
	altPart, err := mixed.CreatePart(altHeader)
	if err != nil {
		return nil, err
	}
	altBuf := &writerBuffer{Writer: altPart}
	alt := multipart.NewWriter(altBuf)
	// reuse same boundary trick: write directly
	fmt.Fprintf(altPart, "--%s\r\n", alt.Boundary())
	_ = alt // we'll write manually for cleaner control

	// Easier: write the alternative block inline
	var bodyBuf bytes.Buffer
	altInner := multipart.NewWriter(&bodyBuf)
	if err := writeAlternativeParts(altInner, msg); err != nil {
		return nil, err
	}
	altInner.Close()
	altPart.Write(bodyBuf.Bytes())

	// Attachments
	for _, path := range msg.Attachments {
		if err := writeAttachment(mixed, path); err != nil {
			return nil, fmt.Errorf("attachment %s: %w", path, err)
		}
	}
	mixed.Close()
	return buf.Bytes(), nil
}

func writeAlternativeParts(w *multipart.Writer, msg Message) error {
	// Plain
	ph := textproto.MIMEHeader{}
	ph.Set("Content-Type", "text/plain; charset=utf-8")
	ph.Set("Content-Transfer-Encoding", "quoted-printable")
	pp, err := w.CreatePart(ph)
	if err != nil {
		return err
	}
	pp.Write([]byte(msg.PlainBody))

	// HTML
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
	encoded := base64.StdEncoding.EncodeToString(data)
	// Write in 76-char lines (RFC 2045)
	for len(encoded) > 76 {
		part.Write([]byte(encoded[:76] + "\r\n"))
		encoded = encoded[76:]
	}
	part.Write([]byte(encoded + "\r\n"))
	return nil
}

// writerBuffer wraps an io.Writer to satisfy io.Writer for multipart.NewWriter.
type writerBuffer struct {
	bytes.Buffer
	Writer interface{ Write([]byte) (int, error) }
}

func (w *writerBuffer) Write(p []byte) (int, error) {
	return w.Writer.Write(p)
}
