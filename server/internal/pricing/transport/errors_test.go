package transport

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expectations repeat the legacy failure-kind mapping of the pricing
// handlers (internal/adapter/http/pricing.WriteFailure); only the channel
// errors that were hardcoded in Chinese now come from i18n.
func TestWriteErrorKeepsLegacyResponses(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		err  error
		want string
		code int
	}{
		{"revision", domain.ErrRevisionMismatch, `{"success":false,"code":"pricing_catalog_revision_mismatch","message":"Pricing catalog revision changed"}`, http.StatusConflict},
		{"invalid query", domain.InvalidQuery(errors.New("invalid limit")), `{"success":false,"message":"invalid limit"}`, http.StatusBadRequest},
		{"using group", domain.InvalidQuery(domain.ErrPricingUsingGroup), `{"success":false,"message":"invalid using_group"}`, http.StatusBadRequest},
		{"user group", domain.ErrInvalidUserGroup, `{"success":false,"message":"invalid user pricing group"}`, http.StatusBadRequest},
		{"user unavailable", domain.ErrUserGroupUnavailable, `{"success":false,"message":"failed to resolve user pricing group"}`, http.StatusInternalServerError},
		{"channel query", domain.ErrChannelQueryFailed, `{"success":false,"message":"Failed to query channel"}`, http.StatusInternalServerError},
		{"model unavailable", domain.ErrModelPricingUnavailable, `{"success":false,"message":"model pricing is unavailable"}`, http.StatusNotFound},
		{"no upstreams", domain.ErrNoUpstreams, `{"success":false,"message":"No valid upstream channel"}`, http.StatusOK},
		{"public unavailable", domain.ErrPublicPricingUnavailable, `{"success":false,"message":"failed to resolve public pricing"}`, http.StatusServiceUnavailable},
		{"plain", fmt.Errorf("failed to resolve pricing catalog revision"), `{"success":false,"message":"failed to resolve pricing catalog revision"}`, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			WriteError(c, tc.err)
			assert.Equal(t, tc.code, recorder.Code)
			assert.JSONEq(t, tc.want, recorder.Body.String())
		})
	}
}

func TestUpstreamErrorTextLocalizesOnlyUnrecognizedData(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	unrecognized := &domain.RatioDecodeError{Err: domain.ErrUpstreamDataUnrecognized, Warning: "unrecognized data format"}
	assert.Equal(t, "Unable to parse the upstream response data", UpstreamErrorText(c, unrecognized))
	assert.Equal(t, "upstream refused", UpstreamErrorText(c, errors.New("upstream refused")))
}
