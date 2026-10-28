package batchtickfixture

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	systemstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/system"
	systemrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"
	account "github.com/QuantumNous/new-api/internal/adapter/http/batchaccount"
	core "github.com/QuantumNous/new-api/internal/app/batch"
	runtime "github.com/QuantumNous/new-api/internal/app/batchruntime"
	"github.com/QuantumNous/new-api/internal/app/httpapi"
	. "github.com/QuantumNous/new-api/internal/app/system"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/batchfixture"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	. "github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relay/batch"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var batchTickManifest = flag.Bool("batch-tick-manifest", false, "list portable Batch accounting/tick cases")
var batchTickPreflight = flag.Bool("batch-tick-preflight", false, "assert Batch accounting without oracle IO")

func init() {
	taskfixture.RegisterUpdate()
	taskfixture.RejectCandidateUpdate()
}

type Case struct {
	Name, Kind, Variant, Endpoint, Mode string
	Family                              int
}

func Cases() []Case {
	cases := []Case{}
	for _, provider := range []struct {
		Name     string
		Family   int
		Endpoint string
	}{
		{"openai", constant.ChannelTypeOpenAI, "/v1/chat/completions"},
		{"anthropic", constant.ChannelTypeAnthropic, "/v1/messages"},
		{"google", constant.ChannelTypeGemini, "/v1beta/generateContent"},
		{"openrouter", constant.ChannelTypeOpenRouter, "/v1/chat/completions"},
	} {
		variants := []string{"success", "in_progress", "transport_error", "ambiguous", "reject_400", "reject_401", "reject_403", "reject_404",
			"reject_413", "reject_422", "reject_429", "reject_500", "poll_error", "poll_bad_json", "foreign_job", "foreign_item", "bad_create_json",
			"missing_usage", "partial", "revoked", "deleted", "expired_token", "unsafe_hold", "config_changed", "key_changed", "cleanup",
			"cleanup_again", "expiry", "cancel", "recover", "recover_foreign", "recover_no_ids"}
		if provider.Family == constant.ChannelTypeOpenAI {
			variants = append(variants, "upload_error", "download_error", "unbilled", "malformed_suffix", "id_then_bad_json")
		}
		if provider.Family == constant.ChannelTypeAnthropic {
			variants = append(variants, "unbilled", "download_error", "malformed_suffix")
		}
		if provider.Family == constant.ChannelTypeGemini {
			variants = append(variants, "file", "download_error")
		}
		if provider.Family == constant.ChannelTypeOpenRouter {
			variants = append(variants, "withheld", "zero_cost", "missing_cost", "null_cost", "negative_cost", "bad_counts", "failed", "expired",
				"cancelled", "finalizing", "cancelling")
		}
		for _, variant := range variants {
			mode := ""
			if provider.Family == constant.ChannelTypeOpenRouter {
				mode = domain.BatchSettlementOpenRouterJobCostV2
			}
			cases = append(cases, Case{Name: "tick_" + provider.Name + "_" + variant, Kind: "tick", Family: provider.Family,
				Endpoint: provider.Endpoint, Mode: mode, Variant: variant})
		}
	}
	for _, endpoint := range []struct{ Name, Path string }{
		{"chat", "/v1/chat/completions"}, {"responses", "/v1/responses"}, {"messages", "/v1/messages"},
		{"google", "/v1beta/generateContent"}, {"embedding", "/v1/embeddings"},
	} {
		for _, variant := range []string{"success", "zero", "missing", "null", "negative", "fractional", "too_large", "non2xx", "unbilled",
			"tools", "no_purchase", "clamp", "frozen_clock"} {
			cases = append(cases, Case{Name: "settle_" + endpoint.Name + "_" + variant, Kind: "settle", Family: constant.ChannelTypeOpenAI,
				Endpoint: endpoint.Path, Variant: variant})
		}
	}
	for _, variant := range []string{"positive", "zero", "missing", "null", "negative", "string", "bad_basis", "nonterminal", "unconfirmed",
		"changed_receipt", "prior_item", "log_disabled", "overflow"} {
		cases = append(cases, Case{Name: "aggregate_" + variant, Kind: "aggregate", Family: constant.ChannelTypeOpenRouter,
			Endpoint: "/v1/chat/completions", Mode: domain.BatchSettlementOpenRouterJobCostV2, Variant: variant})
	}
	for _, variant := range []string{"positive", "zero", "missing", "null", "negative", "non2xx", "invalid_tokens", "unknown_mode"} {
		cases = append(cases, Case{Name: "legacy_cost_" + variant, Kind: "legacy", Family: constant.ChannelTypeOpenRouter,
			Endpoint: "/v1/chat/completions", Mode: domain.BatchSettlementOpenRouterCostV1, Variant: variant})
	}
	for _, kind := range []string{"item_fault", "aggregate_fault"} {
		for _, table := range []string{"users", "tokens", "channels", "batch_items", "batch_jobs", "batch_log_events", "batch_quota_events"} {
			mode := ""
			if kind == "aggregate_fault" {
				mode = domain.BatchSettlementOpenRouterJobCostV2
			}
			cases = append(cases, Case{Name: kind + "_" + table, Kind: kind, Family: constant.ChannelTypeOpenRouter,
				Endpoint: "/v1/chat/completions", Mode: mode, Variant: table})
		}
	}
	for _, variant := range []string{"same", "different_hash", "foreign", "number_precision", "same_receipt", "different_receipt",
		"held_release", "log_retry", "log_disabled", "purchase_repeat", "purchase_conflict", "lease_equal", "lease_busy", "cancelled_context",
		"registration", "scheduler_off", "scheduler_due", "master_once", "handler_history", "handler_cancelled"} {
		cases = append(cases, Case{Name: "durable_" + variant, Kind: "durable", Family: constant.ChannelTypeOpenAI,
			Endpoint: "/v1/chat/completions", Variant: variant})
	}
	for _, variant := range []string{"confirmed", "pending", "unsafe_token", "insufficient", "auth_version", "unsafe_user_hold",
		"redis_error", "event_ok", "event_gap", "event_deleted", "event_bad_owner"} {
		cases = append(cases, Case{Name: "cache_" + variant, Kind: "cache", Family: constant.ChannelTypeOpenAI,
			Endpoint: "/v1/chat/completions", Variant: variant})
	}
	return cases
}

