package taskruntime

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskstore "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var taskTickManifest = flag.Bool("tasks-tick-manifest", false, "list task policy cases")
var taskTickPreflight = flag.Bool("tasks-tick-preflight", false, "assert task policies without goldens")

func init() { fixture.RegisterUpdate(); fixture.RejectCandidateUpdate() }

type taskTickCase struct {
	Name, Kind, Source, Status, Mode, Variant string
	Actual, Target, HTTP                      int
}

func taskTickCases() []taskTickCase {
	result := []taskTickCase{}
	for _, source := range []string{"wallet", "subscription"} {
		for _, actual := range []int{-1, 0, 25, 50, 75} {
			target := actual
			if actual < 0 {
				target = 50
			}
			result = append(result, taskTickCase{Name: fmt.Sprintf("delta_%s_%d", source, actual), Kind: "delta", Source: source, Actual: actual, Target: target})
		}
		for _, variant := range []string{"normal", "zero", "no_token", "funding_error", "token_error", "quota_write_error"} {
			target := 0
			if variant == "funding_error" {
				target = 50
			}
			result = append(result, taskTickCase{Name: "refund_" + source + "_" + variant, Kind: "refund", Source: source, Variant: variant, Target: target})
		}
	}
	for _, variant := range []string{"per_call", "adaptor", "tokens", "tiered", "tiered_error", "tiered_failure", "modern_fx", "invalid_fx", "unknown_mode"} {
		target := 50
		switch variant {
		case "adaptor":
			target = 75
		case "tokens":
			target = 30
		case "tiered":
			target = 40
		case "tiered_failure":
			target = 0
		case "modern_fx":
			target = 60
		}
		result = append(result, taskTickCase{Name: "completion_" + variant, Kind: "completion", Source: "wallet", Variant: variant, Target: target})
	}
	for _, variant := range []string{"fx_absent", "fx_null", "group_null", "fx_partial", "fx_factor_one"} {
		target := 50
		if variant == "fx_absent" || variant == "fx_factor_one" {
			target = 30
		}
		result = append(result, taskTickCase{Name: "completion_raw_" + variant, Kind: "completion", Source: "wallet", Variant: variant, Target: target})
	}
	for _, mode := range []string{model.TaskSettlementOpenRouterCostV1, model.TaskSettlementOpenRouterCostV2} {
		for _, source := range []string{"wallet", "subscription"} {
			for _, variant := range []string{"lower",
				"higher",
				"zero",
				"missing",
				"unconfirmed",
				"negative",
				"missing_token",
				"wrong_sub_owner",
				"subscription_overage",
				"log_error",
				"user_error",
				"channel_error",
				"token_error",
				"commit_error"} {
				if source == "wallet" && (variant == "wrong_sub_owner" || variant == "subscription_overage") {
					continue
				}
				target := 50
				switch variant {
				case "lower", "log_error":
					target = 40
				case "higher":
					target = 100
				case "zero":
					target = 0
				}
				result = append(result,
					taskTickCase{Name: "cost_" + mode + "_" + source + "_" + variant,
						Kind:    "cost",
						Source:  source,
						Mode:    mode,
						Variant: variant,
						Target:  target})
			}
		}
	}
	for _, status := range []string{"SUBMITTED", "QUEUED", "IN_PROGRESS", "SUCCESS", "FAILURE", "UNKNOWN"} {
		target := 50
		if status == "FAILURE" {
			target = 0
		}
		result = append(result, taskTickCase{Name: "poll_" + status, Kind: "poll", Source: "wallet", Status: status, Target: target, HTTP: 200})
	}
	for _, status := range []int{400, 401, 403, 404, 410, 429, 500} {
		target := 50
		if status == 404 || status == 410 || status == 429 {
			target = 0
		}
		result = append(result,
			taskTickCase{Name: fmt.Sprintf("poll_http_%d",
				status),
				Kind:   "poll",
				Source: "wallet",
				Status: "IN_PROGRESS",
				HTTP:   status,
				Target: target})
	}
	for _, variant := range []string{"provisional_zero",
		"terminal_zero",
		"pending_cursor",
		"transport",
		"hook",
		"state",
		"pending",
		"null_id",
		"missing_channel",
		"cancelled",
		"timeout_legacy",
		"timeout_current",
		"timeout_boundary"} {
		target := 50
		if variant == "timeout_current" || variant == "timeout_legacy" || variant == "terminal_zero" {
			target = 0
		}
		result = append(result,
			taskTickCase{Name: "tick_" + variant,
				Kind:    "poll",
				Source:  "wallet",
				Status:  "IN_PROGRESS",
				HTTP:    200,
				Variant: variant,
				Target:  target})
	}
	ordinary := append([]taskTickCase{}, result...)
	for _, tc := range ordinary {
		if tc.Kind == "poll" && tc.Variant == "" {
			tc.Name = "batch_" + tc.Name
			tc.Mode = "batch"
			result = append(result, tc)
		}
	}
	result = append(result,
		taskTickCase{Name: "delta_subscription_foreign_owner",
			Kind:    "delta",
			Source:  "subscription",
			Variant: "foreign_owner",
			Actual:  75,
			Target:  75})
	for _, platform := range []string{"audio", "rerank", "image", "chat_image"} {
		for _, variant := range []string{"positive",
			"zero",
			"missing",
			"null",
			"negative",
			"string_cost",
			"auth",
			"not_found",
			"rate_limit",
			"server_error",
			"redirect",
			"oversized",
			"wrong_id",
			"wrong_model",
			"cancelled",
			"tts_no_time",
			"not_due",
			"active_lease",
			"expired_lease",
			"attempt_limit",
			"deadline",
			"key_rotation"} {
			target := 50
			if variant == "positive" || variant == "tts_no_time" || variant == "key_rotation" {
				target = 40
			}
			if variant == "zero" {
				target = 0
			}
			if platform == "image" || platform == "chat_image" && variant == "key_rotation" || platform != "audio" && variant == "tts_no_time" {
				target = 50
			}
			result = append(result,
				taskTickCase{Name: "sync_" + platform + "_" + variant,
					Kind:    "metadata",
					Mode:    platform,
					Source:  "wallet",
					Variant: variant,
					Target:  target})
		}
	}
	return result
}

func TestTaskTickContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	if updating {
		fixture.CheckRecording(t)
	}
	cases := taskTickCases()
	if *taskTickManifest {
		data, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("TASKS_TICK_MANIFEST " + string(data))
		return
	}
	if name := os.Getenv("TASKS_TICK_CASE"); name != "" {
		for _, tc := range cases {
			if tc.Name == name {
				runTaskTickContract(t, tc, updating)
				return
			}
		}
		t.Fatalf("unknown task policy case %q", name)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			exe, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.CommandContext(t.Context(),
				exe,
				"-test.run=^TestTaskTickContract$",
				"-test.count=1",
				"-test.timeout=90s",
				"-update="+strconv.FormatBool(updating),
				"-tasks-tick-preflight="+strconv.FormatBool(*taskTickPreflight))
			cmd.Env = append(os.Environ(), "TASKS_TICK_CASE="+tc.Name, "TZ=UTC")
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "TASKS_SNAPSHOT ") {
					fmt.Println(line)
				}
			}
		})
	}
}

type taskContractAdaptor struct {
	result                 relaycommon.TaskInfo
	status, adjust, calls  int
	fetchError, parseError error
}

func (*taskContractAdaptor) Init(*relaycommon.RelayInfo) {}
func (a *taskContractAdaptor) FetchTask(_ string, _ string, _ *model.Task, _ string) (*http.Response, error) {
	a.calls++
	if a.fetchError != nil {
		return nil, a.fetchError
	}
	return &http.Response{StatusCode: a.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"fixture":"poll"}`))}, nil
}
func (a *taskContractAdaptor) ParseTaskResult(*model.Task, *http.Response, []byte) (*relaycommon.TaskInfo, error) {
	return &a.result, a.parseError
}
func (a *taskContractAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return a.adjust
}

func runTaskTickContract(t *testing.T, tc taskTickCase, updating bool) {
	if tc.Kind == "metadata" {
		runSyncMetadataContract(t, tc, updating)
		return
	}
	synctest.Test(t, func(t *testing.T) {
		if strings.HasPrefix(tc.Variant, "timeout_") {
			time.Sleep(time.Unix(1772323200, 0).Sub(time.Now()))
		}
		now := time.Now().Unix()
		db := fixture.Open(t)
		require.NoError(t,
			db.Create(&model.User{Id: 1,
				Username:     "tasks-user",
				Group:        "default",
				Quota:        9950,
				UsedQuota:    50,
				RequestCount: 1,
				AuthVersion:  3,
				Status:       1}).Error)
		require.NoError(t,
			db.Create(&model.Token{Id: 11,
				UserId:      1,
				Key:         "taskspolicykey",
				Name:        "policy-key",
				Status:      1,
				ExpiredTime: -1,
				RemainQuota: 9950,
				UsedQuota:   50}).Error)
		baseURL := "https://203.0.113.10"
		require.NoError(t, db.Create(&model.Channel{Id: 71, Type: constant.ChannelTypeKling, Key: "fixture-key", BaseURL: &baseURL, Status: 1, UsedQuota: 50}).Error)
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"task-policy-model":1}`))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
		task := &model.Task{ID: 81,
			TaskID:    "task_policy",
			CreatedAt: now,
			UpdatedAt: now,
			UserId:    1,
			ChannelId: 71,
			Platform:  "kling",
			Action:    constant.TaskActionTextToVideo,
			Group:     "default",
			Quota:     50,

			Status:     model.TaskStatusInProgress,
			Progress:   "30%",
			SubmitTime: now,
			Data:       []byte(`{"keep":true}`),
			Properties: model.Properties{OriginModelName: "task-policy-model"},

			PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-policy",
				BillingSource: tc.Source,
				TokenId:       11,
				NodeName:      "tasks-contract-node",
				BillingContext: &model.TaskBillingContext{OriginModelName: "task-policy-model",
					GroupRatio: 1}}}
		if tc.Source == "subscription" {
			task.PrivateData.SubscriptionId = 21
			require.NoError(t,
				db.Create(&model.UserSubscription{Id: 21,
					UserId:      1,
					PlanId:      31,
					AmountTotal: 1000,
					AmountUsed:  50,
					Status:      "active",
					StartTime:   now - 10,
					EndTime:     now + 10000}).Error)
			require.NoError(t, db.Model(&model.User{}).Where("id=1").Update("quota", 10000).Error)
		}
		bc := task.PrivateData.BillingContext
		adaptor := &taskContractAdaptor{result: relaycommon.TaskInfo{Status: tc.Status, TaskID: "upstream-policy", Reason: ""}, status: tc.HTTP}
		if tc.Status == "FAILURE" {
			adaptor.result.Reason = "fixture provider failure"
		}
		if adaptor.status == 0 {
			adaptor.status = 200
		}
		if tc.Kind == "cost" {
			bc.SettlementMode = tc.Mode
			bc.GroupRatio = 4
			bc.BillingFX = &model.TaskBillingFX{SchemaVersion: 1, Source: "cbr-xml-daily.ru", Rate: 200, PublicationVersion: 1, EffectiveAt: 1, FetchedAt: 1}
			bc.SubmitGroup = &model.TaskBillingSubmitGroup{PureRatio: 2}
			bc.TieredSnapshot = &billingexpr.BillingSnapshot{GroupRatio: 4, QuotaPerUnit: 500000}
			if tc.Mode == model.TaskSettlementOpenRouterCostV2 {
				bc.TieredSnapshot = nil
				bc.OpenRouterCost = &common.OpenRouterCostBasis{Version: tc.Mode, EffectiveGroupRatio: 4, QuotaPerUnit: 500000, EstimatedUSD: 0.000025}
			}
			cost := 0.00002
			if tc.Variant == "higher" || tc.Variant == "subscription_overage" {
				cost = 0.00005
			}
			if tc.Variant == "zero" {
				cost = 0
			}
			if tc.Variant == "negative" {
				cost = -1
			}
			task.PrivateData.Reconciliation = &model.TaskReconciliation{ReservedQuota: 50,
				HasUpstreamCost:       tc.Variant != "missing",
				UpstreamCostConfirmed: tc.Variant != "unconfirmed",
				UpstreamCostUSD:       cost}
		}
		if tc.Variant == "subscription_overage" {
			require.NoError(t, db.Model(&model.UserSubscription{}).Where("id=21").Update("amount_total", 60).Error)
		}
		if tc.Variant == "foreign_owner" {
			require.NoError(t, db.Model(&model.UserSubscription{}).Where("id=21").Update("user_id", 2).Error)
		}
		if tc.Mode == "batch" {
			task.Platform = "sunoapi"
		}
		if tc.Kind == "completion" {
			task.Status = model.TaskStatusSuccess
			adaptor.result.TotalTokens = 30
			switch tc.Variant {
			case "per_call":
				bc.PerCallBilling = true
				adaptor.adjust = 75
			case "adaptor":
				adaptor.adjust = 75
			case "tiered", "tiered_error", "tiered_failure":
				expression := `tier("base",u("seconds") * 0.00001)`
				if tc.Variant == "tiered_error" {
					expression = "broken("
				}
				bc.TieredSnapshot = &billingexpr.BillingSnapshot{TaskUsageBilling: true,
					ExprString:   expression,
					QuotaPerUnit: 500000,
					GroupRatio:   1,
					UsageFacts:   map[string]any{"seconds": float64(8)}}
				if tc.Variant == "tiered_failure" {
					task.Status = model.TaskStatusFailure
				}
			case "modern_fx", "invalid_fx":
				bc.BillingFX = &model.TaskBillingFX{SchemaVersion: 1, Source: "cbr-xml-daily.ru", Rate: 200, PublicationVersion: 1, EffectiveAt: 1, FetchedAt: 1}
				bc.SubmitGroup = &model.TaskBillingSubmitGroup{PureRatio: 1}
				bc.GroupRatio = 2
				if tc.Variant == "invalid_fx" {
					bc.BillingFX.Rate = 0
				}
			case "unknown_mode":
				bc.SettlementMode = "future_mode"
			}
		}
		switch tc.Variant {
		case "no_token":
			task.PrivateData.TokenId = 0
		case "missing_token":
			require.NoError(t, db.Delete(&model.Token{}, 11).Error)
		case "wrong_sub_owner":
			if tc.Source == "subscription" {
				require.NoError(t, db.Model(&model.UserSubscription{}).Where("id=21").Update("user_id", 2).Error)
			}
		case "zero":
			if tc.Kind == "refund" {
				task.Quota = 0
			}
		case "state":
			task.PrivateData.PluginState = []byte(`{"preserve":true}`)
		case "pending":
			task.PrivateData.Reconciliation = &model.TaskReconciliation{ReservationPending: true, ReservedQuota: 50}
		case "null_id":
			task.PrivateData.UpstreamTaskID = ""
		case "missing_channel":
			task.ChannelId = 99
		case "transport":
			adaptor.fetchError = errors.New("fixture transport error")
		case "hook":
			adaptor.parseError = errors.New("fixture hook error")
		case "timeout_legacy":
			task.SubmitTime = 1771718399
		case "timeout_current":
			task.SubmitTime = now - 3600
		case "timeout_boundary":
			task.SubmitTime = now - 1800
		}
		require.NoError(t, db.Create(task).Error)
		if strings.HasPrefix(tc.Variant, "fx_") || tc.Variant == "group_null" {
			raw := `{"origin_model_name":"task-policy-model","group_ratio":1}`
			switch tc.Variant {
			case "fx_null":
				raw = `{"origin_model_name":"task-policy-model","group_ratio":1,"billing_fx":null}`
			case "group_null":
				raw = `{"origin_model_name":"task-policy-model","group_ratio":1,"billing_fx":{"schema_version":1,"source":"cbr-xml-daily.ru","rate":100,"publication_version":1,"effective_at":1,"fetched_at":1},"submit_group":null}`
			case "fx_partial":
				raw = `{"origin_model_name":"task-policy-model","group_ratio":1,"billing_fx":{"schema_version":1,"source":"cbr-xml-daily.ru"},"submit_group":{"pure_ratio":1,"has_special_ratio":false}}`
			case "fx_factor_one":
				raw = `{"origin_model_name":"task-policy-model","group_ratio":1,"billing_fx":{"schema_version":1,"source":"cbr-xml-daily.ru","rate":100,"publication_version":1,"effective_at":1,"fetched_at":1},"submit_group":{"pure_ratio":1,"has_special_ratio":false}}`
			}
			require.NoError(t,
				db.Model(&model.Task{}).Where("id=81").Update("private_data",
					`{"token_id":11,"billing_source":"wallet","billing_context":`+raw+`}`).Error)
			require.NoError(t, db.First(task, 81).Error)
		}
		if tc.Variant == "pending_cursor" {
			task.PrivateData.Reconciliation = &model.TaskReconciliation{ReservationPending: true, ReservedQuota: 50}
			require.NoError(t, taskstore.NewStore(func() *gorm.DB { return model.DB }).UpdateTask(task))
			later := *task
			later.ID = 82
			later.TaskID = "task_later"
			later.Quota = 0
			later.PrivateData = model.TaskPrivateData{UpstreamTaskID: "upstream-later"}
			require.NoError(t, db.Create(&later).Error)
		}
		if tc.Variant == "provisional_zero" || tc.Variant == "terminal_zero" {
			task.PrivateData.Reconciliation = &model.TaskReconciliation{ReservedQuota: 50, Required: true, HasUpstreamCost: true, UpstreamCostConfirmed: true}
			adaptor.result.Status = "FAILURE"
			if tc.Variant == "terminal_zero" {
				zero := 0.0
				adaptor.result.UpstreamCostUSD = &zero
				adaptor.result.UpstreamCostConfirmed = true
			}
			require.NoError(t, taskstore.NewStore(func() *gorm.DB { return model.DB }).UpdateTask(task))
		}
		if tc.Kind == "cost" {
			task.Status = model.TaskStatusSuccess
			prepareOpenRouterCostSettlement(task)
			won, err := taskstore.NewStore(func() *gorm.DB { return model.DB }).UpdateTaskWithStatus(task, model.TaskStatusInProgress)
			require.NoError(t, err)
			require.True(t, won)
		}
		if tc.Variant == "funding_error" || tc.Variant == "token_error" || tc.Variant == "user_error" || tc.Variant == "channel_error" || tc.Variant == "quota_write_error" {
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("task_policy_failure", func(tx *gorm.DB) {
				table := tx.Statement.Table
				fail := false
				switch tc.Variant {
				case "funding_error", "user_error":
					fail = table == "users" || table == "user_subscriptions"
				case "token_error":
					fail = table == "tokens"
				case "channel_error":
					fail = table == "channels"
				case "quota_write_error":
					fail = table == "tasks"
				}
				if fail {
					tx.AddError(errors.New("fixture write failure"))
				}
			}))
		}
		if tc.Variant == "log_error" {
			require.NoError(t,
				model.LOG_DB.Callback().Create().Before("gorm:create").Register("task_policy_log_error",
					func(tx *gorm.DB) { tx.AddError(errors.New("fixture log failure")) }))
		}
		if tc.Variant == "commit_error" {
			pool := &taskContractBeginPool{ConnPool: db.Statement.ConnPool}
			db.Config.ConnPool = pool
			db.Statement.ConnPool = pool
		}

		events := []any{}
		oldFactory := GetTaskAdaptorFunc
		GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
			if tc.Mode == "batch" {
				return &taskContractBatchAdaptor{taskContractAdaptor: adaptor}
			}
			return adaptor
		}
		defer func() { GetTaskAdaptorFunc = oldFactory }()
		constant.TaskTimeoutMinutes, constant.TaskQueryLimit, constant.TaskPollMaxFailures = 0, 100, 3
		if tc.Variant == "pending_cursor" {
			constant.TaskQueryLimit = 1
		}
		if tc.HTTP == 429 {
			task.PrivateData.PollFailures = 2
			require.NoError(t, taskstore.NewStore(func() *gorm.DB { return model.DB }).UpdateTask(task))
		}
		if strings.HasPrefix(tc.Variant, "timeout_") {
			constant.TaskTimeoutMinutes = 30
		}
		for step := range 2 {
			var err error
			var summary any
			switch tc.Kind {
			case "delta":
				RecalculateTaskQuota(t.Context(), task, tc.Actual, "fixture delta")
			case "refund":
				task.Status = model.TaskStatusFailure
				summary = RefundTaskQuota(t.Context(), task, "fixture refund")
			case "completion":
				var handled bool
				handled, err = settleTaskBillingOnComplete(t.Context(), adaptor, task, &adaptor.result)
				summary = handled
				if task.Status == model.TaskStatusFailure && !handled {
					RefundTaskQuota(t.Context(), task, "fixture failure")
				}
			case "cost":
				err = settleOpenRouterCost(task)
			case "poll":
				ctx := t.Context()
				if tc.Variant == "cancelled" {
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					ctx = cancelled
				}
				summary = RunTaskPollingOnce(ctx, nil)
			}
			if tc.Kind == "cost" && (step == 0 || tc.Variant != "log_error") && (strings.HasSuffix(tc.Variant,
				"error") || tc.Variant == "missing_token" || tc.Variant == "wrong_sub_owner" && tc.Source == "subscription" || tc.Variant == "subscription_overage") {
				require.Error(t, err)
			}
			var failure any
			if err != nil {
				failure = err.Error()
			}
			events = append(events, map[string]any{"state": fixture.State(t), "summary": summary, "error": failure, "provider_port_calls": adaptor.calls})
			if step == 0 {
				require.NoError(t, db.First(task, 81).Error)
			}
		}
		// The asserted target is independent of oracle recording.
		if tc.Variant != "funding_error" && tc.Variant != "quota_write_error" && tc.Variant != "wrong_sub_owner" && tc.Kind != "poll" {
			assert.Equal(t, tc.Target, task.Quota, tc.Name)
		}
		if tc.Kind == "poll" {
			require.NoError(t, db.First(task, 81).Error)
			assert.Equal(t, tc.Target, task.Quota)
		}
		fixture.Compare(t, "../../../service/testdata/tasks", tc.Name, fixture.StableRequestIDs(t, events), *taskTickPreflight, updating)
	})
}

