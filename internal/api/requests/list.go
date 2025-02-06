package requests

import (
	"context"
	"github.com/TicketsBot/export/internal/api"
	"github.com/TicketsBot/export/internal/model"
	"github.com/TicketsBot/export/internal/repository"
	"net/http"
	"time"
)

type ListRequestsDto struct {
	model.Request
	ArtifactExpiresAt *time.Time `json:"artifact_expires_at,omitempty"`
}

func (a *API) ListRequests(w http.ResponseWriter, r *http.Request) {
	userId := a.userId(r.Context())

	var requests []model.RequestWithArtifact
	if err := a.Repository.Tx(r.Context(), func(ctx context.Context, tx repository.TransactionContext) (err error) {
		requests, err = tx.Requests().ListForUser(ctx, userId)
		return
	}); err != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusInternalServerError, "Failed to get requests"))
		return
	}

	dto := make([]ListRequestsDto, len(requests))
	for i, request := range requests {
		var artifactExpiresAt *time.Time
		if request.Artifact != nil {
			artifactExpiresAt = &request.Artifact.ExpiresAt
		}

		dto[i] = ListRequestsDto{
			Request:           request.Request,
			ArtifactExpiresAt: artifactExpiresAt,
		}
	}

	a.RespondJson(w, http.StatusOK, dto)
}
