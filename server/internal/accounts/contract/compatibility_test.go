package contract

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	f "github.com/QuantumNous/new-api/internal/contracttest/managementfixture"
	h "github.com/QuantumNous/new-api/internal/contracttest/managementhttp"
	tf "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type compatibilityCase struct {
	Name, Actor, Body, Variant, Path, Display string
	TokenStat                                 bool
}

func compatibilityCases() []compatibilityCase {
	cases := []compatibilityCase{}
	for _, actor := range []string{"anonymous", "user", "admin", "root", "user_pat", "admin_pat", "root_pat", "key"} {
		cases = append(cases, compatibilityCase{Name: "pat_access_" + actor, Actor: actor})
	}
	for _, variant := range []string{"dry_issue", "issue", "dry_reuse", "reuse", "reuse_padding", "blocked_empty",
		"blocked_reuse", "blocked_dry_reuse", "target_admin", "target_root", "target_status", "mismatch", "missing",
		"query_error", "update_error", "capability_missing", "integration_invalid", "principal_disabled", "principal_demoted"} {
		cases = append(cases, compatibilityCase{Name: "pat_" + variant, Actor: "admin_pat", Variant: variant})
	}
	for i, body := range []string{"", "{", "null", `[]`, `{}`, `{"apply":true}`, `{"expected_username":null}`,
		`{"expected_username":42}`, `{"expected_username":" "}`, `{"expected_username":"pat-target","apply":null}`,
		`{"expected_username":"pat-target","apply":"true"}`, `{"expected_username":"pat-target","unknown":true}`,
		`{"expected_username":"pat-target","expected_username":"other"}`,
		`{"expected_username":"pat-target","apply":false,"apply":true}`,
		`{"expected_username":"pat-target"} {}`} {
		cases = append(cases, compatibilityCase{Name: fmt.Sprintf("pat_body_%02d", i), Actor: "admin_pat", Body: body, Variant: "body"})
	}
	for _, id := range []string{"bad", "0", "-1"} {
		cases = append(cases, compatibilityCase{Name: "pat_id_" + strings.ReplaceAll(id, "-", "minus"), Actor: "admin_pat", Path: id})
	}
	for _, size := range []int{4096, 4097} {
		body := `{"expected_username":"pat-target"}`
		cases = append(cases, compatibilityCase{Name: fmt.Sprintf("pat_bytes_%d", size), Actor: "admin_pat",
			Variant: "body", Body: body + strings.Repeat(" ", size-len(body))})
	}
	for _, endpoint := range []string{"subscription", "usage"} {
		for _, tokenStat := range []bool{false, true} {
			for _, display := range []string{"USD", "CNY", "TOKENS", "CUSTOM"} {
				cases = append(cases, compatibilityCase{Name: fmt.Sprintf("billing_%s_token_%t_%s", endpoint, tokenStat, strings.ToLower(display)),
					Actor: "key", Path: "/dashboard/billing/" + endpoint, Display: display, TokenStat: tokenStat})
			}
		}
		for _, variant := range []string{"v1", "zero", "unlimited", "expiry_zero", "expired", "user_error", "token_error"} {
			cases = append(cases, compatibilityCase{Name: "billing_" + endpoint + "_" + variant, Actor: "key",
				Path: "/dashboard/billing/" + endpoint, Display: "USD", TokenStat: variant != "user_error", Variant: variant})
		}
		for _, actor := range []string{"anonymous", "user", "user_pat"} {
			cases = append(cases, compatibilityCase{Name: "billing_" + endpoint + "_access_" + actor, Actor: actor,
				Path: "/dashboard/billing/" + endpoint, Display: "USD"})
		}
	}
	return cases
}

func TestAccountCompatibilityContract(t *testing.T) {
	cases := compatibilityCases()
	names := make([]string, len(cases))
	for i := range cases {
		names[i] = cases[i].Name
	}
	f.Run(t, "TestAccountCompatibilityContract", "account-compatibility", "testdata/compatibility", names, nil,
		func(t *testing.T, name string) any {
			index := slices.IndexFunc(cases, func(tc compatibilityCase) bool { return tc.Name == name })
			require.NotEqual(t, -1, index)
			return compatibilitySnapshot(t, cases[index])
		})
}

