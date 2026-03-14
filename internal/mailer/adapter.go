package mailer

import "flowapp/internal/engine"

// EngineAdapter bridges the mailer.Mailer interface to the engine.Mailer interface,
// avoiding an import cycle between the engine and mailer packages.
// From is the default sender address injected into messages that don't set one explicitly.
type EngineAdapter struct {
	M    Mailer
	From string
}

// Send converts an engine.MailMessage to a mailer.Message and dispatches it.
// If the message has no From address set, the adapter's From field is used as fallback.
func (a EngineAdapter) Send(msg engine.MailMessage) error {
	from := msg.From
	if from == "" {
		from = a.From
	}
	return a.M.Send(Message{
		From:      from,
		To:        msg.To,
		Subject:   msg.Subject,
		PlainBody: msg.PlainBody,
		HTMLBody:  msg.HTMLBody,
	})
}
