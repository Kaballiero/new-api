package batch

import (
	fixture "github.com/QuantumNous/new-api/internal/contracttest/batchfixture"

	math "github.com/QuantumNous/new-api/internal/adapter/billing"

	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	batchdto "github.com/QuantumNous/new-api/internal/batch/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/batch"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"strings"
	"testing"
)

func TestBatchProviderSnapshotDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := fixture.OpenDialect(t, dialect)
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "provider_routing_"}
			require.NoError(t, db.AutoMigrate(&model.BatchJob{}))
			var version string
			query := "SELECT VERSION()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("real %s %s", dialect, version)
			snapshot := math.BatchBillingSnapshot{Provider: []byte(`{"only":["azure"],"allow_fallbacks":false}`)}
			raw, err := common.Marshal(snapshot)
			require.NoError(t, err)
			job := model.BatchJob{PublicID: "routing-snapshot", UserID: 1, IdempotencyKey: "once", BillingSnapshot: string(raw)}
			require.NoError(t, db.Create(&job).Error)
			legacy := model.BatchJob{PublicID: "legacy-snapshot", UserID: 1, IdempotencyKey: "legacy", BillingSnapshot: `{}`}
			require.NoError(t, db.Create(&legacy).Error)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
			// A new connection models recovery without the admission's in-memory
			// provider value; both old and new JSON contracts must survive startup.
			db, err = gorm.Open(db.Dialector, &gorm.Config{NamingStrategy: db.Config.NamingStrategy})
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.BatchJob{}))
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&model.BatchJob{}))
			require.NoError(t, db.First(&job, job.ID).Error)
			var restored math.BatchBillingSnapshot
			require.NoError(t, common.UnmarshalJsonStr(job.BillingSnapshot, &restored))
			assert.JSONEq(t, string(snapshot.Provider), string(restored.Provider))
			wire, err := (batch.OpenRouter{}).Create(batch.Submission{Model: "test/model", Endpoint: "/v1/embeddings", Provider: restored.Provider, Items: []batch.Item{{CustomID: "one", Body: []byte(`{"input":"hi"}`)}}})
			require.NoError(t, err)
			assert.Contains(t, string(wire.Body), `"provider":{"only":["azure"],"allow_fallbacks":false},"requests"`)
			require.NoError(t, db.First(&legacy, legacy.ID).Error)
			// Decode into a fresh value as a worker does after restart.
			var oldContract math.BatchBillingSnapshot
			require.NoError(t, common.UnmarshalJsonStr(legacy.BillingSnapshot, &oldContract))
			assert.Empty(t, oldContract.Provider)
		})
	}
}

