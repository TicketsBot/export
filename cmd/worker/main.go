package main

import (
	"context"
	"github.com/TicketsBot/export/internal/artifactstore"
	"github.com/TicketsBot/export/internal/config"
	"github.com/TicketsBot/export/internal/metrics"
	"github.com/TicketsBot/export/internal/repository"
	"github.com/TicketsBot/export/internal/utils"
	"github.com/TicketsBot/export/internal/worker"
	"github.com/TicketsBot/export/internal/worker/transcriptstore"
	"github.com/aws/aws-sdk-go-v2/aws"
	s3Config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg, err := config.New[config.WorkerConfig]()
	if err != nil {
		panic(err)
	}

	metrics.StartServer(cfg.PrometheusServerAddr)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: utils.ParseLogLevel(cfg.LogLevel, slog.LevelInfo),
	}))

	setupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	cancel()

	s3Client := s3.NewFromConfig(s3Cfg)
	transcriptClient := transcriptstore.NewS3Client(
		logger.With(slog.String("module", "transcript_client")),
		cfg, s3Client,
	)

	artifactClient := artifactstore.NewS3ArtifactStore(
		logger.With(slog.String("module", "artifact_store")),
		s3Client, cfg.ArtifactStore.Bucket, []byte(cfg.ArtifactStore.EncryptionKey),
	)

	key, err := utils.LoadKeyFromDisk(cfg.KeyPath)
	if err != nil {
		logger.Error("Failed to load key", "error", err)
		os.Exit(1)
	}

	daemon := worker.NewDaemon(
		logger.With(slog.String("module", "daemon")),
		cfg, key, repository, transcriptClient, artifactClient)
	go daemon.Start()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch

	logger.Info("Shutting down...")
	daemon.Shutdown()
}