type Hooks struct {
	RecordResult      func(*domain.Job, domain.Result) error
	SettleResults     func(*domain.Job) error
	ReconcilePurchase func(*domain.Job) error
	Verify            func(context.Context, *domain.Job, string, string) error
	Handlers          func() []SystemTaskHandler
	Registry          func(*testing.T, SystemTaskHandler)
	Schedule          func()
}
type harness struct {
	Hooks
	directory string
}

// Owner keeps each private operation in its owning test binary.
func Owner(c Case) string {
	if c.Kind == "tick" && strings.HasPrefix(c.Variant, "recover") {
		return "recovery"
	}
	switch c.Variant {
	case "registration", "scheduler_off", "scheduler_due", "master_once", "handler_history", "handler_cancelled":
		if c.Kind == "durable" {
			return "scheduler"
		}
	}
	return "poll"
}

// Run executes the unchanged characterization cases for one operation owner.
func Run(t *testing.T, owner, directory string, hooks Hooks) {
	updating := flag.Lookup("update").Value.String() == "true"
	if updating {
		taskfixture.CheckRecording(t)
	}
	cases := []Case{}
	for _, c := range Cases() {
		if Owner(c) == owner {
			cases = append(cases, c)
		}
	}
	h := harness{Hooks: hooks, directory: directory}
	names := make([]string, len(cases))
	seen := map[string]bool{}
	for i, tc := range cases {
		require.False(t, seen[tc.Name], tc.Name)
		seen[tc.Name] = true
		names[i] = tc.Name
	}
	if *batchTickManifest {
		data, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("BATCH_TICK_MANIFEST " + string(data))
		return
	}
	fixture.Processes(t, "TestBatchTickContract", "batch-tick", names, *batchTickPreflight, updating, func(name string) {
		for _, tc := range cases {
			if tc.Name == name {
				h.runBatchTickContract(t, tc, updating)
				return
			}
		}
	})
}

