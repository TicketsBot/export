package main

import (
	"context"
	"github.com/TicketsBot/export/internal/api/router"
	"github.com/TicketsBot/export/internal/artifactstore"
	"github.com/TicketsBot/export/internal/config"
	"github.com/TicketsBot/export/internal/metrics"
	"github.com/TicketsBot/export/internal/repository"
	"github.com/TicketsBot/export/internal/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	s3Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg, err := config.New[config.ApiConfig]()
	if err != nil {
		panic(err)
	}

	metrics.StartServer(cfg.PrometheusServerAddr)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: utils.ParseLogLevel(cfg.LogLevel, slog.LevelInfo),
	}))

	setupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repository, err := repository.Connect(setupCtx, cfg.Database)
	if err != nil {
		logger.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}

	s3Cfg, err := s3Config.LoadDefaultConfig(setupCtx, s3Config.WithCredentialsProvider(aws.NewCredentialsCache(
		credentials.NewStaticCredentialsProvider(cfg.S3.AccessKey, cfg.S3.SecretKey, ""))),
		s3Config.WithBaseEndpoint(cfg.S3.Endpoint),
		s3Config.WithRegion(cfg.S3.Region))
	if err != nil {
		logger.Error("Failed to load S3 config", "error", err)
		os.Exit(1)
	}

	s3Client := s3.NewFromConfig(s3Cfg)
	artifacts := artifactstore.NewS3ArtifactStore(
		logger.With("component", "artifactstore"),
		s3Client, cfg.ArtifactStore.Bucket, []byte(cfg.ArtifactStore.EncryptionKey))

	cancel()

	publicKey, err := utils.LoadPublicKeyFromDisk(cfg.PublicKeyPath)
	if err != nil {
		logger.Error("Failed to load key", "error", err)
		os.Exit(1)
	}

	router := router.New(logger, cfg, repository, artifacts, publicKey)

	server := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: router,
	}

	closed := make(chan struct{})
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch

		logger.Info("Shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			logger.Error("Server failed to shut down", "error", err)
			os.Exit(1)
		}

		close(closed)
	}()

	logger.Info("Starting server...", "address", cfg.Server.Address)
	if err := server.ListenAndServe(); err != nil {
		logger.Error("Server failed to start", "error", err)
		os.Exit(1)
	}

	<-closed
}
