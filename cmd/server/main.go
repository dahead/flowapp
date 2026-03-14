package main

import (
	"flag"
	"flowapp/internal/auth"
	"flowapp/internal/mailer"
	"flowapp/internal/store"
	"flowapp/internal/web"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := flag.Int("port", 8080, "HTTP listen port")
	dataDir := flag.String("data", "data", "Directory for instance data files")
	wfDir := flag.String("workflows", "workflows", "Directory for .workflow definition files")
	flag.Parse()

	if err := os.MkdirAll("setup", 0700); err != nil {
		log.Fatal("setup dir:", err)
	}
	users, err := auth.NewUserStore("setup/users.json")
	if err != nil {
		log.Fatal("userstore:", err)
	}

	s, err := store.New(*wfDir, *dataDir)
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

	addr := fmt.Sprintf(":%d", *port)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	log.Printf("FlowApp v2 running on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
