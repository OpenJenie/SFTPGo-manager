package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	_ "sftpgo-manager/docs"

	"sftpgo-manager/internal/config"
	"sftpgo-manager/internal/httpapi"
	"sftpgo-manager/internal/service"
	"sftpgo-manager/internal/sftpgo"
	"sftpgo-manager/internal/sqlite"
	"sftpgo-manager/internal/storage"
)

// @title SFTPGo Manager API
// @version 1.0
// @description Multi-tenant SFTP management with S3-backed storage and automatic CSV ingestion into a records table.

// @host localhost:9090
// @BasePath /api

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter "Bearer <api_key>"

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	repo, err := sqlite.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to init db: %v", err)
	}
	defer func() { _ = repo.Close() }()

	sftpgoClient := sftpgo.New(cfg.SFTPGoURL, cfg.SFTPGoAdminUser, cfg.SFTPGoAdminPass)
	bootstrap := service.NewBootstrapService(cfg, repo)
	auth := service.NewAuthService(repo)
	tenants := service.NewTenantService(cfg, repo, sftpgoClient)
	external := service.NewExternalAuthService(cfg, repo)

	var uploads *service.UploadService
	if cfg.S3Endpoint != "" {
		store, err := storage.NewMinIOStore(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3UseSSL)
		if err != nil {
			log.Printf("warning: object store init failed (CSV processing disabled): %v", err)
		} else {
			uploads = service.NewUploadService(repo, store, cfg.S3Bucket)
		}
	}

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: httpapi.New(bootstrap, auth, tenants, external, uploads),
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("received %s, shutting down", sig)
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Printf("listening on %s", cfg.ListenAddr)
	log.Printf("swagger UI: http://localhost%s/swagger/index.html", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
