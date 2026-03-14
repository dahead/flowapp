package main

import (
	"flowapp/internal/auth"
	"flowapp/internal/mailer"
	"flowapp/internal/store"
	"flowapp/internal/web"
	"log"
	"net/http"
	"os"
)

func main() {
	if err := os.MkdirAll("setup", 0700); err != nil {
		log.Fatal("setup dir:", err)
	}
	users, err := auth.NewUserStore("setup/users.json")
	if err != nil {
		log.Fatal("userstore:", err)
	}

	s, err := store.New("workflows", "data")
	if err != nil {
		log.Fatal("store:", err)
	}

	if cfg, err := mailer.LoadConfig(); err == nil {
		if m, err := mailer.NewMailerFromConfig(cfg); err == nil {
			s.SetMailer(mailer.EngineAdapter{M: m, From: cfg.From}, users.ResolveEmails)
			log.Println("[main] mailer configured:", cfg.Type)
		} else {
			log.Println("[main] mailer init failed:", err)
		}
	} else {
		log.Println("[main] no mail config found — notifications are log-only")
	}

	h, err := web.New(s, users, "internal/web/templates/*.html")
	if err != nil {
		log.Fatal("templates:", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	log.Println("FlowApp v2 running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
