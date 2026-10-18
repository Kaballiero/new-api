package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMatchesExpectedTaskPluginUsesGenericChannelSetting(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin}
	channel.SetSetting(dto.ChannelSettings{TaskPluginKey: "generic-alpha"})

	assert.True(t, channelMatchesExpectedTaskPlugin(nil, channel, "generic-alpha"))
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, channel, "generic-beta"))
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, channel, ""))
}

func TestChannelMatchesExpectedTaskPluginUsesPinnedLegacyIndex(t *testing.T) {
	registry := jsplugin.NewRegistry()
	alpha, err := registry.Register(distributorTaskPluginSource("legacy-alpha", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)
	pinnedGeneration := registry.Generation()

	require.NoError(t, registry.Unregister("legacy-alpha"))
	_, err = registry.Register(distributorTaskPluginSource("legacy-beta", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{
		Generation: pinnedGeneration,
		Plugin:     alpha,
	})
	channel := &model.Channel{Type: constant.ChannelTypeKling}

	assert.True(t, channelMatchesExpectedTaskPlugin(c, channel, "legacy-alpha"))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, channel, "legacy-beta"))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeJimeng}, "legacy-alpha"))
}

func TestChannelMatchesExpectedTaskPluginRejectsUnindexedLegacyChannel(t *testing.T) {
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(distributorTaskPluginSource("legacy-alpha", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{
		Generation: registry.Generation(),
		Plugin:     plugin,
	})

	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeJimeng}, "legacy-alpha"))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: 0}, "legacy-alpha"))
	assert.True(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeJimeng}, ""))
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, &model.Channel{Type: constant.ChannelTypeKling}, "legacy-alpha"))

	c.Set("expected_task_plugin_key", "legacy-alpha")
	setupErr := SetupContextForSelectedChannel(c, &model.Channel{Type: constant.ChannelTypeJimeng}, "task-model")
	require.NotNil(t, setupErr)
	assert.Contains(t, setupErr.Error(), "does not match")
}

func TestSharedEndpointRebindsToSelectedLegacyProvider(t *testing.T) {
	registry := jsplugin.NewRegistry()
	_, err := registry.Register(distributorEndpointPluginSource("gemini-shared", constant.ChannelTypeGemini), jsplugin.Options{})
	require.NoError(t, err)
	_, err = registry.Register(distributorEndpointPluginSource("vertex-shared", constant.ChannelTypeVertexAi), jsplugin.Options{})
	require.NoError(t, err)
	candidates := registry.Generation().LookupEndpointCandidates("POST", "/v1/responses", "task-model")
	require.Len(t, candidates, 2)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: candidates[0].Plugin})
	c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{
		Generation: registry.Generation(),
		Plugin:     candidates[0].Plugin,
		Protocol:   candidates[0].Protocol,
		Operation:  candidates[0].Operation,
		Model:      "task-model",
		Candidates: candidates,
	})
	c.Set("expected_task_plugin_key", candidates[0].Plugin.Meta.Key)

	geminiChannel := &model.Channel{Id: 1, Type: constant.ChannelTypeGemini}
	vertexChannel := &model.Channel{Id: 2, Type: constant.ChannelTypeVertexAi}
	assert.True(t, channelMatchesExpectedTaskPlugin(c, geminiChannel, candidates[0].Plugin.Meta.Key))
	assert.True(t, channelMatchesExpectedTaskPlugin(c, vertexChannel, candidates[0].Plugin.Meta.Key))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeKling}, candidates[0].Plugin.Meta.Key))

	require.Nil(t, SetupContextForSelectedChannel(c, vertexChannel, "task-model"))
	pinnedValue, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint)
	require.True(t, exists)
	pinned, ok := pinnedValue.(jsplugin.PinnedEndpoint)
	require.True(t, ok)
	assert.Equal(t, "vertex-shared", pinned.Plugin.Meta.Key)
	assert.Equal(t, "vertex-shared", c.GetString("expected_task_plugin_key"))
	assert.Equal(t, "vertex-shared", c.GetString("task_plugin_key"))
	assert.True(t, channelMatchesExpectedTaskPlugin(c, geminiChannel, "vertex-shared"), "a retry may select another declared provider")
}

