//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

const invalidAPIToken = "agyn_invalid"

func TestAuthMissingToken(t *testing.T) {
	body := requestBody(t, uuid.NewString(), false)
	resp := doPost(t, newClient(), proxyURL, body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthInvalidToken(t *testing.T) {
	body := requestBody(t, uuid.NewString(), false)
	resp := doPost(t, newAuthenticatedClient(invalidAPIToken), proxyURL, body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthNonAgynToken(t *testing.T) {
	body := requestBody(t, uuid.NewString(), false)
	resp := doPost(t, newAuthenticatedClient("not-agyn-token"), proxyURL, body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthValidToken(t *testing.T) {
	config := apiTokenConfig()
	if config.token == "" {
		t.Skip("api token not configured")
	}

	body := requestBody(t, modelIDOrRandom(), false)
	resp := doPost(t, newAuthenticatedClient(config.token), proxyURL, body)
	defer resp.Body.Close()

	assert.NotEqual(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthZitiToken(t *testing.T) {
	client := newZitiClient(t)

	body := requestBody(t, modelIDOrRandom(), false)
	resp := doPost(t, client, zitiProxyURL(), body)
	defer resp.Body.Close()

	assert.NotEqual(t, http.StatusUnauthorized, resp.StatusCode)
}

func modelIDOrRandom() string {
	if model := modelID(); model != "" {
		return model
	}
	return uuid.NewString()
}