func (h harness) runBatchTickContract(t *testing.T, tc Case, updating bool) {
	taskfixture.Open(t)
	fixture.SplitLogs(t)
	fixture.Migrate(t)
	ginClient := httpclient.GetHttpClient()
	if ginClient == nil {
		httpclient.InitHttpClient()
	}
	provider := fixture.NewProvider(t, tc.Family, tc.Variant)
	client := httpclient.GetHttpClient()
	previous := client.Transport
	client.Transport = &provider.Transport
	t.Cleanup(func() { client.Transport = previous })
	synctest.Test(t, func(t *testing.T) {
		common.CryptoSecret = "batch-contract-test-secret"
		require.Equal(t, taskfixture.Epoch, time.Now().Unix())
		taskfixture.AssertDBClock(t)
		channel := fixture.Seed(t, tc.Family)
		configHash, err := account.BatchChannelConfigHash(channel)
		require.NoError(t, err)
		job := fixture.Reserve(t, core.NewStore().CreateBatchReservation, channel, configHash, tc.Endpoint, tc.Mode, 2)
		if tc.Family == constant.ChannelTypeGemini {
			require.NoError(t, model.DB.Model(job).Update("upstream_model", "gemini-test").Error)
			job.UpstreamModel = "gemini-test"
		}
		observations := []any{fixture.State(t)}
		errorsSeen := []string{}
		switch tc.Kind {
		case "tick":
			h.runBatchLifecycle(t, tc, job, provider, &observations, &errorsSeen)
		case "settle", "legacy":
			h.runBatchItemSettlement(t, tc, job, &observations, &errorsSeen)
		case "aggregate":
			h.runBatchAggregate(t, tc, job, &observations, &errorsSeen)
		case "item_fault", "aggregate_fault":
			runBatchMoneyFault(t, tc, job, &observations, &errorsSeen)
		case "durable":
			h.runBatchDurable(t, tc, job, &observations, &errorsSeen)
		case "cache":
			probe := fixture.NewRedisReplies(t, tc.Variant)
			if strings.HasPrefix(tc.Variant, "event_") {
				require.NoError(t, model.DB.Model(&model.BatchQuotaEvent{}).Where("batch_id=?", job.ID).Update("applied", false).Error)
				if tc.Variant == "event_deleted" {
					require.NoError(t, model.DB.Delete(&model.Token{}, 11).Error)
				}
				if tc.Variant == "event_bad_owner" {
					require.NoError(t, model.DB.Model(&model.BatchQuotaEvent{}).Where("owner_kind=?", "user").Update("owner_kind", "bad").Error)
				}
				_ = batchObservation(t, func() error { return core.NewStore().SyncBatchQuotaEvents(1000) }, &observations, &errorsSeen)
			} else {
				err := batchObservation(t, func() error { return core.NewStore().ConfirmBatchReservation((*domain.Job)(job)) }, &observations, &errorsSeen)
				if tc.Variant == "confirmed" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
			observations = append(observations, map[string]any{"redis_commands": probe.Calls,
				"script_execution": "scripted return codes; real Lua remains a host check"})
		default:
			t.Fatal("unknown contract kind", tc.Kind)
		}
		value := fixture.Stable(t, map[string]any{"states": observations, "errors": errorsSeen, "outbound": provider.Transport.Calls}, false)
		taskfixture.Compare(t, h.directory, tc.Name, value, *batchTickPreflight, updating)
	})
}

func batchObservation(t *testing.T, operation func() error, states *[]any, failures *[]string) error {
	t.Helper()
	err := operation()
	text := ""
	if err != nil {
		text = err.Error()
	}
	*failures = append(*failures, text)
	*states = append(*states, fixture.State(t))
	return err
}

func (h harness) runBatchLifecycle(t *testing.T, tc Case, job *model.BatchJob, provider *fixture.Provider, states *[]any, failures *[]string) {
	updates := map[string]any{}
	switch tc.Variant {
	case "revoked":
		require.NoError(t, model.DB.Model(&model.Token{}).Where("id=11").Update("status", common.TokenStatusDisabled).Error)
	case "deleted":
		require.NoError(t, model.DB.Delete(&model.Token{}, 11).Error)
	case "expired_token":
		require.NoError(t, model.DB.Model(&model.Token{}).Where("id=11").Update("expired_time", taskfixture.Epoch-1).Error)
	case "unsafe_hold":
		require.NoError(t, model.DB.Model(&model.User{}).Where("id=1").Update("batch_held_quota", 0).Error)
	case "config_changed":
		require.NoError(t, model.DB.Model(&model.Channel{}).Where("id=71").Update("base_url", "https://changed.invalid").Error)
	case "key_changed":
		require.NoError(t, model.DB.Model(&model.Channel{}).Where("id=71").Update("key", "changed-fixture-key").Error)
	case "cleanup", "cleanup_again", "expiry":
		updates = map[string]any{"status": "completed", "billing_status": "settled", "purchase_status": "settled", "submit_state": "submitted",
			"upstream_id": "native_1"}
		if tc.Variant == "expiry" {
			updates["expires_at"] = taskfixture.Epoch
		} else {
			updates["deleted_at"] = taskfixture.Epoch
		}
		if tc.Variant == "cleanup_again" {
			updates["cleanup_done"] = true
		}
	case "cancel":
		updates = map[string]any{"submit_state": "cancel_requested", "upstream_id": "native_1", "status": "in_progress"}
	case "recover", "recover_foreign", "recover_no_ids":
		updates = map[string]any{"submit_state": "submission_unknown", "upstream_id": "", "status": "validating"}
	}
	if tc.Family == constant.ChannelTypeGemini && len(updates) > 0 {
		if updates["upstream_id"] == "native_1" {
			updates["upstream_id"] = "batches/native_1"
		}
	}
	if len(updates) > 0 {
		require.NoError(t, model.DB.Model(job).Updates(updates).Error)
	}
	if strings.HasPrefix(tc.Variant, "recover") {
		if tc.Variant == "recover_foreign" {
			provider.Variant = "foreign_item"
		}
		if tc.Variant == "recover_no_ids" {
			provider.Variant = "no_ids"
		}
		claimed, err := core.NewStore().ClaimBatchJob(job.ID, "contract-worker")
		require.NoError(t, err)
		require.True(t, claimed)
		require.NoError(t, model.DB.First(job, job.ID).Error)
		id := "native_1"
		if tc.Family == constant.ChannelTypeGemini {
			id = "batches/native_1"
		}
		_ = batchObservation(t, func() error {
			return h.Verify(context.Background(), (*domain.Job)(job), id, "contract-worker")
		}, states, failures)
		require.NoError(t, model.DB.Model(job).Updates(map[string]any{"lease_owner": "", "lease_until": 0}).Error)
	}
	for range 3 {
		require.NoError(t, batchObservation(t, func() error { return runtime.New().Poll.Execute(context.Background(), "contract-worker") }, states, failures))
	}
	var stored model.BatchJob
	require.NoError(t, model.DB.First(&stored, job.ID).Error)
	require.LessOrEqual(t, provider.PaidPosts, 1, "ambiguous submission must never be replayed")
	if tc.Variant == "success" {
		require.Equal(t, "settled", stored.BillingStatus)
		require.Zero(t, stored.ReservedQuota)
		require.Equal(t, 2, stored.SettledCount)
	}
	if tc.Variant == "ambiguous" || tc.Variant == "transport_error" || tc.Variant == "bad_create_json" {
		assert.Equal(t, "submission_unknown", stored.SubmitState)
		assert.Zero(t, stored.ChargedQuota)
	}
	if tc.Variant == "revoked" || tc.Variant == "deleted" || tc.Variant == "expired_token" {
		assert.Zero(t, provider.PaidPosts)
		assert.Zero(t, stored.ReservedQuota)
	}
	if tc.Variant == "unsafe_hold" {
		assert.Zero(t, provider.PaidPosts)
		assert.EqualValues(t, 200, stored.ReservedQuota)
	}
	if tc.Variant == "foreign_job" {
		assert.Zero(t, stored.ResultCount)
		assert.Zero(t, stored.ChargedQuota)
	}
}

func batchStoredResult(t *testing.T, job *model.BatchJob, customID, body string, status int, unbilled bool) {
	t.Helper()
	encoded, err := common.Marshal(batch.Result{CustomID: customID, StatusCode: status, Body: []byte(body), Unbilled: unbilled})
	require.NoError(t, err)
	require.NoError(t, core.NewStore().StoreBatchResult(job.ID, customID, string(encoded), fixture.Hash(string(encoded))))
}

func (h harness) runBatchItemSettlement(t *testing.T, tc Case, job *model.BatchJob, states *[]any, failures *[]string) {
	body := `{"usage":{"prompt_tokens":2,"completion_tokens":1,"cost":0.000004}}`
	if tc.Endpoint == "/v1/responses" || tc.Endpoint == "/v1/messages" {
		body = `{"usage":{"input_tokens":2,"output_tokens":1,"cost":0.000004}}`
	}
	if tc.Endpoint == "/v1/embeddings" {
		body = `{"usage":{"prompt_tokens":2,"total_tokens":2,"cost":0.000004}}`
	}
	if tc.Endpoint == "/v1beta/generateContent" {
		body = `{"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`
	}
	switch tc.Variant {
	case "zero":
		body = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(body, ":2", ":0"), ":1", ":0"), "0.000004", "0")
	case "missing":
		body = `{}`
	case "null":
		body = `{"usage":null,"usageMetadata":null}`
	case "negative":
		body = strings.ReplaceAll(body, ":2", ":-1")
	case "fractional":
		body = strings.ReplaceAll(body, ":2", ":1.5")
	case "too_large":
		body = strings.ReplaceAll(body, ":2", ":2147483648")
	case "positive":
	case "string":
		body = strings.ReplaceAll(body, "0.000004", `"0.000004"`)
	case "invalid_tokens":
		body = `{"usage":{"prompt_tokens":-1,"cost":0.000004}}`
	case "unknown_mode":
		job.BillingSnapshot = fixture.Snapshot(t, "unknown_mode")
	case "tools":
		require.NoError(t, model.DB.Model(&model.BatchItem{}).Where("batch_id=?", job.ID).Update("upstream_body", `{"tools":[]}`).Error)
	case "no_purchase":
		job.BillingSnapshot = strings.ReplaceAll(job.BillingSnapshot, `"purchase_expression":"tier(\"batch\", p + c * 2)"`, `"purchase_expression":""`)
	case "clamp":
		job.BillingSnapshot = strings.ReplaceAll(job.BillingSnapshot, `p + c * 2`, `p * 1e20`)
	case "frozen_clock":
		job.BillingSnapshot = strings.ReplaceAll(job.BillingSnapshot, `tier(\"batch\", p + c * 2)`,
			`hour(\"UTC\") == 0 ? tier(\"frozen\", p + c * 2) : tier(\"changed\", p * 100)`)
		time.Sleep(time.Hour)
	}
	if tc.Variant == "unknown_mode" || tc.Variant == "no_purchase" || tc.Variant == "clamp" || tc.Variant == "frozen_clock" {
		require.NoError(t, model.DB.Model(job).Update("billing_snapshot", job.BillingSnapshot).Error)
	}
	if tc.Kind == "legacy" && tc.Variant == "negative" {
		body = strings.ReplaceAll(body, "0.000004", "-0.000004")
	}
	status := 200
	if tc.Variant == "non2xx" {
		status = 500
	}
	batchStoredResult(t, job, "client_0", body, status, tc.Variant == "unbilled")
	for range 2 {
		_ = batchObservation(t, func() error { return h.SettleResults((*domain.Job)(job)) }, states, failures)
	}
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	if tc.Variant == "success" || tc.Variant == "positive" {
		require.Less(t, user.Quota, 1000000)
	}
	if tc.Variant == "missing" || tc.Variant == "null" || tc.Variant == "negative" || tc.Variant == "fractional" ||
		tc.Variant == "too_large" || tc.Variant == "non2xx" || tc.Variant == "string" {
		assert.Equal(t, 1000000, user.Quota)
	}
	if tc.Variant == "invalid_tokens" {
		assert.Equal(t, 999998, user.Quota, "authoritative v1 cost does not require valid token evidence")
	}
	_ = batchObservation(t, func() error { return core.NewStore().SyncBatchConsumeLogs(100) }, states, failures)
	_ = batchObservation(t, func() error { return h.ReconcilePurchase((*domain.Job)(job)) }, states, failures)
	_ = batchObservation(t, func() error { return core.NewStore().ReleaseSettledBatchHolds(job.ID) }, states, failures)
}