func distributorTaskPluginSource(key string, channelType int) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1,
  key: %q,
  name: %q,
  version: "1.0.0",
  author: {name: "Test"},
  channelTypes: [%d],
  models: ["task-model"],
  fetchMode: "per_task",
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
`, key, key, channelType)
}

func distributorEndpointPluginSource(key string, channelType int) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1,
  key: %q,
  name: %q,
  version: "1.0.0",
  author: {name: "Test"},
  channelTypes: [%d],
  models: ["task-model"],
  fetchMode: "per_task",
  protocols: [{name: "openai_responses", supports: ["stream", "sync", "background"]}],
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export const protocols = {openai_responses: {
  decodeRequest: function(ctx) { return {kind: "submit", model: "task-model", requestBody: ctx.body.value}; },
  renderEvents: function() { return {events: [], state: null, done: false}; },
  renderFinal: function() { return {output: []}; },
}};
`, key, key, channelType)
}

func TestTokenModelLimitAllowsLegacyAliasAndModifierVariant(t *testing.T) {
	aliasOnly := map[string]bool{"claude-3-7-sonnet-thinking": true}
	assert.True(t, tokenModelLimitAllows(aliasOnly, "claude-3-7-sonnet-thinking"))
	assert.False(t, tokenModelLimitAllows(aliasOnly, "claude-3-7-sonnet"))

	baseOnly := map[string]bool{"claude-3-7-sonnet": true}
	assert.True(t, tokenModelLimitAllows(baseOnly, "claude-3-7-sonnet@thinking:on"))
	assert.True(t, tokenModelLimitAllows(baseOnly, "claude-3-7-sonnet-thinking"))

	wildcard := map[string]bool{"gemini-2.5-flash-thinking-*": true}
	assert.True(t, tokenModelLimitAllows(wildcard, "gemini-2.5-flash-thinking-8192"))
}

func TestTokenModelLimitAllowsExemptAtNameByFullName(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := append([]string(nil), settings.ThinkingModelBlacklist...)
	t.Cleanup(func() { settings.ThinkingModelBlacklist = original })
	settings.ThinkingModelBlacklist = append(original, "re:.*@sha256:.*")

	fullOnly := map[string]bool{"opaque@sha256:deadbeef": true}
	assert.True(t, tokenModelLimitAllows(fullOnly, "opaque@sha256:deadbeef"))

	baseOnly := map[string]bool{"opaque": true}
	assert.False(t, tokenModelLimitAllows(baseOnly, "opaque@sha256:deadbeef"))
}

func TestNoAvailableChannelMessageNamesClaimingTaskPlugin(t *testing.T) {
	require.NoError(t, i18n.Init())
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(distributorTaskPluginSource("claimer", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)

	pinned, _ := gin.CreateTestContext(nil)
	pinned.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	pinned.Request.Header.Set("Accept-Language", "en")
	pinned.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})
	message := noAvailableChannelMessage(pinned, "default", "kling-v1")
	assert.Contains(t, message, `"claimer"`)
	assert.Contains(t, message, "disable or override")
	assert.Contains(t, message, "kling-v1")

	plain, _ := gin.CreateTestContext(nil)
	plain.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	plain.Request.Header.Set("Accept-Language", "en")
	generic := noAvailableChannelMessage(plain, "default", "gpt-4o")
	assert.NotContains(t, generic, "task plugin")
	assert.Contains(t, generic, "gpt-4o")
}

