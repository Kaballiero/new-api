package contract

import (
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"

	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"

	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	f "github.com/QuantumNous/new-api/internal/contracttest/managementfixture"
	h "github.com/QuantumNous/new-api/internal/contracttest/managementhttp"
	tf "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const providerPath = "/api/custom-oauth-provider"
const providerBody = `{"name":"Fixture","slug":"fixture-new","enabled":true,"client_id":"fixture-client",` +
	`"client_secret":"fixture-secret","authorization_endpoint":"https://issuer.invalid/authorize",` +
	`"token_endpoint":"https://issuer.invalid/token","user_info_endpoint":"https://issuer.invalid/userinfo"}`

type providerCase struct {
	Name, Method, Path, Body, Actor, Variant string
	Calls                                    int
}

func providerCases(t *testing.T) []providerCase {
	t.Helper()
	routes := []providerCase{
		{Name: "list", Method: "GET", Path: "/"},
		{Name: "get", Method: "GET", Path: "/11"},
		{Name: "create", Method: "POST", Path: "/", Body: providerBody},
		{Name: "update", Method: "PUT", Path: "/11", Body: `{"name":"Updated"}`},
		{Name: "delete", Method: "DELETE", Path: "/11"},
		{Name: "discovery", Method: "POST", Path: "/discovery", Body: `{}`},
	}
	cases := []providerCase{}
	for _, route := range routes {
		for _, actor := range []string{"anonymous", "user", "admin", "root", "user_pat", "admin_pat", "root_pat", "key"} {
			tc := route
			tc.Name, tc.Actor = "access_"+route.Name+"_"+actor, actor
			cases = append(cases, tc)
		}
	}
	add := func(name, method, path, body, variant string, calls int) {
		cases = append(cases, providerCase{name, method, path, body, "root", variant, calls})
	}
	add("list_empty", "GET", "/", "", "empty", 0)
	add("list_db_error", "GET", "/", "", "query_error", 0)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		for _, id := range []string{"bad", "999", "0", "-1"} {
			add(strings.ToLower(method)+"_id_"+strings.ReplaceAll(id, "-", "minus"), method, "/"+id, `{}`, "", 0)
		}
		add(strings.ToLower(method)+"_lookup_error", method, "/11", `{}`, "query_error", 0)
	}
	for i, body := range []string{"{", `{}`, `null`, `{"name":42}`, strings.Replace(providerBody, `"slug":"fixture-new"`, `"slug":"bad_slug"`, 1),
		strings.Replace(providerBody, `"slug":"fixture-new"`, `"slug":"github"`, 1),
		strings.Replace(providerBody, `"slug":"fixture-new"`, `"slug":"telegram"`, 1),
		strings.Replace(providerBody, `"slug":"fixture-new"`, `"slug":"fixture-alpha"`, 1),
		strings.Replace(providerBody, `"slug":"fixture-new"`, `"slug":"FIXTURE-ALPHA"`, 1),
		strings.Replace(providerBody, `"slug":"fixture-new"`, `"slug":"FIXTURE-UPPER"`, 1)} {
		add(fmt.Sprintf("create_validation_%02d", i), "POST", "/", body, "", 0)
	}
	for i, field := range []string{"name", "slug", "client_id", "client_secret", "authorization_endpoint", "token_endpoint", "user_info_endpoint"} {
		var body map[string]any
		// All values are strings except enabled; this preserves the other required fields.
		require.NoError(t, common.UnmarshalJsonStr(providerBody, &body))
		delete(body, field)
		encoded, err := common.Marshal(body)
		require.NoError(t, err)
		add(fmt.Sprintf("create_missing_%02d", i), "POST", "/", string(encoded), "", 0)
	}
	for i, policy := range []string{`{`, `{"logic":"xor","conditions":[{"field":"role","op":"eq","value":"staff"}]}`,
		`{}`, `{"conditions":[{"op":"eq"}]}`, `{"conditions":[{"field":"role","op":"unknown"}]}`,
		`{"conditions":[{"field":"role","op":"in","value":"staff"}]}`,
		`{"groups":[{}]}`, `{"logic":"or","conditions":[{"field":"role","op":"in","value":["staff","admin"]}]}`} {
		encoded, err := common.Marshal(policy)
		require.NoError(t, err)
		body := strings.TrimSuffix(providerBody, "}") + `,"access_policy":` + string(encoded) + `}`
		add(fmt.Sprintf("create_policy_%02d", i), "POST", "/", body, "", 0)
		add(fmt.Sprintf("update_policy_%02d", i), "PUT", "/11", `{"access_policy":`+string(encoded)+`}`, "", 0)
	}
	add("create_db_error", "POST", "/", providerBody, "create_error", 0)
	add("create_slug_query_error", "POST", "/", providerBody, "query_error", 0)
	add("create_explicit_fields", "POST", "/", strings.TrimSuffix(providerBody, "}")+`,"icon":"Icon",`+
		`"scopes":"profile","user_id_field":"id","username_field":"login","display_name_field":"display",`+
		`"email_field":"mail","well_known":"https://issuer.invalid/discovery","auth_style":2,`+
		`"access_denied_message":"Fixture denied"}`, "", 0)
	for i, body := range []string{"{", `null`, `{}`, `{"enabled":"false"}`, `{"slug":"fixture-beta"}`, `{"slug":"github"}`,
		`{"slug":"telegram"}`, `{"slug":"bad_slug"}`, `{"slug":"FIXTURE-BETA"}`, `{"slug":"fixture-renamed"}`,
		`{"client_secret":""}`, `{"client_secret":"replacement-fixture-secret"}`,
		`{"name":"","client_id":"","scopes":"","authorization_endpoint":"","token_endpoint":"","user_info_endpoint":""}`,
		`{"icon":"","enabled":false,"well_known":"","auth_style":0,"access_policy":"","access_denied_message":""}`,
		`{"icon":null,"enabled":null,"well_known":null,"auth_style":null,"access_policy":null,"access_denied_message":null}`,
		`{"name":"Next","client_id":"next-client","authorization_endpoint":"https://next.invalid/a",` +
			`"token_endpoint":"https://next.invalid/t","user_info_endpoint":"https://next.invalid/u",` +
			`"scopes":"next","user_id_field":"id","username_field":"login","display_name_field":"display","email_field":"mail"}`} {
		add(fmt.Sprintf("update_fields_%02d", i), "PUT", "/11", body, "", 0)
	}
	add("update_db_error", "PUT", "/11", `{"slug":"fixture-renamed"}`, "update_error", 0)
	add("delete_bound", "DELETE", "/11", "", "bound", 0)
	add("delete_binding_error", "DELETE", "/11", "", "binding_error", 0)
	add("delete_bindings_write_error", "DELETE", "/11", "", "binding_delete_error", 0)
	add("delete_provider_write_error", "DELETE", "/11", "", "delete_error", 0)
	for i, body := range []string{"{", `null`, `{}`, `{"well_known_url":42}`, `{"well_known_url":"   ","issuer_url":" "}`,
		`{"well_known_url":"ftp://issuer.invalid/config"}`, `{"well_known_url":"/relative"}`,
		`{"well_known_url":"http://%zz"}`, `{"issuer_url":"issuer.invalid"}`} {
		add(fmt.Sprintf("discovery_validation_%02d", i), "POST", "/discovery", body, "", 0)
	}
	for _, variant := range []string{"success", "issuer", "precedence", "query", "redirect", "http_error", "empty_error", "long_error", "malformed", "null", "array", "scalar", "eof"} {
		calls := 1
		if variant == "redirect" {
			calls = 2
		}
		add("discovery_"+variant, "POST", "/discovery", "", variant, calls)
	}
	return cases
}

