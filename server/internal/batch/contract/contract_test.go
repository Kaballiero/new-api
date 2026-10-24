// Package contract pins Batch HTTP, durable rows and outbound native requests before the move.
package contract

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/contracttest"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/batchfixture"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var batchManifest = flag.Bool("batch-manifest", false, "list Batch HTTP cases without fixtures")
var batchPreflight = flag.Bool("batch-preflight", false, "assert raw Batch contracts without oracle IO")
var batchOriginCheck = flag.Bool("batch-origin-check", false, "verify the pinned origin without fixtures")

func init() { taskfixture.RejectCandidateUpdate() }

type batchCase struct {
	Name, Kind, Actor, Method, Path, Variant, Body, Endpoint string
	Family, Status                                           int
}

func batchCases() []batchCase {
	cases := []batchCase{}
	for _, route := range []struct{ Name, Method, Path string }{
		{"create", "POST", "/v1/batches"}, {"list", "GET", "/v1/batches"}, {"get", "GET", "/v1/batches/own"},
		{"results", "GET", "/v1/batches/own/results"}, {"cancel", "POST", "/v1/batches/own/cancel"},
		{"delete", "DELETE", "/v1/batches/own"}, {"recover", "POST", "/api/batches/own/recover"},
		{"purchase", "POST", "/api/batches/own/purchase"},
	} {
		for _, actor := range []string{"anonymous", "key", "foreign_key", "user", "admin", "root", "user_pat", "admin_pat", "root_pat"} {
			status := 401
			if strings.HasPrefix(route.Path, "/v1/") && (actor == "key" || actor == "foreign_key") {
				status = 200
				if route.Name == "create" {
					status = 202
				}
				if actor == "foreign_key" && route.Name != "create" && route.Name != "list" {
					status = 404
				}
			} else if strings.HasPrefix(route.Path, "/api/") {
				if actor == "root" || actor == "root_pat" {
					status = 200
					if route.Name == "recover" {
						status = 409
					}
				}
				if actor == "user" || actor == "user_pat" || actor == "admin" || actor == "admin_pat" {
					status = 403
				}
			}
			cases = append(cases, batchCase{Name: "access_" + route.Name + "_" + actor, Kind: route.Name, Actor: actor,
				Method: route.Method, Path: route.Path, Status: status, Family: constant.ChannelTypeOpenAI})
		}
	}
	for _, family := range []struct {
		Name      string
		Type      int
		Endpoints []string
	}{
		{"openai", constant.ChannelTypeOpenAI, []string{"/v1/chat/completions", "/v1/responses", "/v1/embeddings"}},
		{"anthropic", constant.ChannelTypeAnthropic, []string{"/v1/messages"}},
		{"google", constant.ChannelTypeGemini, []string{"/v1beta/generateContent"}},
		{"openrouter", constant.ChannelTypeOpenRouter, []string{"/v1/chat/completions", "/v1/responses", "/v1/embeddings", "/v1/messages"}},
	} {
		for i, endpoint := range family.Endpoints {
			for _, kind := range []string{"create", "flow"} {
				cases = append(cases, batchCase{Name: fmt.Sprintf("%s_%s_%d", kind, family.Name, i), Kind: kind, Actor: "key",
					Method: "POST", Path: "/v1/batches", Family: family.Type, Endpoint: endpoint, Status: 202})
			}
		}
		for _, operation := range []string{"get", "list", "results", "cancel", "delete"} {
			method, path, status := "GET", "/v1/batches/own", 200
			switch operation {
			case "list":
				path = "/v1/batches"
			case "results":
				path += "/results"
			case "cancel":
				method = "POST"
				path += "/cancel"
				if family.Type == constant.ChannelTypeOpenRouter {
					status = 400
				}
			case "delete":
				method = "DELETE"
			}
			cases = append(cases, batchCase{Name: operation + "_" + family.Name, Kind: operation, Actor: "key", Method: method,
				Path: path, Family: family.Type, Status: status})
		}
	}
	for _, variant := range []string{"invalid_json", "missing_model", "zero_requests", "too_many", "duplicate_id", "invalid_id",
		"non_object", "duplicate_keys", "different_model", "stream", "n_zero", "n_huge", "no_cap", "cap_zero", "cap_huge",
		"wrong_cap", "bad_provider", "long_key", "oversized"} {
		cases = append(cases, batchCase{Name: "prepare_" + variant, Kind: "create", Actor: "key", Method: "POST",
			Path: "/v1/batches", Variant: variant, Family: constant.ChannelTypeOpenAI, Status: 400})
	}
	for _, variant := range []string{"snake_config", "snake_cap", "snake_count", "cap_zero", "cap_huge", "candidate_zero", "candidate_huge"} {
		cases = append(cases, batchCase{Name: "google_" + variant, Kind: "create", Actor: "key", Method: "POST",
			Path: "/v1/batches", Endpoint: "/v1beta/generateContent", Variant: "google_" + variant, Family: constant.ChannelTypeGemini, Status: 400})
	}
	for _, variant := range []struct {
		Name   string
		Status int
	}{
		{"worker_off", 503}, {"wallet_denied", 403}, {"token_denied", 403}, {"no_price", 503}, {"header_expression", 503},
		{"header_passthrough", 400}, {"override_stream", 400}, {"subscription", 202}, {"mapping", 202}, {"unlimited", 202},
		{"duplicate", 202}, {"conflict", 202}, {"create_db_error", 503}, {"lookup_db_error", 503},
	} {
		cases = append(cases, batchCase{Name: "admission_" + variant.Name, Kind: "create", Actor: "key", Method: "POST",
			Path: "/v1/batches", Variant: variant.Name, Family: constant.ChannelTypeOpenAI, Status: variant.Status})
	}
	for _, query := range []struct {
		Name, Query string
		Status      int
	}{
		{"one", "?limit=1", 200}, {"hundred", "?limit=100", 200}, {"zero", "?limit=0", 400}, {"too_large", "?limit=101", 400},
		{"invalid", "?limit=bad", 400}, {"cursor", "?limit=1&after=third", 200}, {"foreign", "?after=foreign", 400},
		{"deleted", "?after=deleted", 400}, {"missing", "?after=missing", 400}, {"ignored_owner", "?user_id=2&channel_id=999", 200},
	} {
		cases = append(cases, batchCase{Name: "list_" + query.Name, Kind: "list", Actor: "key", Method: "GET", Path: "/v1/batches" + query.Query,
			Family: constant.ChannelTypeOpenAI, Status: query.Status})
	}
	for _, operation := range []string{"get", "results", "cancel", "delete"} {
		for _, id := range []string{"foreign", "deleted", "missing"} {
			method, path := "GET", "/v1/batches/"+id
			if operation == "results" {
				path += "/results"
			}
			if operation == "cancel" {
				method = "POST"
				path += "/cancel"
			}
			if operation == "delete" {
				method = "DELETE"
			}
			cases = append(cases, batchCase{Name: operation + "_" + id, Kind: operation, Actor: "key", Method: method, Path: path,
				Family: constant.ChannelTypeOpenAI, Status: 404})
		}
	}
	for _, variant := range []struct {
		Kind, Name string
		Status     int
	}{
		{"get", "db_error", 404}, {"get", "failed", 200}, {"get", "failed_creation", 200}, {"list", "db_error", 503},
		{"results", "pending", 409}, {"results", "partial", 200}, {"results", "expired", 410}, {"results", "equality", 410},
		{"cancel", "queued", 409}, {"cancel", "unknown", 409}, {"cancel", "submitted", 202},
		{"delete", "running", 409}, {"delete", "billing_pending", 409}, {"delete", "purchase_pending", 409},
	} {
		method, path := "GET", "/v1/batches/own"
		if variant.Kind == "list" {
			path = "/v1/batches"
		}
		if variant.Kind == "results" {
			path += "/results"
		}
		if variant.Kind == "cancel" {
			method = "POST"
			path += "/cancel"
		}
		if variant.Kind == "delete" {
			method = "DELETE"
		}
		cases = append(cases, batchCase{Name: variant.Kind + "_" + variant.Name, Kind: variant.Kind, Actor: "key", Method: method, Path: path,
			Variant: variant.Name, Family: constant.ChannelTypeOpenAI, Status: variant.Status})
	}
	for _, variant := range []struct {
		Name, Body string
		Status     int
	}{
		{"invalid", `{`, 400}, {"negative", `{"cost_usd":"-1","evidence":"invoice"}`, 400},
		{"exponent", `{"cost_usd":"1e-13","evidence":"invoice"}`, 400}, {"evidence", `{"cost_usd":"0","evidence":" "}`, 400},
		{"zero", `{"cost_usd":"0","evidence":"invoice"}`, 200}, {"repeat", `{"cost_usd":"0.01","evidence":"invoice"}`, 200},
	} {
		cases = append(cases, batchCase{Name: "purchase_" + variant.Name, Kind: "purchase", Actor: "root", Method: "POST",
			Path: "/api/batches/own/purchase", Family: constant.ChannelTypeOpenAI, Body: variant.Body, Variant: variant.Name, Status: variant.Status})
	}
	for _, path := range []string{"/v1/batches/own/files", "/v1/files", "/api/batches", "/api/batch"} {
		status := 404
		if path == "/v1/files" {
			status = 400
		} // Existing placeholder goes through Distribute before NotImplemented.
		cases = append(cases, batchCase{Name: fmt.Sprintf("absent_route_%d", len(cases)), Kind: "absent", Actor: "key", Method: "GET",
			Path: path, Family: constant.ChannelTypeOpenAI, Status: status})
	}
	for _, variant := range []string{"provider_only", "provider_override"} {
		cases = append(cases, batchCase{Name: "flow_openrouter_" + variant, Kind: "flow", Actor: "key", Method: "POST", Path: "/v1/batches",
			Family: constant.ChannelTypeOpenRouter, Variant: variant, Status: 202})
	}
	for _, route := range []struct {
		Name, Method, Path string
		Status             int
	}{
		{"slash", "GET", "/v1/batches/", 301}, {"head", "HEAD", "/v1/batches/own", 404}, {"wrong_method", "PUT", "/v1/batches/own", 404},
	} {
		cases = append(cases, batchCase{Name: "route_" + route.Name, Kind: "route", Actor: "key", Method: route.Method, Path: route.Path,
			Family: constant.ChannelTypeOpenAI, Status: route.Status})
	}
	for _, variant := range []struct {
		Name, Body string
		Status     int
	}{
		{"invalid", `{`, 400}, {"empty", `{"upstream_id":""}`, 400}, {"busy", `{"upstream_id":"native_1"}`, 409},
	} {
		cases = append(cases, batchCase{Name: "recover_" + variant.Name, Kind: "recover", Actor: "root", Method: "POST", Path: "/api/batches/own/recover",
			Family: constant.ChannelTypeOpenAI, Body: variant.Body, Variant: variant.Name, Status: variant.Status})
	}
	return cases
}

func TestBatchHTTPContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	if *batchOriginCheck {
		taskfixture.CheckRecording(t)
		return
	}
	if updating {
		taskfixture.CheckRecording(t)
	}
	cases := batchCases()
	names := make([]string, len(cases))
	seen := map[string]bool{}
	for i, tc := range cases {
		require.False(t, seen[tc.Name], tc.Name)
		seen[tc.Name] = true
		names[i] = tc.Name
	}
	if *batchManifest {
		data, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("BATCH_MANIFEST " + string(data))
		return
	}
	if target := os.Getenv("BATCH_CONTRACT_LOCALES"); target != "" && os.Getenv("BATCH_CONTRACT_CASE") == "" {
		batchLocales(t, target)
		return
	}
	fixture.Processes(t, "TestBatchHTTPContract", "batch", names, *batchPreflight, updating, func(name string) {
		for _, tc := range cases {
			if tc.Name == name {
				runBatchHTTP(t, tc, updating)
				return
			}
		}
	})
}

func batchBody(t *testing.T, tc batchCase) string {
	t.Helper()
	if tc.Body != "" {
		return tc.Body
	}
	endpoint := tc.Endpoint
	if endpoint == "" {
		endpoint = "/v1/chat/completions"
	}
	body := `{"messages":[],"max_tokens":8,"temperature":0,"stream":false}`
	switch endpoint {
	case "/v1/responses":
		body = `{"input":"fixture","max_output_tokens":8,"temperature":0}`
	case "/v1/embeddings":
		body = `{"input":"fixture","dimensions":0}`
	case "/v1/messages":
		body = `{"messages":[],"max_tokens":8,"temperature":0}`
	case "/v1beta/generateContent":
		body = `{"contents":[],"generationConfig":{"maxOutputTokens":8,"temperature":0}}`
	}
	switch tc.Variant {
	case "invalid_json":
		return `{`
	case "non_object":
		body = `[]`
	case "duplicate_keys":
		body = `{"max_tokens":8,"max_tokens":4}`
	case "different_model":
		body = `{"model":"foreign","max_tokens":8}`
	case "stream":
		body = `{"stream":true,"max_tokens":8}`
	case "n_zero":
		body = `{"n":0,"max_tokens":8}`
	case "n_huge":
		body = `{"n":18446744073686646784,"max_tokens":8}`
	case "no_cap":
		body = `{}`
	case "cap_zero":
		body = `{"max_tokens":0}`
	case "cap_huge":
		body = `{"max_tokens":18446744073686646784}`
	case "wrong_cap":
		body = `{"max_output_tokens":8}`
	case "google_snake_config":
		body = `{"generation_config":{"maxOutputTokens":8}}`
	case "google_snake_cap":
		body = `{"generationConfig":{"max_output_tokens":8}}`
	case "google_snake_count":
		body = `{"generationConfig":{"maxOutputTokens":8,"candidate_count":1}}`
	case "google_cap_zero":
		body = `{"generationConfig":{"maxOutputTokens":0}}`
	case "google_cap_huge":
		body = `{"generationConfig":{"maxOutputTokens":18446744073686646784}}`
	case "google_candidate_zero":
		body = `{"generationConfig":{"maxOutputTokens":8,"candidateCount":0}}`
	case "google_candidate_huge":
		body = `{"generationConfig":{"maxOutputTokens":8,"candidateCount":18446744073686646784}}`
	}
	requests := []map[string]any{{"custom_id": "one", "body": json.RawMessage(body)}}
	if tc.Variant == "zero_requests" {
		requests = []map[string]any{}
	}
	if tc.Variant == "duplicate_id" {
		requests = append(requests, requests[0])
	}
	if tc.Variant == "invalid_id" {
		requests[0]["custom_id"] = "bad/id"
	}
	if tc.Variant == "too_many" {
		for range 1000 {
			requests = append(requests, requests[0])
		}
	}
	modelName := fixture.Model
	if tc.Variant == "missing_model" {
		modelName = ""
	}
	envelope := map[string]any{"model": modelName, "endpoint": endpoint, "requests": requests}
	if tc.Variant == "provider_only" || tc.Variant == "provider_override" {
		envelope["provider"] = map[string]any{"only": []string{"cheap"}, "allow_fallbacks": false}
	}
	if tc.Variant == "bad_provider" {
		envelope["provider"] = true
	}
	if tc.Variant == "oversized" {
		envelope["padding"] = strings.Repeat("x", 16<<20)
	}
	data, err := common.Marshal(envelope)
	require.NoError(t, err)
	return string(data)
}

