package router

import (
	"crypto/ed25519"
	"github.com/TicketsBot/export/internal/api"
	"github.com/TicketsBot/export/internal/api/auth"
	"github.com/TicketsBot/export/internal/api/health"
	"github.com/TicketsBot/export/internal/api/keys"
	"github.com/TicketsBot/export/internal/api/middleware"
	"github.com/TicketsBot/export/internal/api/requests"
	"github.com/TicketsBot/export/internal/artifactstore"
	"github.com/TicketsBot/export/internal/config"
	"github.com/TicketsBot/export/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	slogchi "github.com/samber/slog-chi"
	"log/slog"
	"net/http"
)

func New(
	logger *slog.Logger,
	config config.ApiConfig,
	repository *repository.Repository,
	artifacts artifactstore.ArtifactStore,
	publicKey ed25519.PublicKey,
) *chi.Mux {
	core := api.NewCore(logger, config, repository, artifacts)

	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Metrics)
	r.Use(slogchi.New(logger))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   config.Server.AllowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
	}))

	// Routes
	r.Get("/livez", health.Livez)

	// /auth
	r.Group(func(r chi.Router) {
		api := auth.NewAPI(core)

		r.Post("/auth/exchange", api.Exchange)
		//r.Post("/auth/guilds", api.FetchGuilds)
	})

	// /requests
	r.Group(func(r chi.Router) {
		api := requests.NewAPI(core)

		r.Use(middleware.Authenticate(core))

		r.Get("/requests", api.ListRequests)
		r.Post("/requests", api.CreateRequest)

		r.Get("/requests/{requestId}/artifact", api.GetArtifact)
	})

	// /keys
	r.Group(func(r chi.Router) {
		api := keys.NewAPI(core, publicKey)

		r.Get("/keys/signing", api.SigningKey)
	})

	return r
}
