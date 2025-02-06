package auth

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"github.com/TicketsBot/export/internal/api"
	"io"
	"net/http"
	"slices"
)

func (a *API) FetchGuilds(w http.ResponseWriter, r *http.Request) {
	// Reuse body struct
	var body ExchangeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusBadRequest, "Invalid body"))
		return
	}

	if err := a.Validator.Struct(body); err != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusBadRequest, "Invalid body"))
		return
	}

	// Exchange code for token
	bearerToken, err := a.exchangeToken(body.Code)
	if err != nil {
		a.HandleError(r.Context(), w, err)
		return
	}

	// Fetch guilds
	guilds, err := a.retrieveGuilds(r.Context(), bearerToken)
	if err != nil {
		a.HandleError(r.Context(), w, err)
		return
	}

	slices.SortFunc(guilds, func(a, b guild) int {
		return cmp.Compare(a.Name, b.Name)
	})

	// Write response
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(guilds)
}

type guild struct {
	Id    uint64  `json:"id,string"`
	Name  string  `json:"name"`
	Icon  *string `json:"icon"`
	Owner bool    `json:"owner"`
}

func (a *API) retrieveGuilds(ctx context.Context, token string) ([]guild, *api.Error) {
	uri := fmt.Sprintf("%s/api/v%d/users/@me/guilds", a.Config.Discord.RootUrl, DiscordApiVersion)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, api.NewError(err, http.StatusInternalServerError, "Failed to create requests")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	res, err := a.client.Do(req)
	if err != nil {
		return nil, api.NewError(err, http.StatusInternalServerError, "Failed to perform requests")
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		err := fmt.Errorf("unexpected status code during guild fetch %d: %s", res.StatusCode, body)

		return nil, api.NewError(err, http.StatusInternalServerError, "Failed to fetch guilds")
	}

	var guilds []guild
	if err := json.NewDecoder(res.Body).Decode(&guilds); err != nil {
		return nil, api.NewError(err, http.StatusInternalServerError, "Failed to decode guilds")
	}

	return guilds, nil
}
