//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationEmptyBody(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))

	resp := doPost(t, client, proxyURL, nil)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestValidationMalformedJSON(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))

	resp := doPost(t, client, proxyURL, []byte("{"))
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestValidationMissingModel(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))
	body, err := json.Marshal(map[string]any{"input": "hello"})
	require.NoError(t, err)

	resp := doPost(t, client, proxyURL, body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestValidationModelNotUUID(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))
	body, err := json.Marshal(map[string]any{"model": "not-a-uuid", "input": "hello"})
	require.NoError(t, err)

	resp := doPost(t, client, proxyURL, body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestValidationWrongMethod(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))

	resp := doRequest(t, client, http.MethodGet, responsesURL(proxyURL), nil, "")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestValidationWrongPath(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))
	body := requestBody(t, uuid.NewString(), false)
	url := strings.TrimRight(proxyURL, "/") + "/v1/chat/completions"

	resp := doRequest(t, client, http.MethodPost, url, body, "application/json")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
