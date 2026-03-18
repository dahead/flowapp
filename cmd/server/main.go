package main

import (
	"context"
	"flag"
	"flowapp/internal/auth"
	"flowapp/internal/logger"
	"flowapp/internal/mailer"
	"flowapp/internal/store"
	"flowapp/internal/web"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 8080, "HTTP listen port")
	dataDir := flag.String("data", "data", "Directory for all runtime data (instances, users, notifications, session secret)")
	configDir := flag.String("config", "config", "Directory for configuration files (mail.json)")
	wfDir := flag.String("workflows", "workflows", "Directory for .workflow definition files")
	debug := flag.Bool("debug", false, "Enable debug-level logging")
	flag.Parse()

	// standard log package: date + time, no file/line prefix
	log.SetFlags(log.Ldate | log.Ltime)

	if *debug {
		logger.SetDebug(true)
	}

	main := logger.New("main")

	// ── Directories ───────────────────────────────────────────────────────────
	for _, dir := range []string{*dataDir, *configDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			main.Fatal("mkdir %s: %v", dir, err)
		}
	}

	// ── Session secret ────────────────────────────────────────────────────────
	auth.InitSessionSecret(*dataDir)

	// ── Mail config ───────────────────────────────────────────────────────────
	mailer.SetConfigDir(*configDir)

	// ── User store ────────────────────────────────────────────────────────────
	usersPath := filepath.Join(*dataDir, "users.json")
	users, err := auth.NewUserStore(usersPath)
	if err != nil {
		main.Fatal("userstore: %v", err)
	}

	// ── Data store ────────────────────────────────────────────────────────────
	s, err := store.New(*wfDir, *dataDir)
	if err != nil {
		main.Fatal("store: %v", err)
	}

	// ── Mailer ────────────────────────────────────────────────────────────────
	if cfg, err := mailer.LoadConfig(); err == nil {
		if m, err := mailer.NewMailerFromConfig(cfg); err == nil {
			s.SetMailer(mailer.EngineAdapter{M: m, From: cfg.From}, users.ResolveEmails)
			main.Info("mailer configured: %s", cfg.Type)
		} else {
			main.Warn("mailer init failed: %v", err)
		}
	} else {
		main.Info("no mail config found — in-app notifications only")
	}

	// ── Notification fan-out index ────────────────────────────────────────────
	s.SetUserResolver(users.ResolveUserIDs)
	refreshIndex := func() {
		s.SetAdminIDs(users.AdminIDs())
	}
	refreshIndex()
	users.OnChange(refreshIndex)

	// ── HTTP handler ──────────────────────────────────────────────────────────
	h, err := web.New(s, users, "internal/web/templates/*.html", *dataDir)
	if err != nil {
		main.Fatal("templates: %v", err)
	}

	addr := fmt.Sprintf(":%d", *port)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		main.Info("FlowApp running on http://localhost%s  (data=%s  config=%s)", addr, *dataDir, *configDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			main.Fatal("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	main.Info("shutdown signal received — draining requests...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		main.Error("forced shutdown: %v", err)
	}
	main.Info("server stopped cleanly")
}
