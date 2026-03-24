//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProxyConnectivity(t *testing.T) {
	resp := doRequest(t, newClient(), http.MethodGet, responsesURL(proxyURL), nil, "")
	defer resp.Body.Close()

	assert.Less(t, resp.StatusCode, http.StatusInternalServerError)
}
