package main

import (
	"flag"
	"flowapp/internal/mailer"
	"flowapp/internal/store"
	"flowapp/internal/web"
	"fmt"
	"log"
	"net/http"
)

func main() {
	port := flag.Int("port", 8080, "Port auf dem der Server lauscht")
	verbose := flag.Bool("verbose", false, "Ausführliche Protokollierung aktivieren")
	initMailConfig := flag.Bool("init-mail-config", false, "E-Mail-Konfiguration initialisieren")
	flag.Parse()

	if *initMailConfig {
		if err := mailer.InitConfig(); err != nil {
			log.Fatalf("Error initializing mail config: %v", err)
		}
		path, _ := mailer.GetConfigPath()
		fmt.Printf("Mail configuration initialized at: %s\n", path)
		return
	}

	if *verbose {
		log.Printf("Starte FlowApp mit Port: %d (verbose mode an)\n", *port)
	}

	s, err := store.New("workflows", "data")
	if err != nil {
		log.Fatal("store:", err)
	}
	h, err := web.New(s, "internal/web/templates/*.html")
	if err != nil {
		log.Fatal("templates:", err)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("FlowApp v2 running on http://localhost:%d\n", *port)
	log.Fatal(http.ListenAndServe(addr, mux))
}