func TestDistributeProviderRoutingConstraints(t *testing.T) {
	require.NoError(t, i18n.Init())
	originalDB := model.DB
	originalMemory := common.MemoryCacheEnabled
	t.Cleanup(func() {
		if originalMemory && originalDB != nil && originalDB.Migrator().HasTable(&model.Channel{}) && originalDB.Migrator().HasTable(&model.Ability{}) {
			model.InitChannelCache()
		}
	})
	setupOriginTaskDB(t)
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
	previousMemory, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
	common.MemoryCacheEnabled, common.RedisEnabled = true, false
	t.Cleanup(func() { common.MemoryCacheEnabled, common.RedisEnabled = previousMemory, previousRedis })
	previousRatios := ratio_setting.GroupRatio2JSONString()
	previousMaxGroups := setting.GetMaxTokenAutoGroups()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1,"incompatible-only":1}`))
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("2"))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(fmt.Sprint(previousMaxGroups)))
	})
	previousUsable := setting.UserUsableGroups2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","incompatible-only":"Incompatible"}`))
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsable)) })
	affinity := operation_setting.GetChannelAffinitySetting()
	previousAffinity := *affinity
	*affinity = operation_setting.ChannelAffinitySetting{Enabled: true, DefaultTTLSeconds: 60, KeepOnChannelDisabled: true, Rules: []operation_setting.ChannelAffinityRule{{
		Name: "provider-routing-test", ModelRegex: []string{"^routing-model$"}, KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Test-Affinity"}}, IncludeRuleName: true, IncludeUsingGroup: true,
	}}}
	t.Cleanup(func() { *affinity = previousAffinity })
	for _, channel := range []model.Channel{
		{Id: 920001, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "fixture-key", Name: "incompatible", Group: "default", Models: "routing-model", Priority: common.GetPointer(int64(100))},
		{Id: 920002, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Key: "fixture-key", Name: "compatible", Group: "vip", Models: "routing-model", Priority: common.GetPointer(int64(10))},
	} {
		require.NoError(t, model.DB.Create(&channel).Error)
		require.NoError(t, model.DB.Create(&model.Ability{Group: channel.Group, Model: channel.Models, ChannelId: channel.Id, Enabled: true, Priority: channel.Priority, Weight: 100}).Error)
	}
	// The same model is also offered by OR in default, behind the incompatible
	// channel's higher priority. Auto's first group deliberately has no OR.
	require.NoError(t, model.DB.Create(&model.Ability{Group: "default", Model: "routing-model", ChannelId: 920002, Enabled: true, Priority: common.GetPointer(int64(10)), Weight: 100}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{Group: "incompatible-only", Model: "routing-model", ChannelId: 920001, Enabled: true, Priority: common.GetPointer(int64(100)), Weight: 100}).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 920001).Update("group", "default,incompatible-only").Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 920002).Update("group", "default,vip").Error)
	model.InitChannelCache()
	for _, tc := range []struct {
		name, body, path, group, contentType string
		pin, affinityID, wantChannel         int
		wantError                            string
	}{
		{name: "without provider", body: `{"model":"routing-model"}`, wantChannel: 920001},
		{name: "explicit null", body: `{"model":"routing-model","provider":null}`, wantChannel: 920001},
		{name: "Claude messages provider", path: "/v1/messages", body: `{"model":"routing-model","max_tokens":8,"messages":[{"role":"user","content":"hello"}],"provider":{"only":["openai"]}}`, wantChannel: 920002},
		{name: "top level", body: `{"model":"routing-model","provider":{"only":["openai"],"allow_fallbacks":false}}`, wantChannel: 920002},
		{name: "nested", body: `{"model":"routing-model","extra_body":{"provider":{"only":["openai"]}}}`, wantChannel: 920002},
		{name: "matching dual shape", body: `{"model":"routing-model","provider":{"only":["openai"]},"extra_body":{"provider":{"only":["openai"]}}}`, wantChannel: 920002},
		{name: "duplicate provider rejected", body: `{"model":"routing-model","provider":{"only":["azure"]},"provider":{"only":["openai"]}}`, wantError: "duplicate JSON keys"},
		{name: "urlencoded provider rejected", path: "/v1/audio/speech", contentType: "application/x-www-form-urlencoded", body: `model=routing-model&input=test&provider=%7B%22zdr%22%3Atrue%7D`, wantError: "require a JSON request"},
		{name: "urlencoded without provider unchanged", path: "/v1/audio/speech", contentType: "application/x-www-form-urlencoded", body: `model=routing-model&input=test`, wantChannel: 920001},
		{name: "conflict", body: `{"model":"routing-model","provider":{"only":["openai"]},"extra_body":{"provider":{"only":["azure"]}}}`, wantError: "conflicting provider"},
		{name: "pinned incompatible channel", body: `{"model":"routing-model","provider":{"only":["openai"]}}`, pin: 920001, wantError: "provider_routing"},
		{name: "pinned compatible channel", body: `{"model":"routing-model","provider":{"only":["openai"]}}`, pin: 920002, wantChannel: 920002},
		{name: "pin without provider unchanged", body: `{"model":"routing-model"}`, pin: 920001, wantChannel: 920001},
		{name: "incompatible affinity ignored", body: `{"model":"routing-model","provider":{"only":["openai"]}}`, affinityID: 920001, wantChannel: 920002},
		{name: "no compatible channel", body: `{"model":"routing-model","provider":{"only":["openai"]}}`, group: "incompatible-only", wantError: "No available channel"},
		{name: "auto skips incompatible group", body: `{"model":"routing-model","provider":{"only":["openai"]}}`, group: "auto", wantChannel: 920002},
		{name: "TTS rejects executor selection", path: "/v1/audio/speech", body: `{"model":"routing-model","input":"test","provider":{"only":["openai"]}}`, wantError: "unsupported provider field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if path == "" {
				path = "/v1/chat/completions"
			}
			group := tc.group
			if group == "" {
				group = "default"
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(tc.body))
			contentType := tc.contentType
			if contentType == "" {
				contentType = "application/json"
			}
			c.Request.Header.Set("Content-Type", contentType)
			common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			if group == "auto" {
				common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, []string{"incompatible-only", "vip"})
				require.Equal(t, []string{"incompatible-only", "vip"}, service.GetRequestAutoGroups(c, "default"))
			}
			if tc.pin != 0 {
				service.GetChannelConstraints(c).AddPin(taskdto.ChannelPin{ChannelId: tc.pin, Source: taskdto.PinSourceToken, Rank: taskdto.PinRankToken})
			}
			if tc.affinityID != 0 {
				c.Request.Header.Set("X-Test-Affinity", t.Name())
				_, _ = service.GetPreferredChannelByAffinity(c, "routing-model", group)
				service.RecordChannelAffinity(c, tc.affinityID)
				t.Cleanup(func() { service.ClearCurrentChannelAffinityCache(c) })
				id, found := service.GetPreferredChannelByAffinity(c, "routing-model", group)
				require.True(t, found)
				require.Equal(t, tc.affinityID, id)
			}
			Distribute()(c)
			if tc.wantError != "" {
				require.True(t, c.IsAborted())
				assert.Contains(t, recorder.Body.String(), tc.wantError)
				assert.Zero(t, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
				return
			}
			require.False(t, c.IsAborted(), recorder.Body.String())
			require.Equal(t, tc.wantChannel, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
			if tc.wantChannel == 920002 && tc.pin == 0 {
				retry := service.RetryParam{Ctx: c, TokenGroup: group, ModelName: "routing-model", RequestPath: path, Retry: common.GetPointer(1)}
				selected, _, err := service.CacheGetRandomSatisfiedChannel(&retry)
				require.NoError(t, err)
				require.NotNil(t, selected)
				assert.Equal(t, 920002, selected.Id)
			}
		})
	}
}
