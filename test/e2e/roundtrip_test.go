//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTripNonStream(t *testing.T) {
	client := newAuthenticatedClient(requireAPIToken(t))
	model := requireModelID(t)

	resp := doPost(t, client, proxyURL, requestBody(t, model, false))
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var payload map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.NotEmpty(t, payload)
	assert.NotEmpty(t, payload["object"])
}

func TestRoundTripStream(t *testing.T) {
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