type syncContractTransport struct {
	calls []map[string]any
	tc    taskTickCase
	t     *testing.T
}

func (p *syncContractTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "203.0.113.10" {
		return nil, errors.New("paid request or undeclared metadata destination")
	}
	assert.Equal(p.t, "Bearer frozen-fixture-account", r.Header.Get("Authorization"))
	assert.Empty(p.t, r.Header.Get("Content-Type"))
	assert.Empty(p.t, r.Header.Get("Content-Length"))
	p.calls = append(p.calls, map[string]any{"method": r.Method, "url": r.URL.String(), "headers": r.Header.Clone()})
	status := 200
	cost := `,"total_cost":0.04`
	id, modelName, apiType, cancelled, generationTime := "gen-fixture", "vendor/audio", "", "false", "20"
	if p.tc.Mode == "rerank" {
		apiType = "rerank"
	}
	if p.tc.Mode == "chat_image" {
		apiType = "completions"
	}
	switch p.tc.Variant {
	case "zero":
		cost = `,"total_cost":0`
	case "missing":
		cost = ""
	case "null":
		cost = `,"total_cost":null`
	case "negative":
		cost = `,"total_cost":-1`
	case "string_cost":
		cost = `,"total_cost":"0"`
	case "auth":
		status = 401
	case "not_found":
		status = 404
	case "rate_limit":
		status = 429
	case "server_error":
		status = 503
	case "redirect":
		status = 307
	case "wrong_id":
		id = "different-id"
	case "wrong_model":
		modelName = "vendor/other"
	case "cancelled":
		cancelled = "true"
	case "tts_no_time":
		apiType = "tts"
		generationTime = "null"
	}
	body := fmt.Sprintf(`{"data":{"id":%q,"model":%q,"api_type":%q,"cancelled":%s,"generation_time":%s%s}}`,
		id,
		modelName,
		apiType,
		cancelled,
		generationTime,
		cost)
	if strings.Contains(r.URL.Path, "/model/") {
		body = `{"data":{"id":"vendor/audio","canonical_slug":"vendor/audio"}}`
	}
	if p.tc.Variant == "oversized" {
		body = strings.Repeat("x", (2<<20)+1)
	}
	return &http.Response{StatusCode: status,
			Request: r,
			Header: http.Header{"Retry-After": {"3600"},
				"Location": {"https://203.0.113.10/must-not-follow"}},
			Body: io.NopCloser(strings.NewReader(body))},
		nil
}

