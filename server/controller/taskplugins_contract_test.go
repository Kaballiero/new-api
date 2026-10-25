package controller

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	pluginsapp "github.com/QuantumNous/new-api/internal/app/taskplugins"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/pluginfixture"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	pluginsdomain "github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var pluginsManifest = flag.Bool("plugins-manifest", false, "list private plugin contracts without fixtures")
var pluginsPreflight = flag.Bool("plugins-preflight", false, "emit private plugin snapshots without oracle IO")
var pluginsOriginCheck = flag.Bool("plugins-origin-check", false, "check origin boundary without fixtures")

func init() { taskfixture.RegisterUpdate(); taskfixture.RejectCandidateUpdate() }

type pluginPrivateCase struct{ Name, Kind, Variant string }

func pluginPrivateCases() []pluginPrivateCase {
	cases := []pluginPrivateCase{}
	for _, group := range []struct {
		kind     string
		variants []string
	}{
		{"sync", []string{"empty", "first", "same", "batch", "removed", "disabled", "compile_new", "compile_incumbent",
			"recover", "query_error", "publish_error", "metadata_only", "revision_disabled", "follower"}},
		{"limiter", []string{"global", "plugin", "user", "token", "invalid_plugin", "invalid_user", "invalid_token", "release"}},
		{"observe", []string{"sync_success", "sync_failure", "sync_timeout", "sync_load_error", "sync_missing", "sync_hook_error",
			"stream_success", "stream_failure", "stream_timeout", "stream_load_error", "stream_hook_error", "stream_state_null",
			"stream_state_absent", "stream_invalid_events", "stream_heartbeat", "background", "background_stream", "background_write_error",
			"admission_denied", "invalid_identity", "unsupported", "bad_context", "invalid_outcome", "submission_400", "submission_401",
			"submission_403", "submission_429", "submission_502", "disconnect_submission", "disconnect_observation", "db_overload"}},
		{"alias", []string{"exposed", "folded", "cycle", "hidden", "many_tails", "many_plugins", "ttl", "generation", "disabled_channel"}},
	} {
		for _, variant := range group.variants {
			cases = append(cases, pluginPrivateCase{group.kind + "_" + variant, group.kind, variant})
		}
	}
	return cases
}

func TestTaskPluginsPrivateContract(t *testing.T) {
	updating := fixture.Updating(t)
	if *pluginsOriginCheck {
		taskfixture.CheckRecording(t)
		return
	}
	cases := pluginPrivateCases()
	names := make([]string, len(cases))
	seen := map[string]bool{}
	for i, tc := range cases {
		require.False(t, seen[tc.Name])
		seen[tc.Name] = true
		names[i] = tc.Name
	}
	if *pluginsManifest {
		data, err := common.Marshal(cases)
		require.NoError(t, err)
		fmt.Println("PLUGINS_PRIVATE_MANIFEST " + string(data))
		return
	}
	fixture.Processes(t, "TestTaskPluginsPrivateContract", names, *pluginsPreflight, updating, func(name string) {
		for _, tc := range cases {
			if tc.Name != name {
				continue
			}
			taskfixture.Open(t)
			require.NoError(t, model.DB.AutoMigrate(&model.TaskPlugin{}, &model.Option{}, &model.Ability{}))
			synctest.Test(t, func(t *testing.T) {
				taskfixture.AssertDBClock(t)
				var result any
				switch tc.Kind {
				case "sync":
					result = pluginSyncContract(t, tc.Variant)
				case "limiter":
					result = pluginLimiterContract(t, tc.Variant)
				case "observe":
					result = pluginObservationContract(t, tc.Variant)
				case "alias":
					result = pluginAliasContract(t, tc.Variant)
				}
				taskfixture.Compare(t, "testdata/taskplugins", tc.Name, result, *pluginsPreflight, updating)
			})
			return
		}
	})
}

