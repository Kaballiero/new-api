// Package contract pins task-plugin API access, state and outbound bytes before migration.
package contract

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"
	pluginsapp "github.com/QuantumNous/new-api/internal/app/taskplugins"
	"github.com/QuantumNous/new-api/internal/contracttest"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/pluginfixture"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var manifest = flag.Bool("plugins-manifest", false, "list plugin cases without fixtures")
var preflight = flag.Bool("plugins-preflight", false, "emit snapshots without oracle IO")
var originCheck = flag.Bool("plugins-origin-check", false, "check verified recording boundary before fixtures")

func init() { taskfixture.RejectCandidateUpdate() }

type pluginCase struct {
	Name, Kind, Actor, Method, Path, Body, Variant string
	Status                                         int
}

func pluginCases() []pluginCase {
	base := "/api/plugin/task"
	routes := []pluginCase{
		{Name: "list", Method: "GET", Path: base},
		{Name: "upload_post", Method: "POST", Path: base},
		{Name: "upload_put", Method: "PUT", Path: base},
		{Name: "runtime", Method: "GET", Path: base + "/runtime/status"},
		{Name: "sources", Method: "GET", Path: base + "/marketplace/sources"},
		{Name: "update_sources", Method: "PUT", Path: base + "/marketplace/sources", Body: `[]`},
		{Name: "detail", Method: "GET", Path: base + "/contract-plugin"},
		{Name: "icon", Method: "GET", Path: base + "/contract-plugin/icon"},
		{Name: "versions", Method: "GET", Path: base + "/contract-plugin/versions"},
		{Name: "activate", Method: "POST", Path: base + "/contract-plugin/activate", Body: `{"version":"2.0.0"}`},
		{Name: "status", Method: "POST", Path: base + "/contract-plugin/status", Body: `{"enabled":true}`},
		{Name: "dryrun", Method: "POST", Path: base + "/contract-plugin/dryrun", Body: `{"hook":"echo","args":[{"a":0,"b":false}]}`},
		{Name: "delete", Method: "DELETE", Path: base + "/contract-plugin/versions/2.0.0"},
		{Name: "options", Method: "GET", Path: "/api/task_plugin_options"},
	}
	cases := []pluginCase{}
	for _, route := range routes {
		for _, actor := range []string{"anonymous", "key", "foreign_key", "user", "admin", "root", "user_pat", "admin_pat", "root_pat"} {
			tc := route
			tc.Name, tc.Kind, tc.Actor, tc.Status = "access_"+route.Name+"_"+actor, "access", actor, 200
			if actor == "anonymous" || actor == "key" || actor == "foreign_key" {
				tc.Status = 401
			} else if actor != "root" && actor != "root_pat" {
				tc.Status = 403
			}
			cases = append(cases, tc)
		}
	}
	for _, item := range []struct {
		route    string
		variants []string
	}{
		{"upload_post", []string{"missing_source", "invalid_json", "too_large", "bad_hash", "good_hash", "invalid_js", "invalid_meta",
			"invalid_icon", "disabled", "same_hash", "different_hash", "force", "insert_fault", "publish_fault"}},
		{"list", []string{"empty_db", "disabled", "compile_error", "inactive_only", "master_off", "query_fault", "ignored_pagination"}},
		{"detail", []string{"missing", "explicit_version", "missing_version", "factory", "compile_error", "query_fault"}},
		{"icon", []string{"missing", "missing_version", "malformed_icon", "factory", "explicit_version", "query_fault"}},
		{"versions", []string{"missing", "empty_db", "ignored_pagination", "query_fault"}},
		{"runtime", []string{"empty_db", "disabled", "compile_error", "query_fault", "master_off"}},
		{"options", []string{"disabled", "master_off", "factory_disabled", "query_fault"}},
		{"activate", []string{"missing_version", "invalid_json", "not_found", "compile_error", "repeat", "query_fault", "publish_fault"}},
		{"delete", []string{"active", "missing", "in_use", "force", "promote_disabled", "factory", "query_fault", "publish_fault"}},
		{"status", []string{"missing_enabled", "invalid_json", "disable", "in_use", "cascade", "force", "cascade_force", "factory",
			"factory_override", "enable_factory", "query_fault", "publish_fault"}},
		{"sources", []string{"unset", "null", "malformed", "empty"}},
		{"update_sources", []string{"null", "trim", "missing_name", "relative_url", "bad_scheme", "duplicate", "invalid_json"}},
		{"dryrun", []string{"missing_hook", "invalid_json", "unknown_hook", "member", "null_args", "factory", "compile_error", "exception"}},
	} {
		for _, variant := range item.variants {
			for _, route := range routes {
				if route.Name != item.route {
					continue
				}
				tc := route
				tc.Name, tc.Kind, tc.Actor, tc.Variant, tc.Status = item.route+"_"+variant, item.route, "root", variant, 200
				if item.route == "icon" && (variant == "missing" || variant == "missing_version" || variant == "malformed_icon" || variant == "factory") {
					tc.Status = 404
				}
				cases = append(cases, tc)
			}
		}
	}
	for _, query := range []struct {
		name, path, body string
		status           int
	}{
		{"own", "/wave5c/query/task_seed_one", "", 200},
		{"foreign", "/wave5c/query/task_seed_foreign", "", 404},
		{"missing", "/wave5c/query/missing", "", 404},
		{"wrong_platform", "/wave5c/query/task_seed_wrong", "", 404},
		{"ordered", "/wave5c/query", `{"ids":["task_seed_two","task_seed_one","task_seed_two"]}`, 200},
		{"empty", "/wave5c/query", `{"ids":[]}`, 200},
		{"missing_id", "/wave5c/query", `{"ids":["task_seed_one","missing"]}`, 404},
		{"invalid_ids", "/wave5c/query", `{"ids":[1]}`, 400},
		{"nonarray", "/wave5c/query", `{"ids":null}`, 400},
	} {
		method := "GET"
		if query.body != "" {
			method = "POST"
		}
		cases = append(cases, pluginCase{Name: "native_query_" + query.name, Kind: "query", Actor: "key", Method: method,
			Path: query.path, Body: query.body, Status: query.status})
	}
	for _, variant := range []string{"success", "zero_false", "provider_error", "insert_fault", "quota_denied", "wrong_plugin",
		"origin_own", "origin_foreign", "origin_platform", "origin_disabled", "origin_mixed"} {
		status := 200
		if strings.HasPrefix(variant, "origin_") && variant != "origin_own" {
			status = 400
		}
		if variant == "quota_denied" {
			status = 403
		}
		if variant == "provider_error" || variant == "insert_fault" {
			status = 500
		}
		if variant == "wrong_plugin" {
			status = 503
		}
		cases = append(cases, pluginCase{Name: "submit_" + variant, Kind: "submit", Actor: "key", Method: "POST", Path: "/wave5c/submit",
			Body: `{"model":"contract-video","prompt":"fixture","duration":1}`, Variant: variant, Status: status})
	}
	for _, variant := range []string{"pending", "success", "failure", "foreign", "missing", "bad_prefix", "db_error"} {
		status := 200
		if variant == "foreign" || variant == "missing" || variant == "bad_prefix" {
			status = 404
		}
		if variant == "db_error" {
			status = 500
		}
		cases = append(cases, pluginCase{Name: "retrieve_" + variant, Kind: "retrieve", Actor: "key", Method: "GET",
			Path: "/v1/responses/resp_seed_one", Variant: variant, Status: status})
	}

	for _, route := range []pluginCase{
		{Name: "responses_create", Method: "POST", Path: "/v1/responses",
			Body: `{"model":"contract-video","background":true,"input":"fixture"}`, Status: 200},
		{Name: "responses_retrieve", Method: "GET", Path: "/v1/responses/resp_seed_one", Status: 200},
		{Name: "videos_create", Method: "POST", Path: "/v1/videos", Body: `{"model":"contract-video","prompt":"fixture","duration":1}`, Status: 200},
		{Name: "videos_retrieve", Method: "GET", Path: "/v1/videos/task_seed_one", Status: 200},
		{Name: "videos_content_get", Method: "GET", Path: "/v1/videos/task_seed_one/content", Status: 410},
		{Name: "videos_content_head", Method: "HEAD", Path: "/v1/videos/task_seed_one/content", Status: 410},
	} {
		for _, actor := range []string{"anonymous", "key", "user", "admin", "root", "user_pat", "admin_pat", "root_pat"} {
			tc := route
			tc.Name = "protocol_access_" + route.Name + "_" + actor
			tc.Kind = "protocol"
			tc.Actor = actor
			if actor != "key" {
				tc.Status = 401
			}
			cases = append(cases, tc)
		}
	}

	for i, route := range []pluginCase{
		{Method: "POST", Path: "/ali/api/v1/services/aigc/video-generation/video-synthesis", Body: `{"model":"invalid"}`, Status: 400},
		{Method: "POST", Path: "/ali/api/v1/services/aigc/image2video/video-synthesis", Body: `{"model":"invalid"}`, Status: 400},
		{Method: "GET", Path: "/ali/api/v1/tasks/missing", Status: 404},
		{Method: "POST", Path: "/doubao/api/v3/contents/generations/tasks", Body: `{"model":"invalid"}`, Status: 400},
		{Method: "GET", Path: "/doubao/api/v3/contents/generations/tasks/missing", Status: 404},
		{Method: "POST", Path: "/kling/v1/videos/text2video", Body: `{"model_name":"invalid"}`, Status: 400},
		{Method: "POST", Path: "/kling/v1/videos/image2video", Body: `{"model_name":"invalid"}`, Status: 400},
		{Method: "GET", Path: "/kling/v1/videos/text2video/missing", Status: 404},
		{Method: "GET", Path: "/kling/v1/videos/image2video/missing", Status: 404},
		{Method: "POST", Path: "/jimeng/", Body: `{}`, Status: 307},
		{Method: "POST", Path: "/suno/submit/music", Body: `{"model":"invalid"}`, Status: 503},
		{Method: "POST", Path: "/suno/fetch", Body: `{"ids":[]}`, Status: 200},
		{Method: "GET", Path: "/suno/fetch/missing", Status: 404},
	} {
		for _, actor := range []string{"anonymous", "key"} {
			tc := route
			tc.Name = fmt.Sprintf("factory_native_%02d_%s", i, actor)
			tc.Kind = "factory_native"
			tc.Actor = actor
			if actor == "anonymous" && route.Path != "/jimeng/" {
				tc.Status = 401
			}
			cases = append(cases, tc)
		}
	}
	for _, route := range []pluginCase{
		{Name: "create", Method: "POST", Path: "/api/channel/",
			Body: `{"mode":"single","channel":{"type":61,"key":"fixture-bind-key","name":"bound","models":"contract-video",` +
				`"group":"default","setting":"{\"task_plugin_key\":\"contract-plugin\"}"}}`},
		{Name: "update", Method: "PUT", Path: "/api/channel/",
			Body: `{"id":71,"type":61,"name":"updated","models":"contract-video",` +
				`"group":"default","setting":"{\"task_plugin_key\":\"contract-plugin\"}"}`},
		{Name: "copy", Method: "POST", Path: "/api/channel/copy/71"},
	} {
		for _, actor := range []string{"anonymous", "user", "admin", "root", "root_pat"} {
			tc := route
			tc.Name = "binding_" + route.Name + "_" + actor
			tc.Kind = "binding"
			tc.Actor = actor
			tc.Status = 200
			if route.Name == "create" {
				tc.Status = 201
			}
			if actor == "anonymous" {
				tc.Status = 401
			}
			if actor == "user" || actor == "admin" {
				tc.Status = 403
			}
			cases = append(cases, tc)
		}
	}
	return cases
}

