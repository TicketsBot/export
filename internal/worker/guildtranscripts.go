package worker

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"github.com/TicketsBot/export/internal/metrics"
	"github.com/TicketsBot/export/internal/model"
	"github.com/TicketsBot/export/internal/repository"
	"github.com/TicketsBot/export/internal/utils"
	"golang.org/x/sync/errgroup"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

const transcriptExpiry = time.Hour * 24 * 3
const maxActiveSize = 250 * 1024 * 1024 * 1024

func (d *Daemon) handleGuildTranscriptsTask(ctx context.Context, task model.Task, request model.Request) error {
	if request.GuildId == nil || *request.GuildId == 0 {
		d.logger.Error("Guild ID is nil", slog.String("task_id", task.Id.String()))
		return fmt.Errorf("guild ID is nil")
	}

	guildId := *request.GuildId

	logger := d.logger.With(slog.Uint64("guild_id", guildId), "request_id", request.Id)

	transcripts, err := d.transcripts.GetTranscriptsForGuild(ctx, guildId)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to get transcripts for guild", "error", err)
		return err
	}

	logger.InfoContext(ctx, "Got transcripts for guild")

	files := make(map[string][]byte)
	mu := sync.Mutex{}

	guildIdStr := fmt.Sprintf("%d", guildId)
	files["guild_id.txt"] = []byte(guildIdStr)
	files["guild_id.txt.sig"] = []byte(utils.Base64Encode(ed25519.Sign(d.privateKey, []byte(guildIdStr))))

	if len(transcripts.Failed) > 0 {
		marshalled := make([]string, 0, len(transcripts.Failed))
		for _, ticketId := range transcripts.Failed {
			marshalled = append(marshalled, strconv.Itoa(ticketId))
		}

		content := []byte("The following tickets failed to export:\n" + strings.Join(marshalled, ", "))
		files["failed.txt"] = content
		files["failed.txt.sig"] = []byte(utils.Base64Encode(ed25519.Sign(d.privateKey, content)))
	}

	type transcriptData struct {
		ticketId   int
		transcript []byte
	}

	ch := make(chan transcriptData, len(transcripts.Transcripts))
	group, _ := errgroup.WithContext(ctx)

	for i := 0; i < d.config.Daemon.SigningWorkers; i++ {
		group.Go(func() error {
			for data := range ch {
				sigData := make([]byte, 0, len(data.transcript)+len(guildIdStr)+2+6)
				sigData = append(sigData, []byte(guildIdStr)...)
				sigData = append(sigData, byte('|'))
				sigData = append(sigData, []byte(fmt.Sprintf("%d", data.ticketId))...)
				sigData = append(sigData, byte('|'))
				sigData = append(sigData, data.transcript...)

				signed := []byte(utils.Base64Encode(ed25519.Sign(d.privateKey, sigData)))

				mu.Lock()
				files[fmt.Sprintf("transcripts/%d.json", data.ticketId)] = data.transcript
				files[fmt.Sprintf("transcripts/%d.json.sig", data.ticketId)] = signed
				mu.Unlock()
			}

			return nil
		})
	}

	for ticketId, transcript := range transcripts.Transcripts {
		ch <- transcriptData{
			ticketId:   ticketId,
			transcript: transcript,
		}
	}
	close(ch)

	if err := group.Wait(); err != nil {
		logger.ErrorContext(ctx, "Failed to sign transcripts", "error", err)
		return err
	}

	artifact, err := utils.BuildZip(files)
	if err != nil {
		d.logger.Error("Failed to build zip", err)
		return err
	}

	artifactSize := int64(len(artifact))

	var globalArtifactSize int64
	if err := d.repository.Tx(ctx, func(ctx context.Context, tx repository.TransactionContext) (err error) {
		globalArtifactSize, err = tx.Artifacts().GetGlobalSize(ctx)
		return err
	}); err != nil {
		d.logger.Error("Failed to get global artifact size", "error", err)
		return err
	}

	if globalArtifactSize+artifactSize > maxActiveSize {
		d.logger.Error("Artifact size exceeds maximum", slog.Int64("size", globalArtifactSize+artifactSize))
		return fmt.Errorf("artifact size exceeds maximum")
	}

	logger.InfoContext(ctx, "Uploading artifact", slog.Int64("size", artifactSize))

	key := utils.RandomString(32)
	expiresAt := time.Now().Add(transcriptExpiry)
	if err := d.artifacts.Store(ctx, request.Id, key, expiresAt, artifact); err != nil {
		d.logger.Error("Failed to store artifact", "error", err)
		return err
	}

	metrics.ArtifactsUploaded.WithLabelValues(request.Type.String()).Inc()
	metrics.ArtifactsUploadedBytes.WithLabelValues(request.Type.String()).Add(float64(artifactSize))

	if err := d.repository.Tx(ctx, func(ctx context.Context, tx repository.TransactionContext) error {
		if err := tx.Requests().SetStatus(ctx, request.Id, model.RequestStatusCompleted); err != nil {
			return err
		}

		if err := tx.Artifacts().Create(ctx, request.Id, key, expiresAt, artifactSize); err != nil {
			return err
		}

		return nil
	}); err != nil {
		d.logger.Error("Failed to update request status", "error", err)
		return err
	}

	return nil
}