func pluginSyncContract(t *testing.T, variant string) any {
	observations := []any{}
	start := pluginruntime.DefaultRegistry.Generation().Number
	run := func(public bool) {
		var err error
		if public {
			SyncTaskPluginsOnce()
		} else {
			err = syncTaskPluginsOnceContext(context.Background())
		}
		observations = append(observations, pluginSyncObservation(t, start, err))
	}
	if variant == "empty" {
		run(true)
		return observations
	}
	first := fixture.Save(t, fixture.Key, "1.0.0", true)
	run(true)
	switch variant {
	case "same", "first":
		run(false)
	case "batch":
		fixture.Save(t, "second-plugin", "1.0.0", true)
		run(false)
	case "removed":
		require.NoError(t, model.DB.Delete(first).Error)
		run(false)
	case "disabled", "revision_disabled":
		require.NoError(t, pluginsapp.NewStore().SetTaskPluginEnabled(fixture.Key, false))
		run(false)
	case "metadata_only":
		require.NoError(t, model.DB.Model(first).Update("remark", "new remark").Error)
		run(false)
	case "compile_new":
		fixture.Save(t, "invalid-plugin", "1.0.0", true)
		require.NoError(t, model.DB.Model(&model.TaskPlugin{}).
			Where("key=?", "invalid-plugin").Updates(map[string]any{"source": "export const =", "source_hash": "bad"}).Error)
		run(false)
	case "compile_incumbent", "recover":
		originalSource, originalHash := first.Source, first.SourceHash
		require.NoError(t, model.DB.Model(first).Updates(map[string]any{"source": "export const =", "source_hash": "bad"}).Error)
		run(false)
		if variant == "recover" {
			require.NoError(t, model.DB.Model(first).Updates(map[string]any{"source": originalSource, "source_hash": originalHash}).Error)
			run(false)
		}
	case "query_error":
		require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("plugin-sync-fault", func(tx *gorm.DB) {
			if tx.Statement.Table == "task_plugins" {
				tx.AddError(errors.New("fixture sync read failure"))
			}
		}))
		err := syncTaskPluginsOnce()
		require.Error(t, err)
		require.NoError(t, model.DB.Callback().Query().Remove("plugin-sync-fault"))
		observations = append(observations, pluginSyncObservation(t, start, err))
		run(false)
	case "publish_error":
		calls := 0
		require.NoError(t, pluginruntime.DefaultRegistry.SetGenerationPreparer(func(
			candidate, current *pluginruntime.RoutingGeneration,
		) (pluginruntime.PreparedRoutingGeneration, error) {
			calls++
			if calls > 1 {
				return pluginruntime.PreparedRoutingGeneration{}, errors.New("fixture publish failure")
			}
			return pluginruntime.PreparedRoutingGeneration{Generation: candidate.WithRuntime(current.RuntimeHandler())}, nil
		}))
		fixture.Save(t, "new-plugin", "1.0.0", true)
		run(false)
	case "follower":
		common.IsMasterNode = false
		run(true)
	}
	return observations
}

func pluginSyncObservation(t *testing.T, start uint64, err error) any {
	message := ""
	if err != nil {
		message = err.Error()
	}
	taskPluginSyncState.Lock()
	hashes := map[string]string{}
	for key, value := range taskPluginSyncState.Hashes {
		hashes[key] = value
	}
	errorsByKey := map[string]string{}
	for key, value := range taskPluginSyncState.Errors {
		errorsByKey[key] = value
	}
	outcome := taskPluginSyncState.LastRebuild
	outcome.Generation -= start
	taskPluginSyncState.Unlock()
	effective := map[string]string{}
	for key, p := range pluginruntime.DefaultRegistry.ActiveOverridePlugins() {
		effective[key] = p.Meta.Version
	}
	return map[string]any{"error": message, "generation_delta": pluginruntime.DefaultRegistry.Generation().Number - start,
		"outcome": outcome, "hashes": hashes, "errors": errorsByKey, "effective": effective, "state": fixture.State(t)}
}

func pluginLimiterContract(t *testing.T, variant string) any {
	limits := pluginsdomain.ObservationLimits{Global: 3, PerPlugin: 3, PerUser: 3, PerToken: 3}
	switch variant {
	case "global":
		limits.Global = 1
	case "plugin":
		limits.PerPlugin = 1
	case "user":
		limits.PerUser = 1
	case "token":
		limits.PerToken = 1
	}
	limiter := pluginsdomain.NewObservationLimiter(limits)
	results := []any{}
	held, err := limiter.Acquire("fixture", 1, 1)
	require.NoError(t, err)
	key, user, token := "other", 2, 2
	switch variant {
	case "plugin":
		key = "fixture"
	case "user":
		user = 1
	case "token":
		token = 1
	case "invalid_plugin":
		key = " "
	case "invalid_user":
		user = 0
	case "invalid_token":
		token = 0
	}
	release, err := limiter.Acquire(key, user, token)
	message := ""
	if err != nil {
		message = err.Error()
	}
	results = append(results, map[string]any{"admitted": release != nil, "error": message, "active": observationCount(t, limiter, limits.Global)})
	if release != nil {
		release()
		release()
	}
	held()
	held()
	assert.Zero(t, observationCount(t, limiter, limits.Global))
	reset, err := limiter.Acquire("fixture", 1, 1)
	require.NoError(t, err)
	reset()
	return map[string]any{"results": results, "after_release": observationCount(t, limiter, limits.Global), "reset_admitted": true}
}

