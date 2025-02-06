package repository

import (
	"context"
	_ "embed"
	"errors"
	"github.com/TicketsBot/export/internal/model"
	"github.com/TicketsBot/export/internal/utils"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type RequestRepository struct {
	tx pgx.Tx
}

var (
	//go:embed sql/requests/create.sql
	queryRequestsCreate string

	//go:embed sql/requests/list_for_user.sql
	queryRequestsListForUser string

	//go:embed sql/requests/get_by_id.sql
	queryRequestsGetById string

	//go:embed sql/requests/set_status.sql
	queryRequestsSetStatus string
)

func NewRequestRepository(tx pgx.Tx) *RequestRepository {
	return &RequestRepository{
		tx: tx,
	}
}

func (r *RequestRepository) Create(ctx context.Context, userId uint64, requestType model.RequestType, guildId *uint64) (model.Request, error) {
	request := model.Request{
		UserId:  userId,
		Type:    requestType,
		GuildId: guildId,
		Status:  model.RequestStatusQueued,
	}

	if err := r.tx.QueryRow(ctx, queryRequestsCreate, userId, requestType, guildId).Scan(
		&request.Id, &request.CreatedAt,
	); err != nil {
		return model.Request{}, err
	}

	return request, nil
}

func (r *RequestRepository) ListForUser(ctx context.Context, userId uint64) ([]model.RequestWithArtifact, error) {
	requests := make([]model.RequestWithArtifact, 0)

	rows, err := r.tx.Query(ctx, queryRequestsListForUser, userId)
	if err != nil {
		return nil, err
	}

	defer rows.Close()
	for rows.Next() {
		var request model.Request

		var (
			artifactId        *uuid.UUID
			artifactRequestId *uuid.UUID
			artifactKey       *string
			artifactExpiresAt *time.Time
		)

		if err := rows.Scan(
			&request.Id,
			&request.UserId,
			&request.Type,
			&request.CreatedAt,
			&request.GuildId,
			&request.Status,
			&artifactId,
			&artifactRequestId,
			&artifactKey,
			&artifactExpiresAt,
		); err != nil {
			return nil, err
		}

		var artifact *model.Artifact
		if artifactId != nil {
			artifact = &model.Artifact{
				Id:        *artifactId,
				RequestId: *artifactRequestId,
				Key:       *artifactKey,
				ExpiresAt: *artifactExpiresAt,
			}
		}

		requests = append(requests, model.RequestWithArtifact{
			Request:  request,
			Artifact: artifact,
		})
	}

	return requests, nil
}

func (r *RequestRepository) GetById(ctx context.Context, requestId uuid.UUID) (*model.RequestWithArtifact, error) {
	var request model.Request

	var (
		artifactId        *uuid.UUID
		artifactRequestId *uuid.UUID
		artifactKey       *string
		artifactExpiresAt *time.Time
	)

	if err := r.tx.QueryRow(ctx, queryRequestsGetById, requestId).Scan(
		&request.Id,
		&request.UserId,
		&request.Type,
		&request.CreatedAt,
		&request.GuildId,
		&request.Status,
		&artifactId,
		&artifactRequestId,
		&artifactKey,
		&artifactExpiresAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	var artifact *model.Artifact
	if artifactId != nil {
		artifact = &model.Artifact{
			Id:        *artifactId,
			RequestId: *artifactRequestId,
			Key:       *artifactKey,
			ExpiresAt: *artifactExpiresAt,
		}
	}

	return utils.Ptr(model.NewRequestWithArtifact(request, artifact)), nil
}

func (r *RequestRepository) SetStatus(ctx context.Context, requestId uuid.UUID, status model.RequestStatus) error {
	_, err := r.tx.Exec(ctx, queryRequestsSetStatus, status, requestId)
	return err
}