func runBatchHTTP(t *testing.T, tc batchCase, updating bool) {
	taskfixture.InstallDBClock(t)
	env := contracttest.New(t)
	taskfixture.Configure()
	fixture.SplitLogs(t)
	fixture.Migrate(t)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"group_ratio_setting.group_ratio": `{"default":1}`, "group_ratio_setting.group_group_ratio": `{}`,
		"channel_affinity_setting.enabled": "false"}))
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	settings.BatchBillingExpr = map[string]string{fixture.Model: `tier("batch", p + c * 2)`}
	common.CryptoSecret = "batch-contract-test-secret"
	common.RetryTimes = 0
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{})
	setting.ModelRequestRateLimitEnabled = false
	constant.UpdateTask = true
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	identities := map[string]service.AuthIdentity{}
	for actor, token := range map[string]string{"user": env.User, "admin": env.Admin, "root": env.Root} {
		identity, err := service.ParseAccessToken(token)
		require.NoError(t, err)
		identities[actor] = identity
	}
	for id, pat := range map[int]string{2: "batch-admin-pat", 3: "batch-root-pat"} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id=?", id).Update("access_token", pat).Error)
	}
	env.MountRelay()
	service.InitHttpClient()
	provider := fixture.NewProvider(t, tc.Family, "")
	service.GetHttpClient().Transport = &provider.Transport
	synctest.Test(t, func(t *testing.T) {
		require.Equal(t, taskfixture.Epoch, time.Now().Unix())
		taskfixture.AssertDBClock(t)
		bearers := map[string]string{"key": "sk-" + fixture.Key, "foreign_key": "sk-batchforeignfixturekey", "user_pat": env.UserPAT,
			"admin_pat": "batch-admin-pat", "root_pat": "batch-root-pat"}
		for actor, identity := range identities {
			token, _, err := service.IssueAccessToken(identity)
			require.NoError(t, err)
			bearers[actor] = token
		}
		channel := fixture.Seed(t, tc.Family)
		require.NoError(t, env.DB.Create(&model.Token{Id: 12, UserId: 2, Key: "batchforeignfixturekey", Name: "foreign",
			Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000000, Group: "default"}).Error)
		require.NoError(t, env.DB.Create(&model.ModelCostFXRow{Source: "cbr-xml-daily.ru", EffectiveAt: taskfixture.Epoch,
			FetchedAt: taskfixture.Epoch, Version: 1, RatesJSON: `{"USD":100}`}).Error)
		require.NoError(t, model.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
		for i, id := range []string{"own", "second", "third", "foreign", "deleted"} {
			user := 1
			if id == "foreign" {
				user = 2
			}
			job := model.BatchJob{ID: int64(i + 1), PublicID: id, UserID: user, TokenID: 11, IdempotencyKey: id, ChannelID: 71, ChannelType: tc.Family,
				Model: fixture.Model, Endpoint: "/v1/chat/completions", Status: "completed", BillingStatus: "settled", PurchaseStatus: "settled",
				SubmitState: "submitted", UpstreamID: "native_1", CreatedAt: taskfixture.Epoch, UpdatedAt: taskfixture.Epoch, ExpiresAt: taskfixture.Epoch + 3600}
			if id == "deleted" {
				job.DeletedAt = taskfixture.Epoch
			}
			require.NoError(t, env.DB.Create(&job).Error)
		}
		updates := map[string]any{}
		switch tc.Variant {
		case "worker_off":
			constant.UpdateTask = false
		case "wallet_denied":
			require.NoError(t, env.DB.Model(&model.User{}).Where("id=1").Update("quota", 0).Error)
		case "token_denied":
			require.NoError(t, env.DB.Model(&model.Token{}).Where("id=11").Update("remain_quota", 1).Error)
		case "no_price":
			settings.BatchBillingExpr = map[string]string{}
		case "header_expression":
			settings.BatchBillingExpr[fixture.Model] = `tier("x", header("x") == "y" ? 1 : 2)`
		case "header_passthrough":
			require.NoError(t, env.DB.Model(channel).Update("header_override", `{"*":"*"}`).Error)
		case "override_stream":
			require.NoError(t, env.DB.Model(channel).Update("param_override", `{"stream":true}`).Error)
		case "mapping":
			require.NoError(t, env.DB.Model(channel).Update("model_mapping", `{"test/model":"upstream-model"}`).Error)
		case "provider_override":
			require.NoError(t, env.DB.Model(channel).Update("param_override",
				`{"operations":[{"mode":"set","path":"provider","value":{"only":["expensive"],"allow_fallbacks":false},`+
					`"conditions":[{"path":"endpoint","mode":"full","value":"/v1/chat/completions"}]}]}`).Error)
		case "busy":
			updates["lease_until"] = taskfixture.Epoch + 120
		case "unlimited":
			require.NoError(t, env.DB.Model(&model.Token{}).Where("id=11").Updates(map[string]any{"unlimited_quota": true, "remain_quota": 0}).Error)
		case "subscription":
			require.NoError(t, env.DB.Create(&model.UserSubscription{Id: 21, UserId: 1, AmountTotal: 5000, AmountUsed: 7, Status: "active",
				StartTime: taskfixture.Epoch - 1, EndTime: taskfixture.Epoch + 3600}).Error)
			require.NoError(t, env.DB.Model(&model.User{}).Where("id=1").Update("setting", `{"billing_preference":"subscription_only"}`).Error)
		case "failed":
			updates["status"] = "failed"
		case "failed_creation":
			updates["status"] = "failed"
			updates["last_error"] = `{"version":1,"phase":"create","http_status":400}`
		case "pending":
			updates["status"] = "in_progress"
		case "expired", "equality":
			updates["expires_at"] = taskfixture.Epoch
		case "queued":
			updates["status"] = "in_progress"
			updates["submit_state"] = "queued"
		case "unknown":
			updates["status"] = "in_progress"
			updates["submit_state"] = "submission_unknown"
		case "submitted", "running":
			updates["status"] = "in_progress"
		case "billing_pending":
			updates["billing_status"] = "reserved"
		case "purchase_pending":
			updates["purchase_status"] = "reconciliation_required"
		case "partial":
			updates["status"] = "in_progress"
			updates["result_count"] = 1
			require.NoError(t, env.DB.Create(&model.BatchItem{BatchID: 1, CustomID: "client_seed", ProviderCustomID: "private",
				CustomIDHash: "hash", Result: `{"custom_id":"client_seed","body":{"text":"fixture"}}`, ResultHash: "result"}).Error)
		}
		if tc.Kind == "purchase" {
			updates["purchase_status"] = "reconciliation_required"
		}
		if len(updates) > 0 {
			require.NoError(t, env.DB.Model(&model.BatchJob{}).Where("id=1").Updates(updates).Error)
		}
		if tc.Family == constant.ChannelTypeGemini {
			require.NoError(t, env.DB.Model(channel).Update("model_mapping", `{"test/model":"gemini-test"}`).Error)
		}
		fault := ""
		if tc.Variant == "db_error" || tc.Variant == "lookup_db_error" {
			fault = "batch_http_query_fault"
			require.NoError(t, env.DB.Callback().Query().Before("gorm:query").Register(fault, func(tx *gorm.DB) {
				if tx.Statement.Table == "batch_jobs" {
					tx.AddError(errors.New("fixture batch query failure"))
				}
			}))
		}
		if tc.Variant == "create_db_error" {
			fault = "batch_http_insert_fault"
			require.NoError(t, env.DB.Callback().Create().Before("gorm:create").Register(fault, func(tx *gorm.DB) {
				if tx.Statement.Table == "batch_jobs" {
					tx.AddError(errors.New("fixture batch insert failure"))
				}
			}))
		}
		body := ""
		if tc.Kind == "create" || tc.Kind == "flow" {
			body = batchBody(t, tc)
		}
		if tc.Kind == "purchase" {
			body = tc.Body
			if body == "" {
				body = `{"cost_usd":"0.01","evidence":"invoice"}`
			}
		}
		if tc.Kind == "recover" {
			body = tc.Body
			if body == "" {
				body = `{"upstream_id":"native_1"}`
			}
		}
		request := httptest.NewRequest(tc.Method, tc.Path, strings.NewReader(body))
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		language := os.Getenv("BATCH_CONTRACT_LANGUAGE")
		if language == "" {
			language = "zh-CN"
		}
		if language != "default" {
			request.Header.Set("Accept-Language", language)
		}
		if bearer := bearers[tc.Actor]; bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		if tc.Variant == "long_key" {
			request.Header.Set("Idempotency-Key", strings.Repeat("k", 257))
		}
		if tc.Variant == "duplicate" || tc.Variant == "conflict" {
			request.Header.Set("Idempotency-Key", "repeat-key")
		}
		response := env.Serve(request)
		synctest.Wait()
		require.Equal(t, tc.Status, response.Code, response.Body.String())
		observations := []any{map[string]any{"status": response.Code, "body": response.Body.String(), "headers": response.Header(), "state": nil}}
		if fault != "" {
			require.NoError(t, env.DB.Callback().Query().Remove(fault))
			require.NoError(t, env.DB.Callback().Create().Remove(fault))
		}
		observations[0].(map[string]any)["state"] = fixture.State(t)
		if response.Code == 202 && (tc.Kind == "create" || tc.Kind == "flow") {
			var job model.BatchJob
			require.NoError(t, env.DB.Order("id desc").First(&job).Error)
			var user model.User
			require.NoError(t, env.DB.First(&user, job.UserID).Error)
			assert.Equal(t, 1000000, user.Quota, "admission holds money without debiting it")
			assert.Equal(t, job.ReservedQuota, user.BatchHeldQuota)
			assert.Equal(t, "queued", job.SubmitState)
			if tc.Variant == "provider_only" || tc.Variant == "provider_override" {
				var frozen struct {
					Provider json.RawMessage `json:"provider"`
				}
				require.NoError(t, common.UnmarshalJsonStr(job.BillingSnapshot, &frozen))
				name := "cheap"
				if tc.Variant == "provider_override" {
					name = "expensive"
				}
				require.JSONEq(t, fmt.Sprintf(`{"only":[%q],"allow_fallbacks":false}`, name), string(frozen.Provider))
			}
			if tc.Variant == "subscription" {
				var sub model.UserSubscription
				require.NoError(t, env.DB.First(&sub, 21).Error)
				assert.EqualValues(t, 7, sub.AmountUsed)
			}
		}
		if tc.Kind == "flow" {
			for range 2 {
				require.NoError(t, service.RunBatchPollingOnce(context.Background(), "contract-worker"))
				observations = append(observations, map[string]any{"state": fixture.State(t)})
			}
			require.Equal(t, 1, provider.PaidPosts)
			var job model.BatchJob
			require.NoError(t, env.DB.Order("id desc").First(&job).Error)
			assert.Equal(t, "settled", job.BillingStatus)
			assert.Zero(t, job.ReservedQuota)
			resultRequest := httptest.NewRequest("GET", "/v1/batches/"+job.PublicID+"/results", nil)
			resultRequest.Header = request.Header.Clone()
			result := env.Serve(resultRequest)
			require.Equal(t, 200, result.Code)
			observations = append(observations, map[string]any{"status": result.Code, "body": result.Body.String(), "headers": result.Header()})
		}
		if tc.Variant == "duplicate" || tc.Variant == "conflict" || tc.Variant == "repeat" {
			if tc.Variant == "conflict" {
				body = strings.ReplaceAll(body, `"temperature":0`, `"temperature":1`)
			}
			repeat := httptest.NewRequest(tc.Method, tc.Path, strings.NewReader(body))
			repeat.Header = request.Header.Clone()
			result := env.Serve(repeat)
			synctest.Wait()
			status := 200
			if tc.Variant == "conflict" {
				status = 409
			}
			require.Equal(t, status, result.Code, result.Body.String())
			observations = append(observations, map[string]any{"status": result.Code, "body": result.Body.String(), "headers": result.Header(),
				"state": fixture.State(t)})
		}
		if tc.Kind == "get" || tc.Kind == "list" {
			for _, secret := range []string{fixture.ProviderKey, "credential_hash", "channel_id", "upstream_id"} {
				assert.NotContains(t, response.Body.String(), secret)
			}
		}
		if tc.Kind != "flow" {
			require.Zero(t, provider.PaidPosts, "create HTTP must not submit asynchronously")
		}
		value := fixture.Stable(t, map[string]any{"observations": observations, "outbound": provider.Transport.Calls},
			tc.Variant != "duplicate" && tc.Variant != "conflict")
		taskfixture.Compare(t, "testdata/golden", tc.Name, value, *batchPreflight, updating)
	})
}

