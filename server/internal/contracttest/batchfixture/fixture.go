// Package batchfixture supplies portable Batch contract data without importing service or router.
package batchfixture

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const Key = "batchfixturekey000000000000000000000000000000000"
const ProviderKey = "batch-fixture-upstream-key"
const Model = "test/model"

// Processes isolates process-global caches and rate limits without production reset hooks.
func Processes(t *testing.T, root, flagName string, names []string, preflight, updating bool, run func(string)) {
	t.Helper()
	if selected := os.Getenv("BATCH_CONTRACT_CASE"); selected != "" {
		require.Contains(t, names, selected)
		run(selected)
		return
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+root+"$", "-test.count=1",
				"-test.timeout=120s", "-update="+strconv.FormatBool(updating), "-"+flagName+"-preflight="+strconv.FormatBool(preflight))
			command.Env = append(os.Environ(), "BATCH_CONTRACT_CASE="+name, "TZ=UTC")
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			for line := range strings.SplitSeq(string(output), "\n") {
				if strings.HasPrefix(line, "TASKS_SNAPSHOT ") {
					fmt.Println(line)
				}
			}
		})
	}
}

// SplitLogs keeps log receipts and rows on a different real SQLite database.
func SplitLogs(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "batch-logs.db")+"?_pragma=synchronous(OFF)"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	previous := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() {
		model.LOG_DB = previous
		pool, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, pool.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.AuditLog{}, &model.BatchLogReceipt{}))
}

func Migrate(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.BatchJob{}, &model.BatchItem{}, &model.BatchQuotaEvent{},
		&model.BatchLogEvent{}, &model.BatchLogReceipt{}, &model.ModelCostFXRow{}, &model.QuotaData{}, &channelrecord.Ability{}))
}

func Seed(t *testing.T, family int) *model.Channel {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("id=1").Count(&count).Error)
	if count == 0 {
		require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "batch-owner", Password: "unused", AuthVersion: 1,
			Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", Quota: 1000000, AffCode: "batch-owner"}).Error)
	}
	require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 1, Key: Key, Name: "batch-token",
		Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000000, Group: "default"}).Error)
	base := "https://batch.invalid"
	if family == constant.ChannelTypeOpenRouter {
		base += "/api"
	}
	channel := &model.Channel{Id: 71, Type: family, Key: ProviderKey, BaseURL: &base, Name: "batch-channel",
		Status: common.ChannelStatusEnabled, Models: Model, Group: "default"}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, channelstore.AddAbilities(channel, model.DB))
	return channel
}

// Snapshot uses the pre-move persisted schema, not a candidate-only production type.
func Snapshot(t *testing.T, mode string) string {
	t.Helper()
	var basis *common.OpenRouterCostBasis
	if mode != "" {
		basis = &common.OpenRouterCostBasis{Version: mode, EffectiveGroupRatio: 1, QuotaPerUnit: 500000}
	}
	value := struct {
		Provider           json.RawMessage             `json:"provider,omitempty"`
		SettlementMode     string                      `json:"settlement_mode,omitempty"`
		OpenRouterCost     *common.OpenRouterCostBasis `json:"openrouter_cost,omitempty"`
		EvaluatedAt        time.Time                   `json:"evaluated_at"`
		Client             billingexpr.BillingSnapshot `json:"client"`
		PurchaseExpression string                      `json:"purchase_expression"`
		FX                 any                         `json:"fx"`
		PureGroupRatio     float64                     `json:"pure_group_ratio"`
		Headers            map[string]string           `json:"headers"`
	}{SettlementMode: mode, OpenRouterCost: basis, EvaluatedAt: time.Now().UTC(),
		Client: billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ModelName: Model,
			ExprString: `tier("batch", p + c * 2)`, ExprVersion: 1, GroupRatio: 1, QuotaPerUnit: 500000},
		PurchaseExpression: `tier("batch", p + c * 2)`, PureGroupRatio: 1,
		Headers: map[string]string{"anthropic-version": "2023-06-01", "anthropic-beta": "fixture-beta"}}
	encoded, err := common.Marshal(value)
	require.NoError(t, err)

	return string(encoded)
}