func (h harness) runBatchAggregate(t *testing.T, tc Case, job *model.BatchJob, states *[]any, failures *[]string) {
	updates := map[string]any{"status": "completed", "submit_state": "submitted", "upstream_id": "native_1",
		"upstream_usage": `{"cost":0.000008,"prompt_tokens":4,"completion_tokens":2}`}
	switch tc.Variant {
	case "zero":
		updates["upstream_usage"] = `{"cost":0}`
	case "missing":
		updates["upstream_usage"] = `{}`
	case "null":
		updates["upstream_usage"] = `{"cost":null}`
	case "negative":
		updates["upstream_usage"] = `{"cost":-1}`
	case "string":
		updates["upstream_usage"] = `{"cost":"0.01"}`
	case "bad_basis":
		updates["billing_snapshot"] = `{"settlement_mode":"openrouter_job_cost_v2","openrouter_cost":null}`
	case "nonterminal":
		updates["status"] = "in_progress"
	case "unconfirmed":
		updates["submit_state"] = "submission_unknown"
		updates["upstream_id"] = ""
	case "prior_item":
		require.NoError(t, model.DB.Model(&model.BatchItem{}).Where("batch_id=?", job.ID).Update("charged_quota", 1).Error)
	case "log_disabled":
		common.LogConsumeEnabled = false
	case "overflow":
		require.NoError(t, model.DB.Model(&model.User{}).Where("id=1").Update("used_quota", 2147483647).Error)
	}
	require.NoError(t, model.DB.Model(job).Updates(updates).Error)
	for i := range 2 {
		if i == 1 && tc.Variant == "changed_receipt" {
			require.NoError(t, model.DB.Model(job).Update("upstream_usage", `{"cost":0.000010}`).Error)
		}
		_ = batchObservation(t, func() error { return core.NewStore().SettleBatchJobCost(job.ID) }, states, failures)
	}
	_ = batchObservation(t, func() error { return core.NewStore().SyncBatchConsumeLogs(100) }, states, failures)
	_ = batchObservation(t, func() error { return h.ReconcilePurchase((*domain.Job)(job)) }, states, failures)
	_ = batchObservation(t, func() error { return core.NewStore().ReleaseSettledBatchHolds(job.ID) }, states, failures)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	if tc.Variant == "positive" || tc.Variant == "changed_receipt" || tc.Variant == "log_disabled" {
		assert.Equal(t, 999996, user.Quota)
	}
	if tc.Variant == "missing" || tc.Variant == "null" || tc.Variant == "negative" || tc.Variant == "string" {
		assert.Equal(t, 1000000, user.Quota)
	}
}