func pluginObservationContract(t *testing.T, variant string) any {
	stream := strings.HasPrefix(variant, "stream_") || variant == "background_stream"
	hooks := `export const protocols={openai_responses:{
renderFinal:function(){return {output:[]};},renderEvents:function(){return {events:[],done:true};}}};`
	if variant == "stream_state_null" {
		hooks = `let n=0;export const protocols={openai_responses:{renderFinal:function(){return {};},
renderEvents:function(ctx,task,previous){n++;if(n===1)return {events:[],state:null,done:false};
if(previous!==null)throw new Error("explicit null state lost");return {events:[],done:true};}}};`
	}
	if variant == "stream_state_absent" {
		hooks = `let n=0;export const protocols={openai_responses:{renderFinal:function(){return {};},
renderEvents:function(){n++;if(n===1)return {events:[],done:false};
if(arguments.length!==2)throw new Error("absent state unexpectedly supplied");return {events:[],done:true};}}};`
	}
	if strings.Contains(variant, "hook_error") {
		hooks = `export const protocols={openai_responses:{renderFinal:function(){throw new Error("private secret");},
renderEvents:function(){throw new Error("private secret");}}};`
	}
	if variant == "stream_invalid_events" {
		hooks = `export const protocols={openai_responses:{renderFinal:function(){return {};},
renderEvents:function(){return {events:[{type:"forbidden"}],done:true};}}};`
	}
	if variant == "stream_timeout" || variant == "stream_heartbeat" {
		hooks = `export const protocols={openai_responses:{renderFinal:function(){return {};},
renderEvents:function(){return {events:[],done:false};}}};`
	}
	pinned := compilePluginProtocolTestEndpoint(t, "contract-observer", hooks)
	c, response := newPluginProtocolTestContext(stream, stream)
	c.Set("resolved_task_model", "video-model")
	deps := pluginProtocolTestDeps()
	deps.now = time.Now
	deps.observationTimeout = 5500 * time.Millisecond
	deps.tickInterval = time.Second
	deps.heartbeatInterval = 700 * time.Millisecond
	deps.loadTimeout = 500 * time.Millisecond
	deps.submissionTimeout = time.Minute
	if variant == "stream_timeout" {
		deps.heartbeatInterval = time.Hour
	}
	task := &model.Task{ID: 1, UserId: 71, TaskID: "task_seed_observer", Platform: "contract-observer", Status: model.TaskStatusSuccess,
		CreatedAt: taskfixture.Epoch, UpdatedAt: taskfixture.Epoch, Properties: model.Properties{OriginModelName: "video-model"}}
	require.NoError(t, model.DB.Create(task).Error)
	submits, loads := 0, 0
	calls := []string{}
	clientContext, cancelClient := context.WithCancel(c.Request.Context())
	defer cancelClient()
	c.Request = c.Request.WithContext(clientContext)
	deps.submit = func(current *gin.Context, info *relaycommon.RelayInfo) (*taskSubmissionOutcome, *dto.TaskError) {
		submits++
		calls = append(calls, "submit")
		assert.Empty(t, response.Body.String())
		assert.False(t, response.Flushed)
		if variant == "disconnect_submission" {
			cancelClient()
			require.NoError(t, current.Request.Context().Err())
		}
		if strings.HasPrefix(variant, "submission_") {
			status := map[string]int{"submission_400": 400, "submission_401": 401,
				"submission_403": 403, "submission_429": 429, "submission_502": 502}[variant]
			return nil, &dto.TaskError{Code: "invalid_request", Message: "fixture validation", StatusCode: status}
		}
		outcome := pluginProtocolTestOutcome(info, pinned.Plugin.Meta.Key, task.TaskID)
		outcome.Task = task
		if variant == "invalid_outcome" {
			outcome.Task = &model.Task{UserId: 999, Platform: task.Platform}
		}
		return outcome, nil
	}
	deps.loadTask = func(ctx context.Context, user int, platform constant.TaskPlatform, id string) (*model.Task, bool, error) {
		loads++
		calls = append(calls, "load")
		require.Equal(t, 71, user)
		require.Equal(t, task.Platform, platform)
		require.Equal(t, task.TaskID, id)
		switch variant {
		case "sync_load_error", "stream_load_error":
			return nil, false, errors.New("private database credentials")
		case "sync_missing":
			return nil, false, nil
		case "disconnect_observation":
			cancelClient()
			return nil, false, context.Canceled
		case "db_overload":
			if loads == 1 {
				<-ctx.Done()
				return nil, false, ctx.Err()
			}
		}
		copy := *task
		if variant == "sync_failure" || variant == "stream_failure" {
			copy.Status = model.TaskStatusFailure
			copy.FailReason = "private failure"
		}
		if variant == "sync_timeout" || variant == "stream_timeout" || variant == "stream_heartbeat" || loads == 1 &&
			(variant == "stream_state_null" || variant == "stream_state_absent") {
			copy.Status = model.TaskStatusInProgress
		}
		return &copy, true, nil
	}
	if strings.HasPrefix(variant, "background") {
		setProtocolRequestBackground(c, true)
	}
	if variant == "background_write_error" {
		require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register("background-fault",
			func(tx *gorm.DB) { tx.AddError(errors.New("fixture background write failure")) }))
		defer model.DB.Callback().Update().Remove("background-fault")
	}
	if variant == "admission_denied" {
		deps.admissions = pluginsdomain.NewObservationLimiter(pluginsdomain.ObservationLimits{})
	}
	if variant == "invalid_identity" {
		common.SetContextKey(c, constant.ContextKeyTokenId, 0)
	}
	if variant == "unsupported" {
		pinned.Protocol = "unsupported"
	}
	if variant == "bad_context" {
		c.Set(pluginruntime.ContextKeyProtocolRequest, "invalid")
	}
	observationGlobalLimit := pluginsdomain.DefaultObservationLimits.Global
	if variant == "admission_denied" {
		observationGlobalLimit = 0
	}
	serveTaskPluginProtocol(c, pinned, deps)
	assert.NotContains(t, response.Body.String(), "private secret")
	assert.NotContains(t, response.Body.String(), "private database credentials")
	assert.Zero(t, observationCount(t, deps.admissions, observationGlobalLimit), "every exit releases observation capacity")
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, 1).Error)
	return map[string]any{"status": response.Code, "headers": response.Header(), "body": response.Body.String(), "flushed": response.Flushed,
		"submits": submits, "loads": loads, "calls": calls, "stored": stored, "stored_private": stored.PrivateData,
		"elapsed_ms": time.Now().UnixMilli() - taskfixture.Epoch*1000}
}