// Reserve calls the real durable hold transaction, generating the private request identities there.
func Reserve(t *testing.T, reserve func(*domain.Job, []domain.Item) error, channel *model.Channel, configHash, endpoint, mode string, count int) *model.BatchJob {
	t.Helper()
	job := &model.BatchJob{PublicID: "batch_fixture", UserID: 1, TokenID: 11, IdempotencyKey: "fixture-once", RequestHash: "fixture-request",
		ChannelID: channel.Id, ChannelType: channel.Type, CredentialHash: Hash(ProviderKey), ChannelConfigHash: configHash,
		Model: Model, UpstreamModel: Model, UsingGroup: "default", Endpoint: endpoint, Status: "validating", BillingStatus: "reserved",
		SubmitState: "queued", BillingSnapshot: Snapshot(t, mode), RequestCount: count, ReservedQuota: int64(count * 100),
		CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(), ExpiresAt: time.Now().Unix() + 3600}
	items := make([]model.BatchItem, count)
	for i := range items {
		body := `{"messages":[],"max_tokens":8,"temperature":0,"stream":false}`
		switch endpoint {
		case "/v1/responses":
			body = `{"input":"fixture","max_output_tokens":8,"temperature":0}`
		case "/v1/messages":
			body = `{"messages":[],"max_tokens":8,"temperature":0}`
		case "/v1/embeddings":
			body = `{"input":"fixture","dimensions":0}`
		case "/v1beta/generateContent":
			body = `{"contents":[],"generationConfig":{"maxOutputTokens":8,"temperature":0}}`
		}
		items[i] = model.BatchItem{CustomID: fmt.Sprintf("client_%d", i), Ordinal: i, Body: body, UpstreamBody: body,
			BillingStatus: "reserved", ReservedQuota: 100}
	}
	require.NoError(t, reserve((*domain.Job)(job), domainItems(items)))
	return job
}