func runSyncMetadataContract(t *testing.T, tc taskTickCase, updating bool) {
	service.InitHttpClient()
	synctest.Test(t, func(t *testing.T) {
		db := fixture.Open(t)
		require.NoError(t, db.Create(&model.User{Id: 1, Username: "sync-user", Quota: 10000, Group: "default", Status: 1}).Error)
		require.NoError(t, db.Create(&model.Token{Id: 11, UserId: 1, Key: "syncfixturekey", Status: 1, ExpiredTime: -1, RemainQuota: 10000}).Error)
		baseURL := "https://203.0.113.10"
		require.NoError(t, db.Create(&model.Channel{Id: 71, Type: constant.ChannelTypeOpenRouter, Key: "frozen-fixture-account", BaseURL: &baseURL, Status: 1}).Error)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/audio/speech", nil)
		c.Set(common.RequestIdKey, "sync-request-fixture")
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 71,
			ChannelType:       constant.ChannelTypeOpenRouter,
			ApiKey:            "frozen-fixture-account",
			ChannelBaseUrl:    baseURL,
			UpstreamModelName: "vendor/audio"},

			RelayMode:       relayconstant.RelayModeAudioSpeech,
			UserId:          1,
			TokenId:         11,
			TokenKey:        "syncfixturekey",
			OriginModelName: "audio-alias",
			UsingGroup:      "default",
			RequestId:       "sync-request-fixture",
			ForcePreConsume: true,

			OpenRouterCost: &relaycommon.OpenRouterCostState{Basis: common.OpenRouterCostBasis{Version: model.TaskSettlementOpenRouterCostV1,
				EffectiveGroupRatio: 1,
				QuotaPerUnit:        1000,
				EstimatedUSD:        0.05}}}
		switch tc.Mode {
		case "rerank":
			info.RelayMode = relayconstant.RelayModeRerank
		case "image":
			info.RelayMode = relayconstant.RelayModeImagesGenerations
			info.NativeImage = &relaycommon.NativeImageInfo{}
		case "chat_image":
			info.RelayMode = relayconstant.RelayModeChatCompletions
			info.OpenRouterCost.ChatImage = true
		}
		info.PriceData.Quota = 50
		info.UserSetting.BillingPreference = "wallet_only"
		require.NoError(t, BeginOpenRouterSyncTask(c, info, 50))
		events := []any{map[string]any{"stage": "intent", "state": fixture.State(t)}}
		require.Nil(t, service.PreConsumeBilling(c, 50, info))
		require.NoError(t, CommitOpenRouterSyncReservation(c, info))
		events = append(events, map[string]any{"stage": "reserved", "state": fixture.State(t)})
		require.NoError(t,
			ConfigureOpenRouterSyncSubmission(info,
				baseURL+"/v1/generation",
				http.Header{"Authorization": {"Bearer frozen-fixture-account"},
					"Content-Type": {"application/json"}}))
		require.NoError(t, RecordOpenRouterSyncSubmission(info, &http.Response{StatusCode: 200, Header: http.Header{"X-Generation-Id": {"gen-fixture"}}}, nil))
		require.NoError(t, CompleteOpenRouterSyncTask(c, info, []byte("fixture audio"), "audio/pcm"))
		task, err := openRouterSyncTask(info)
		require.NoError(t, err)
		state := task.PrivateData.OpenRouterSyncState()
		switch tc.Variant {
		case "not_due":
			state.NextPoll = fixture.Epoch + 3600
		case "active_lease":
			task.PrivateData.Reconciliation.SubmissionPending = true
		case "expired_lease":
			task.PrivateData.Reconciliation.ReservationPending = true
			state.LeaseUntil = fixture.Epoch - 1
		case "attempt_limit":
			state.Attempts = 16
		case "deadline":
			state.Deadline = fixture.Epoch
		case "key_rotation":
			require.NoError(t, db.Model(&model.Channel{}).Where("id=71").Update("key", "rotated-account").Error)
		}
		require.NoError(t, taskstore.NewStore(func() *gorm.DB { return model.DB }).UpdateOpenRouterSync(task, state.Revision))
		transport := &syncContractTransport{tc: tc, t: t, calls: []map[string]any{}}
		service.GetHttpClient().Transport = transport
		constant.TaskQueryLimit, constant.TaskTimeoutMinutes = 100, 0
		for range 2 {
			summary := RunTaskPollingOnce(t.Context(), nil)
			events = append(events,
				map[string]any{"stage": "poll",
					"summary": summary,
					"state":   fixture.State(t),
					"outbound": append([]map[string]any{},
						transport.calls...)})
		}
		task, err = openRouterSyncTask(info)
		require.NoError(t, err)
		assert.Equal(t, tc.Target, task.Quota)
		var user model.User
		require.NoError(t, db.First(&user, 1).Error)
		assert.Equal(t, 10000-tc.Target, user.Quota)
		require.True(t, task.PrivateData.OpenRouterSyncState().ResultReady)
		if tc.Target != 50 {
			require.Len(t, transport.calls, 1)
			assert.True(t, task.PrivateData.Reconciliation.Finalized)
		}
		snapshot := fixture.StableRequestIDs(t, events)
		old, new := fixture.BindTaskID(t, task.TaskID)
		snapshot = []byte(strings.ReplaceAll(string(snapshot), old, new))
		fixture.Compare(t, "../../../service/testdata/tasks", tc.Name, snapshot, *taskTickPreflight, updating)
	})
}

