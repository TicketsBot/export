package requests

import (
	"context"
	"encoding/json"
	"github.com/TicketsBot/export/internal/api"
	"github.com/TicketsBot/export/internal/metrics"
	"github.com/TicketsBot/export/internal/model"
	"github.com/TicketsBot/export/internal/repository"
	"github.com/TicketsBot/export/internal/utils"
	"net/http"
	"time"
)

type CreateRequestBody struct {
	RequestType model.RequestType `json:"request_type"`
	GuildId     *uint64           `json:"guild_id,string"`
}

func (a *API) CreateRequest(w http.ResponseWriter, r *http.Request) {
	userId := a.userId(r.Context())
	ownedGuilds := a.ownedGuilds(r.Context())

	var body CreateRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusBadRequest, "Invalid body"))
		return
	}

	var pastRequests []model.RequestWithArtifact
	if err := a.Repository.Tx(r.Context(), func(ctx context.Context, tx repository.TransactionContext) (err error) {
		pastRequests, err = tx.Requests().ListForUser(ctx, userId)
		return
	}); err != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusInternalServerError, "Failed to fetch requests"))
		return
	}

	for _, request := range pastRequests {
		if request.Request.Type != body.RequestType {
			continue
		}

		if request.Request.GuildId != nil && *request.Request.GuildId != *body.GuildId {
			continue
		}

		if request.Request.Status == model.RequestStatusQueued {
			a.RespondJson(w, http.StatusBadRequest, utils.Map{
				"error": "You already have a request queued for this server",
			})
			return
		}

		if request.Request.CreatedAt.Before(time.Now().Add(-time.Hour * 24)) {
			a.RespondJson(w, http.StatusBadRequest, utils.Map{
				"error": "You have already made a request for this server in the last 24 hours",
			})
			return
		}
	}

	var request model.Request
	if body.RequestType == model.RequestTypeGuildTranscripts {
		if body.GuildId == nil {
			a.HandleError(r.Context(), w, api.NewError(nil, http.StatusBadRequest, "Guild ID required for this request type"))
			return
		}

		if !utils.Contains(ownedGuilds, *body.GuildId) {
			a.HandleError(r.Context(), w, api.NewError(nil, http.StatusForbidden, "User does not own this guild"))
			return
		}

		request = model.Request{
			UserId:  userId,
			Type:    body.RequestType,
			GuildId: body.GuildId,
		}
	} else {
		a.HandleError(r.Context(), w, api.NewError(nil, http.StatusBadRequest, "Invalid request type"))
		return
	}

	err := a.Repository.Tx(r.Context(), func(ctx context.Context, tx repository.TransactionContext) error {
		tmp, err := tx.Requests().Create(ctx, userId, request.Type, request.GuildId)
		if err != nil {
			return err
		}

		if _, err := tx.Tasks().Create(ctx, tmp.Id); err != nil {
			return err
		}

		request = tmp
		return nil
	})
	if err != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusInternalServerError, "Failed to log requests"))
		return
	}

	metrics.RequestsCreated.WithLabelValues(string(request.Type)).Inc()
	a.Logger.DebugContext(r.Context(), "Request created", "user_id", userId)

	a.RespondJson(w, http.StatusCreated, request)
}
