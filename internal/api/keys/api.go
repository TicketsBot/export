package keys

import (
	"crypto/ed25519"
	"github.com/TicketsBot/export/internal/api"
)

type API struct {
	*api.Core
	publicKey ed25519.PublicKey
}

func NewAPI(core *api.Core, publicKey ed25519.PublicKey) *API {
	return &API{
		Core:      core,
		publicKey: publicKey,
	}
}
