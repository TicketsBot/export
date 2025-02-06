package worker

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"github.com/TicketsBot/export/internal/artifactstore"
	"github.com/TicketsBot/export/internal/config"
	"github.com/TicketsBot/export/internal/metrics"
	"github.com/TicketsBot/export/internal/model"
	"github.com/TicketsBot/export/internal/repository"
	"github.com/TicketsBot/export/internal/worker/transcriptstore"
	"log/slog"
	"time"
)

type Daemon struct {
	logger      *slog.Logger
	config      config.WorkerConfig
	privateKey  ed25519.PrivateKey
	repository  *repository.Repository
	transcripts transcriptstore.Client
	artifacts   artifactstore.ArtifactStore

	shutdownCh chan struct{}
}

func NewDaemon(
	logger *slog.Logger,
	config config.WorkerConfig,
	privateKey ed25519.PrivateKey,
	repository *repository.Repository,
	transcripts transcriptstore.Client,
	artifacts artifactstore.ArtifactStore,
) *Daemon {
	return &Daemon{
		logger:      logger,
		config:      config,
		privateKey:  privateKey,
		repository:  repository,
		transcripts: transcripts,
		artifacts:   artifacts,
		shutdownCh:  make(chan struct{}),
	}
}

func (d *Daemon) Start() {
	d.logger.Info("Starting daemon", slog.Duration("interval", d.config.Daemon.Interval))
	ticker := time.NewTicker(d.config.Daemon.Interval)

	for {
		select {
		case <-d.shutdownCh:
			return
		case <-ticker.C:
			task, err := d.getNextTask(context.Background())
			if err != nil {
				d.logger.Error("Failed to get next task", err)
				ticker.Reset(d.config.Daemon.Interval)
				continue
			}

			if task == nil {
				ticker.Reset(d.config.Daemon.Interval)
				continue
			}

			var status model.RequestStatus
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
			switch d.handleNext(ctx, task) {
			case nil:
				d.logger.Info("Task handled successfully", slog.String("request_id", task.First.RequestId.String()))
				status = model.RequestStatusCompleted
			default:
				d.logger.Error("Task failed", slog.String("request_id", task.First.RequestId.String()))
				status = model.RequestStatusFailed
			}
			cancel()

			metrics.RequestsProcessed.WithLabelValues(task.Second.Type.String(), status.String()).Inc()

			if err := d.repository.Tx(context.Background(), func(ctx context.Context, tx repository.TransactionContext) error {
				if err := tx.Requests().SetStatus(ctx, task.First.RequestId, status); err != nil {
					return err
				}

				return tx.Tasks().Delete(ctx, task.First.Id)
			}); err != nil {
				d.logger.Error("Failed to update task status", err)
			}

			ticker.Reset(d.config.Daemon.Interval)
		}
	}
}

func (d *Daemon) Shutdown() {
	close(d.shutdownCh)
}

func (d *Daemon) handleNext(ctx context.Context, next *model.Union[model.Task, model.Request]) error {
	task := next.First
	request := next.Second

	d.logger.Info("Handling task", slog.String("request_id", request.Id.String()),
		slog.String("type", string(request.Type)), slog.Uint64("user_id", request.UserId))

	switch request.Type {
	case model.RequestTypeGuildTranscripts:
		return d.handleGuildTranscriptsTask(ctx, task, request)
	default:
		d.logger.Error("Unknown request type", slog.String("type", string(request.Type)))
		return fmt.Errorf("unknown request type: %s", request.Type)
	}
}

func (d *Daemon) getNextTask(ctx context.Context) (*model.Union[model.Task, model.Request], error) {
	timedCtx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	var task *model.Union[model.Task, model.Request]
	if err := d.repository.Tx(timedCtx, func(ctx context.Context, tx repository.TransactionContext) (err error) {
		task, err = tx.Tasks().GetNext(ctx)
		return
	}); err != nil {
		return nil, err
	}

	return task, nil
}
