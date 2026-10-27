// Package contract pins option HTTP, persistence and runtime publication before migration.
package contract

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	routing "github.com/QuantumNous/new-api/gateway/routing"
	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"
	"github.com/QuantumNous/new-api/internal/contracttest"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var manifest = flag.Bool("settings-manifest", false, "list settings cases without fixtures")
var preflight = flag.Bool("settings-preflight", false, "emit snapshots without oracle IO")
var originCheck = flag.Bool("settings-origin-check", false, "verify the origin recording boundary before fixtures")

func init() { fixture.RejectCandidateUpdate() }

type optionCase struct {
	Name, Kind, Actor, Method, Path, Body, Key, Value, Variant string
	Status                                                     int
}

func optionCases() []optionCase {
	cases := []optionCase{}
	for _, route := range []optionCase{
		{Name: "get", Method: "GET", Path: "/api/option/", Status: 200},
		{Name: "put", Method: "PUT", Path: "/api/option/", Body: `{"key":"Notice","value":"fixture"}`, Status: 200},
		{Name: "pricing_get", Method: "GET", Path: "/api/option/model_pricing"},
		{Name: "pricing_patch", Method: "PATCH", Path: "/api/option/model_pricing", Body: `{`},
		{Name: "preview", Method: "GET", Path: "/api/option/effective_pricing"},
		{Name: "compliance", Method: "POST", Path: "/api/option/payment_compliance", Body: `{`},
		{Name: "affinity_get", Method: "GET", Path: "/api/option/channel_affinity_cache"},
		{Name: "affinity_delete", Method: "DELETE", Path: "/api/option/channel_affinity_cache"},
		{Name: "ratio_reset", Method: "POST", Path: "/api/option/rest_model_ratio"},
		{Name: "catalog", Method: "GET", Path: "/api/option/waffo-pancake/catalog"},
		{Name: "pair", Method: "POST", Path: "/api/option/waffo-pancake/pair", Body: `{`},
		{Name: "save", Method: "POST", Path: "/api/option/waffo-pancake/save", Body: `{`},
		{Name: "product", Method: "POST", Path: "/api/option/waffo-pancake/subscription-product", Body: `{`},
		{Name: "products", Method: "GET", Path: "/api/option/waffo-pancake/subscription-product-options"},
	} {
		for _, actor := range []string{"anonymous", "key", "user", "admin", "root", "user_pat", "admin_pat", "root_pat"} {
			tc := route
			tc.Name, tc.Kind, tc.Actor = "access_"+route.Name+"_"+actor, "access", actor
			if actor == "anonymous" || actor == "key" {
				tc.Status = 401
			} else if actor != "root" && actor != "root_pat" {
				tc.Status = 403
			}
			if route.Name == "compliance" && actor == "root_pat" {
				tc.Status = 403 // Compliance confirmation requires a browser session in addition to RootAuth.
			}
			cases = append(cases, tc)
		}
	}
	for key, value := range legacyValues {
		for name, current := range legacyRuntime() {
			if _, field, ok := strings.Cut(name, "."); ok && field == key {
				if enabled, ok := current.(bool); ok {
					value = strconv.FormatBool(!enabled)
				}
			}
		}
		cases = append(cases, optionCase{Name: "legacy_" + key, Kind: "put", Key: key, Value: value})
	}
	// The production registry determines which namespaces are actually linked; no dormant package is imported.
	for key := range config.GlobalConfig.ExportAllConfigs() {
		namespace, field, _ := strings.Cut(key, ".")
		value := reflect.ValueOf(config.GlobalConfig.Get(namespace)).Elem()
		for i := range value.NumField() {
			if value.Type().Field(i).Tag.Get("json") != field {
				continue
			}
			kind := value.Field(i).Kind()
			valid := namespaceValue(key, kind)
			cases = append(cases, optionCase{Name: "namespace_" + strings.ReplaceAll(key, ".", "_"), Kind: "put", Key: key, Value: valid})
			if kind == reflect.Bool {
				cases = append(cases, optionCase{Name: "namespace_false_" + strings.ReplaceAll(key, ".", "_"),
					Kind: "put", Key: key, Value: "false"})
			}
			if kind != reflect.String {
				cases = append(cases, optionCase{Name: "namespace_bad_" + strings.ReplaceAll(key, ".", "_"),
					Kind: "put", Key: key, Value: "malformed"})
			}
		}
	}
	for name, body := range map[string]string{
		"invalid_json": `{`, "array": `[]`, "null": `null`, "missing": `{}`, "missing_value": `{"key":"Notice"}`,
		"bool_true": `{"key":"Notice","value":true}`, "bool_false": `{"key":"Notice","value":false}`,
		"zero": `{"key":"Notice","value":0}`, "fraction": `{"key":"Notice","value":1.25}`,
		"large": `{"key":"Notice","value":9007199254740993}`, "null_value": `{"key":"Notice","value":null}`,
		"object": `{"key":"Notice","value":{"a":0,"b":false}}`, "list": `{"key":"Notice","value":[0,false,"x"]}`,
		"key_number": `{"key":1,"value":"x"}`, "trailing_json": `{"key":"Notice","value":"x"} {}`,
	} {
		cases = append(cases, optionCase{Name: "coercion_" + name, Kind: "body", Body: body})
	}
	for _, key := range []string{"GitHubOAuthEnabled", "LinuxDOOAuthEnabled", "discord.enabled", "oidc.enabled",
		"EmailDomainRestrictionEnabled", "WeChatAuthEnabled", "TurnstileCheckEnabled", "TelegramOAuthEnabled"} {
		for _, value := range []string{"true", "false", "TRUE", "1"} {
			cases = append(cases, optionCase{Name: "prerequisite_" + strings.ReplaceAll(key, ".", "_") + "_" + value,
				Kind: "put", Key: key, Value: value, Variant: "unconfigured"})
		}
	}
	for _, key := range []string{"QuotaForInviter", "QuotaForInvitee"} {
		for i, value := range []string{"1", "1.25", " 2 ", "0", "-1", "invalid"} {
			for _, variant := range []string{"unconfirmed", "confirmed"} {
				cases = append(cases, optionCase{Name: fmt.Sprintf("invite_%s_%d_%s", key, i, variant),
					Kind: "put", Key: key, Value: value, Variant: variant})
			}
		}
	}
	for _, key := range []string{"GroupRatio", "group_ratio_setting.group_ratio", "GroupGroupRatio",
		"group_ratio_setting.group_group_ratio", "tool_price_setting.prices", "gemini.safety_settings",
		"claude.default_max_tokens", "ModelRequestRateLimitGroup", "AutomaticDisableStatusCodes", "AutomaticRetryStatusCodes",
		"MaxTokenAutoGroups", "ImageRatio", "AudioRatio", "AudioCompletionRatio", "CreateCacheRatio"} {
		for i, value := range []string{"null", "{", "[]", "-1", "0"} {
			cases = append(cases, optionCase{Name: fmt.Sprintf("invalid_%s_%d", strings.ReplaceAll(key, ".", "_"), i),
				Kind: "put", Key: key, Value: value})
		}
	}
	for _, key := range []string{"TaskPublicAddress", "console_setting.api_info", "console_setting.announcements",
		"console_setting.faq", "console_setting.uptime_kuma_groups", "billing_setting.billing_expr", "billing_setting.batch_billing_expr"} {
		for _, variant := range []string{"valid", "empty", "malformed", "invalid", "repeat"} {
			cases = append(cases, optionCase{Name: "validation_" + strings.ReplaceAll(key, ".", "_") + "_" + variant,
				Kind: "validation", Key: key, Variant: variant})
		}
	}
	for _, key := range []string{"Unknown.Key", "unknown.value", "general_setting.unknown", "general_setting.extra.dot", ""} {
		cases = append(cases, optionCase{Name: "unknown_" + strings.ReplaceAll(key, ".", "_"), Kind: "put", Key: key, Value: "fixture"})
	}
	for _, key := range []string{setting.TaskPluginMarketplaceSourcesKey, setting.TaskPluginDisabledFactoryKeysKey, "WaffoPayMethods"} {
		for i, value := range []string{"", "null", "[]", "malformed", `["kling","kling"," unknown "]`} {
			cases = append(cases, optionCase{Name: fmt.Sprintf("option_list_%s_%d", key, i), Kind: "put", Key: key, Value: value})
		}
	}
	for _, key := range []string{"ModelPrice", "ModelRatio", "CompletionRatio", "CacheRatio", "CreateCacheRatio",
		"ImageRatio", "AudioRatio", "AudioCompletionRatio", "GroupRatio"} {
		cases = append(cases, optionCase{Name: "explicit_zero_" + key, Kind: "put", Key: key, Value: `{"settings-model":0}`})
	}
	for _, variant := range []string{"defaults", "empty_map", "masking", "meta", "effective", "missing_slash", "wrong_method"} {
		cases = append(cases, optionCase{Name: "editor_" + variant, Kind: "editor", Variant: variant})
	}
	for _, route := range []struct{ name, path, key string }{
		{"notice", "/api/notice", "Notice"}, {"about", "/api/about", "About"},
		{"home", "/api/home_page_content", "HomePageContent"}, {"agreement", "/api/user-agreement", "legal.user_agreement"},
		{"privacy", "/api/privacy-policy", "legal.privacy_policy"},
	} {
		for _, variant := range []string{"empty", "unicode", "etag_hit", "etag_miss", "etag_list", "etag_star"} {
			cases = append(cases, optionCase{Name: "content_" + route.name + "_" + variant, Kind: "content",
				Path: route.path, Key: route.key, Variant: variant})
		}
	}
	for _, variant := range []string{"defaults", "changed", "console", "fx", "plugin_off", "factory_off", "sources", "ratio",
		"oauth_one", "oauth_multiple", "oauth_disabled", "pricing"} {
		cases = append(cases, optionCase{Name: "projection_" + variant, Kind: "projection", Variant: variant})
	}
	for _, variant := range []string{"single_create", "single_save", "single_publish", "bulk_second", "bulk_publish",
		"bulk_alias_conflict", "bulk_empty", "bulk_alias", "pricing_rollback"} {
		cases = append(cases, optionCase{Name: "failure_" + variant, Kind: "failure", Variant: variant})
	}
	for _, variant := range []string{"master", "follower", "slow", "read_error", "malformed", "unknown", "alias_first",
		"canonical_first", "alias_only", "removed", "plugin", "theme"} {
		cases = append(cases, optionCase{Name: "sync_" + variant, Kind: "sync", Variant: variant})
	}
	for _, variant := range []string{"defaults", "current_globals", "database", "malformed", "read_error"} {
		cases = append(cases, optionCase{Name: "init_" + variant, Kind: "init", Variant: variant})
	}
	slices.SortFunc(cases, func(a, b optionCase) int { return strings.Compare(a.Name, b.Name) })
	return cases
}

