// Package managementhttp assembles real management routes for the portable Wave 6c tests.
package managementhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest"
	f "github.com/QuantumNous/new-api/internal/contracttest/managementfixture"
	tf "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

type Fixture struct {
	Env          *contracttest.Env
	Bearers      map[string]string
	Observations []any
}

// Run issues fresh access tokens inside the frozen clock without changing production authentication.
func Run(t *testing.T, relay bool, execute func(*testing.T, *Fixture) any) any {
	t.Helper()
	tf.InstallDBClock(t)
	env := contracttest.New(t)
	tf.Configure()
	common.StartTime = tf.Epoch
	common.OptionMap = map[string]string{}
	service.InitHttpClient()
	service.GetChannelAffinityCacheStats()
	model.InvalidateSubscriptionPlanCache(1)
	identities := map[string]service.AuthIdentity{}
	for actor, token := range map[string]string{"user": env.User, "admin": env.Admin, "root": env.Root} {
		identity, err := service.ParseAccessToken(token)
		require.NoError(t, err)
		identities[actor] = identity
	}
	for id, pat := range map[int]string{2: "wave6c-admin-pat", 3: "wave6c-root-pat"} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id=?", id).Update("access_token", pat).Error)
	}
	if relay {
		env.MountRelay()
	}
	var result any
	synctest.Test(t, func(t *testing.T) {
		h := &Fixture{Env: env, Bearers: map[string]string{"anonymous": "", "key": "sk-wave6ckey",
			"user_pat": env.UserPAT, "admin_pat": "wave6c-admin-pat", "root_pat": "wave6c-root-pat"},
			Observations: []any{}}
		for actor, identity := range identities {
			token, _, err := service.IssueAccessToken(identity)
			require.NoError(t, err)
			h.Bearers[actor] = token
		}
		require.NoError(t, env.DB.Create(&model.Token{Id: 101, UserId: 1, Key: "wave6ckey",
			Name: "fixture", Status: 1, ExpiredTime: -1, RemainQuota: 1000000, Group: "default"}).Error)
		result = execute(t, h)
		synctest.Wait()
	})
	return result
}

// Do preserves the raw response body and every relevant response header.
func (h *Fixture) Do(t *testing.T, actor, method, path, body string, headers ...http.Header) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if language := f.Locale(); language != "default" {
		request.Header.Set("Accept-Language", language)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if bearer := h.Bearers[actor]; bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	for _, values := range headers {
		for name, items := range values {
			request.Header[name] = items
		}
	}
	response := h.Env.Serve(request)
	observed := http.Header{}
	for _, name := range []string{"Content-Type", "Cache-Control", "Retry-After", "Location", "Vary", "Content-Disposition"} {
		if values := response.Header().Values(name); len(values) > 0 {
			observed[name] = values
		}
	}
	h.Observations = append(h.Observations, map[string]any{"path": path, "status": response.Code, "headers": observed, "body": response.Body.String()})
	return response
}

func (h *Fixture) Snapshot(t *testing.T, extra map[string]any) any {
	t.Helper()
	synctest.Wait()
	state := f.State(t)
	state["observations"] = h.Observations
	for name, value := range extra {
		state[name] = value
	}
	return tf.StableRequestIDs(t, state)
}
