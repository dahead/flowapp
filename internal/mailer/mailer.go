package mailer

// Message holds all the metadata and content for a single outbound email.
type Message struct {
	From        string
	To          []string
	CC          []string
	BCC         []string
	Subject     string
	PlainBody   string
	HTMLBody    string
	Attachments []string // file paths to attach
}

// Mailer is the interface satisfied by both the SMTP and Graph implementations.
// Call Send to dispatch a single message; errors are returned but not retried.
type Mailer interface {
	Send(msg Message) error
}