func TestBatchUsageRequiresNativeFacts(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, body string
		valid                bool
	}{
		{"cost only", "/v1/chat/completions", `{"usage":{"cost":0.001}}`, false},
		{"total only", "/v1/chat/completions", `{"usage":{"total_tokens":10}}`, false},
		{"cache only", "/v1/messages", `{"usage":{"cache_read_input_tokens":10}}`, false},
		{"explicit zero", "/v1/chat/completions", `{"usage":{"prompt_tokens":0,"completion_tokens":0}}`, true},
		{"negative", "/v1/chat/completions", `{"usage":{"prompt_tokens":-1,"completion_tokens":0}}`, false},
		{"fractional", "/v1/chat/completions", `{"usage":{"prompt_tokens":1.5,"completion_tokens":0}}`, false},
		{"embedding", "/v1/embeddings", `{"usage":{"prompt_tokens":10,"total_tokens":10}}`, true},
		{"responses", "/v1/responses", `{"usage":{"input_tokens":2,"output_tokens":1}}`, true},
		{"claude", "/v1/messages", `{"usage":{"input_tokens":2,"output_tokens":1}}`, true},
		{"google", "/v1beta/generateContent", `{"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage, err := (Usage{}).Decode(&domain.Job{Endpoint: tc.endpoint}, domain.Result{StatusCode: 200, Body: []byte(tc.body)})
			if tc.valid {
				require.NoError(t, err)
				require.NotNil(t, usage)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestBatchFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, body, requestID, wantCode, wantRequestID string
	}{
		{"known classification", `{"error":{"code":"model_not_found","message":"private prompt Bearer sk-secret"}}`, "req-123", "model_not_found", "req-123"},
		{"numeric envelope", `{"error":{"code":400,"message":"private prompt Bearer sk-secret"}}`, "", "400", ""},
		{"numeric snapshot", `{"code":402,"message":"private prompt Bearer sk-secret"}`, "", "402", ""},
		{"out of range numeric", `{"error":{"code":999}}`, "", "provider_error", ""},
		{"boolean code", `{"error":{"code":true}}`, "", "provider_error", ""},
		{"unknown classification", `{"error":{"code":"sk-secret","message":"private prompt"}}`, "Bearer sk-secret", "provider_error", ""},
		{"secret shaped request ID", `{"error":{"code":"invalid_request"}}`, "req-sk-secret", "invalid_request", ""},
		{"malformed JSON", `private prompt Bearer sk-secret`, "", "provider_error", ""},
		{"oversized body", `{"error":{"code":"model_not_found"},"message":"` + strings.Repeat("x", 16<<10) + `private prompt"}`, strings.Repeat("x", 129), "provider_error", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := (Gateway{}).Failure("create", 400, tc.requestID, []byte(tc.body))
			assert.Less(t, len(stored), 4096)
			assert.NotContains(t, stored, "private prompt")
			assert.NotContains(t, stored, "sk-secret")
			var diagnostic model.BatchFailure
			require.NoError(t, common.UnmarshalJsonStr(stored, &diagnostic))
			assert.Equal(t, tc.wantCode, diagnostic.Code)
			assert.Equal(t, tc.wantRequestID, diagnostic.RequestID)
			job := batchdto.Job{PublicID: "batch_test", Status: "failed", LastError: stored}
			for _, response := range []any{job, &job, gin.H{"data": []batchdto.Job{job}}} {
				data, err := common.Marshal(response)
				require.NoError(t, err)
				assert.Contains(t, string(data), `"code":"batch_creation_rejected"`)
				assert.NotContains(t, string(data), `"code":"`+tc.wantCode+`"`)
				assert.NotContains(t, string(data), "request_id")
				assert.NotContains(t, string(data), "http_status")
			}
		})
	}
	// These six real rejection templates previously collapsed to numeric HTTP 400.
	for _, name := range []string{"openai/gpt-4-turbo", "openai/gpt-4.1-mini", "openai/gpt-4.1-nano", "openai/gpt-5.4-pro", "openai/gpt-5.5-pro", "openai/gpt-5.5"} {
		t.Run(name, func(t *testing.T) {
			body := fmt.Sprintf(`{"error":{"code":400,"message":"Model '%s:batch' does not have a :batch endpoint."}}`, name)
			var diagnostic model.BatchFailure
			require.NoError(t, common.UnmarshalJsonStr((Gateway{}).Failure("create", 400, "req-123", []byte(body)), &diagnostic))
			assert.Equal(t, "batch_endpoint_unavailable", diagnostic.Code)
			assert.Equal(t, "endpoint_unavailable", diagnostic.Category)
			assert.Equal(t, "The provider reported that the requested model has no Batch endpoint.", diagnostic.Message)
			assert.NotContains(t, diagnostic.Message, name)
		})
	}
	for _, tc := range []struct {
		name, body, code, category string
	}{
		{"unknown 400 is not capacity", `{"error":{"code":400,"message":"unrecognized rejection"}}`, "400", "unknown"},
		{"typed capacity", `{"error":{"code":400,"metadata":{"error_type":"provider_overloaded","raw":"Bearer sk-secret"}}}`, "provider_overloaded", "capacity"},
		{"typed parameters", `{"error":{"code":400,"metadata":{"error_type":"context_length_exceeded"}}}`, "context_length_exceeded", "invalid_request"},
		{"typed access", `{"error":{"code":403,"metadata":{"error_type":"permission_denied","raw":"private prompt"}}}`, "permission_denied", "access"},
		{"anthropic access", `{"type":"error","error":{"type":"permission_error","message":"private prompt"}}`, "permission_error", "access"},
		{"google parameters", `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"private prompt"}}`, "INVALID_ARGUMENT", "invalid_request"},
		{"resource limit is not capacity", `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`, "RESOURCE_EXHAUSTED", "rate_limit"},
		{"snapshot endpoint", `{"code":400,"message":"Model 'openai/gpt-4o:batch' does not have a :batch endpoint."}`, "batch_endpoint_unavailable", "endpoint_unavailable"},
		{"no substring guessing", `{"error":{"code":400,"message":"private prompt says Model 'openai/gpt-4o:batch' does not have a :batch endpoint."}}`, "400", "unknown"},
		{"secret in matched model discarded", `{"error":{"code":400,"message":"Model 'sk-secret:batch' does not have a :batch endpoint."}}`, "batch_endpoint_unavailable", "endpoint_unavailable"},
		{"URL and raw secrets discarded", `{"error":{"code":400,"message":"https://example.test?token=secret-token","metadata":{"error_type":"sk-secret","raw":{"Authorization":"Bearer sk-secret","prompt":"private prompt"}}}}`, "400", "unknown"},
		{"outer reason not inherited", `{"metadata":{"error_type":"provider_overloaded"},"error":{"code":400}}`, "400", "unknown"},
		{"malformed nested error", `{"error":"Bearer sk-secret"}`, "provider_error", "unknown"},
		{"oversized valid JSON", `{"error":{"code":400,"message":"Model 'openai/gpt-4o:batch' does not have a :batch endpoint."},"padding":"` + strings.Repeat("x", 16<<10) + `"}`, "provider_error", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := (Gateway{}).Failure("create", 400, "req-123", []byte(tc.body))
			var diagnostic model.BatchFailure
			require.NoError(t, common.UnmarshalJsonStr(stored, &diagnostic))
			assert.Equal(t, tc.code, diagnostic.Code)
			assert.Equal(t, tc.category, diagnostic.Category)
			assert.Less(t, len(stored), 4096)
			for _, secret := range []string{"sk-secret", "private prompt", "secret-token", "Authorization", "https://"} {
				assert.NotContains(t, stored, secret)
			}
			data, err := common.Marshal(batchdto.Job{Status: "failed", LastError: stored})
			require.NoError(t, err)
			assert.Contains(t, string(data), `"code":"batch_creation_rejected"`)
			assert.NotContains(t, string(data), "category")
			assert.NotContains(t, string(data), "req-123")
		})
	}

	for _, tc := range []struct {
		status, lastError string
		failed            bool
	}{
		{"completed", "internal transport Bearer sk-secret", false},
		{"validating", "internal transport Bearer sk-secret", false},
		{"failed", "internal transport Bearer sk-secret", true},
		{"failed", `{"version":99,"phase":"create","http_status":400,"message":"sk-secret"}`, true},
		{"failed", `{"version":1,"phase":"create","http_status":400,"message":"sk-secret"`, true},
	} {
		data, err := common.Marshal(batchdto.Job{Status: tc.status, LastError: tc.lastError})
		require.NoError(t, err)
		assert.NotContains(t, string(data), "sk-secret")
		assert.NotContains(t, string(data), "internal transport")
		if tc.failed {
			assert.Contains(t, string(data), `"code":"batch_failed"`)
		} else {
			assert.NotContains(t, string(data), `"error"`)
		}
	}
}