func compatibilitySnapshot(t *testing.T, tc compatibilityCase) any {
	return h.Run(t, false, func(t *testing.T, env *h.Fixture) any {
		if strings.HasPrefix(tc.Name, "pat_") {
			return patSnapshot(t, env, tc)
		}
		return billingSnapshot(t, env, tc)
	})
}

func patSnapshot(t *testing.T, env *h.Fixture, tc compatibilityCase) any {
	const path = "/api/getapi/users/41/pat"
	target := model.User{Id: 41, Username: "pat-target", Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AffCode: "pat-target", AuthVersion: 1, Quota: 1234567, UsedQuota: 125007}
	if strings.Contains(tc.Variant, "reuse") {
		pat := "fixture-existing-personal-token"
		if tc.Variant == "reuse_padding" {
			pat += "   "
		}
		target.AccessToken = &pat
	}
	if strings.HasPrefix(tc.Variant, "blocked") {
		target.Status = common.UserStatusDisabled
	}
	if tc.Variant == "target_admin" {
		target.Role = common.RoleAdminUser
	}
	if tc.Variant == "target_root" {
		target.Role = common.RoleRootUser
	}
	if tc.Variant == "target_status" {
		target.Status = 99
	}
	require.NoError(t, model.DB.Create(&target).Error)
	configuration := `[{"integration_id":"fixture-admin","principal_user_id":2,"capabilities":["getapi.users.initialize-pat"]},` +
		`{"integration_id":"fixture-root","principal_user_id":3,"capabilities":["getapi.users.initialize-pat"]}]`
	if tc.Variant == "capability_missing" {
		configuration = `[]`
	}
	if tc.Variant == "integration_invalid" {
		configuration = `{`
	}
	t.Setenv("GETAPI_INTEGRATIONS", configuration)
	if tc.Variant == "principal_disabled" || tc.Variant == "principal_demoted" {
		column, value := "status", common.UserStatusDisabled
		if tc.Variant == "principal_demoted" {
			column, value = "role", common.RoleCommonUser
		}
		require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 2).Update(column, value).Error)
	}
	body, requestPath := `{"expected_username":"pat-target","apply":false}`, path
	if tc.Variant == "body" {
		body = tc.Body
	}
	if slices.Contains([]string{"issue", "reuse", "reuse_padding", "blocked_empty", "blocked_reuse", "update_error"}, tc.Variant) {
		body = `{"expected_username":"pat-target","apply":true}`
	}
	if tc.Variant == "mismatch" {
		body = `{"expected_username":"another"}`
	}
	if tc.Variant == "missing" {
		requestPath = "/api/getapi/users/999/pat"
	}
	if tc.Path != "" {
		requestPath = "/api/getapi/users/" + tc.Path + "/pat"
	}
	faults := 0
	if tc.Variant == "query_error" || tc.Variant == "update_error" {
		callbacks := model.DB.Callback().Query()
		if tc.Variant == "update_error" {
			callbacks = model.DB.Callback().Update()
		}
		require.NoError(t, callbacks.After("gorm:"+strings.TrimSuffix(tc.Variant, "_error")).Register("compatibility_fault", func(tx *gorm.DB) {
			user, _ := tx.Statement.Dest.(*model.User)
			if tc.Variant == "update_error" {
				user, _ = tx.Statement.Model.(*model.User)
			}
			if tx.Statement.Table == "users" && user != nil && user.Id == 41 {
				faults++
				tx.AddError(errors.New("fixture database failure"))
			}
		}))
		t.Cleanup(func() { require.NoError(t, callbacks.Remove("compatibility_fault")) })
	}
	response := env.Do(t, tc.Actor, http.MethodPost, requestPath, body)
	if tc.Variant == "query_error" || tc.Variant == "update_error" {
		require.Equal(t, 1, faults)
	}
	rows := tf.Rows(t, model.DB, "users", "id,username,role,status,quota,used_quota,auth_version,access_token,access_token_created_at", "id")
	var envelope struct {
		Data struct {
			AccessToken *string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
	state := env.Snapshot(t, map[string]any{"credentials": rows})
	if tc.Variant == "issue" {
		require.Equal(t, http.StatusCreated, response.Code)
		require.NotNil(t, envelope.Data.AccessToken)
		token := *envelope.Data.AccessToken
		decoded, err := base64.StdEncoding.DecodeString(token)
		require.NoError(t, err)
		require.Len(t, decoded, 24)
		require.Len(t, token, 32)
		require.Equal(t, token, rows[len(rows)-1]["access_token"])
		// Preserve the generated token's exact shape and its equality with the committed SQL value.
		wire, err := common.Marshal(state)
		require.NoError(t, err)
		state = json.RawMessage(strings.ReplaceAll(string(wire), token, strings.Repeat("x", len(token))))
	}
	for _, row := range tf.Rows(t, model.LOG_DB, "audit_logs", "*", "id") {
		wire, err := common.Marshal(row)
		require.NoError(t, err)
		assert.NotContains(t, string(wire), "wave6c-admin-pat")
		assert.NotContains(t, string(wire), "fixture-existing-personal-token")
		if envelope.Data.AccessToken != nil {
			assert.NotContains(t, string(wire), *envelope.Data.AccessToken)
		}
	}
	return state
}

func billingSnapshot(t *testing.T, env *h.Fixture, tc compatibilityCase) any {
	common.DisplayTokenStatEnabled = tc.TokenStat && tc.Variant != "zero"
	operation_setting.GetGeneralSetting().QuotaDisplayType = tc.Display
	operation_setting.USDExchangeRate = 7.25
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).
		Updates(map[string]any{"quota": 1234567, "used_quota": 125007}).Error)
	updates := map[string]any{"remain_quota": 765432, "used_quota": 87654, "expired_time": tf.Epoch + 3600}
	switch tc.Variant {
	case "zero":
		require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).
			Updates(map[string]any{"quota": 0, "used_quota": 0}).Error)
	case "unlimited":
		updates["unlimited_quota"] = true
	case "expiry_zero":
		updates["expired_time"] = 0
	case "expired":
		updates["expired_time"] = tf.Epoch - 1
	default:
		if tc.Variant == "v1" {
			updates["expired_time"] = -1
		}
	}
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 101).Updates(updates).Error)
	faults := 0
	if tc.Variant == "user_error" || tc.Variant == "token_error" {
		require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("compatibility_fault", func(tx *gorm.DB) {
			if tc.Variant == "user_error" && tx.Statement.Table == "users" && slices.Contains(tx.Statement.Selects, "used_quota") {
				faults++
				tx.AddError(errors.New("fixture database failure"))
			}
			if tc.Variant == "token_error" && tx.Statement.Table == "tokens" {
				where, _ := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
				for _, expression := range where.Exprs {
					if query, ok := expression.(clause.Expr); ok && query.SQL == "id = ?" && len(query.Vars) == 1 && query.Vars[0] == 101 {
						faults++
						tx.AddError(errors.New("fixture database failure"))
					}
				}
			}
		}))
		t.Cleanup(func() { require.NoError(t, model.DB.Callback().Query().Remove("compatibility_fault")) })
	}
	engine := gin.New()
	engine.Use(middleware.RequestId(), middleware.I18n())
	router.SetDashboardRouter(engine)
	path := tc.Path
	if tc.Variant == "v1" {
		path = "/v1" + path
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Language", f.Locale())
	if bearer := env.Bearers[tc.Actor]; bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if tc.Variant == "user_error" || tc.Variant == "token_error" {
		require.Equal(t, 1, faults)
	}
	headers := http.Header{}
	for _, name := range []string{"Content-Type", "Cache-Control", "Retry-After", "Location", "Vary", "Content-Disposition"} {
		if values := response.Header().Values(name); len(values) > 0 {
			headers[name] = values
		}
	}
	env.Observations = append(env.Observations, map[string]any{"path": path, "status": response.Code,
		"headers": headers, "body": response.Body.String()})
	state := env.Snapshot(t, map[string]any{"tokens": tf.Rows(t, model.DB, "tokens",
		"id,user_id,status,remain_quota,used_quota,unlimited_quota,expired_time", "id")})
	requestID := response.Header().Get(common.RequestIdKey)
	require.Regexp(t, `^[0-9]{23}[A-Za-z0-9]{16}$`, requestID)
	require.True(t, strings.HasPrefix(requestID, common.GetTimeString()))
	wire, err := common.Marshal(state)
	require.NoError(t, err)
	return json.RawMessage(strings.ReplaceAll(string(wire), requestID, "fixture-request-id"))
}
