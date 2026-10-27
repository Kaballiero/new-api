// Package contract pins catalog, reusable groups and gateway model-list contracts before migration.
package contract

import (
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"

	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"

	f "github.com/QuantumNous/new-api/internal/contracttest/managementfixture"

	h "github.com/QuantumNous/new-api/internal/contracttest/managementhttp"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type catalogCase struct{ Name, Method, Path, Body, Actor, Kind, Variant string }

func catalogRoutes() []catalogCase {
	return []catalogCase{
		{Name: "presets", Method: "GET", Path: "/api/models"},
		{Name: "models", Method: "GET", Path: "/api/models/"},
		{Name: "search", Method: "GET", Path: "/api/models/search"},
		{Name: "model", Method: "GET", Path: "/api/models/1"},
		{Name: "model_create", Method: "POST", Path: "/api/models/", Body: `{"model_name":"new-model","vendor_id":1,"status":0,"sync_official":0}`},
		{Name: "model_update", Method: "PUT", Path: "/api/models/", Body: `{"id":1,"model_name":"alpha","status":1,"sync_official":1,"vendor_id":1}`},
		{Name: "model_delete", Method: "DELETE", Path: "/api/models/1"},
		{Name: "model_batch", Method: "POST", Path: "/api/models/delete", Body: `{"model_ids":[1,2]}`},
		{Name: "missing", Method: "GET", Path: "/api/models/missing"},
		{Name: "sync_preview", Method: "GET", Path: "/api/models/sync_upstream/preview?locale=bad"},
		{Name: "sync_apply", Method: "POST", Path: "/api/models/sync_upstream", Body: `{"locale":"bad","selections":[]}`},
		{Name: "vendors", Method: "GET", Path: "/api/vendors/"},
		{Name: "vendor_search", Method: "GET", Path: "/api/vendors/search"},
		{Name: "vendor", Method: "GET", Path: "/api/vendors/1"},
		{Name: "vendor_create", Method: "POST", Path: "/api/vendors/", Body: `{"name":"New vendor","description":"fixture","icon":"i"}`},
		{Name: "vendor_update", Method: "PUT", Path: "/api/vendors/", Body: `{"id":1,"name":"Vendor changed"}`},
		{Name: "vendor_delete", Method: "DELETE", Path: "/api/vendors/2"},
		{Name: "vendor_preview", Method: "POST", Path: "/api/vendors/operations/preview", Body: `{"action":"assign","model_ids":[1],"target_vendor_id":2}`},
		{Name: "vendor_apply", Method: "POST", Path: "/api/vendors/operations",
			Body: `{"action":"assign","model_ids":[1],"target_vendor_id":2,"expected_version":"stale"}`},
		{Name: "prefill_list", Method: "GET", Path: "/api/prefill_group/"},
		{Name: "prefill_create", Method: "POST", Path: "/api/prefill_group/",
			Body: `{"name":"new-group","type":"model","items":["new"],"description":"fixture"}`},
		{Name: "prefill_update", Method: "PUT", Path: "/api/prefill_group/", Body: `{"id":1,"name":"group-one","type":"tag","items":[],"description":""}`},
		{Name: "prefill_delete", Method: "DELETE", Path: "/api/prefill_group/1"},
		{Name: "openai", Method: "GET", Path: "/v1/models", Kind: "relay"},
		{Name: "retrieve", Method: "GET", Path: "/v1/models/gpt-4", Kind: "relay"},
		{Name: "gemini", Method: "GET", Path: "/v1beta/models", Kind: "relay"},
		{Name: "gemini_openai", Method: "GET", Path: "/v1beta/openai/models", Kind: "relay"},
	}
}
func catalogCases() []catalogCase {
	cases := []catalogCase{}
	for _, route := range catalogRoutes() {
		for _, actor := range []string{"anonymous", "user", "admin", "root", "user_pat", "admin_pat", "root_pat", "key"} {
			tc := route
			tc.Name = "access_" + route.Name + "_" + actor
			tc.Actor = actor
			cases = append(cases, tc)
		}
	}
	add := func(name, method, path, body, kind, variant string) {
		cases = append(cases, catalogCase{name, method, path, body, "root", kind, variant})
	}
	for i, query := range []string{"p=2&page_size=1", "p=-1&page_size=-1", "page_size=999",
		"keyword=alpha", "vendor=1", "vendor=Vendor", "status=0", "status=bad", "sync_official=false",
		"include_channel_models=true", "square_state=visible", "square_state=hidden&include_channel_models=true",
		"square_state=invalid", "status=1&sync_official=1", "keyword=missing", "vendor=0&include_channel_models=true"} {
		add(fmt.Sprintf("models_filter_%02d", i), "GET", "/api/models/?"+query, "", "read", "")
		add(fmt.Sprintf("search_filter_%02d", i), "GET", "/api/models/search?"+query, "", "read", "")
	}
	for i, query := range []string{"keyword=Vendor", "association=linked", "association=unlinked",
		"association=bad", "p=2&page_size=1", "keyword=no-match", "keyword=%25"} {
		for _, path := range []string{"/api/vendors/", "/api/vendors/search"} {
			add(fmt.Sprintf("vendors_filter_%d_%d", i, len(path)), "GET", path+"?"+query, "", "read", "")
		}
	}
	for _, route := range catalogRoutes() {
		if route.Kind == "relay" || route.Name == "presets" || strings.HasPrefix(route.Name, "sync_") {
			continue
		}
		if route.Body != "" {
			add("malformed_"+route.Name, route.Method, route.Path, "{", "mutation", "")
		}
		if route.Method == "GET" {
			add("sql_failure_"+route.Name, route.Method, route.Path, "", "fault", "")
		}
	}
	for i, body := range []string{`{}`, `{"model_name":"alpha"}`, `{"model_name":"   "}`,
		`{"model_name":"x","vendor_id":-1}`, `{"model_name":"x","vendor_id":99}`, `{"model_name":"x","status":2}`,
		`{"model_name":"x","name_rule":99}`, `{"model_name":"x","endpoints":"42"}`, `{"model_name":"x","endpoints":"[\"chat\"]","status":0,"sync_official":0}`,
		`{"model_name":"x","endpoints":"{\"chat\":{\"path\":\"/v1/chat\",\"method\":\"BAD\"}}"}`} {
		add(fmt.Sprintf("model_create_%02d", i), "POST", "/api/models/", body, "mutation", "")
	}
	for i, body := range []string{`{}`, `{"id":1,"model_name":""}`, `{"id":1,"model_name":"beta"}`,
		`{"id":999,"model_name":"missing","status":1}`, `{"id":1,"model_name":"renamed","status":0,"sync_official":0}`,
		`{"id":1,"model_name":"alpha","vendor_id":-1}`} {
		add(fmt.Sprintf("model_update_%02d", i), "PUT", "/api/models/", body, "mutation", "")
	}
	for _, value := range []string{"0", "1", "2"} {
		add("model_status_"+value, "PUT", "/api/models/?status_only=true", `{"id":1,"status":`+value+`}`, "mutation", "")
	}
	for i, path := range []string{"/api/models/999", "/api/models/bad", "/api/vendors/999", "/api/vendors/bad"} {
		add(fmt.Sprintf("lookup_%d", i), "GET", path, "", "read", "")
	}
	for i, body := range []string{`{}`, `{"name":""}`, `{"name":" vendor one "}`, `{"name":"` + strings.Repeat("x",
		129) + `"}`, `{"name":"new","icon":"` + strings.Repeat("x", 129) + `"}`, `{"id":99,"name":"Fresh","status":0}`} {
		add(fmt.Sprintf("vendor_create_%02d", i), "POST", "/api/vendors/", body, "mutation", "")
	}
	for i, body := range []string{`{}`, `{"id":1,"name":""}`, `{"id":1,"name":"Vendor two"}`,
		`{"id":1,"name":"changed","version":"stale"}`, `{"id":99,"name":"changed"}`} {
		add(fmt.Sprintf("vendor_update_%02d", i), "PUT", "/api/vendors/", body, "mutation", "")
	}
	for _, action := range []string{"assign", "merge", "delete"} {
		for _, variant := range []string{"apply", "stale", "invalid", "referenced"} {
			if variant == "referenced" && action != "delete" {
				continue
			}
			add("vendor_operation_"+action+"_"+variant, "POST", "/api/vendors/operations", "", "vendor_operation", action+":"+variant)
		}
	}
	for i, path := range []string{"/api/models/1?remove_from_channels=true", "/api/models/3?remove_from_channels=true",
		"/api/models/1?remove_pricing=true", "/api/models/999", "/api/models/bad"} {
		add(fmt.Sprintf("delete_model_%d", i), "DELETE", path, "", "delete", "")
	}
	add("delete_pricing_admin", "DELETE", "/api/models/1?remove_pricing=true", "", "delete", "admin")
	for _, count := range []int{1000, 1001} {
		add(fmt.Sprintf("delete_limit_%d", count), "POST", "/api/models/delete", `{"model_ids":[`+strings.Repeat("1,", count-1)+`1]}`, "delete", "")
	}
	for i, body := range []string{`{"model_ids":[]}`, `{"model_ids":[-1]}`, `{"model_ids":[1,1]}`,
		`{"model_ids":[1,999]}`, `{"model_ids":[1,2],"remove_from_channels":true}`, `{"model_ids":[1],"remove_pricing":true}`} {
		add(fmt.Sprintf("delete_batch_%02d", i), "POST", "/api/models/delete", body, "delete", "")
	}
	for _, variant := range []string{"empty", "all_configured", "prefix_only", "db_error"} {
		add("missing_"+variant, "GET", "/api/models/missing", "", "missing", variant)
	}
	for i, body := range []string{`{}`, `{"name":"x"}`, `{"name":"group-one","type":"tag"}`,
		`{"name":" raw ","type":" odd ","items":null}`, `{"name":"x","type":"model","items":[]}`,
		`{"name":"x","type":"model","items":{}}`, `{"name":"x","type":"model","items":0}`,
		`{"name":"x","type":"model","items":false}`, `{"name":"x","type":"model","items":"raw"}`,
		`{"id":77,"name":"explicit-id","type":"model","created_time":8}`} {
		add(fmt.Sprintf("prefill_create_%02d", i), "POST", "/api/prefill_group/", body, "prefill", "")
	}
	for i, body := range []string{`{}`, `{"id":1}`, `{"id":1,"name":"group-two","type":"model"}`,
		`{"id":88,"name":"upsert","type":"unknown","items":[]}`, `{"id":1,"name":"group-one","type":"","items":null,"created_time":42}`,
		`{"id":-1,"name":"negative","type":"model"}`} {
		add(fmt.Sprintf("prefill_update_%02d", i), "PUT", "/api/prefill_group/", body, "prefill", "")
	}
	for i, path := range []string{"/api/prefill_group/?type=model", "/api/prefill_group/?type=tag",
		"/api/prefill_group/?type=missing", "/api/prefill_group/?p=2&page_size=1"} {
		add(fmt.Sprintf("prefill_filter_%d", i), "GET", path, "", "prefill", "")
	}
	for _, id := range []string{"bad", "0", "-1", "999"} {
		add("prefill_delete_"+id, "DELETE", "/api/prefill_group/"+id, "", "prefill", "")
	}
	add("prefill_reuse_deleted", "DELETE", "/api/prefill_group/1", "", "prefill", "reuse")
	for _, variant := range []string{"default", "selfuse", "accept_unpriced", "limited",
		"limited_empty", "auto", "auto_empty", "reserve", "no_enabled", "owner_priority",
		"db_error", "anthropic", "gemini_headers", "unknown_retrieve"} {
		add("availability_"+variant, "GET", "/v1/models", "", "availability", variant)
	}
	for _, variant := range []string{"preview", "apply", "stale_source", "stale_record",
		"blocked", "create", "bad_locale", "malformed", "failure", "array", "cache", "cache_miss",
		"missing_vendor", "duplicate", "size"} {
		add("sync_"+variant, "GET", "/api/models/sync_upstream/preview?locale=en", "", "sync", variant)
	}
	return cases
}
func TestCatalogContract(t *testing.T) {
	cases := catalogCases()
	names := make([]string, len(cases))
	byName := map[string]catalogCase{}
	for i, tc := range cases {
		names[i] = tc.Name
		byName[tc.Name] = tc
	}
	f.Run(t, "TestCatalogContract", "catalog", "testdata/golden", names, []string{"access_models_anonymous",
		"access_models_user", "prefill_create_00", "prefill_update_00", "model_create_00",
		"availability_unknown_retrieve"}, func(t *testing.T, name string) any {
		return runCatalog(t,
			byName[name])
	})
}
func seedCatalog(t *testing.T) {
	priority := int64(1)
	t.Helper()
	operation_setting.SelfUseModeEnabled = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"alpha":1,"beta":2,"route-visible":1}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": "{}", "billing_expr": "{}"})
	for _, vendor := range []model.Vendor{{Id: 1, Name: "Vendor one", Status: 1, CreatedTime: 1,
		UpdatedTime: 2}, {Id: 2, Name: "Vendor two", Status: 1, CreatedTime: 1, UpdatedTime: 3}} {
		require.NoError(t, model.DB.Create(&vendor).Error)
	}
	for _, value := range []model.Model{{Id: 1, ModelName: "alpha", VendorID: 1, Status: 1,
		SyncOfficial: 1, CreatedTime: 1, UpdatedTime: 2}, {Id: 2, ModelName: "beta", Status: 0,
		SyncOfficial: 1, CreatedTime: 1, UpdatedTime: 3}, {Id: 3, ModelName: "prefix-", NameRule: 1,
		Status: 1, SyncOfficial: 1, CreatedTime: 1, UpdatedTime: 4}} {
		status, syncOfficial := value.Status, value.SyncOfficial
		require.NoError(t, model.DB.Create(&value).Error)
		require.NoError(t, model.DB.Model(&model.Model{}).Where("id=?", value.Id).Updates(map[string]any{"status": status, "sync_official": syncOfficial}).Error)
	}
	for _, value := range []model.PrefillGroup{{Id: 1, Name: "group-one", Type: "model",
		Items: model.JSONValue(`["alpha"]`), CreatedTime: 1, UpdatedTime: 2}, {Id: 2, Name: "group-two",
		Type: "tag", Items: model.JSONValue(`[]`), CreatedTime: 1, UpdatedTime: 3}} {
		require.NoError(t, model.DB.Create(&value).Error)
	}
	for _, channel := range []model.Channel{{Id: 21, Name: "enabled", Type: 1, Key: "fixture",
		Status: 1, Group: "default", Models: "alpha,beta,prefix-child,route-visible,unpriced",
		Priority: &priority}, {Id: 22, Name: "disabled", Type: 1, Key: "fixture", Status: 2,
		Group: "vip", Models: "disabled-only"}} {
		require.NoError(t, channelstore.NewChannelPersistence().Insert(&(channel)))
	}
	pricingruntime.RefreshPricing()
}
func runCatalog(t *testing.T, tc catalogCase) any {
	var provider *metadataProvider
	if tc.Kind == "sync" && tc.Variant != "bad_locale" {
		provider = startMetadataProvider(t, tc)
	}
	return h.Run(t, true, func(t *testing.T, env *h.Fixture) any {
		seedCatalog(t)
		extra := map[string]any{}
		actor := tc.Actor
		if actor == "" {
			actor = "root"
		}
		if tc.Kind == "availability" {
			availabilityCase(t, env, tc)
			return env.Snapshot(t, extra)
		}
		if tc.Kind == "sync" {
			return syncCase(t, env, tc, provider)
		}
		if tc.Kind == "vendor_operation" {
			vendorOperationCase(t, env, tc)
			return env.Snapshot(t, extra)
		}
		if tc.Kind == "fault" || tc.Kind == "missing" && tc.Variant == "db_error" {
			require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("wave6c-fault", func(tx *gorm.DB) {
				if tx.Statement.Table == "models" || tx.Statement.Table == "vendors" || tx.Statement.Table == "prefill_groups" {
					tx.AddError(fmt.Errorf("fixture query failure"))
				}
			}))
		}
		if tc.Kind == "missing" {
			if tc.Variant == "empty" {
				require.NoError(t, model.DB.Where("1=1").Delete(&channelrecord.Ability{}).Error)
			}
			if tc.Variant == "all_configured" {
				for _, name := range []string{"prefix-child", "route-visible", "unpriced"} {
					require.NoError(t, model.DB.Create(&model.Model{ModelName: name, Status: 1}).Error)
				}
			}
			if tc.Variant == "prefix_only" {
				require.NoError(t, model.DB.Where("model <> ?", "prefix-child").Delete(&channelrecord.Ability{}).Error)
			}
		}
		if tc.Variant == "admin" {
			actor = "admin"
		}
		response := env.Do(t, actor, tc.Method, tc.Path, tc.Body)
		if tc.Path == "/api/models" && response.Code == http.StatusOK {
			// Pending order-only debt: AWS presets originate from map iteration on both layouts.
			// Replace only that array's bytes; retain its exact multiset and the remaining response bytes.
			var body struct {
				Data map[string][]string `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			models := body.Data[fmt.Sprint(constant.ChannelTypeAws)]
			require.NotEmpty(t, models)
			before, err := common.Marshal(models)
			require.NoError(t, err)
			slices.Sort(models)
			after, err := common.Marshal(models)
			require.NoError(t, err)
			require.Equal(t, 1, strings.Count(response.Body.String(), string(before)))
			env.Observations[len(env.Observations)-1].(map[string]any)["body"] = strings.Replace(response.Body.String(), string(before), string(after), 1)
		}
		if strings.HasPrefix(tc.Name, "access_") {
			want := 200
			if tc.Kind == "relay" {
				if actor != "key" {
					want = 401
				}
			} else if actor == "anonymous" || actor == "key" {
				want = 401
			} else if strings.HasPrefix(actor, "user") && tc.Name != "access_presets_"+actor {
				want = 403
			}
			if want != 200 {
				assert.Equal(t, want, response.Code)
			} else {
				assert.NotContains(t, []int{401, 403}, response.Code)
			}
		}
		if tc.Kind == "prefill" && tc.Variant == "reuse" {
			env.Do(t, "root", "POST", "/api/prefill_group/", `{"name":"group-one","type":"model","items":[]}`)
		}
		if tc.Kind == "delete" || tc.Kind == "mutation" || tc.Kind == "prefill" {
			env.Do(t, "root", "GET", "/api/models/", "")
			env.Do(t, "root", "GET", "/api/vendors/", "")
			env.Do(t, "root", "GET", "/api/prefill_group/", "")
		}
		extra["enabled_models"] = sortedStrings(channelstore.GetEnabledModels())
		return env.Snapshot(t, extra)
	})
}
func sortedStrings(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
}
func availabilityCase(t *testing.T, env *h.Fixture, tc catalogCase) {
	t.Helper()
	var headers http.Header
	if tc.Variant == "selfuse" {
		operation_setting.SelfUseModeEnabled = true
	}
	if tc.Variant == "accept_unpriced" {
		require.NoError(t, model.DB.Model(&model.User{}).Where("id=1").Update("setting", `{"accept_unset_model_ratio_model":true}`).Error)
	}
	if tc.Variant == "limited" || tc.Variant == "limited_empty" {
		limit := "alpha"
		if tc.Variant == "limited_empty" {
			limit = ""
		}
		require.NoError(t, model.DB.Model(&model.Token{}).Where("id=101").Updates(map[string]any{"model_limits_enabled": true, "model_limits": limit}).Error)
	}
	if tc.Variant == "auto" || tc.Variant == "auto_empty" {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["vip","default"]`))
		require.NoError(t, setting.UpdateMaxTokenAutoGroups("2"))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"default","vip":"vip","auto":"auto"}`))
		require.NoError(t, model.DB.Model(&model.Token{}).Where("id=101").Updates(map[string]any{"group": "auto",
			"auto_groups": map[bool]string{true: "[]", false: `["default","vip"]`}[tc.Variant == "auto_empty"]}).Error)
	}
	if tc.Variant == "reserve" {
		reserveSettings := `{"openrouter_model_reserve_usd":{"unpriced":0.1}}`
		channel := model.Channel{Id: 23, Name: "reserve", Type: constant.ChannelTypeOpenRouter,
			Key: "fixture", Status: 1, Group: "default", Models: "unpriced", Setting: &reserveSettings}
		require.NoError(t, channelstore.NewChannelPersistence().Insert(&(channel)))
	}
	if tc.Variant == "no_enabled" {
		require.NoError(t, model.DB.Where("1=1").Delete(&channelrecord.Ability{}).Error)
	}
	if tc.Variant == "owner_priority" {
		priority := int64(9)
		channel := model.Channel{Id: 23, Name: "owner", Type: constant.ChannelTypeOpenRouter,
			Key: "fixture", Status: 1, Group: "default", Models: "alpha", Priority: &priority}
		require.NoError(t, channelstore.NewChannelPersistence().Insert(&(channel)))
	}
	if tc.Variant == "db_error" {
		require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("availability-fault", func(tx *gorm.DB) {
			if tx.Statement.Table == "abilities" {
				tx.AddError(fmt.Errorf("fixture availability failure"))
			}
		}))
	}
	if tc.Variant == "anthropic" {
		headers = http.Header{"X-Api-Key": []string{"sk-wave6ckey"}, "Anthropic-Version": []string{"2023-06-01"}}
	}
	if tc.Variant == "gemini_headers" {
		headers = http.Header{"X-Goog-Api-Key": []string{"sk-wave6ckey"}}
	}
	path := tc.Path
	if tc.Variant == "unknown_retrieve" {
		path = "/v1/models/fixture-unknown"
	}
	response := env.Do(t, "key", "GET", path, "", headers)
	require.Equal(t, 200, response.Code)
	if tc.Variant != "unknown_retrieve" {
		require.NotContains(t, response.Body.String(), `"error":`)
	}
}
func vendorOperationCase(t *testing.T, env *h.Fixture, tc catalogCase) {
	t.Helper()
	action, variant, _ := strings.Cut(tc.Variant, ":")
	selected := `"vendor_ids":[1]`
	if action == "assign" {
		selected = `"model_ids":[1]`
	}
	target := 2
	if variant == "invalid" {
		target = -1
		if action == "delete" {
			selected = `"vendor_ids":[-1]`
		}
	}
	body := fmt.Sprintf(`{"action":%q,%s,"target_vendor_id":%d}`, action, selected, target)
	response := env.Do(t, "root", "POST", "/api/vendors/operations/preview", body)
	var preview struct {
		Success bool `json:"success"`
		Data    struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &preview))
	version := preview.Data.Version
	if variant == "stale" {
		version = "stale"
	}
	apply := strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"expected_version":%q}`, version)
	env.Do(t, "root", "POST", "/api/vendors/operations", apply)
}
func syncCase(t *testing.T, env *h.Fixture, tc catalogCase, provider *metadataProvider) any {
	t.Helper()
	if tc.Variant == "bad_locale" {
		env.Do(t, "root", "GET", "/api/models/sync_upstream/preview?locale=bad", "")
		return env.Snapshot(t, map[string]any{"outbound": []f.Request{}})
	}
	server, mu, requests := provider.server, provider.mu, provider.requests
	t.Setenv("SYNC_UPSTREAM_BASE", server.URL)
	t.Setenv("SYNC_HTTP_RETRY", "1")
	fmt.Println("WAVE6C_METADATA " + tc.Name + " local_fixture_configured")
	if tc.Variant == "size" {
		t.Setenv("SYNC_HTTP_MAX_MB", "0")
	}
	if tc.Variant == "blocked" {
		require.NoError(t, model.DB.Model(&model.Model{}).Where("id=1").Update("sync_official", 0).Error)
	}
	response := env.Do(t, "root", "GET", "/api/models/sync_upstream/preview?locale=en", "")
	var preview struct {
		Success bool `json:"success"`
		Data    struct {
			Source struct {
				Version string `json:"version"`
			} `json:"source"`
			Candidates []struct {
				ModelName     string `json:"model_name"`
				RecordVersion string `json:"record_version"`
				Kind          string
			} `json:"candidates"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &preview))
	if tc.Variant == "cache" {
		env.Do(t, "root", "GET", "/api/models/sync_upstream/preview?locale=en", "")
	}
	if slices.Contains([]string{"apply", "stale_source", "stale_record", "blocked", "create"}, tc.Variant) && preview.Success {
		name := "alpha"
		if tc.Variant == "create" {
			name = "fresh"
		}
		recordVersion := ""
		for _, candidate := range preview.Data.Candidates {
			if candidate.ModelName == name {
				recordVersion = candidate.RecordVersion
			}
		}
		version := preview.Data.Source.Version
		if tc.Variant == "stale_source" {
			version = "stale"
		}
		if tc.Variant == "stale_record" {
			recordVersion = "stale"
		}
		body := fmt.Sprintf(`{"locale":"en","source_version":%q,"selections":[{"model_name":%q,"record_version":%q,"create":%t,"fields":["description"]}]}`,
			version, name, recordVersion, tc.Variant == "create")
		env.Do(t, "root", "POST", "/api/models/sync_upstream", body)
	}
	mu.Lock()
	captured := slices.Clone(*requests)
	mu.Unlock()
	expectedPairs := 1
	if slices.Contains([]string{"cache", "apply", "stale_source", "stale_record", "blocked", "create"}, tc.Variant) {
		expectedPairs = 2
	}
	require.Len(t, captured, 2*expectedPairs, "every metadata round must reach both fixture endpoints")
	endpointCounts := map[string]int{}
	for _, request := range captured {
		require.Equal(t, http.MethodGet, request.Method)
		endpointCounts[request.URL]++
	}
	require.Equal(t, map[string]int{
		"http://fixture.invalid/api/i18n/en/newapi/models.json":  expectedPairs,
		"http://fixture.invalid/api/i18n/en/newapi/vendors.json": expectedPairs,
	}, endpointCounts)
	for i := 0; i < len(captured); i += 2 {
		end := min(i+2, len(captured))
		copy(captured[i:end], f.CanonicalOrder(t, tc.Name, captured[i:end]))
	}
	server.Close()
	fmt.Printf("WAVE6C_METADATA %s fixture_requests=%d keep_alive=false\n", tc.Name, len(captured))
	return f.FixturePaths(t, env.Snapshot(t, map[string]any{"outbound": captured}), server.URL)
}

type metadataProvider struct {
	server   *httptest.Server
	mu       *sync.Mutex
	requests *[]f.Request
}

func startMetadataProvider(t *testing.T, tc catalogCase) *metadataProvider {
	t.Helper()
	var mu sync.Mutex
	requests := []f.Request{}
	server := f.NewMetadataFixtureServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, f.Request{Method: r.Method, URL: "http://fixture.invalid" + r.URL.RequestURI(), Header: r.Header.Clone(), Body: ""})
		mu.Unlock()
		w.Header().Set("ETag", `"fixture-v1"`)
		if tc.Variant == "cache_miss" || tc.Variant == "cache" && r.Header.Get("If-None-Match") != "" {
			w.WriteHeader(304)
			return
		}
		if tc.Variant == "malformed" {
			fmt.Fprint(w, "{")
			return
		}
		if tc.Variant == "failure" {
			w.WriteHeader(500)
			fmt.Fprint(w, "fixture metadata failure")
			return
		}
		if strings.Contains(r.URL.Path, "vendors") {
			fmt.Fprint(w, `{"success":true,"data":[{"name":"Vendor one","description":"upstream vendor","status":1}]}`)
			return
		}
		data := `[{"model_name":"alpha","description":"upstream alpha","vendor_name":"Vendor one","status":1,"name_rule":0,"endpoints":["chat"]}]`
		if tc.Variant == "create" {
			data = `[{"model_name":"fresh","description":"fresh","vendor_name":"Vendor one","status":1,"name_rule":0}]`
		}
		if tc.Variant == "missing_vendor" {
			data = `[{"model_name":"fresh","vendor_name":"Missing","status":1}]`
		}
		if tc.Variant == "duplicate" {
			data = `[{"model_name":"alpha","status":1},{"model_name":"alpha","status":1}]`
		}
		if tc.Variant == "size" {
			data = strings.Repeat("x", 2048)
		}
		if tc.Variant == "array" {
			fmt.Fprint(w, data)
		} else {
			fmt.Fprint(w, `{"success":true,"data":`+data+`}`)
		}
	}))
	return &metadataProvider{server, &mu, &requests}
}
