package transcriptstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/TicketsBot/common/encryption"
	"github.com/TicketsBot/export/internal/config"
	"github.com/TicketsBot/export/internal/utils"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"golang.org/x/sync/errgroup"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

type S3Client struct {
	logger *slog.Logger
	config config.WorkerConfig
	client *s3.Client
}

var _ Client = (*S3Client)(nil)

func NewS3Client(logger *slog.Logger, config config.WorkerConfig, client *s3.Client) *S3Client {
	return &S3Client{
		logger: logger,
		config: config,
		client: client,
	}
}

func (c *S3Client) GetTranscriptsForGuild(ctx context.Context, guildId uint64) (*GetTranscriptsResponse, error) {
	logger := c.logger.With(slog.Uint64("guild_id", guildId))

	keys, err := c.listForGuild(ctx, guildId)
	if err != nil {
		return nil, err
	}

	transcriptCount := 0
	for _, objKeys := range keys {
		transcriptCount += len(objKeys)
	}

	logger.Info("Found transcripts for guild", slog.Int("transcript_count", transcriptCount))

	if transcriptCount > 500_000 {
		return nil, fmt.Errorf("too many transcripts for guild %d: %d", guildId, transcriptCount)
	}

	prefix := fmt.Sprintf("%d/", guildId)

	keysCh := make(chan object, transcriptCount)

	mu := sync.Mutex{}
	files := make(map[int][]byte)

	wrappedCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, _ := errgroup.WithContext(wrappedCtx)
	for i := 0; i < c.config.Daemon.DownloadWorkers; i++ {
		logger := logger.With(slog.Int("worker_id", i))

		group.Go(func() error {
			for objMetadata := range keysCh {
				ticketId, bytes, err := c.downloadTranscript(ctx, logger, prefix, objMetadata)
				for err != nil {
					var awsErr smithy.APIError
					if errors.As(err, &awsErr) && awsErr.ErrorCode() == "TooManyRequests" {
						logger.WarnContext(ctx, "Too many requests, backing off...", "error", err)
						time.Sleep(time.Second * 2)
						ticketId, bytes, err = c.downloadTranscript(ctx, logger, prefix, objMetadata)
					} else {
						logger.ErrorContext(ctx, "Failed to download transcript", "error", err)
						cancel()
						return err
					}
				}

				mu.Lock()
				files[ticketId] = bytes
				mu.Unlock()
			}

			return nil
		})
	}

	for bucket, objKeys := range keys {
		for _, key := range objKeys {
			keysCh <- object{
				bucket: bucket,
				key:    key,
			}
		}
	}
	close(keysCh)

	if err := group.Wait(); err != nil {
		return nil, err
	}

	// Decompress + decrypt
	response := GetTranscriptsResponse{
		Transcripts: make(map[int][]byte),
		Failed:      make([]int, 0),
	}

	for ticketId, data := range files {
		decompressed, err := encryption.Decompress(data)
		if err != nil {
			response.Failed = append(response.Failed, ticketId)
			logger.WarnContext(ctx, "Failed to decompress ticket", "ticket_id", ticketId, "error", err)
		}

		decrypted, err := encryption.Decrypt([]byte(c.config.TranscriptS3.EncryptionKey), decompressed)
		if err != nil {
			response.Failed = append(response.Failed, ticketId)
			logger.WarnContext(ctx, "Failed to decrypt ticket", "ticket_id", ticketId, "error", err)
		}

		response.Transcripts[ticketId] = decrypted
	}

	return &response, nil
}

type object struct {
	bucket string
	key    string
}

func (c *S3Client) listForGuild(ctx context.Context, guildId uint64) (map[string][]string, error) {
	prefix := fmt.Sprintf("%d/", guildId)

	keys := make(map[string][]string)

	for _, bucket := range c.config.TranscriptS3.Buckets {
		paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
			Bucket: utils.Ptr(bucket),
			Prefix: utils.Ptr(prefix),
		})

		bucketKeys := make([]string, 0)
		for paginator.HasMorePages() {
			output, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, err
			}

			for _, obj := range output.Contents {
				if obj.Key == nil || !strings.HasPrefix(*obj.Key, prefix) {
					return nil, fmt.Errorf("unexpected key: %s", *obj.Key)
				}

				bucketKeys = append(bucketKeys, *obj.Key)
			}

			keys[bucket] = bucketKeys
		}
	}

	return keys, nil
}

func (c *S3Client) downloadTranscript(
	ctx context.Context,
	logger *slog.Logger,
	prefix string,
	objMetadata object,
) (int, []byte, error) {
	logger.DebugContext(ctx, "Next transcript", slog.String("key", objMetadata.key))

	if !strings.HasPrefix(objMetadata.key, prefix) {
		return 0, nil, fmt.Errorf("unexpected key: %s", objMetadata.key)
	}

	trimmed := strings.TrimPrefix(strings.TrimPrefix(objMetadata.key, prefix), "free-")
	ticketId, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, nil, err
	}

	logger.DebugContext(ctx, "Downloading transcript", slog.Int("ticket_id", ticketId))
	obj, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: utils.Ptr(objMetadata.bucket),
		Key:    utils.Ptr(objMetadata.key),
	})
	if err != nil {
		return 0, nil, err
	}

	logger.DebugContext(ctx, "Reading bytes from object", slog.Int("ticket_id", ticketId))

	bytes, err := io.ReadAll(obj.Body)
	if err != nil {
		return 0, nil, err
	}

	logger.DebugContext(ctx, "Downloaded transcript", slog.Int("ticket_id", ticketId))

	return ticketId, bytes, nil
}
