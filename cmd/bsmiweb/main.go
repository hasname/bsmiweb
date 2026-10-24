package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"github.com/hasname/bsmiweb/internal/collector"
	"github.com/hasname/bsmiweb/internal/config"
	"github.com/hasname/bsmiweb/internal/db"
	"github.com/hasname/bsmiweb/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		sentry.CaptureException(err)
		sentry.Flush(2 * time.Second)
		log.Print(err)
		os.Exit(1)
	}
}
func run(args []string) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	if command == "help" || command == "-h" || command == "--help" {
		fmt.Println("Usage: bsmiweb [serve|migrate|cron|collect JOB|import-certificates|import-authorizations|healthcheck]\nJobs: import, recent, pchome, momo, books, friday")
		return nil
	}
	switch command {
	case "serve", "migrate", "cron", "import-certificates", "import-authorizations", "healthcheck":
		if len(args) > 1 {
			return fmt.Errorf("%s takes no arguments", command)
		}
	case "collect":
		if len(args) != 2 {
			return fmt.Errorf("usage: bsmiweb collect JOB")
		}
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if command == "healthcheck" {
		return healthcheck(cfg.HTTPAddr)
	}
	dsn := cfg.SentryCron
	if command == "serve" {
		dsn = cfg.SentryWeb
	}
	if dsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: dsn, AttachStacktrace: true, EnableTracing: true, TracesSampleRate: 0.1}); err != nil {
			return err
		}
		defer sentry.Flush(2 * time.Second)
	}
	store, err := db.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		return err
	}
	defer store.DB.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if command == "migrate" {
		if err := store.Migrate(ctx); err != nil {
			return err
		}
		log.Printf("database migrations are up to date (version %d)", db.Version)
		return nil
	}
	if err := store.CheckVersion(ctx); err != nil {
		return err
	}
	client := collector.NewClient()
	jobs := collector.Jobs{Client: client, Store: store, Location: cfg.Location}
	switch command {
	case "cron":
		return jobs.Daemon(ctx)
	case "collect":
		return jobs.Run(ctx, args[1])
	case "import-certificates", "import-authorizations":
		return jobs.Run(ctx, command)
	}
	gin.SetMode(gin.ReleaseMode)
	app, err := web.New(store, client.FetchRegistration, cfg.SentryFrontend)
	if err != nil {
		return err
	}
	defer app.Close()
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: app, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second}
	done := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
		case <-done:
			return
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Printf("HTTP shutdown: %v", err)
			_ = server.Close()
		}
	}()
	log.Printf("listening on %s", cfg.HTTPAddr)
	err = server.ListenAndServe()
	close(done)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func healthcheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned HTTP %d", res.StatusCode)
	}
	return nil
}