func Hash(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func State(t *testing.T) map[string]any {
	t.Helper()
	state := map[string]any{}
	for _, table := range []string{"batch_jobs", "batch_items", "batch_quota_events", "batch_log_events",
		"user_subscriptions", "subscription_pre_consume_records", "top_ups", "subscription_orders", "quota_data", "system_tasks"} {
		state[table] = taskfixture.Rows(t, model.DB, table, "*", "id")
	}
	state["batch_log_receipts"] = taskfixture.Rows(t, model.DB, "batch_log_receipts", "*", "request_id")
	state["system_task_locks"] = taskfixture.Rows(t, model.DB, "system_task_locks", "*", "type")
	state["users"] = taskfixture.Rows(t, model.DB, "users",
		"id,username,quota,used_quota,request_count,status,auth_version,batch_held_quota,batch_quota_version,group,setting", "id")
	state["tokens"] = taskfixture.Rows(t, model.DB, "tokens",
		"id,user_id,status,expired_time,remain_quota,used_quota,unlimited_quota,batch_held_quota,batch_quota_version,accessed_time", "id")
	state["channels"] = taskfixture.Rows(t, model.DB, "channels", "id,type,status,used_quota", "id")
	state["logs"] = taskfixture.Rows(t, model.LOG_DB, "logs", "*", "id")
	state["audit"] = taskfixture.Rows(t, model.LOG_DB, "audit_logs", "*", "id")
	state["main_logs"] = taskfixture.Rows(t, model.DB, "logs", "*", "id")
	state["log_receipts"] = taskfixture.Rows(t, model.LOG_DB, "batch_log_receipts", "*", "request_id")
	return state
}

// Stable validates each generated ID and all its derived identities before exact substitution.
func Stable(t *testing.T, value any, defaultKey bool) json.RawMessage {
	t.Helper()
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	if top, ok := value.(map[string]any); ok {
		if observations, ok := top["observations"].([]any); ok {
			for i, observation := range observations {
				entry, ok := observation.(map[string]any)
				if !ok {
					continue
				}
				headers, ok := entry["headers"].(http.Header)
				if !ok {
					continue
				}
				identity := headers.Get(common.RequestIdKey)
				if identity == "" || identity == "billing-contract-request" {
					continue
				}
				require.Regexp(t, `^[0-9]{23}[A-Za-z0-9]{16}$`, identity)
				encoded = []byte(strings.ReplaceAll(string(encoded), identity, fmt.Sprintf("<http-request-%d>", i)))
			}
		}
	}
	var jobs []model.BatchJob
	require.NoError(t, model.DB.Order("id").Find(&jobs).Error)
	var systemTasks []model.SystemTask
	require.NoError(t, model.DB.Order("id").Find(&systemTasks).Error)
	for _, task := range systemTasks {
		require.Regexp(t, `^systask_[A-Za-z0-9]{32}$`, task.TaskID)
		encoded = []byte(strings.ReplaceAll(string(encoded), task.TaskID, fmt.Sprintf("systask_<generated-%d>", task.ID)))
	}
	for _, job := range jobs {
		if !regexp.MustCompile(`^batch_[A-Za-z0-9]{32}$`).MatchString(job.PublicID) {
			continue
		}
		require.Regexp(t, regexp.MustCompile(`^batch_[A-Za-z0-9]{32}$`), job.PublicID)
		var items []model.BatchItem
		require.NoError(t, model.DB.Where("batch_id = ?", job.ID).Order("ordinal").Find(&items).Error)
		for _, item := range items {
			require.Equal(t, Hash(job.PublicID+":"+item.CustomID), item.ProviderCustomID)
			require.Equal(t, Hash(item.CustomID), item.CustomIDHash)
			encoded = []byte(strings.ReplaceAll(string(encoded), item.ProviderCustomID, fmt.Sprintf("<private-id-%d-%d>", job.ID, item.Ordinal)))
		}
		if defaultKey {
			require.Equal(t, Hash(job.PublicID), job.IdempotencyKey)
			encoded = []byte(strings.ReplaceAll(string(encoded), job.IdempotencyKey, fmt.Sprintf("<default-key-hash-%d>", job.ID)))
		}
		encoded = []byte(strings.ReplaceAll(string(encoded), Hash(job.PublicID+":aggregate:v2"), fmt.Sprintf("<aggregate-id-%d>", job.ID)))
		encoded = []byte(strings.ReplaceAll(string(encoded), job.PublicID, fmt.Sprintf("batch_<generated-%d>", job.ID)))
	}
	return taskfixture.StableRequestIDs(t, json.RawMessage(encoded))
}

type Outgoing struct {
	Method, URL, Body string
	Headers           http.Header
	Host              string
	ContentLength     int64
	TransferEncoding  []string
}

type Transport struct {
	T      *testing.T
	Calls  []Outgoing
	Handle func(*http.Request, []byte) (int, string, error)
}

func (f *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	require.Equal(f.T, "batch.invalid", request.URL.Host, "undeclared destination must never reach the network")
	var raw []byte
	var err error
	if request.Body != nil {
		raw, err = io.ReadAll(request.Body)
		require.NoError(f.T, err)
		require.NoError(f.T, request.Body.Close())
	}
	observation := Outgoing{Method: request.Method, URL: request.URL.String(), Headers: request.Header.Clone(), Host: request.Host,
		ContentLength: request.ContentLength, TransferEncoding: request.TransferEncoding, Body: string(raw)}
	if strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data;") {
		kind, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		require.NoError(f.T, err)
		require.Equal(f.T, "multipart/form-data", kind)
		require.Regexp(f.T, `^[0-9a-f]{60}$`, params["boundary"])
		parts := multipart.NewReader(strings.NewReader(string(raw)), params["boundary"])
		purpose, err := parts.NextPart()
		require.NoError(f.T, err)
		require.Equal(f.T, "purpose", purpose.FormName())
		data, err := io.ReadAll(purpose)
		require.NoError(f.T, err)
		require.Equal(f.T, "batch", string(data))
		file, err := parts.NextPart()
		require.NoError(f.T, err)
		require.Equal(f.T, "file", file.FormName())
		require.True(f.T, strings.HasSuffix(file.FileName(), ".jsonl"))
		_, err = io.ReadAll(file)
		require.NoError(f.T, err)
		_, err = parts.NextPart()
		require.ErrorIs(f.T, err, io.EOF)
		observation.Body = strings.ReplaceAll(observation.Body, params["boundary"], "<multipart-boundary>")
		observation.Headers.Set("Content-Type", "multipart/form-data; boundary=<multipart-boundary>")
	}
	f.Calls = append(f.Calls, observation)
	status, body, err := f.Handle(request, raw)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: http.Header{"X-Request-Id": {"req-123"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

func domainItems(items []model.BatchItem) []domain.Item {
	out := make([]domain.Item, len(items))
	for i := range items {
		out[i] = domain.Item(items[i])
	}
	return out
}
