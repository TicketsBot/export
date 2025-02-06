package keys

import (
	"crypto/x509"
	"encoding/pem"
	"github.com/TicketsBot/export/internal/api"
	"net/http"
)

func (a *API) SigningKey(w http.ResponseWriter, r *http.Request) {
	marshalled, err := x509.MarshalPKIXPublicKey(a.publicKey)
	if err != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusInternalServerError, "Failed to marshal public key"))
		return
	}

	block := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: marshalled,
	}

	w.Header().Set("Content-Type", "application/x-pem-file")
	w.WriteHeader(http.StatusOK)
	_ = pem.Encode(w, block)
}
