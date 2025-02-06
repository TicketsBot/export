package auth

import (
	"encoding/json"
	"fmt"
	"github.com/TicketsBot/export/internal/api"
	"github.com/TicketsBot/export/internal/api/constants"
	"github.com/TicketsBot/export/internal/metrics"
	"github.com/TicketsBot/export/internal/utils"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwe"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ExchangeBody struct {
	Code string `json:"code" validate:"required"`
}

const DiscordApiVersion = 10

func (a *API) Exchange(w http.ResponseWriter, r *http.Request) {
	var body ExchangeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusBadRequest, "Invalid body"))
		return
	}

	if err := a.Validator.Struct(body); err != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusBadRequest, "Invalid body"))
		return
	}

	bearerToken, err := a.exchangeToken(body.Code)
	if err != nil {
		a.HandleError(r.Context(), w, err)
		return
	}

	// Get user ID
	userId, err := a.fetchUserId(bearerToken)
	if err != nil {
		a.HandleError(r.Context(), w, err)
		return
	}

	guilds, err := a.retrieveGuilds(r.Context(), bearerToken)
	if err != nil {
		a.HandleError(r.Context(), w, err)
		return
	}

	var ownedGuilds []string
	for _, g := range guilds {
		if g.Owner {
			ownedGuilds = append(ownedGuilds, strconv.FormatUint(g.Id, 10))
		}
	}

	token, tokenErr := jwt.NewBuilder().
		Issuer("https://export.ticketsbot.net").
		IssuedAt(time.Now()).
		Subject(strconv.FormatUint(userId, 10)).
		Expiration(time.Now().Add(a.Config.Jwt.Expiry)).
		NotBefore(time.Now()).
		Claim(constants.JwtClaimOwnedGuilds, ownedGuilds).
		Build()
	if tokenErr != nil {
		a.HandleError(r.Context(), w, api.NewError(tokenErr, http.StatusInternalServerError, "Failed to issue token"))
		return
	}

	signed, signErr := jwt.Sign(token, jwt.WithKey(jwa.HS256(), []byte(a.Config.Jwt.Secret)))
	if signErr != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusInternalServerError, "Failed to issue token"))
		return
	}

	encrypted, encryptErr := jwe.Encrypt(signed, jwe.WithKey(jwa.DIRECT(), []byte(a.Config.Jwt.EncryptionKey)))
	if encryptErr != nil {
		a.HandleError(r.Context(), w, api.NewError(err, http.StatusInternalServerError, "Failed to issue token"))
		return
	}

	metrics.ApiTokenExchanges.Inc()

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]any{
		"token":  utils.Base64Encode(encrypted),
		"guilds": guilds,
	})
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

func (a *API) exchangeToken(code string) (string, *api.Error) {
	uri := fmt.Sprintf("%s/api/v%d/oauth2/token", a.Config.Discord.RootUrl, DiscordApiVersion)

	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("code", code)
	body.Set("redirect_uri", a.Config.Discord.RedirectUri)
	body.Set("client_id", a.Config.Discord.ClientId)
	body.Set("client_secret", a.Config.Discord.ClientSecret)

	res, err := a.client.Post(uri, "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
	if err != nil {
		return "", api.NewError(err, http.StatusInternalServerError, "Failed to exchange code for token")
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		err := fmt.Errorf("unexpected status code during token exchange %d: %s", res.StatusCode, body)

		return "", api.NewError(err, http.StatusInternalServerError, "Failed to exchange code for token")
	}

	var token tokenResponse
	if err := json.NewDecoder(res.Body).Decode(&token); err != nil {
		return "", api.NewError(err, http.StatusInternalServerError, "Failed to decode token response")
	}

	return token.AccessToken, nil
}

func (a *API) fetchUserId(token string) (uint64, *api.Error) {
	uri := fmt.Sprintf("%s/api/v%d/users/@me", a.Config.Discord.RootUrl, DiscordApiVersion)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return 0, api.NewError(err, http.StatusInternalServerError, "Failed to create requests")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	res, err := a.client.Do(req)
	if err != nil {
		return 0, api.NewError(err, http.StatusInternalServerError, "Failed to fetch user ID")
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		err := fmt.Errorf("unexpected status code during user fetch %d: %s", res.StatusCode, body)

		return 0, api.NewError(err, http.StatusInternalServerError, "Failed to fetch user ID")
	}

	var user struct {
		Id uint64 `json:"id,string"`
	}

	if err := json.NewDecoder(res.Body).Decode(&user); err != nil {
		return 0, api.NewError(err, http.StatusInternalServerError, "Failed to fetch user")
	}

	return user.Id, nil
}
