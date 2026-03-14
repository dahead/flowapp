package mailer

import "flowapp/internal/engine"

// EngineAdapter wraps a Mailer so it satisfies the engine.Mailer interface.
// From is the sender address injected into every outgoing message.
type EngineAdapter struct {
	M    Mailer
	From string
}

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