func batchLocales(t *testing.T, target string) {
	names := []string{"access_get_anonymous", "access_purchase_user", "prepare_stream", "admission_no_price", "get_failed_creation"}
	values := map[string]json.RawMessage{}
	for _, language := range []string{"default", "en", "ru", "zh-CN", "zh-TW", "unknown"} {
		for _, name := range names {
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestBatchHTTPContract$", "-test.count=1", "-test.timeout=120s",
				"-batch-preflight")
			command.Env = append(os.Environ(), "BATCH_CONTRACT_CASE="+name, "BATCH_CONTRACT_LANGUAGE="+language, "BATCH_CONTRACT_LOCALES=")
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			found := false
			for line := range strings.SplitSeq(string(output), "\n") {
				if raw, ok := strings.CutPrefix(line, "TASKS_SNAPSHOT "+name+" "); ok {
					require.False(t, found)
					found = true
					values[language+"/"+name] = json.RawMessage(raw)
				}
			}
			require.True(t, found)
		}
	}
	data, err := common.Marshal(values)
	require.NoError(t, err)
	data, err = common.IndentJson(data)
	require.NoError(t, err)
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	require.NoError(t, err)
	_, err = file.Write(append(data, '\n'))
	require.NoError(t, err)
	require.NoError(t, file.Close())
}