func namespaceValue(key string, kind reflect.Kind) string {
	switch key {
	case "group_ratio_setting.group_ratio":
		return `{"default":1.5}`
	case "group_ratio_setting.group_group_ratio":
		return `{"default":{"default":0.5}}`
	case "claude.default_max_tokens":
		return `{"default":0,"settings-model":3}`
	case "gemini.safety_settings":
		return `{"default":"BLOCK_NONE","HARM_CATEGORY_HARASSMENT":""}`
	case "console_setting.api_info", "console_setting.announcements", "console_setting.faq", "console_setting.uptime_kuma_groups":
		return "[]"
	case "billing_setting.billing_expr", "billing_setting.batch_billing_expr":
		return `{"settings-model":"tier(\"base\",p*2+c*3)"}`
	case "billing_setting.billing_mode":
		return `{"settings-model":"tiered_expr"}`
	}
	switch kind {
	case reflect.Bool:
		return "true"
	case reflect.Int, reflect.Int64:
		return "3"
	case reflect.Float64:
		return "1.25"
	case reflect.Slice:
		return "[]"
	case reflect.Map, reflect.Pointer, reflect.Struct:
		return "{}"
	default:
		return "fixture"
	}
}

func TestSettingsContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	if updating || *originCheck {
		fixture.CheckRecording(t)
	}
	if *originCheck {
		return
	}
	cases := optionCases()
	seen := map[string]bool{}
	for _, tc := range cases {
		require.False(t, seen[tc.Name], tc.Name)
		seen[tc.Name] = true
	}
	if *manifest {
		data, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("SETTINGS_MANIFEST " + string(data))
		return
	}
	if target := os.Getenv("SETTINGS_CONTRACT_LOCALES"); target != "" && os.Getenv("SETTINGS_CONTRACT_CASE") == "" {
		settingsLocales(t, target)
		return
	}
	if selected := os.Getenv("SETTINGS_CONTRACT_CASE"); selected != "" {
		for _, tc := range cases {
			if tc.Name == selected {
				runOption(t, tc, updating)
				return
			}
		}
		t.Fatalf("unknown settings case %q", selected)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel() // Each case owns a separate process and private SQLite databases.
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSettingsContract$", "-test.count=1",
				"-test.timeout=90s", "-update="+strconv.FormatBool(updating), "-settings-preflight="+strconv.FormatBool(*preflight))
			command.Env = append(os.Environ(), "SETTINGS_CONTRACT_CASE="+tc.Name)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			for line := range strings.SplitSeq(string(output), "\n") {
				if strings.HasPrefix(line, "TASKS_SNAPSHOT ") || strings.HasPrefix(line, "SETTINGS_ORDER ") {
					fmt.Println(line)
				}
				if strings.HasPrefix(line, "SETTINGS_INHERITED_BODY ") {
					fmt.Println(line)
				}
			}
		})
	}
}

type noNetwork struct{ calls int }

func (n *noNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	n.calls++
	return nil, fmt.Errorf("undeclared outbound destination: %s %s", r.Method, r.URL.Host)
}

