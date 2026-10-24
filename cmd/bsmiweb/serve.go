package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hasname/bsmiweb/internal/config"
	"github.com/hasname/bsmiweb/internal/db"
	"github.com/hasname/bsmiweb/internal/web"
)

func serve(ctx context.Context, cfg config.Config, store *db.Store, fetch web.FetchRegistration) error {
	gin.SetMode(gin.ReleaseMode)
	app, err := web.New(store, fetch, cfg.SentryFrontend)
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
