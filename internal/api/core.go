package api

import (
	"context"
	"encoding/json"
	"github.com/TicketsBot/export/internal/artifactstore"
	"github.com/TicketsBot/export/internal/config"
	"github.com/TicketsBot/export/internal/repository"
	"github.com/go-playground/validator/v10"
	"log/slog"
	"net/http"
)

type Core struct {
	Logger     *slog.Logger
	Config     config.ApiConfig
	Repository *repository.Repository
	Validator  *validator.Validate
	Artifacts  artifactstore.ArtifactStore
}

func NewCore(
	logger *slog.Logger,
	config config.ApiConfig,
	repository *repository.Repository,
	artifacts artifactstore.ArtifactStore,
) *Core {
	return &Core{
		Logger:     logger,
		Config:     config,
		Repository: repository,
		Validator:  validator.New(),
		Artifacts:  artifacts,
	}
}

func (c *Core) HandleError(ctx context.Context, w http.ResponseWriter, err *Error) {
	if err.StatusCode >= http.StatusInternalServerError && err.StatusCode < http.StatusInternalServerError+100 {
		c.Logger.ErrorContext(ctx, "", "error", err.Err)
	}

	err.Write(w)
}

func (c *Core) RespondJson(w http.ResponseWriter, statusCode int, response interface{}) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}
