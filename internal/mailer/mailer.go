package mailer

// Message holds all email metadata and content.
type Message struct {
	From        string
	To          []string
	CC          []string
	BCC         []string
	Subject     string
	PlainBody   string
	HTMLBody    string
	Attachments []string // file paths
}

// Mailer is the interface both SMTP and Graph implementations satisfy.
type Mailer interface {
	Send(msg Message) error
}
