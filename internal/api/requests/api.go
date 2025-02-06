package requests

import (
	"context"
	"github.com/TicketsBot/export/internal/api"
)

type API struct {
	*api.Core
}

func NewAPI(core *api.Core) *API {
	return &API{
		Core: core,
	}
}

func (a *API) userId(ctx context.Context) uint64 {
	return ctx.Value("userId").(uint64)
}

func (a *API) ownedGuilds(ctx context.Context) []uint64 {
	return ctx.Value("ownedGuilds").([]uint64)
}
