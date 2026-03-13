package mailer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Type string `json:"type"` // "smtp" or "graph"
	From string `json:"from"`

	// SMTP settings
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`

	// Graph settings
	GraphTenantID     string `json:"graph_tenant_id"`
	GraphClientID     string `json:"graph_client_id"`
	GraphClientSecret string `json:"graph_client_secret"`
	GraphSenderUPN    string `json:"graph_sender_upn"`
}

func GetConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home dir: %w", err)
	}
	return filepath.Join(home, ".config", "flowapp", "mail-config.json"), nil
}

func InitConfig() error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	// Create directory if not exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}

	// Check if file already exists
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("config file already exists: %s", configPath)
	}

	// Empty config (or with empty fields)
	cfg := Config{}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal empty config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("write config file %s: %w", configPath, err)
	}

	return nil
}

func LoadConfig() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", configPath, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &cfg, nil
}

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
