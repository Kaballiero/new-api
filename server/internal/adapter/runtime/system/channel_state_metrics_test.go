package system

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelStateMetricsExportsOneCurrentSamplePerChannel(t *testing.T) {
	queryCalls := 0
	handler := NewChannelStateMetricsHandler(func() ([]ChannelStateSnapshot, error) {
		queryCalls++
		return []ChannelStateSnapshot{
			{ID: 1, Type: 1, Status: common.ChannelStatusEnabled},
			{ID: 2, Type: 14, Status: common.ChannelStatusManuallyDisabled},
			{ID: 3, Type: 18, Status: common.ChannelStatusAutoDisabled},
			{ID: 4, Type: 0, Status: 99},
		}, nil
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 1, queryCalls)
	assert.Contains(t, recorder.Body.String(), `# HELP new_api_channel_state Current persisted state of an AI channel (1 = current state).
# TYPE new_api_channel_state gauge
new_api_channel_state{channel_id="1",channel_type="1",state="enabled"} 1
new_api_channel_state{channel_id="2",channel_type="14",state="manually_disabled"} 1
new_api_channel_state{channel_id="3",channel_type="18",state="auto_disabled"} 1
new_api_channel_state{channel_id="4",channel_type="0",state="unknown"} 1
`)
	assert.Equal(t, 4, strings.Count(recorder.Body.String(), "new_api_channel_state{"))
}

func TestChannelStateMetricsExposeOnlyBoundedStateContext(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		status                   int
		otherInfo                string
		category, upstreamStatus string
		timestamp                int64
	}{
		{"credits", common.ChannelStatusAutoDisabled, `{"status_reason":"status_code=402, Insufficient credits; token=secret-token https://secret.invalid/request-123","status_time":1700000000}`, "credits", "402", 1700000000},
		{"request timeout", common.ChannelStatusAutoDisabled, `{"status_reason":"status_code=408, provider timeout","status_time":1700000001}`, "timeout", "408", 1700000001},
		{"auth", common.ChannelStatusAutoDisabled, `{"status_reason":"bad response status code 401, invalid key"}`, "auth", "401", 0},
		{"rate limit", common.ChannelStatusAutoDisabled, `{"status_reason":"status_code=429, limit exceeded"}`, "rate_limit", "429", 0},
		{"provider error", common.ChannelStatusAutoDisabled, `{"status_reason":"status_code=503, unavailable"}`, "provider_error", "503", 0},
		{"credits text", common.ChannelStatusAutoDisabled, `{"status_reason":"Insufficient upstream credits"}`, "credits", "unknown", 0},
		{"timeout text", common.ChannelStatusAutoDisabled, `{"status_reason":"Post https://secret.invalid: context deadline exceeded"}`, "timeout", "unknown", 0},
		{"manual", common.ChannelStatusManuallyDisabled, `{"status_reason":"status_code=402, old failure","status_time":1700000002}`, "manual", "none", 1700000002},
		{"enabled retains persisted metadata", common.ChannelStatusEnabled, `{"status_reason":"status_code=402, old failure","status_time":1700000003}`, "none", "none", 1700000003},
		{"unknown state", 99, `{"status_reason":"status_code=402, old failure","status_time":1700000004}`, "unknown", "unknown", 1700000004},
		{"missing metadata", common.ChannelStatusAutoDisabled, "", "unknown", "unknown", 0},
		{"malformed metadata", common.ChannelStatusAutoDisabled, `{"status_time":1700000005,"status_reason":"secret-token`, "unknown", "unknown", 0},
		{"invalid reason type", common.ChannelStatusAutoDisabled, `{"status_reason":{"message":"secret-token"},"status_time":1700000006}`, "unknown", "unknown", 1700000006},
		{"null timestamp", common.ChannelStatusAutoDisabled, `{"status_reason":"status_code=402","status_time":null}`, "credits", "402", 0},
		{"negative timestamp", common.ChannelStatusAutoDisabled, `{"status_reason":"status_code=402","status_time":-1}`, "credits", "402", 0},
		{"fractional timestamp", common.ChannelStatusAutoDisabled, `{"status_reason":"status_code=402","status_time":1700000000.5}`, "credits", "402", 0},
		{"overflow timestamp", common.ChannelStatusAutoDisabled, `{"status_reason":"status_code=402","status_time":1e309}`, "credits", "402", 0},
		{"unknown reason and untrusted digits", common.ChannelStatusAutoDisabled, `{"status_reason":"https://secret.invalid/status_code=402?token=secret-token","request_id":"request-123"}`, "unknown", "unknown", 0},
		{"invalid status code", common.ChannelStatusAutoDisabled, `{"status_reason":"status_code=4021, secret-token"}`, "unknown", "unknown", 0},
		{"client quota is not upstream credits", common.ChannelStatusAutoDisabled, `{"status_reason":"token quota is not enough"}`, "unknown", "unknown", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const name = "ОР \"main\"\\region\nbackup"
			handler := NewChannelStateMetricsHandler(func() ([]ChannelStateSnapshot, error) {
				return []ChannelStateSnapshot{{ID: 20, Type: 20, Status: tc.status, Name: name, OtherInfo: tc.otherInfo}, {ID: 21, Type: 99999, Status: 99, Name: "Unknown channel"}}, nil
			})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			require.Equal(t, http.StatusOK, recorder.Code)
			body := recorder.Body.String()
			assert.Contains(t, body, fmt.Sprintf(`new_api_channel_info{channel_id="20",channel_name=%q,channel_type="20",provider="OpenRouter"} 1`, name))
			assert.Contains(t, body, `new_api_channel_info{channel_id="21",channel_name="Unknown channel",channel_type="99999",provider="Unknown"} 1`)
			assert.Contains(t, body, fmt.Sprintf(`new_api_channel_state_reason{channel_id="20",channel_type="20",reason_category=%q,state=%q,upstream_status=%q} 1`, tc.category, channelStateLabel(tc.status), tc.upstreamStatus))
			assert.Contains(t, body, `new_api_channel_state{channel_id="21",channel_type="99999",state="unknown"} 1`)
			if tc.timestamp > 0 {
				assert.Contains(t, body, fmt.Sprintf(`new_api_channel_state_changed_timestamp_seconds{channel_id="20",channel_type="20"} %g`, float64(tc.timestamp)))
			} else {
				assert.NotContains(t, body, `new_api_channel_state_changed_timestamp_seconds{channel_id="20"`)
			}
			assert.NotContains(t, body, "secret-token")
			assert.NotContains(t, body, "secret.invalid")
			assert.NotContains(t, body, "request-123")
			assert.NotContains(t, body, "old failure")
		})
	}
}

func TestChannelStateMetricsReturnsRedacted500OnQueryError(t *testing.T) {
	handler := NewChannelStateMetricsHandler(func() ([]ChannelStateSnapshot, error) {
		return nil, errors.New("postgres://user:secret@db/channel_state")
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "channel state snapshot unavailable")
	assert.NotContains(t, recorder.Body.String(), "secret")
	assert.NotContains(t, recorder.Body.String(), "postgres://")
	assert.NotContains(t, recorder.Body.String(), "new_api_channel_state{")
}

func TestChannelStateMetricsHandlersUseIsolatedRegistries(t *testing.T) {
	query := func() ([]ChannelStateSnapshot, error) { return nil, nil }
	require.NotPanics(t, func() {
		first := NewChannelStateMetricsHandler(query)
		second := NewChannelStateMetricsHandler(query)
		for _, handler := range []http.Handler{first, second} {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			assert.Equal(t, http.StatusOK, recorder.Code)
		}
	})
}
