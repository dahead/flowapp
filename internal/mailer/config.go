package mailer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the mail backend configuration loaded from disk.
// Set Type to "smtp" or "graph" and fill the corresponding fields.
type Config struct {
	Type string `json:"type"` // "smtp" or "graph"
	From string `json:"from"` // sender address used in the From header

	// SMTP settings (used when Type == "smtp")
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`

	// Microsoft Graph settings (used when Type == "graph")
	GraphTenantID     string `json:"graph_tenant_id"`
	GraphClientID     string `json:"graph_client_id"`
	GraphClientSecret string `json:"graph_client_secret"`
	GraphSenderUPN    string `json:"graph_sender_upn"` // mailbox UPN, e.g. "workflow@example.com"
}

// configPath is set at startup via SetConfigDir. Falls back to legacy ~/.config path.
var configPath string

// SetConfigDir sets the directory where mail.json is stored.
// Call this from main() before LoadConfig or SaveMailConfig.
func SetConfigDir(dir string) {
	configPath = filepath.Join(dir, "mail.json")
}

// GetConfigPath returns the active mail config file path.
// If SetConfigDir was called, returns that path; otherwise falls back to ~/.config/flowapp/mail-config.json.
func GetConfigPath() (string, error) {
	if configPath != "" {
		return configPath, nil
	}
	// legacy fallback
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home dir: %w", err)
	}
	return filepath.Join(home, ".config", "flowapp", "mail-config.json"), nil
}

// LoadConfig reads and parses the mail config from the active path.
func LoadConfig() (*Config, error) {
	path, err := GetConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}

// NewMailerFromConfig constructs the appropriate Mailer implementation
// based on the Type field of the config ("smtp" or "graph").
func NewMailerFromConfig(cfg *Config) (Mailer, error) {
	switch cfg.Type {
	case "smtp":
		return NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword), nil
	case "graph":
		return NewGraphMailer(cfg.GraphTenantID, cfg.GraphClientID, cfg.GraphClientSecret, cfg.GraphSenderUPN), nil
	default:
		return nil, fmt.Errorf("unknown mailer type: %s", cfg.Type)
	}
}