func TestTaskPluginsHTTPContract(t *testing.T) {
	updating := fixture.Updating(t)
	if *originCheck {
		taskfixture.CheckRecording(t)
		return
	}
	cases := pluginCases()
	names := make([]string, len(cases))
	seen := map[string]bool{}
	for i, tc := range cases {
		require.False(t, seen[tc.Name])
		seen[tc.Name] = true
		names[i] = tc.Name
	}
	if *manifest {
		data, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("PLUGINS_HTTP_MANIFEST " + string(data))
		return
	}
	if target := os.Getenv("PLUGINS_CONTRACT_LOCALES"); target != "" && os.Getenv("PLUGINS_CONTRACT_CASE") == "" {
		pluginLocales(t, target)
		return
	}
	fixture.Processes(t, "TestTaskPluginsHTTPContract", names, *preflight, updating, func(name string) {
		for _, tc := range cases {
			if tc.Name == name {
				runPluginHTTP(t, tc, updating)
				return
			}
		}
	})
}

func runPluginHTTP(t *testing.T, tc pluginCase, updating bool) {
	taskfixture.InstallDBClock(t)
	env := contracttest.New(t)
	settingsapp.Initialize()
	taskfixture.Configure()
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"group_ratio_setting.group_ratio": `{"default":1}`,
		"group_ratio_setting.group_group_ratio": `{}`, "channel_affinity_setting.enabled": "false", "billing_setting.billing_mode": `{}`,
		"billing_setting.billing_expr": `{}`}))
	common.RetryTimes = 0
	common.QuotaRemindThreshold = -1000000000
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{})
	setting.ModelRequestRateLimitEnabled = false
	constant.UpdateTask = true
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	require.NoError(t, env.DB.Create(&model.ModelCostFXRow{Source: "cbr-xml-daily.ru", EffectiveAt: taskfixture.Epoch,
		FetchedAt: taskfixture.Epoch, Version: 1, RatesJSON: `{"USD":100}`}).Error)
	require.NoError(t, model.LoadModelCostFX(context.Background(), "cbr-xml-daily.ru"))
	identities := map[string]service.AuthIdentity{}
	for actor, bearer := range map[string]string{"user": env.User, "admin": env.Admin, "root": env.Root} {
		identity, err := service.ParseAccessToken(bearer)
		require.NoError(t, err)
		identities[actor] = identity
	}
	for id, pat := range map[int]string{2: "plugins-admin-pat", 3: "plugins-root-pat"} {
		require.NoError(t, env.DB.Model(&model.User{}).Where("id=?", id).Update("access_token", pat).Error)
	}
	env.MountTasks()
	httpclient.InitHttpClient()
	transport := &fixture.Transport{T: t, Calls: []fixture.Outgoing{}}
	httpclient.GetHttpClient().Transport = transport
	synctest.Test(t, func(t *testing.T) {
		require.Equal(t, taskfixture.Epoch, time.Now().Unix())
		taskfixture.AssertDBClock(t)
		bearers := map[string]string{"key": "sk-" + fixture.Token, "foreign_key": "sk-pluginsforeignkey", "user_pat": env.UserPAT,
			"admin_pat": "plugins-admin-pat", "root_pat": "plugins-root-pat"}
		for actor, identity := range identities {
			bearer, _, err := service.IssueAccessToken(identity)
			require.NoError(t, err)
			bearers[actor] = bearer
		}
		for id, user := range map[int]int{101: 1, 102: 2} {
			key := fixture.Token
			if user == 2 {
				key = "pluginsforeignkey"
			}
			require.NoError(t, env.DB.Create(&model.Token{Id: id, UserId: user, Key: key, Name: "fixture", Status: 1,
				ExpiredTime: -1, RemainQuota: 1000000, Group: "default"}).Error)
		}
		require.NoError(t, env.DB.Model(&model.User{}).Where("id=1").Update("setting", `{"billing_preference":"wallet_only","language":"zh-CN"}`).Error)
		first := fixture.Save(t, fixture.Key, "1.0.0", true)
		fixture.Save(t, fixture.Key, "2.0.0", true)
		icon := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
		require.NoError(t, env.DB.Model(first).Update("icon", icon).Error)
		pluginsapp.SyncOnce()
		baseURL := "https://plugins.invalid"
		settings := `{"task_plugin_key":"contract-plugin"}`
		channel := model.Channel{Id: 71, Type: constant.ChannelTypeTaskPlugin, Key: fixture.ProviderKey, Status: common.ChannelStatusEnabled,
			Name: "plugin channel", BaseURL: &baseURL, Models: fixture.Model, Group: "default", Setting: &settings, AutoBan: common.GetPointer(0)}
		require.NoError(t, env.DB.Create(&channel).Error)
		require.NoError(t, channel.AddAbilities(nil))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"contract-video":0.001}`))
		for i, name := range []string{"one", "two", "foreign", "wrong"} {
			user := 1
			platform := constant.TaskPlatform(fixture.Key)
			if name == "foreign" {
				user = 2
			}
			if name == "wrong" {
				platform = "other"
			}
			require.NoError(t, env.DB.Create(&model.Task{ID: int64(300 + i), UserId: user, TaskID: "task_seed_" + name, Platform: platform,
				Status: model.TaskStatusSuccess, ChannelId: 71, CreatedAt: taskfixture.Epoch, UpdatedAt: taskfixture.Epoch,
				Properties: model.Properties{OriginModelName: fixture.Model}, Data: []byte(`{"id":"private-seed","task_id":"private-seed"}`),
				PrivateData: model.TaskPrivateData{UpstreamTaskID: "private-seed"}}).Error)
		}
		body := tc.Body
		path := tc.Path
		if tc.Kind == "upload_post" || tc.Name == "access_upload_post_root" || tc.Name == "access_upload_put_root" ||
			tc.Name == "access_upload_post_root_pat" || tc.Name == "access_upload_put_root_pat" {
			source := fixture.Source(fixture.Key, "3.0.0")
			request := map[string]any{"source": source}
			switch tc.Variant {
			case "missing_source":
				request = map[string]any{}
			case "too_large":
				request["source"] = strings.Repeat("x", (1<<20)+1)
			case "bad_hash":
				request["sourceSha256"] = "bad"
			case "good_hash":
				request["sourceSha256"] = strings.ToUpper(fmt.Sprintf("%x", sha256.Sum256([]byte(source))))
			case "invalid_js":
				request["source"] = "export const ="
			case "invalid_meta":
				request["source"] = strings.Replace(source, "apiVersion:1", "apiVersion:99", 1)
			case "invalid_icon":
				request["icon"] = "data:text/html;base64,eA=="
			case "disabled":
				request["enabled"] = false
			case "same_hash":
				request["source"] = fixture.Source(fixture.Key, "1.0.0")
				request["remark"] = "updated"
			case "different_hash":
				request["source"] = fixture.Source(fixture.Key, "1.0.0") + "\n// changed"
			case "force":
				request["force"] = true
			case "publish_fault":
				request["source"] = fixture.Source("new-plugin", "1.0.0")
				request["force"] = true
			}
			encoded, err := common.Marshal(request)
			require.NoError(t, err)
			body = string(encoded)
		}
		switch tc.Variant {
		case "empty_db":
			require.NoError(t, env.DB.Where("1=1").Delete(&model.TaskPlugin{}).Error)
			pluginsapp.SyncOnce()
		case "missing":
			path = strings.ReplaceAll(path, "contract-plugin", "absent")
			path = strings.ReplaceAll(path, "resp_seed_one", "resp_absent")
		case "explicit_version":
			path += "?version=1.0.0"
		case "missing_version":
			if tc.Kind == "activate" {
				body = `{}`
			} else {
				path += "?version=missing"
			}
		case "ignored_pagination":
			path += "?p=7&page_size=1"
		case "master_off":
			jsplugin.DefaultRegistry.SetEnabled(false)
		case "factory_disabled":
			jsplugin.DefaultRegistry.SetDisabledFactoryKeys([]string{"kling"})
		case "inactive_only":
			require.NoError(t, env.DB.Model(&model.TaskPlugin{}).Where("1=1").Update("active", false).Error)
		case "compile_error":
			require.NoError(t, env.DB.Model(first).Updates(map[string]any{"source": "export const =", "source_hash": "bad"}).Error)
			if tc.Kind == "activate" {
				body = `{"version":"1.0.0"}`
			}
			pluginsapp.SyncOnce()
		case "disabled":
			if tc.Kind != "upload_post" {
				require.NoError(t, pluginsapp.NewStore().SetTaskPluginEnabled(fixture.Key, false))
				pluginsapp.SyncOnce()
			}
		case "factory", "factory_override", "enable_factory":
			path = strings.ReplaceAll(path, "contract-plugin", "kling")
			if tc.Variant == "factory_override" {
				fixture.Save(t, "kling", "1.0.0", true)
				pluginsapp.SyncOnce()
			}
			if tc.Variant == "enable_factory" {
				body = `{"enabled":true}`
				jsplugin.DefaultRegistry.SetDisabledFactoryKeys([]string{"kling"})
			}
			if tc.Kind == "status" {
				body = `{"enabled":false}`
			}
			if tc.Kind == "dryrun" {
				body = `{"hook":"missing"}`
			}
		case "active":
			path = strings.Replace(path, "/versions/2.0.0", "/versions/1.0.0", 1)
		case "promote_disabled":
			require.NoError(t, env.DB.Model(&model.TaskPlugin{}).Where("version=?", "2.0.0").Update("enabled", false).Error)
			path = strings.Replace(path, "/versions/2.0.0", "/versions/1.0.0", 1)
		case "in_use", "force", "cascade", "cascade_force":
			if tc.Kind == "delete" {
				path = strings.Replace(path, "/versions/2.0.0", "/versions/1.0.0", 1)
			}
			if tc.Kind == "status" {
				body = `{"enabled":false}`
			}
			if tc.Kind != "upload_post" {
				require.NoError(t, env.DB.Model(&model.Task{}).Where("id=300").Update("status", model.TaskStatusInProgress).Error)
			}
			if tc.Variant == "force" {
				path += "?force=true"
			}
			if tc.Variant == "cascade" {
				path += "?cascade=true"
			}
			if tc.Variant == "cascade_force" {
				path += "?cascade=true&force=true"
			}
		case "disable":
			body = `{"enabled":false}`
		case "not_found":
			body = `{"version":"missing"}`
		case "missing_enabled", "missing_hook":
			if tc.Kind == "status" || tc.Kind == "dryrun" || tc.Kind == "activate" {
				body = `{}`
			}
		case "malformed_icon":
			require.NoError(t, env.DB.Model(first).Update("icon", "bad").Error)
		case "unset", "null", "malformed", "empty":
			if tc.Kind == "sources" {
				value := ""
				if tc.Variant == "null" {
					value = "null"
				}
				if tc.Variant == "malformed" {
					value = "{"
				}
				if tc.Variant == "empty" {
					value = "[]"
				}
				require.NoError(t, settingsapp.Default().Write.Execute(setting.TaskPluginMarketplaceSourcesKey, value))
			}
			if tc.Kind == "update_sources" {
				body = "null"
			}
		case "trim":
			body = `[{"name":" Test ","index_url":" https://example.test/index.json "}]`
		case "missing_name":
			body = `[{"name":" ","index_url":"https://example.test"}]`
		case "relative_url":
			body = `[{"name":"Test","index_url":"/index.json"}]`
		case "bad_scheme":
			body = `[{"name":"Test","index_url":"ftp://example.test"}]`
		case "duplicate":
			body = `[{"name":"Test","index_url":"https://example.test"},{"name":"Test","index_url":"https://example.test"}]`
		case "unknown_hook":
			body = `{"hook":"missing"}`
		case "member":
			body = `{"hook":"native","member":"queries","args":[{},[]]}`
		case "null_args":
			body = `{"hook":"echo","args":[null]}`
		case "exception":
			body = `{"hook":"buildContentRequest"}`
		case "zero_false":
			body = `{"model":"contract-video","prompt":"fixture","duration":1,"temperature":0,"stream":false}`
		case "provider_error":
			transport.Status = 500
			transport.Body = `{"error":"fixture upstream failure"}`
		case "quota_denied":
			require.NoError(t, env.DB.Model(&model.User{}).Where("id=1").Update("quota", 0).Error)
		case "wrong_plugin":
			require.NoError(t, env.DB.Model(&channel).Update("setting", `{"task_plugin_key":"other"}`).Error)
		case "origin_own", "origin_foreign", "origin_platform", "origin_disabled", "origin_mixed":
			ids := `["task_seed_one"]`
			if tc.Variant == "origin_foreign" {
				ids = `["task_seed_foreign"]`
			}
			if tc.Variant == "origin_platform" {
				ids = `["task_seed_wrong"]`
			}
			if tc.Variant == "origin_mixed" {
				require.NoError(t, env.DB.Model(&model.Task{}).Where("id=301").Update("channel_id", 72).Error)
				ids = `["task_seed_one","task_seed_two"]`
			}
			if tc.Variant == "origin_disabled" {
				require.NoError(t, env.DB.Model(&channel).Update("status", common.ChannelStatusManuallyDisabled).Error)
			}
			body = `{"model":"contract-video","prompt":"fixture","duration":1,"origins":` + ids + `}`
		case "pending":
			require.NoError(t, env.DB.Model(&model.Task{}).Where("id=300").Update("status", model.TaskStatusInProgress).Error)
		case "failure":
			require.NoError(t, env.DB.Model(&model.Task{}).Where("id=300").Updates(map[string]any{
				"status": model.TaskStatusFailure, "fail_reason": "private error",
			}).Error)
		case "foreign":
			path = "/v1/responses/resp_seed_foreign"
		case "bad_prefix":
			path = "/v1/responses/bad"
		}
		if tc.Variant == "invalid_json" {
			body = "{"
		}
		before := fixture.State(t)
		faultTable := "task_plugins"
		if tc.Variant == "db_error" || tc.Kind == "submit" {
			faultTable = "tasks"
		}
		faultQuery := tc.Variant == "query_fault" || tc.Variant == "db_error"
		faultCreate := tc.Variant == "insert_fault"
		if faultQuery {
			require.NoError(t, env.DB.Callback().Query().Before("gorm:query").Register("plugins-query", func(tx *gorm.DB) {
				if tx.Statement.Table == faultTable {
					tx.AddError(errors.New("fixture plugin query failure"))
				}
			}))
		}
		if faultCreate {
			require.NoError(t, env.DB.Callback().Create().Before("gorm:create").Register("plugins-create", func(tx *gorm.DB) {
				if tx.Statement.Table == faultTable {
					tx.AddError(errors.New("fixture plugin insert failure"))
				}
			}))
		}
		if tc.Variant == "publish_fault" {
			calls := 0
			require.NoError(t, jsplugin.DefaultRegistry.SetGenerationPreparer(func(
				candidate, current *jsplugin.RoutingGeneration,
			) (jsplugin.PreparedRoutingGeneration, error) {
				calls++
				if calls > 1 {
					return jsplugin.PreparedRoutingGeneration{}, errors.New("fixture publish failure")
				}
				return jsplugin.PreparedRoutingGeneration{Generation: candidate.WithRuntime(current.RuntimeHandler())}, nil
			}))
		}
		observations := []any{}
		repeats := 1
		if tc.Variant == "repeat" {
			repeats = 2
		}
		for i := range repeats {
			request := httptest.NewRequest(tc.Method, path, strings.NewReader(body))
			if body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			language := os.Getenv("PLUGINS_CONTRACT_LANGUAGE")
			if language == "" {
				language = "zh-CN"
			}
			if language != "default" {
				request.Header.Set("Accept-Language", language)
			}
			if bearer := bearers[tc.Actor]; bearer != "" {
				request.Header.Set("Authorization", "Bearer "+bearer)
			}
			response := env.Serve(request)
			synctest.Wait()
			require.Equal(t, tc.Status, response.Code, "%s repeat%d: %s", tc.Name, i, response.Body.String())
			observations = append(observations, map[string]any{
				"status": response.Code, "body": response.Body.String(), "headers": response.Header(), "flushed": response.Flushed,
			})
		}
		if faultQuery {
			require.NoError(t, env.DB.Callback().Query().Remove("plugins-query"))
		}
		if faultCreate {
			require.NoError(t, env.DB.Callback().Create().Remove("plugins-create"))
		}
		after := fixture.State(t)
		if tc.Kind != "submit" && !(tc.Kind == "protocol" && tc.Method == "POST" && tc.Actor == "key") {
			assert.Empty(t, transport.Calls, "management/query/retrieve never submit upstream")
		}
		value := map[string]any{"before": before, "observations": observations, "after": after, "outgoing": transport.Calls}
		taskfixture.Compare(t, "testdata/golden", tc.Name, fixture.CanonicalIDs(t, value), *preflight, updating)
	})
}

func pluginLocales(t *testing.T, target string) {
	require.False(t, fixture.Updating(t))
	result := map[string]any{}
	for _, name := range []string{"access_list_anonymous", "access_list_admin", "detail_missing", "status_missing_enabled", "retrieve_missing"} {
		for _, language := range []string{"default", "en", "ru", "zh-CN", "zh-TW", "unknown"} {
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTaskPluginsHTTPContract$", "-plugins-preflight=true", "-update=false")
			command.Env = append(os.Environ(), "PLUGINS_CONTRACT_CASE="+name, "PLUGINS_CONTRACT_LANGUAGE="+language, "PLUGINS_CONTRACT_LOCALES=")
			out, err := command.CombinedOutput()
			require.NoError(t, err, "%s", out)
			found := false
			for line := range strings.SplitSeq(string(out), "\n") {
				if raw, ok := strings.CutPrefix(line, "TASKS_SNAPSHOT "+name+" "); ok {
					require.False(t, found)
					var value any
					require.NoError(t, common.Unmarshal([]byte(raw), &value))
					result[language+"/"+name] = value
					found = true
				}
			}
			require.True(t, found, name)
		}
	}
	data, err := common.Marshal(result)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(target, append(data, '\n'), 0600))
}