func runBatchMoneyFault(t *testing.T, tc Case, job *model.BatchJob, states *[]any, failures *[]string) {
	if tc.Kind == "aggregate_fault" {
		require.NoError(t, model.DB.Model(job).Updates(map[string]any{"status": "completed", "submit_state": "submitted",
			"upstream_id": "native_1", "upstream_usage": `{"cost":0.000008}`}).Error)
	}
	before := fixture.State(t)
	callback := func(tx *gorm.DB) {
		if tx.Statement.Table == tc.Variant {
			tx.AddError(errors.New("fixture atomic write failure"))
		}
	}
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("batch_money_fault", callback))
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("batch_money_fault", callback))
	operation := func() error {
		return core.NewStore().SettleBatchItem(job.ID, "client_0", 2, `{"purchase_status":"calculated","purchase_usd":0.000004}`)
	}
	if tc.Kind == "aggregate_fault" {
		operation = func() error { return core.NewStore().SettleBatchJobCost(job.ID) }
	}
	err := batchObservation(t, operation, states, failures)
	require.ErrorContains(t, err, "fixture atomic write failure")
	require.NoError(t, model.DB.Callback().Update().Remove("batch_money_fault"))
	require.NoError(t, model.DB.Callback().Create().Remove("batch_money_fault"))
	assert.Equal(t, before, fixture.State(t), "every DB leg and outbox must roll back")
	require.NoError(t, batchObservation(t, operation, states, failures))
	require.NoError(t, batchObservation(t, operation, states, failures))
}

