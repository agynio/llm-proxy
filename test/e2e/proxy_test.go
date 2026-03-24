//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyUnknownModel(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))
	body := requestBody(t, uuid.NewString(), false)

	resp := doPost(t, client, proxyURL, body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestProxyUnauthorizedModel(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))
	body := requestBody(t, requireUnauthorizedModelID(t), false)

	resp := doPost(t, client, proxyURL, body)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestProxyNonStreamSuccess(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))
	model := requireModelID(t)

	resp := doPost(t, client, proxyURL, requestBody(t, model, false))
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var payload map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))

	object, ok := payload["object"].(string)
	require.True(t, ok, "expected object string")
	assert.Equal(t, "response", object)

	modelName, ok := payload["model"].(string)
	require.True(t, ok, "expected model string")
	assert.NotEmpty(t, modelName)
	assert.NotEqual(t, model, modelName)
	_, err := uuid.Parse(modelName)
	assert.Error(t, err)
}

func TestProxyStreamSuccess(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))
	model := requireModelID(t)

	resp := doPost(t, client, proxyURL, requestBody(t, model, true))
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")

	events := readSSEEvents(t, resp.Body)
	require.NotEmpty(t, events)
	assert.Equal(t, "response.completed", events[len(events)-1])
}