type taskContractBatchAdaptor struct{ *taskContractAdaptor }

func (*taskContractBatchAdaptor) FetchMode() string { return "batch" }
func (a *taskContractBatchAdaptor) FetchBatchTasks(base, key string, tasks []*model.Task, proxy string) (*http.Response, error) {
	return a.FetchTask(base, key, tasks[0], proxy)
}
func (a *taskContractBatchAdaptor) ParseBatchResult(tasks []*model.Task, _ *http.Response, _ []byte) (map[string]*BatchTaskResult, error) {
	result := make(map[string]*BatchTaskResult, len(tasks))
	for _, task := range tasks {
		result[task.GetUpstreamTaskID()] = &BatchTaskResult{TaskInfo: a.result, Action: "MUSIC", SubmitTime: fixture.Epoch}
	}
	return result, a.parseError
}

type taskContractBeginPool struct{ gorm.ConnPool }

func (p taskContractBeginPool) GetDBConn() (*sql.DB, error) { return p.ConnPool.(*sql.DB), nil }
func (p taskContractBeginPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.ConnPool.(gorm.TxBeginner).BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &taskContractCommitPool{ConnPool: tx, committer: tx}, nil
}

type taskContractCommitPool struct {
	gorm.ConnPool
	committer gorm.TxCommitter
}

func (p taskContractCommitPool) Commit() error {
	if err := p.committer.Rollback(); err != nil {
		return err
	}
	return errors.New("fixture commit failure")
}
func (p taskContractCommitPool) Rollback() error {
	err := p.committer.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return err
}