func pluginAliasContract(t *testing.T, variant string) any {
	first := fixture.Register(t, fixture.Key, "1.0.0")
	if variant == "many_plugins" {
		source := strings.ReplaceAll(fixture.Source("other-plugin", "1.0.0"), "other-plugin-video", "other-model")
		source = strings.ReplaceAll(source, "/wave5c/", "/other-wave5c/")
		_, err := pluginruntime.DefaultRegistry.Register(source, pluginruntime.Options{})
		require.NoError(t, err)
	}
	models, mapping := "alias", `{"alias":"contract-video"}`
	if variant == "cycle" {
		mapping = `{"alias":"b","b":"alias"}`
	}
	if variant == "hidden" {
		models = "other"
	}
	channel := &model.Channel{Id: 71, Type: constant.ChannelTypeTaskPlugin, Models: models, ModelMapping: &mapping, Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(channel).Error)
	if variant == "many_tails" || variant == "many_plugins" {
		other := "other-model"
		if variant == "many_tails" {
			source := strings.ReplaceAll(fixture.Source(fixture.Key, "2.0.0"), `models:["contract-video"]`, `models:["contract-video","other-model"]`)
			_, err := pluginruntime.DefaultRegistry.Register(source, pluginruntime.Options{})
			require.NoError(t, err)
		}
		otherMapping := `{"ALIAS":"` + other + `"}`
		require.NoError(t, model.DB.Create(&model.Channel{Id: 72, Models: "ALIAS", ModelMapping: &otherMapping, Status: common.ChannelStatusEnabled}).Error)
	}
	if variant == "disabled_channel" {
		require.NoError(t, model.DB.Model(channel).Update("status", common.ChannelStatusManuallyDisabled).Error)
	}
	name := "alias"
	if variant == "folded" {
		name = "ALIAS"
	}
	generation := pluginruntime.DefaultRegistry.Generation()
	target, ok := model.ResolveTaskModelAlias(generation, name)
	result := []any{map[string]any{"target": target, "found": ok, "plugin": first.Meta.Key}}
	if variant == "ttl" || variant == "generation" {
		require.NoError(t, model.DB.Model(channel).Update("models", "hidden").Error)
		if variant == "ttl" {
			time.Sleep(60 * time.Second)
		} else {
			fixture.Register(t, fixture.Key, "2.0.0")
			generation = pluginruntime.DefaultRegistry.Generation()
		}
		next, found := model.ResolveTaskModelAlias(generation, name)
		result = append(result, map[string]any{"target": next, "found": found})
	}
	return result
}
