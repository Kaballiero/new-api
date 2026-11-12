package notifications

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelegramGatewaySend(t *testing.T) {
	const apiToken = "gateway-token"
	tests := []struct {
		name     string
		text     string
		status   int
		wantText string
		wantErr  string
	}{
		{name: "sends the text", text: "Upstream model check notification\n\nsummary", status: http.StatusOK, wantText: "Upstream model check notification\n\nsummary"},
		{name: "keeps a text at the limit", text: strings.Repeat("я", 12000), status: http.StatusOK, wantText: strings.Repeat("я", 12000)},
		{name: "truncates to the limit", text: strings.Repeat("я", 12001), status: http.StatusOK, wantText: strings.Repeat("я", 12000)},
		{name: "does not split a surrogate pair", text: "a" + strings.Repeat("😀", 6000), status: http.StatusOK, wantText: "a" + strings.Repeat("😀", 5999)},
		{name: "fails on a non-2xx status", text: "summary", status: http.StatusBadGateway, wantText: "summary", wantErr: "telegram gateway request failed with status code: 502"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request *http.Request
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request = r
				body, _ = io.ReadAll(r.Body)
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			gateway := NewTelegramGateway(func() string { return server.URL + "/" }, func() string { return apiToken })
			err := gateway.Send(tt.text)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
			require.NotNil(t, request)
			assert.Equal(t, http.MethodPost, request.Method)
			assert.Equal(t, "/send", request.URL.Path)
			assert.Equal(t, "Bearer "+apiToken, request.Header.Get("Authorization"))
			assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
			var payload map[string]string
			require.NoError(t, common.Unmarshal(body, &payload))
			assert.Equal(t, map[string]string{"text": tt.wantText}, payload)
		})
	}
}

func TestTelegramGatewaySendDoesNotFollowRedirects(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer target.Close()
	server := httptest.NewServer(http.RedirectHandler(target.URL+"/send", http.StatusTemporaryRedirect))
	defer server.Close()
	gateway := NewTelegramGateway(func() string { return server.URL }, func() string { return "gateway-token" })

	err := gateway.Send("summary")

	require.EqualError(t, err, "telegram gateway request failed with status code: 307")
	assert.False(t, redirected)
}

func TestTelegramGatewaySendWithoutURL(t *testing.T) {
	gateway := NewTelegramGateway(func() string { return "" }, func() string { return "" })

	assert.NoError(t, gateway.Send("summary"))
}