func TestCustomOAuthManagementContract(t *testing.T) {
	cases := providerCases(t)
	names := make([]string, len(cases))
	for i := range cases {
		names[i] = cases[i].Name
	}
	f.Run(t, "TestCustomOAuthManagementContract", "custom-oauth", "testdata/custom_oauth", names, nil,
		func(t *testing.T, name string) any {
			index := slices.IndexFunc(cases, func(tc providerCase) bool { return tc.Name == name })
			require.NotEqual(t, -1, index)
			return providerSnapshot(t, cases[index])
		})
}

func providerSnapshot(t *testing.T, tc providerCase) any {
	return h.Run(t, false, func(t *testing.T, env *h.Fixture) any {
		for i, slug := range []string{"fixture-alpha", "fixture-beta"} {
			if tc.Variant == "empty" {
				break
			}
			provider := &identityrecord.CustomOAuthProvider{Id: 11 + i, Name: slug, Slug: slug, Icon: "Fixture", Enabled: i == 0,
				ClientId: "seed-client", ClientSecret: "seed-fixture-secret", AuthorizationEndpoint: "https://seed.invalid/authorize",
				TokenEndpoint: "https://seed.invalid/token", UserInfoEndpoint: "https://seed.invalid/userinfo", WellKnown: "https://seed.invalid/discovery",
				AuthStyle: 2, AccessPolicy: `{"conditions":[{"field":"role","op":"eq","value":"staff"}]}`, AccessDeniedMessage: "Seed denied"}
			require.NoError(t, identitystorage.SQL().CreateCustomOAuthProvider(provider))
			oauth.RegisterOrUpdateCustomProvider(provider)
		}
		if tc.Variant == "bound" {
			require.NoError(t, env.Env.DB.Create(&identityrecord.UserOAuthBinding{Id: 21, UserId: 1, ProviderId: 11, ProviderUserId: "fixture-sub"}).Error)
		}
		captured := []f.Request{}
		var mu sync.Mutex
		var server *httptest.Server
		origin := ""
		if tc.Calls > 0 {
			server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Admit only this fixture's exact endpoints before recording any request.
				expectedQuery := ""
				if tc.Variant == "query" {
					expectedQuery = "key=fixture"
				}
				if r.Method != "GET" || r.URL.RawQuery != expectedQuery ||
					!slices.Contains([]string{"/config", "/redirect", "/issuer/.well-known/openid-configuration"}, r.URL.Path) {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				body, err := io.ReadAll(r.Body)
				mu.Lock()
				captured = append(captured, f.Request{URL: "http://" + r.Host + r.URL.RequestURI(), Method: r.Method, Header: r.Header.Clone(), Body: string(body)})
				mu.Unlock()
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch tc.Variant {
				case "redirect":
					if r.URL.Path == "/redirect" {
						http.Redirect(w, r, "/config", http.StatusFound)
						return
					}
				case "http_error":
					w.WriteHeader(http.StatusBadGateway)
					_, _ = io.WriteString(w, "  fixture provider failure  \n")
					return
				case "empty_error":
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				case "long_error":
					w.WriteHeader(http.StatusBadGateway)
					_, _ = io.WriteString(w, strings.Repeat("X", 600))
					return
				case "malformed", "null", "array", "scalar":
					_, _ = io.WriteString(w, map[string]string{"malformed": "{", "null": "null", "array": "[]", "scalar": "42"}[tc.Variant])
					return
				case "eof":
					hijacker, ok := w.(http.Hijacker)
					if !ok {
						return
					}
					connection, _, err := hijacker.Hijack()
					if err == nil {
						_ = connection.Close()
					}
					return
				}
				_, _ = io.WriteString(w, `{"issuer":"https://issuer.invalid","authorization_endpoint":"https://issuer.invalid/authorize",`+
					`"token_endpoint":"https://issuer.invalid/token","userinfo_endpoint":"https://issuer.invalid/userinfo","scopes_supported":["openid","email"]}`)
			}))
			server.Config.SetKeepAlivesEnabled(false)
			server.Start()
			t.Cleanup(server.Close)
			origin = server.URL
			switch tc.Variant {
			case "issuer":
				tc.Body = `{"issuer_url":" ` + origin + `/issuer/// "}`
			case "precedence":
				tc.Body = `{"well_known_url":" ` + origin + `/config ","issuer_url":"http://unused.invalid"}`
			case "query":
				tc.Body = `{"well_known_url":"` + origin + `/config?key=fixture"}`
			case "redirect":
				tc.Body = `{"well_known_url":"` + origin + `/redirect"}`
			default:
				tc.Body = `{"well_known_url":"` + origin + `/config"}`
			}
			// An unowned loopback probe must be rejected before capture.
			response, err := server.Client().Get(origin + "/unowned")
			require.NoError(t, err)
			require.Equal(t, http.StatusNotFound, response.StatusCode)
			require.NoError(t, response.Body.Close())
		}
		installProviderFault(t, env.Env.DB, tc.Variant)
		response := env.Do(t, tc.Actor, tc.Method, providerPath+tc.Path, tc.Body)
		removeProviderFault(t, env.Env.DB)
		if server != nil {
			// Stop real network I/O before managementhttp.Run waits for its synctest bubble.
			server.Close()
		}
		mu.Lock()
		outbound := slices.Clone(captured)
		mu.Unlock()
		require.Len(t, outbound, tc.Calls, "outbound requests must complete in causal order")
		for _, request := range outbound {
			assert.Equal(t, http.MethodGet, request.Method)
			assert.Equal(t, "application/json", request.Header.Get("Accept"))
			assert.Empty(t, request.Body)
			assert.Empty(t, request.Header.Get("Authorization"))
		}
		registry := map[string]any{}
		for _, slug := range []string{"fixture-alpha", "fixture-beta", "fixture-new", "fixture-renamed", "fixture-upper", "github", "telegram"} {
			entry := map[string]any{"registered": oauth.IsProviderRegistered(slug), "custom": oauth.IsCustomProvider(slug), "conflict": oauth.HasCustomProviderConflict(slug)}
			if provider, ok := oauth.GetProvider(slug).(*oauth.GenericOAuthProvider); ok {
				entry["config"], entry["fixture_secret"] = provider.GetConfig(), provider.GetConfig().ClientSecret
			}
			registry[slug] = entry
		}
		audit := tf.Rows(t, model.LOG_DB, "audit_logs", "*", "id")
		auditJSON, err := common.Marshal(audit)
		require.NoError(t, err)
		for _, secret := range []string{"fixture-secret", "seed-fixture-secret", "replacement-fixture-secret"} {
			assert.NotContains(t, response.Body.String(), secret)
			assert.NotContains(t, string(auditJSON), secret)
		}
		for _, bearer := range env.Bearers {
			if bearer != "" {
				assert.NotContains(t, string(auditJSON), bearer)
			}
		}
		snapshot := tf.StableRequestIDs(t, map[string]any{"observations": env.Observations,
			"providers": tf.Rows(t, env.Env.DB, "custom_oauth_providers", "*", "id"),
			"bindings":  tf.Rows(t, env.Env.DB, "user_oauth_bindings", "*", "id"), "registry": registry, "outbound": outbound,
			"audit": audit})
		if origin != "" {
			return f.FixturePaths(t, snapshot, origin)
		}
		return snapshot
	})
}

func installProviderFault(t *testing.T, db *gorm.DB, variant string) {
	t.Helper()
	query := func(tx *gorm.DB) {
		if (variant == "query_error" && tx.Statement.Table == "custom_oauth_providers") ||
			(variant == "binding_error" && tx.Statement.Table == "user_oauth_bindings") {
			tx.AddError(errors.New("fixture database failure"))
		}
	}
	write := func(tx *gorm.DB) {
		if tx.Statement.Table == "custom_oauth_providers" || (variant == "binding_delete_error" && tx.Statement.Table == "user_oauth_bindings") {
			tx.AddError(errors.New("fixture database failure"))
		}
	}
	switch variant {
	case "query_error", "binding_error":
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fixture:oauth", query))
	case "create_error":
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fixture:oauth", write))
	case "update_error":
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fixture:oauth", write))
	case "delete_error", "binding_delete_error":
		require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("fixture:oauth", write))
	}
}

func removeProviderFault(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Callback().Query().Remove("fixture:oauth"))
	require.NoError(t, db.Callback().Create().Remove("fixture:oauth"))
	require.NoError(t, db.Callback().Update().Remove("fixture:oauth"))
	require.NoError(t, db.Callback().Delete().Remove("fixture:oauth"))
}