func (h harness) runBatchDurable(t *testing.T, tc Case, job *model.BatchJob, states *[]any, failures *[]string) {
	switch tc.Variant {
	case "number_precision":
		var item model.BatchItem
		require.NoError(t, model.DB.Where("batch_id=? AND ordinal=0", job.ID).First(&item).Error)
		first := batch.Result{CustomID: item.ProviderCustomID, StatusCode: 200, Body: []byte(`{"integer":9007199254740993,"text":"fixture"}`)}
		require.NoError(t, batchObservation(t, func() error { return h.RecordResult((*domain.Job)(job), domain.Result(first)) }, states, failures))
		first.Body = []byte(`{"text":"fixture","integer":9007199254740993}`)
		require.NoError(t, batchObservation(t, func() error { return h.RecordResult((*domain.Job)(job), domain.Result(first)) }, states, failures))
		require.NoError(t, model.DB.First(&item, item.ID).Error)
		require.Contains(t, item.Result, "9007199254740993")
		var stored model.BatchJob
		require.NoError(t, model.DB.First(&stored, job.ID).Error)
		require.Equal(t, 1, stored.ResultCount)
	case "same", "different_hash", "foreign":
		result := `{"custom_id":"client_0","body":{"integer":123}}`
		hash := fixture.Hash(result)
		require.NoError(t, batchObservation(t, func() error { return core.NewStore().StoreBatchResult(job.ID, "client_0", result, hash) }, states, failures))
		id := "client_0"
		if tc.Variant == "foreign" {
			id = "foreign"
		}
		if tc.Variant == "different_hash" {
			hash = "changed"
		}
		_ = batchObservation(t, func() error { return core.NewStore().StoreBatchResult(job.ID, id, result, hash) }, states, failures)
	case "same_receipt", "different_receipt", "held_release", "log_retry", "log_disabled":
		if tc.Variant == "log_disabled" {
			common.LogConsumeEnabled = false
		}
		details := `{"purchase_status":"calculated","purchase_usd":0.000004}`
		require.NoError(t, batchObservation(t, func() error { return core.NewStore().SettleBatchItem(job.ID, "client_0", 2, details) }, states, failures))
		if tc.Variant == "different_receipt" {
			details = `{"purchase_status":"calculated","purchase_usd":0.000005}`
		}
		_ = batchObservation(t, func() error { return core.NewStore().SettleBatchItem(job.ID, "client_0", 2, details) }, states, failures)
		if tc.Variant == "log_retry" {
			require.NoError(t, model.LOG_DB.Callback().Create().Before("gorm:create").Register("batch_log_fault", func(tx *gorm.DB) {
				if tx.Statement.Table == "logs" {
					tx.AddError(errors.New("fixture log write failure"))
				}
			}))
			_ = batchObservation(t, func() error { return core.NewStore().SyncBatchConsumeLogs(100) }, states, failures)
			require.NoError(t, model.LOG_DB.Callback().Create().Remove("batch_log_fault"))
		}
		for range 2 {
			require.NoError(t, batchObservation(t, func() error { return core.NewStore().SyncBatchConsumeLogs(100) }, states, failures))
		}
		for range 2 {
			require.NoError(t, batchObservation(t, func() error { return core.NewStore().ReleaseSettledBatchHolds(job.ID) }, states, failures))
		}
	case "purchase_repeat", "purchase_conflict":
		require.NoError(t, model.DB.Model(job).Update("status", "completed").Error)
		require.NoError(t, batchObservation(t, func() error { return core.NewStore().ConfirmBatchPurchaseCost(job.PublicID, "0.01", "invoice") }, states, failures))
		cost := "0.01"
		if tc.Variant == "purchase_conflict" {
			cost = "0.02"
		}
		_ = batchObservation(t, func() error { return core.NewStore().ConfirmBatchPurchaseCost(job.PublicID, cost, "invoice") }, states, failures)
	case "lease_equal", "lease_busy":
		until := taskfixture.Epoch
		if tc.Variant == "lease_busy" {
			until++
		}
		require.NoError(t, model.DB.Model(job).Update("lease_until", until).Error)
		claimed, err := core.NewStore().ClaimBatchJob(job.ID, "contract-worker")
		require.NoError(t, err)
		require.False(t, claimed)
	case "cancelled_context":
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		require.ErrorIs(t, batchObservation(t, func() error { return runtime.New().Poll.Execute(ctx, "contract-worker") }, states, failures), context.Canceled)
	case "master_once":
		common.IsMasterNode = false
		StartSystemTaskRunner()
		common.IsMasterNode = true
		StartSystemTaskRunner()
		var count int64
		require.NoError(t, model.DB.Model(&model.SystemTask{}).Count(&count).Error)
		require.Zero(t, count, "once is consumed on the follower as in production")
	case "registration", "scheduler_off", "scheduler_due", "handler_history", "handler_cancelled":
		httpapi.RegisterScheduledSystemTasks()
		var handler ScheduledSystemTaskHandler
		for _, candidate := range h.Handlers() {
			if candidate.Type() == "provider_batch_poll" {
				handler = candidate.(ScheduledSystemTaskHandler)
			}
		}
		require.NotNil(t, handler, "scheduler test binary must retain production registration")
		require.Equal(t, 15*time.Second, handler.Interval())
		require.Nil(t, handler.NewPayload())
		constant.UpdateTask = tc.Variant != "scheduler_off"
		require.Equal(t, constant.UpdateTask, handler.Enabled())
		if tc.Variant == "handler_history" || tc.Variant == "handler_cancelled" {
			task, err := systemStoreHarness.CreateSystemTask(handler.Type(), nil, nil)
			require.NoError(t, err)
			claimed, won, err := systemStoreHarness.ClaimSystemTask(task.ID, handler.Type(), "contract-worker", taskfixture.Epoch+60)
			require.NoError(t, err)
			require.True(t, won)
			ctx := context.Background()
			if tc.Variant == "handler_cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			handler.Run(ctx, claimed, "contract-worker")
			var stored model.SystemTask
			require.NoError(t, model.DB.First(&stored, task.ID).Error)
			if tc.Variant == "handler_cancelled" {
				require.Equal(t, systemrecord.SystemTaskStatusFailed, stored.Status)
			} else {
				require.Equal(t, systemrecord.SystemTaskStatusSucceeded, stored.Status)
			}
			*states = append(*states, fixture.State(t))
			return
		}
		if tc.Variant == "registration" {
			constant.UpdateTask = true
			*states = append(*states, map[string]any{"type": handler.Type(), "interval_seconds": handler.Interval().Seconds(), "enabled": handler.Enabled()})
			return
		}
		h.Registry(t, handler)
		h.Schedule()
		h.Schedule()
		var count int64
		require.NoError(t, model.DB.Model(&model.SystemTask{}).Where("type=?", handler.Type()).Count(&count).Error)
		if tc.Variant == "scheduler_off" {
			require.Zero(t, count)
		} else {
			require.EqualValues(t, 1, count)
		}
	default:
		t.Fatal("unknown durable case", tc.Variant)
	}
	*states = append(*states, fixture.State(t))
}

var _ http.RoundTripper = (*fixture.Transport)(nil)

var systemStoreHarness = systemstorage.Store{DB: func() *gorm.DB { return model.DB }}