func runOption(t *testing.T, tc optionCase, updating bool) {
	fixture.InstallDBClock(t)
	env := contracttest.New(t)
	fixture.Configure()
	common.StartTime = fixture.Epoch
	constant.Setup = true
	settingsapp.Initialize()
	httpclient.InitHttpClient()
	transport := &noNetwork{}
	httpclient.GetHttpClient().Transport = transport
	// Create the existing cache janitor outside the virtual-time bubble.
	routing.GetChannelAffinityCacheStats()
	identities := map[string]service.AuthIdentity{}
	for actor, token := range map[string]string{"user": env.User, "admin": env.Admin, "root": env.Root} {
		identity, err := service.ParseAccessToken(token)
		require.NoError(t, err)
		identities[actor] = identity
	}
	for id, pat := range map[int]string{2: "settings-admin-pat", 3: "settings-root-pat"} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id=?", id).Update("access_token", pat).Error)
	}
	synctest.Test(t, func(t *testing.T) {
		require.Equal(t, fixture.Epoch, time.Now().Unix())
		fixture.AssertDBClock(t)
		bearers := map[string]string{"anonymous": "", "key": "sk-settings-api-key", "user_pat": env.UserPAT,
			"admin_pat": "settings-admin-pat", "root_pat": "settings-root-pat"}
		for actor, identity := range identities {
			token, _, err := service.IssueAccessToken(identity)
			require.NoError(t, err)
			bearers[actor] = token
		}
		require.NoError(t, env.DB.Create(&model.Token{Id: 101, UserId: 1, Key: "settings-api-key", Name: "fixture",
			Status: 1, ExpiredTime: -1, RemainQuota: 1000000, Group: "default"}).Error)
		seedPrerequisites(t)
		registerSchema(t)
		before := optionRows(t)
		observations := []any{}
		state := map[string]any{}
		actor := tc.Actor
		if actor == "" {
			actor = "root"
		}
		request := func(method, path, body string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Accept-Language", "zh-CN")
			if language := os.Getenv("SETTINGS_CONTRACT_LANG"); language != "" && language != "default" {
				r.Header.Set("Accept-Language", language)
			} else if language == "default" {
				r.Header.Del("Accept-Language")
			}
			if body != "" {
				r.Header.Set("Content-Type", "application/json")
			}
			if bearer := bearers[actor]; bearer != "" {
				r.Header.Set("Authorization", "Bearer "+bearer)
			}
			response := env.Serve(r)
			observations = append(observations, responseValue(t, path, response))
			return response
		}
		switch tc.Kind {
		case "access":
			r := request(tc.Method, tc.Path, tc.Body)
			if tc.Status != 0 {
				assert.Equal(t, tc.Status, r.Code)
			} else {
				assert.NotContains(t, []int{401, 403}, r.Code)
			}
		case "put", "validation", "body":
			prepareOption(t, &tc)
			body := tc.Body
			if tc.Kind != "body" {
				body = optionBody(t, tc.Key, tc.Value)
			}
			response := request("PUT", "/api/option/", body)
			if tc.Kind == "validation" && (tc.Variant == "valid" || tc.Variant == "repeat") {
				assert.Contains(t, response.Body.String(), `"success":true`)
			}
			if tc.Variant == "repeat" {
				request("PUT", "/api/option/", body)
			}
			request("GET", "/api/status", "")
		case "editor":
			prepareEditor(t, tc.Variant)
			method, path := "GET", "/api/option/"
			if tc.Variant == "missing_slash" {
				path = "/api/option"
			} else if tc.Variant == "wrong_method" {
				method = "POST"
			}
			request(method, path, "")
		case "content":
			contentContract(t, env, tc, &observations)
		case "projection":
			projectionContract(t, tc, request)
		case "failure":
			state["operation"] = failureContract(t, tc)
			request("GET", "/api/status", "")
		case "sync":
			state["sync"] = syncContract(t, tc, request)
		case "init":
			initializeContract(t, tc)
			request("GET", "/api/status", "")
		default:
			t.Fatalf("unknown case kind: %s", tc.Kind)
		}
		synctest.Wait()
		state["before_options"], state["options"] = before, optionRows(t)
		state["runtime"] = runtimeValue()
		state["audit"] = fixture.Rows(t, model.LOG_DB, "audit_logs", "*", "id")
		state["users"] = fixture.Rows(t, model.DB, "users", "id,quota,used_quota,request_count", "id")
		state["tokens"] = fixture.Rows(t, model.DB, "tokens", "id,remain_quota,used_quota", "id")
		state["observations"] = observations
		state["outbound_calls"] = transport.calls
		require.Zero(t, transport.calls, "option operations must not contact a provider")
		data := canonicalIDs(t, state)
		fixture.Compare(t, "testdata/golden", tc.Name, data, *preflight, updating)
	})
}

func optionBody(t *testing.T, key string, value any) string {
	t.Helper()
	data, err := common.Marshal(map[string]any{"key": key, "value": value})
	require.NoError(t, err)
	return string(data)
}

func seedPrerequisites(t *testing.T) {
	t.Helper()
	common.GitHubClientId, common.GitHubClientSecret = "fixture-client", "fixture-secret"
	common.LinuxDOClientId, common.LinuxDOClientSecret = "fixture-client", "fixture-secret"
	common.WeChatServerAddress, common.TurnstileSiteKey = "https://wechat.invalid", "fixture-site"
	common.EmailDomainWhitelist = []string{"example.invalid"}
	for _, namespace := range []string{"discord", "oidc", "telegram"} {
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get(namespace),
			map[string]string{"client_id": "fixture-client", "client_secret": "fixture-secret"}))
	}
}

func registerSchema(t *testing.T) {
	t.Helper()
	_, err := jsplugin.DefaultRegistry.Register(`
export const meta={apiVersion:1,key:"settings-plugin",name:"Settings fixture",version:"1.0.0",
author:{name:"Test"},models:["settings-video"],fetchMode:"per_task",
usageSchema:{seconds:{type:"number",unit:"second"}}};
export function buildSubmitRequest(){return {};}
export function parseSubmitResponse(){return {};}
export function buildQueryRequest(){return {};}
export function parseTaskResult(){return {};}
`, jsplugin.Options{})
	require.NoError(t, err)
	base := "https://provider.invalid"
	settings := `{"task_plugin_key":"settings-plugin"}`
	require.NoError(t, model.DB.Create(&model.Channel{Id: 71, Type: constant.ChannelTypeTaskPlugin,
		Key: "sk-test-only-settings-provider", Status: common.ChannelStatusEnabled, Name: "fixture",
		BaseURL: &base, Models: "settings-alias", ModelMapping: common.GetPointer(`{"settings-alias":"settings-video"}`),
		Group: "default", Setting: &settings}).Error)
}

func prepareOption(t *testing.T, tc *optionCase) {
	t.Helper()
	if tc.Variant == "unconfigured" {
		common.GitHubClientId, common.LinuxDOClientId, common.WeChatServerAddress, common.TurnstileSiteKey = "", "", "", ""
		common.EmailDomainWhitelist = nil
		for _, namespace := range []string{"discord", "oidc", "telegram"} {
			require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get(namespace),
				map[string]string{"client_id": "", "client_secret": ""}))
		}
	}
	if tc.Variant == "confirmed" {
		operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	}
	if tc.Kind != "validation" {
		return
	}
	tc.Value = ""
	if tc.Variant == "malformed" {
		tc.Value = "{"
		return
	}
	if tc.Variant == "empty" {
		return
	}
	switch tc.Key {
	case "TaskPublicAddress":
		tc.Value = "https://artifacts.invalid/base"
		if tc.Variant == "invalid" {
			tc.Value = "https://user:secret@artifacts.invalid/base?query=1"
		}
	case "console_setting.api_info":
		tc.Value = `[{"url":"https://api.invalid","route":"Default","description":"Fixture","color":"blue"}]`
		if tc.Variant == "invalid" {
			tc.Value = `[{"url":"javascript:fixture"}]`
		}
	case "console_setting.announcements":
		tc.Value = `[{"content":"Fixture","type":"success","publishDate":"2000-01-01T00:00:00Z"}]`
		if tc.Variant == "invalid" {
			tc.Value = `[{"content":"<script>fixture</script>"}]`
		}
	case "console_setting.faq":
		tc.Value = `[{"question":"Question","answer":"Answer"}]`
		if tc.Variant == "invalid" {
			tc.Value = `[{"question":"Question"}]`
		}
	case "console_setting.uptime_kuma_groups":
		tc.Value = `[{"id":1,"categoryName":"Fixture","url":"https://uptime.invalid","slug":"fixture","description":""}]`
		if tc.Variant == "invalid" {
			tc.Value = `[{"id":1,"url":"file:///fixture","slug":"!"}]`
		}
	case "billing_setting.billing_expr":
		tc.Value = `{"settings-video":"tier(\"base\",u(\"seconds\")*.2)","settings-alias":"tier(\"base\",u(\"seconds\")*.1)"}`
		if tc.Variant == "invalid" {
			tc.Value = `{"settings-video":"tier(\"base\",u(\"undeclared\"))"}`
		}
	case "billing_setting.batch_billing_expr":
		tc.Value = `{"settings-model":"tier(\"batch\",provider_cost)"}`
		if tc.Variant == "invalid" {
			tc.Value = `{"settings-model":"tier(\"base\","}`
		}
	}
}

