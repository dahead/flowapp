package main

import (
	"os"
	"flowapp/internal/auth"
	"flowapp/internal/store"
	"flowapp/internal/web"
	"log"
	"net/http"
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

	h, err := web.New(s, users, "internal/web/templates/*.html")
	if err != nil {
		log.Fatal("templates:", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	log.Println("FlowApp v2 running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