func prepareEditor(t *testing.T, variant string) {
	t.Helper()
	if variant == "effective" {
		require.NoError(t, model.UpdateModelPricingOptions(map[string]string{
			"billing_setting.billing_mode": `{"settings-model":"tiered_expr"}`,
			"billing_setting.billing_expr": `{"settings-model":"tier(\"base\",p*2)"}`,
		}))
	}
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	if variant == "empty_map" {
		common.OptionMap = map[string]string{}
	}
	if variant == "masking" {
		for _, key := range []string{"FixtureToken", "FixtureSecret", "FixtureKey", "fixture_secret", "fixture_api_key"} {
			common.OptionMap[key] = "test-only-hidden-value"
		}
		for _, key := range []string{"FixtureTOKEN", "FixtureSECRET", "FixtureKEY", "fixture_apikey", "fixture_api_key_extra"} {
			common.OptionMap[key] = "test-only-visible-value"
		}
		common.OptionMap["theme.frontend"] = "default"
	}
	if variant == "meta" {
		common.OptionMap["ModelPrice"] = `{"priced":0,"shared":1}`
		common.OptionMap["CompletionRatio"] = `{"completion":0,"shared":2}`
		common.OptionMap["ImageRatio"] = "malformed"
	}
}

func responseValue(t *testing.T, path string, r *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if identity := r.Header().Get(common.RequestIdKey); identity != "" {
		require.Regexp(t, `^[0-9]{23}[A-Za-z0-9]{16}$`, identity)
		require.True(t, strings.HasPrefix(identity, common.GetTimeString()))
	}
	body := r.Body.String()
	if path == "/api/option/rest_model_ratio" && r.Code == 200 {
		// Pricing changed its envelope field order before this wave. Pin each layout literally before comparison.
		before := `{"message":"重置模型倍率成功","success":true}`
		current := `{"success":true,"message":"重置模型倍率成功"}`
		marker, err := os.ReadFile(filepath.Join("..", "..", "..", ".tasks-origin"))
		if err == nil {
			require.Equal(t, fixture.Origin+"\n", string(marker))
			require.Equal(t, before, body)
		} else {
			require.ErrorIs(t, err, os.ErrNotExist)
			require.Equal(t, current, body)
		}
		encoded, err := common.Marshal(body)
		require.NoError(t, err)
		fmt.Println("SETTINGS_INHERITED_BODY " + os.Getenv("SETTINGS_CONTRACT_CASE") + " " + string(encoded))
		body = before
	}
	if path == "/api/option/" && r.Code == 200 && strings.Contains(body, `"data":[`) {
		var envelope struct {
			Data []json.RawMessage `json:"data"`
		}
		require.NoError(t, common.Unmarshal(r.Body.Bytes(), &envelope))
		require.NotEmpty(t, envelope.Data)
		var tail struct {
			Key string `json:"key"`
		}
		require.NoError(t, common.Unmarshal(envelope.Data[len(envelope.Data)-1], &tail))
		require.Equal(t, "CompletionRatioMeta", tail.Key)
		order := make([]string, len(envelope.Data))
		byKey := map[string]string{}
		for i, raw := range envelope.Data {
			var row model.Option
			require.NoError(t, common.Unmarshal(raw, &row))
			require.NotContains(t, byKey, row.Key, "order canonicalization must not hide duplicate options")
			byKey[row.Key], order[i] = string(raw), row.Key
		}
		fmt.Println("SETTINGS_ORDER " + os.Getenv("SETTINGS_CONTRACT_CASE") + " editor " + strings.Join(order, ","))
		original := "[" + strings.Join(rawStrings(envelope.Data), ",") + "]"
		slices.SortFunc(envelope.Data[:len(envelope.Data)-1], func(a, b json.RawMessage) int {
			var left, right model.Option
			require.NoError(t, common.Unmarshal(a, &left))
			require.NoError(t, common.Unmarshal(b, &right))
			return strings.Compare(left.Key, right.Key)
		})
		canonical := "[" + strings.Join(rawStrings(envelope.Data), ",") + "]"
		require.Equal(t, 1, strings.Count(body, original))
		body = strings.Replace(body, original, canonical, 1)
	}
	if path == "/api/status" && strings.Contains(body, `"custom_oauth_providers":[`) {
		var envelope struct {
			Data struct {
				Providers []json.RawMessage `json:"custom_oauth_providers"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(r.Body.Bytes(), &envelope))
		values := envelope.Data.Providers
		original := "[" + strings.Join(rawStrings(values), ",") + "]"
		order := []string{}
		seen := map[string]bool{}
		for _, raw := range values {
			var provider struct {
				Slug string `json:"slug"`
			}
			require.NoError(t, common.Unmarshal(raw, &provider))
			require.False(t, seen[provider.Slug])
			seen[provider.Slug] = true
			order = append(order, provider.Slug)
		}
		fmt.Println("SETTINGS_ORDER " + os.Getenv("SETTINGS_CONTRACT_CASE") + " oauth " + strings.Join(order, ","))
		slices.SortFunc(values, func(a, b json.RawMessage) int { return strings.Compare(string(a), string(b)) })
		require.Equal(t, 1, strings.Count(body, original))
		body = strings.Replace(body, original, "["+strings.Join(rawStrings(values), ",")+"]", 1)
	}
	return map[string]any{"status": r.Code, "headers": r.Header().Clone(), "body": body}
}

func rawStrings(values []json.RawMessage) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = string(value)
	}
	return out
}

func optionRows(t *testing.T) []map[string]any {
	t.Helper()
	return fixture.Rows(t, model.DB, "options", "*", "key")
}

func runtimeValue() map[string]any {
	common.OptionMapRWMutex.RLock()
	raw := maps.Clone(common.OptionMap)
	common.OptionMapRWMutex.RUnlock()
	_, plugin := jsplugin.DefaultRegistry.Generation().Get("settings-plugin")
	_, factory := jsplugin.DefaultRegistry.Generation().Get("kling")
	return map[string]any{
		"raw": raw, "legacy_effects": legacyRuntime(), "config": config.GlobalConfig.ExportAllConfigs(), "registry_enabled": jsplugin.DefaultRegistry.Enabled(),
		"plugin_available": plugin, "factory_kling_available": factory, "disabled_factory": setting.GetTaskPluginDisabledFactoryKeys(),
		"sources": setting.GetTaskPluginMarketplaceSources(), "waffo_methods": setting.GetWaffoPayMethods(),
		"model_ratio": ratio_setting.ModelRatio2JSONString(), "model_price": ratio_setting.ModelPrice2JSONString(),
		"completion": ratio_setting.CompletionRatio2JSONString(), "image": ratio_setting.ImageRatio2JSONString(),
		"audio": ratio_setting.AudioRatio2JSONString(), "audio_completion": ratio_setting.AudioCompletionRatio2JSONString(),
		"cache": ratio_setting.CacheRatio2JSONString(), "create_cache": ratio_setting.CreateCacheRatio2JSONString(),
		"group": ratio_setting.GroupRatio2JSONString(), "special_group": ratio_setting.GroupGroupRatio2JSONString(),
		"auto_groups": setting.GetAutoGroups(), "usable_groups": setting.GetUserUsableGroupsCopy(),
		"rate_group": setting.ModelRequestRateLimitGroup2JSONString(), "max_auto_groups": setting.GetMaxTokenAutoGroups(),
		"tool_price": operation_setting.GetToolPriceForModel("web_search", "settings-model"),
		"chats":      setting.Chats, "permissions": []int{common.FileUploadPermission, common.FileDownloadPermission,
			common.ImageUploadPermission, common.ImageDownloadPermission},
		"smtp": []any{common.SMTPServer, common.SMTPPort, common.SMTPAccount, common.SMTPFrom, common.SMTPToken,
			common.SMTPSSLEnabled, common.SMTPStartTLSEnabled, common.SMTPInsecureSkipVerify, common.SMTPForceAuthLogin},
		"quota": []any{common.QuotaForNewUser, common.QuotaForInviter, common.QuotaForInvitee, common.PreConsumedQuota,
			common.QuotaRemindThreshold, common.QuotaPerUnit},
		"task_address": system_setting.TaskPublicAddress, "server_address": system_setting.ServerAddress,
	}
}

func canonicalIDs(t *testing.T, state map[string]any) json.RawMessage {
	t.Helper()
	data := []byte(fixture.StableRequestIDs(t, state))
	pattern := regexp.MustCompile(`^[0-9]{23}[A-Za-z0-9]{16}$`)
	for i, observation := range state["observations"].([]any) {
		identity := observation.(map[string]any)["headers"].(http.Header).Get(common.RequestIdKey)
		if identity == "" {
			continue
		}
		require.Regexp(t, pattern, identity)
		data = []byte(strings.ReplaceAll(string(data), identity, fmt.Sprintf("<http-request-id-%d>", i)))
	}
	return json.RawMessage(data)
}

func contentContract(t *testing.T, env *contracttest.Env, tc optionCase, observations *[]any) {
	t.Helper()
	content := "<b>Привет 世界</b>\nfixture"
	if tc.Variant == "empty" {
		content = ""
	}
	require.NoError(t, settingsapp.Default().Write.Execute(tc.Key, content))
	first := env.Serve(httptest.NewRequest("GET", tc.Path, nil))
	assert.Equal(t, 200, first.Code)
	*observations = append(*observations, responseValue(t, tc.Path, first))
	if !strings.HasPrefix(tc.Variant, "etag_") {
		return
	}
	request := httptest.NewRequest("GET", tc.Path, nil)
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag)
	switch tc.Variant {
	case "etag_hit":
		request.Header.Set("If-None-Match", etag)
	case "etag_miss":
		request.Header.Set("If-None-Match", `W/"missing"`)
	case "etag_list":
		request.Header.Set("If-None-Match", `"missing", `+etag)
	case "etag_star":
		request.Header.Set("If-None-Match", "*")
	}
	response := env.Serve(request)
	if tc.Variant == "etag_miss" {
		assert.Equal(t, 200, response.Code)
	} else {
		assert.Equal(t, 304, response.Code)
		assert.Empty(t, response.Body.String())
	}
	*observations = append(*observations, responseValue(t, tc.Path, response))
}

func projectionContract(t *testing.T, tc optionCase, request func(string, string, string) *httptest.ResponseRecorder) {
	t.Helper()
	switch tc.Variant {
	case "changed":
		require.NoError(t, settingsapp.Default().Bulk.Execute(context.Background(), map[string]string{"SystemName": "Fixture Ω", "Footer": "<b>Footer</b>",
			"Logo": "https://logo.invalid/icon", "ServerAddress": "https://api.invalid", "RegisterEnabled": "false",
			"HeaderNavModules": `{"pricing":{"enabled":false}}`, "SidebarModulesAdmin": `{"token":false}`,
			"legal.user_agreement": "Terms", "general_setting.quota_display_type": "TOKENS"}))
	case "console":
		require.NoError(t, settingsapp.Default().Bulk.Execute(context.Background(), map[string]string{"console_setting.api_info_enabled": "false",
			"console_setting.announcements_enabled": "true", "console_setting.faq_enabled": "true",
			"console_setting.announcements": `[{"content":"Fixture","type":"success","publishDate":"2000-01-01T00:00:00Z"}]`,
			"console_setting.faq":           `[{"question":"Question","answer":"Answer"}]`}))
	case "fx":
		require.NoError(t, model.DB.Create(&model.ModelCostFXRow{Source: "cbr-xml-daily.ru", EffectiveAt: fixture.Epoch,
			FetchedAt: fixture.Epoch, Version: 1, RatesJSON: `{"USD":100}`}).Error)
		require.NoError(t, model.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
	case "plugin_off":
		require.NoError(t, settingsapp.Default().Write.Execute("TaskPluginEnabled", "false"))
	case "factory_off":
		require.NoError(t, settingsapp.Default().Write.Execute(setting.TaskPluginDisabledFactoryKeysKey, `["kling"]`))
	case "sources":
		require.NoError(t, settingsapp.Default().Write.Execute(setting.TaskPluginMarketplaceSourcesKey, "null"))
	case "ratio":
		require.NoError(t, settingsapp.Default().Write.Execute("ExposeRatioEnabled", "true"))
		request("GET", "/api/ratio_config", "")
		require.NoError(t, settingsapp.Default().Write.Execute("ModelRatio", `{"settings-model":0.25}`))
		request("GET", "/api/ratio_config", "")
	case "pricing":
		request("PUT", "/api/option/", optionBody(t, "ModelPrice", `{"settings-alias":0.25}`))
		request("GET", "/api/option/model_pricing", "")
		request("PUT", "/api/option/", optionBody(t, "ModelPrice", `{ "settings-alias":0.25 }`))
		request("GET", "/api/option/model_pricing", "")
	case "oauth_one", "oauth_multiple", "oauth_disabled":
		count := 1
		if tc.Variant == "oauth_multiple" {
			count = 3
		}
		for i := range count {
			oauth.RegisterOrUpdateCustomProvider(&model.CustomOAuthProvider{Id: 100 + i, Name: "Fixture " + strconv.Itoa(i),
				Slug: "settings-provider-" + strconv.Itoa(i), Icon: "fixture", Enabled: tc.Variant != "oauth_disabled",
				ClientId: "fixture-client", ClientSecret: "test-only-provider-secret", Scopes: "openid profile",
				AuthorizationEndpoint: "https://oauth.invalid/authorize", TokenEndpoint: "https://oauth.invalid/token"})
		}
	}
	request("GET", "/api/status", "")
	if tc.Variant == "plugin_off" || tc.Variant == "factory_off" {
		request("GET", "/api/task_plugin_options", "")
	}
}

func failureContract(t *testing.T, tc optionCase) map[string]any {
	t.Helper()
	var operationError error
	writes := 0
	if tc.Variant == "single_save" {
		require.NoError(t, model.DB.Create(&model.Option{Key: "Footer", Value: "before"}).Error)
	}
	failCreate := tc.Variant == "single_create"
	failUpdate := tc.Variant == "single_save" || tc.Variant == "bulk_second" || tc.Variant == "pricing_rollback"
	if failCreate {
		require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("settings_fault", func(tx *gorm.DB) {
			if tx.Statement.Table == "options" {
				tx.AddError(errors.New("fixture option create failure"))
			}
		}))
		defer func() { require.NoError(t, model.DB.Callback().Create().Remove("settings_fault")) }()
	}
	if failUpdate {
		require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("settings_fault", func(tx *gorm.DB) {
			if tx.Statement.Table != "options" {
				return
			}
			writes++
			if tc.Variant == "single_save" || writes == 2 {
				tx.AddError(errors.New("fixture option save failure"))
			}
		}))
		defer func() { require.NoError(t, model.DB.Callback().Update().Remove("settings_fault")) }()
	}
	switch tc.Variant {
	case "single_create", "single_save":
		operationError = settingsapp.Default().Write.Execute("Footer", "after")
		require.NoError(t, operationError, "legacy SQL errors must remain ignored")
	case "single_publish":
		operationError = settingsapp.Default().Write.Execute("Chats", "{")
		require.Error(t, operationError)
	case "bulk_second":
		operationError = settingsapp.Default().Bulk.Execute(context.Background(), map[string]string{"About": "after", "Notice": "after"})
		require.Error(t, operationError)
	case "bulk_publish":
		operationError = settingsapp.Default().Bulk.Execute(context.Background(), map[string]string{"About": "after", "Chats": "{"})
		require.Error(t, operationError)
	case "bulk_alias_conflict":
		operationError = settingsapp.Default().Bulk.Execute(context.Background(), map[string]string{"GroupRatio": `{"default":1}`, "group_ratio_setting.group_ratio": `{}`})
		require.Error(t, operationError)
	case "bulk_empty":
		operationError = settingsapp.Default().Bulk.Execute(context.Background(), nil)
		require.NoError(t, operationError)
	case "bulk_alias":
		input := map[string]string{"group_ratio_setting.group_ratio": `{"default":1.5}`, "About": "after"}
		before := maps.Clone(input)
		operationError = settingsapp.Default().Bulk.Execute(context.Background(), input)
		require.NoError(t, operationError)
		assert.Equal(t, before, input)
	case "pricing_rollback":
		operationError = settingsapp.Default().Write.Execute("ModelRatio", `{"settings-model":2}`)
		require.Error(t, operationError)
	}
	message := ""
	if operationError != nil {
		message = operationError.Error()
	}
	return map[string]any{"error": message, "writes": writes}
}

func syncContract(t *testing.T, tc optionCase, request func(string, string, string) *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	rows := []model.Option{{Key: "Notice", Value: "first"}}
	switch tc.Variant {
	case "alias_first":
		rows = append(rows, model.Option{Key: "group_ratio_setting.group_ratio", Value: `{"default":2}`},
			model.Option{Key: "GroupRatio", Value: `{"default":1.5}`})
	case "canonical_first":
		rows = append(rows, model.Option{Key: "GroupRatio", Value: `{"default":1.5}`},
			model.Option{Key: "group_ratio_setting.group_ratio", Value: `{"default":2}`})
	case "alias_only":
		rows = append(rows, model.Option{Key: "group_ratio_setting.group_ratio", Value: `{"default":2}`})
	case "unknown":
		rows = append(rows, model.Option{Key: "unknown.namespace", Value: "opaque"})
	case "malformed":
		rows = append(rows, model.Option{Key: "Chats", Value: "{"},
			model.Option{Key: "tool_price_setting.prices", Value: `{"web_search":0,"invalid":"x"}`})
	case "plugin":
		rows = append(rows, model.Option{Key: "TaskPluginEnabled", Value: "false"},
			model.Option{Key: setting.TaskPluginDisabledFactoryKeysKey, Value: `["kling"]`})
	case "theme":
		rows = append(rows, model.Option{Key: "theme.frontend", Value: "classic"})
	}
	require.NoError(t, model.DB.Create(&rows).Error)
	common.IsMasterNode = tc.Variant != "follower"
	type stopped struct{}
	stop := &stopped{}
	times := []int64{}
	observedInputs := 0
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("settings_loop", func(tx *gorm.DB) {
		if tx.Statement.Table == "models" {
			observedInputs++
		}
		if tx.Statement.Table != "options" {
			return
		}
		times = append(times, time.Now().Unix()-fixture.Epoch)
		if len(times) == 3 {
			panic(stop)
		}
		if tc.Variant == "slow" && len(times) == 1 {
			time.Sleep(2 * time.Second)
		}
		if tc.Variant == "read_error" {
			tx.AddError(errors.New("fixture option read failure"))
		}
	}))
	defer func() { require.NoError(t, model.DB.Callback().Query().Remove("settings_loop")) }()
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		settingsapp.SyncOptions(1)
	}()
	synctest.Wait()
	require.Empty(t, times, "sync must sleep before its first query")
	firstDelay := time.Second
	if tc.Variant == "slow" {
		firstDelay += 2 * time.Second
	}
	time.Sleep(firstDelay)
	synctest.Wait()
	first := syncValue()
	request("GET", "/api/status", "")
	if tc.Variant == "removed" {
		require.NoError(t, model.DB.Delete(&model.Option{Key: "Notice"}).Error)
	} else {
		require.NoError(t, model.DB.Model(&model.Option{}).Where("key = ?", "Notice").Update("value", "second").Error)
	}
	time.Sleep(time.Second)
	synctest.Wait()
	second := syncValue()
	time.Sleep(time.Second)
	assert.Equal(t, stop, <-done)
	if tc.Variant == "slow" {
		assert.Equal(t, []int64{1, 4, 5}, times)
	} else {
		assert.Equal(t, []int64{1, 2, 3}, times)
	}
	assert.Equal(t, 2, observedInputs, "pricing observation must run after each reload, including failures")
	return map[string]any{"query_times": times, "first": first, "second": second, "pricing_observations": observedInputs}
}

func syncValue() map[string]any {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return map[string]any{"notice": common.OptionMap["Notice"], "unknown": common.OptionMap["unknown.namespace"],
		"group": ratio_setting.GroupRatio2JSONString(), "registry_enabled": jsplugin.DefaultRegistry.Enabled()}
}

func initializeContract(t *testing.T, tc optionCase) {
	t.Helper()
	switch tc.Variant {
	case "current_globals":
		common.SystemName, common.SMTPServer, common.QuotaForNewUser = "Current name", "smtp.invalid", 77
	case "database":
		require.NoError(t, model.DB.Create(&[]model.Option{{Key: "Notice", Value: "persisted"},
			{Key: "unknown.namespace", Value: "opaque"}}).Error)
	case "malformed":
		require.NoError(t, model.DB.Create(&model.Option{Key: "gemini.safety_settings", Value: "{"}).Error)
	case "read_error":
		require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("settings_init", func(tx *gorm.DB) {
			if tx.Statement.Table == "options" {
				tx.AddError(errors.New("fixture option read failure"))
			}
		}))
		defer func() { require.NoError(t, model.DB.Callback().Query().Remove("settings_init")) }()
	}
	settingsapp.Initialize()
	settingsapp.Initialize()
}

func settingsLocales(t *testing.T, target string) {
	t.Helper()
	values := map[string]json.RawMessage{}
	for _, language := range []string{"default", "en", "ru", "zh-CN", "zh-TW", "unknown"} {
		for _, name := range []string{"access_get_anonymous", "access_get_admin", "coercion_invalid_json",
			"prerequisite_GitHubOAuthEnabled_true", "validation_billing_setting_billing_expr_malformed", "content_notice_unicode"} {
			t.Run(language+"/"+name, func(t *testing.T) {
				command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSettingsContract$", "-test.count=1",
					"-test.timeout=90s", "-update=false", "-settings-preflight")
				command.Env = append(os.Environ(), "SETTINGS_CONTRACT_CASE="+name, "SETTINGS_CONTRACT_LANG="+language)
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
				found := 0
				for line := range strings.SplitSeq(string(output), "\n") {
					if after, ok := strings.CutPrefix(line, "TASKS_SNAPSHOT "+name+" "); ok {
						values[language+"/"+name] = json.RawMessage(after)
						found++
					}
				}
				require.Equal(t, 1, found)
			})
		}
	}
	data, err := common.Marshal(values)
	require.NoError(t, err)
	stream, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	require.NoError(t, err)
	_, err = stream.Write(data)
	require.NoError(t, err)
	require.NoError(t, stream.Close())
}

// Legacy options retain their existing type coercion and parser semantics.
var legacyValues = map[string]string{
	"AudioCompletionRatio":                 "{\"settings-model\":0.5}",
	"AudioRatio":                           "{\"settings-model\":0.5}",
	"AutoGroups":                           "[\"default\"]",
	"AutomaticDisableChannelEnabled":       "true",
	"AutomaticDisableKeywords":             "fixture",
	"AutomaticDisableStatusCodes":          "400, 500-502",
	"AutomaticEnableChannelEnabled":        "true",
	"AutomaticRetryStatusCodes":            "500-502",
	"CacheRatio":                           "{\"settings-model\":0.5}",
	"ChannelDisableThreshold":              "3",
	"Chats":                                "[]",
	"CheckSensitiveEnabled":                "true",
	"CheckSensitiveOnPromptEnabled":        "true",
	"CompletionRatio":                      "{\"settings-model\":0.5}",
	"CreateCacheRatio":                     "{\"settings-model\":0.5}",
	"CreemApiKey":                          "fixture",
	"CreemProducts":                        "fixture",
	"CreemTestMode":                        "fixture",
	"CreemWebhookSecret":                   "fixture",
	"CustomCallbackAddress":                "fixture",
	"DataExportDefaultTime":                "fixture",
	"DataExportEnabled":                    "true",
	"DataExportInterval":                   "3",
	"DefaultCollapseSidebar":               "true",
	"DefaultUseAutoGroup":                  "true",
	"DemoSiteEnabled":                      "true",
	"DisplayInCurrencyEnabled":             "true",
	"DisplayTokenStatEnabled":              "true",
	"DrawingEnabled":                       "true",
	"EmailAliasRestrictionEnabled":         "true",
	"EmailDomainRestrictionEnabled":        "true",
	"EmailDomainWhitelist":                 "fixture",
	"EmailVerificationEnabled":             "true",
	"EpayId":                               "fixture",
	"EpayKey":                              "fixture",
	"ExposeRatioEnabled":                   "true",
	"FileDownloadPermission":               "3",
	"FileUploadPermission":                 "3",
	"Footer":                               "fixture",
	"GitHubClientId":                       "fixture",
	"GitHubClientSecret":                   "fixture",
	"GitHubOAuthEnabled":                   "true",
	"GroupGroupRatio":                      "{\"default\":{\"default\":0.5}}",
	"GroupRatio":                           "{\"default\":1.5}",
	"ImageDownloadPermission":              "3",
	"ImageRatio":                           "{\"settings-model\":0.5}",
	"ImageUploadPermission":                "3",
	"LinuxDOClientId":                      "fixture",
	"LinuxDOClientSecret":                  "fixture",
	"LinuxDOMinimumTrustLevel":             "3",
	"LinuxDOOAuthEnabled":                  "true",
	"LogConsumeEnabled":                    "true",
	"Logo":                                 "fixture",
	"MaxTokenAutoGroups":                   "3",
	"MinTopUp":                             "3",
	"MjAccountFilterEnabled":               "true",
	"MjActionCheckSuccessEnabled":          "true",
	"MjForwardUrlEnabled":                  "true",
	"MjModeClearEnabled":                   "true",
	"MjNotifyEnabled":                      "true",
	"ModelPrice":                           "{\"settings-model\":0.5}",
	"ModelRatio":                           "{\"settings-model\":0.5}",
	"ModelRequestRateLimitCount":           "3",
	"ModelRequestRateLimitDurationMinutes": "3",
	"ModelRequestRateLimitEnabled":         "true",
	"ModelRequestRateLimitGroup":           "{\"default\":[2,3]}",
	"ModelRequestRateLimitSuccessCount":    "3",
	"PasswordLoginEnabled":                 "true",
	"PasswordRegisterEnabled":              "true",
	"PayAddress":                           "fixture",
	"PayMethods":                           "[]",
	"PreConsumedQuota":                     "3",
	"Price":                                "3",
	"QuotaForInvitee":                      "3",
	"QuotaForInviter":                      "3",
	"QuotaForNewUser":                      "3",
	"QuotaPerUnit":                         "3",
	"QuotaRemindThreshold":                 "3",
	"RegisterEnabled":                      "true",
	"RetryTimes":                           "3",
	"SMTPAccount":                          "fixture",
	"SMTPForceAuthLogin":                   "true",
	"SMTPFrom":                             "fixture",
	"SMTPInsecureSkipVerify":               "true",
	"SMTPPort":                             "3",
	"SMTPSSLEnabled":                       "true",
	"SMTPServer":                           "fixture",
	"SMTPStartTLSEnabled":                  "true",
	"SMTPToken":                            "fixture",
	"SelfUseModeEnabled":                   "true",
	"SensitiveWords":                       "fixture",
	"ServerAddress":                        "https://api.invalid",
	"StopOnSensitiveEnabled":               "true",
	"StreamCacheQueueLength":               "3",
	"StripeApiSecret":                      "fixture",
	"StripeMinTopUp":                       "3",
	"StripePriceId":                        "fixture",
	"StripePromotionCodesEnabled":          "true",
	"StripeUnitPrice":                      "3",
	"StripeWebhookSecret":                  "fixture",
	"SystemName":                           "fixture",
	"TaskEnabled":                          "true",
	"TaskPluginDisabledFactoryKeys":        "[]",
	"TaskPluginEnabled":                    "true",
	"TaskPluginMarketplaceSources":         "[]",
	"TaskPublicAddress":                    "https://artifacts.invalid/base",
	"TelegramBotName":                      "fixture",
	"TelegramBotToken":                     "fixture",
	"TelegramOAuthEnabled":                 "true",
	"TopUpLink":                            "fixture",
	"TopupGroupRatio":                      "{\"default\":1.2}",
	"TurnstileCheckEnabled":                "true",
	"TurnstileSecretKey":                   "fixture",
	"TurnstileSiteKey":                     "fixture",
	"USDExchangeRate":                      "3",
	"UserUsableGroups":                     "{\"default\":\"Default\"}",
	"WaffoApiKey":                          "fixture",
	"WaffoCurrency":                        "fixture",
	"WaffoEnabled":                         "true",
	"WaffoMerchantId":                      "fixture",
	"WaffoMinTopUp":                        "3",
	"WaffoNotifyUrl":                       "fixture",
	"WaffoPancakeMerchantID":               "fixture",
	"WaffoPancakeMinTopUp":                 "3",
	"WaffoPancakePrivateKey":               "fixture",
	"WaffoPancakeProductID":                "fixture",
	"WaffoPancakeReturnURL":                "fixture",
	"WaffoPancakeStoreID":                  "fixture",
	"WaffoPancakeUnitPrice":                "3",
	"WaffoPayMethods":                      "[]",
	"WaffoPrivateKey":                      "fixture",
	"WaffoPublicCert":                      "fixture",
	"WaffoReturnUrl":                       "fixture",
	"WaffoSandbox":                         "fixture",
	"WaffoSandboxApiKey":                   "fixture",
	"WaffoSandboxPrivateKey":               "fixture",
	"WaffoSandboxPublicCert":               "fixture",
	"WaffoSubscriptionReturnUrl":           "fixture",
	"WaffoUnitPrice":                       "3",
	"WeChatAccountQRCodeImageURL":          "fixture",
	"WeChatAuthEnabled":                    "true",
	"WeChatServerAddress":                  "fixture",
	"WeChatServerToken":                    "fixture",
	"WorkerAllowHttpImageRequestEnabled":   "true",
	"WorkerUrl":                            "fixture",
	"WorkerValidKey":                       "fixture",
}

// Legacy globals are observed directly, so persisted bytes cannot conceal a missed runtime assignment.
func legacyRuntime() map[string]any {
	return map[string]any{
		"common.AutomaticDisableChannelEnabled":             common.AutomaticDisableChannelEnabled,
		"common.AutomaticEnableChannelEnabled":              common.AutomaticEnableChannelEnabled,
		"common.ChannelDisableThreshold":                    common.ChannelDisableThreshold,
		"common.DataExportDefaultTime":                      common.DataExportDefaultTime,
		"common.DataExportEnabled":                          common.DataExportEnabled,
		"common.DataExportInterval":                         common.DataExportInterval,
		"common.DefaultCollapseSidebar":                     common.DefaultCollapseSidebar,
		"common.DisplayTokenStatEnabled":                    common.DisplayTokenStatEnabled,
		"common.DrawingEnabled":                             common.DrawingEnabled,
		"common.EmailAliasRestrictionEnabled":               common.EmailAliasRestrictionEnabled,
		"common.EmailDomainRestrictionEnabled":              common.EmailDomainRestrictionEnabled,
		"common.EmailDomainWhitelist":                       common.EmailDomainWhitelist,
		"common.EmailVerificationEnabled":                   common.EmailVerificationEnabled,
		"common.FileDownloadPermission":                     common.FileDownloadPermission,
		"common.FileUploadPermission":                       common.FileUploadPermission,
		"common.Footer":                                     common.Footer,
		"common.GitHubClientId":                             common.GitHubClientId,
		"common.GitHubClientSecret":                         common.GitHubClientSecret,
		"common.GitHubOAuthEnabled":                         common.GitHubOAuthEnabled,
		"common.ImageDownloadPermission":                    common.ImageDownloadPermission,
		"common.ImageUploadPermission":                      common.ImageUploadPermission,
		"common.LinuxDOClientId":                            common.LinuxDOClientId,
		"common.LinuxDOClientSecret":                        common.LinuxDOClientSecret,
		"common.LinuxDOMinimumTrustLevel":                   common.LinuxDOMinimumTrustLevel,
		"common.LinuxDOOAuthEnabled":                        common.LinuxDOOAuthEnabled,
		"common.LogConsumeEnabled":                          common.LogConsumeEnabled,
		"common.Logo":                                       common.Logo,
		"common.PasswordLoginEnabled":                       common.PasswordLoginEnabled,
		"common.PasswordRegisterEnabled":                    common.PasswordRegisterEnabled,
		"common.PreConsumedQuota":                           common.PreConsumedQuota,
		"common.QuotaForInvitee":                            common.QuotaForInvitee,
		"common.QuotaForInviter":                            common.QuotaForInviter,
		"common.QuotaForNewUser":                            common.QuotaForNewUser,
		"common.QuotaPerUnit":                               common.QuotaPerUnit,
		"common.QuotaRemindThreshold":                       common.QuotaRemindThreshold,
		"common.RegisterEnabled":                            common.RegisterEnabled,
		"common.RetryTimes":                                 common.RetryTimes,
		"common.SMTPAccount":                                common.SMTPAccount,
		"common.SMTPForceAuthLogin":                         common.SMTPForceAuthLogin,
		"common.SMTPFrom":                                   common.SMTPFrom,
		"common.SMTPInsecureSkipVerify":                     common.SMTPInsecureSkipVerify,
		"common.SMTPPort":                                   common.SMTPPort,
		"common.SMTPSSLEnabled":                             common.SMTPSSLEnabled,
		"common.SMTPServer":                                 common.SMTPServer,
		"common.SMTPStartTLSEnabled":                        common.SMTPStartTLSEnabled,
		"common.SMTPToken":                                  common.SMTPToken,
		"common.SystemName":                                 common.SystemName,
		"common.TaskEnabled":                                common.TaskEnabled,
		"common.TelegramBotName":                            common.TelegramBotName,
		"common.TelegramBotToken":                           common.TelegramBotToken,
		"common.TelegramOAuthEnabled":                       common.TelegramOAuthEnabled,
		"common.TopUpLink":                                  common.TopUpLink,
		"common.TurnstileCheckEnabled":                      common.TurnstileCheckEnabled,
		"common.TurnstileSecretKey":                         common.TurnstileSecretKey,
		"common.TurnstileSiteKey":                           common.TurnstileSiteKey,
		"common.WeChatAccountQRCodeImageURL":                common.WeChatAccountQRCodeImageURL,
		"common.WeChatAuthEnabled":                          common.WeChatAuthEnabled,
		"common.WeChatServerAddress":                        common.WeChatServerAddress,
		"common.WeChatServerToken":                          common.WeChatServerToken,
		"constant.TaskPluginEnabled":                        constant.TaskPluginEnabled,
		"operation_setting.CustomCallbackAddress":           operation_setting.CustomCallbackAddress,
		"operation_setting.DemoSiteEnabled":                 operation_setting.DemoSiteEnabled,
		"operation_setting.EpayId":                          operation_setting.EpayId,
		"operation_setting.EpayKey":                         operation_setting.EpayKey,
		"operation_setting.MinTopUp":                        operation_setting.MinTopUp,
		"operation_setting.PayAddress":                      operation_setting.PayAddress,
		"operation_setting.Price":                           operation_setting.Price,
		"operation_setting.SelfUseModeEnabled":              operation_setting.SelfUseModeEnabled,
		"operation_setting.USDExchangeRate":                 operation_setting.USDExchangeRate,
		"setting.CheckSensitiveEnabled":                     setting.CheckSensitiveEnabled,
		"setting.CheckSensitiveOnPromptEnabled":             setting.CheckSensitiveOnPromptEnabled,
		"setting.CreemApiKey":                               setting.CreemApiKey,
		"setting.CreemProducts":                             setting.CreemProducts,
		"setting.CreemTestMode":                             setting.CreemTestMode,
		"setting.CreemWebhookSecret":                        setting.CreemWebhookSecret,
		"setting.DefaultUseAutoGroup":                       setting.DefaultUseAutoGroup,
		"setting.MjAccountFilterEnabled":                    setting.MjAccountFilterEnabled,
		"setting.MjActionCheckSuccessEnabled":               setting.MjActionCheckSuccessEnabled,
		"setting.MjForwardUrlEnabled":                       setting.MjForwardUrlEnabled,
		"setting.MjModeClearEnabled":                        setting.MjModeClearEnabled,
		"setting.MjNotifyEnabled":                           setting.MjNotifyEnabled,
		"setting.ModelRequestRateLimitCount":                setting.ModelRequestRateLimitCount,
		"setting.ModelRequestRateLimitDurationMinutes":      setting.ModelRequestRateLimitDurationMinutes,
		"setting.ModelRequestRateLimitEnabled":              setting.ModelRequestRateLimitEnabled,
		"setting.ModelRequestRateLimitSuccessCount":         setting.ModelRequestRateLimitSuccessCount,
		"setting.StopOnSensitiveEnabled":                    setting.StopOnSensitiveEnabled,
		"setting.StreamCacheQueueLength":                    setting.StreamCacheQueueLength,
		"setting.StripeApiSecret":                           setting.StripeApiSecret,
		"setting.StripeMinTopUp":                            setting.StripeMinTopUp,
		"setting.StripePriceId":                             setting.StripePriceId,
		"setting.StripePromotionCodesEnabled":               setting.StripePromotionCodesEnabled,
		"setting.StripeUnitPrice":                           setting.StripeUnitPrice,
		"setting.StripeWebhookSecret":                       setting.StripeWebhookSecret,
		"setting.WaffoApiKey":                               setting.WaffoApiKey,
		"setting.WaffoCurrency":                             setting.WaffoCurrency,
		"setting.WaffoEnabled":                              setting.WaffoEnabled,
		"setting.WaffoMerchantId":                           setting.WaffoMerchantId,
		"setting.WaffoMinTopUp":                             setting.WaffoMinTopUp,
		"setting.WaffoNotifyUrl":                            setting.WaffoNotifyUrl,
		"setting.WaffoPancakeMerchantID":                    setting.WaffoPancakeMerchantID,
		"setting.WaffoPancakeMinTopUp":                      setting.WaffoPancakeMinTopUp,
		"setting.WaffoPancakePrivateKey":                    setting.WaffoPancakePrivateKey,
		"setting.WaffoPancakeProductID":                     setting.WaffoPancakeProductID,
		"setting.WaffoPancakeReturnURL":                     setting.WaffoPancakeReturnURL,
		"setting.WaffoPancakeStoreID":                       setting.WaffoPancakeStoreID,
		"setting.WaffoPancakeUnitPrice":                     setting.WaffoPancakeUnitPrice,
		"setting.WaffoPrivateKey":                           setting.WaffoPrivateKey,
		"setting.WaffoPublicCert":                           setting.WaffoPublicCert,
		"setting.WaffoReturnUrl":                            setting.WaffoReturnUrl,
		"setting.WaffoSandbox":                              setting.WaffoSandbox,
		"setting.WaffoSandboxApiKey":                        setting.WaffoSandboxApiKey,
		"setting.WaffoSandboxPrivateKey":                    setting.WaffoSandboxPrivateKey,
		"setting.WaffoSandboxPublicCert":                    setting.WaffoSandboxPublicCert,
		"setting.WaffoSubscriptionReturnUrl":                setting.WaffoSubscriptionReturnUrl,
		"setting.WaffoUnitPrice":                            setting.WaffoUnitPrice,
		"system_setting.ServerAddress":                      system_setting.ServerAddress,
		"system_setting.TaskPublicAddress":                  system_setting.TaskPublicAddress,
		"system_setting.WorkerAllowHttpImageRequestEnabled": system_setting.WorkerAllowHttpImageRequestEnabled,
		"system_setting.WorkerUrl":                          system_setting.WorkerUrl,
		"system_setting.WorkerValidKey":                     system_setting.WorkerValidKey,
	}
}
