package taskruntime

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gateway/taskplugins"
	gormtasks "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/list_task_artifacts"
	"github.com/QuantumNous/new-api/internal/tasks/transport"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
)

func setupGenericTaskTest(t *testing.T) *model.Task {
	t.Helper()
	originalDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Task{}, &model.Channel{}, &model.User{}))
	model.DB = database
	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = previousRedisEnabled
	})

	require.NoError(t, database.Create(&model.User{
		Id: 7, Username: "artifact-owner", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, Group: "default",
	}).Error)
	baseURL := "https://example.com"
	require.NoError(t, database.Create(&model.Channel{
		Id: 1, Name: "artifact", Key: "key", BaseURL: &baseURL, Status: common.ChannelStatusEnabled,
	}).Error)
	task := &model.Task{
		TaskID: "task_generic", Platform: "document", UserId: 7, ChannelId: 1,
		Status: model.TaskStatusSuccess, Progress: "100%", SubmitTime: 10, FinishTime: 20,
	}
	require.NoError(t, database.Create(task).Error)
	return task
}

func allowPrivateTaskMediaTest(t *testing.T) {
	t.Helper()
	originalFetchSetting := *system_setting.GetFetchSetting()
	system_setting.GetFetchSetting().EnableSSRFProtection = true
	system_setting.GetFetchSetting().AllowPrivateIp = true
	system_setting.GetFetchSetting().AllowedPorts = []string{"1-65535"}
	t.Cleanup(func() { *system_setting.GetFetchSetting() = originalFetchSetting })
	service.InitHttpClient()
}

func TestGetTaskDoesNotProjectArtifacts(t *testing.T) {
	task := setupGenericTaskTest(t)
	task.FailReason = "https://stale-upstream.invalid/video.mp4"
	task.PrivateData = model.TaskPrivateData{
		ResultURL: "https://private-upstream.invalid/video.mp4",
		Execution: &model.TaskExecutionSnapshot{
			TaskPlugin: &model.TaskPluginSnapshot{Key: "missing-plugin", Name: "Missing"},
		},
	}
	require.NoError(t, model.DB.Save(task).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 7)
	c.Params = gin.Params{{Key: "key", Value: task.TaskID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID, nil)

	NewHTTP().GetTask(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, task.TaskID, response["task_id"])
	assert.NotContains(t, response, "artifacts")
	assert.NotContains(t, recorder.Body.String(), "upstream.invalid")
}

func TestGetTaskArtifactsReturnsEmptyForLegacyTask(t *testing.T) {
	task := setupGenericTaskTest(t)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", task.UserId)
	c.Params = gin.Params{{Key: "key", Value: task.TaskID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID+"/artifacts", nil)

	NewHTTP().GetTaskArtifacts(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	var response struct {
		TaskID    string                         `json:"task_id"`
		Artifacts []list_task_artifacts.Artifact `json:"artifacts"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, task.TaskID, response.TaskID)
	assert.Empty(t, response.Artifacts)
}

func TestTaskArtifactAuthorizationKeepsForeignTasksHidden(t *testing.T) {
	task := setupGenericTaskTest(t)

	commonUser, _ := gin.CreateTestContext(httptest.NewRecorder())
	commonUser.Set("id", 8)
	commonUser.Set("role", common.RoleCommonUser)
	_, exists, err := readArtifactTaskForTest(commonUser, task.TaskID)
	require.NoError(t, err)
	assert.False(t, exists)

	admin, _ := gin.CreateTestContext(httptest.NewRecorder())
	admin.Set("id", 8)
	admin.Set("role", common.RoleAdminUser)
	found, exists, err := readArtifactTaskForTest(admin, task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, task.TaskID, found.TaskID)

	apiToken, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiToken.Set("id", 8)
	apiToken.Set("role", common.RoleRootUser)
	apiToken.Set("token_id", 99)
	_, exists, err = readArtifactTaskForTest(apiToken, task.TaskID)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestDashboardTaskArtifactsReturnsLegacyCapabilityWithoutUpstreamURL(t *testing.T) {
	task := setupGenericTaskTest(t)
	previousSecret := common.CryptoSecret
	previousPublicAddress := system_setting.TaskPublicAddress
	common.CryptoSecret = "controller-task-artifact-access-secret"
	system_setting.TaskPublicAddress = "https://gateway.example/prefix"
	t.Cleanup(func() {
		common.CryptoSecret = previousSecret
		system_setting.TaskPublicAddress = previousPublicAddress
	})
	task.Action = constant.TaskActionTextToVideo
	task.FailReason = "https://upstream.invalid/private-video.mp4?signature=secret"
	require.NoError(t, model.DB.Save(task).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", task.UserId)
	c.Set("role", common.RoleCommonUser)
	c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/task/"+task.TaskID+"/artifacts", nil)

	NewHTTP().GetDashboardTaskArtifacts(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Artifacts        []list_task_artifacts.Artifact `json:"artifacts"`
			LegacyContentURL string                         `json:"legacy_content_url"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Empty(t, response.Data.Artifacts)
	contentURL, err := url.Parse(response.Data.LegacyContentURL)
	require.NoError(t, err)
	assert.Equal(t, "/prefix/v1/tasks/"+task.TaskID+"/artifacts/video/content", contentURL.Path)
	assert.True(t, service.VerifyTaskArtifactAccess(
		contentURL.Query().Get(service.TaskArtifactAccessQueryParameter),
		task.TaskID,
		"video",
	))
	assert.NotContains(t, recorder.Body.String(), "upstream.invalid")
	assert.NotContains(t, recorder.Body.String(), "signature=secret")
}

func TestTaskArtifactAccessRequiresActiveOwner(t *testing.T) {
	task := setupGenericTaskTest(t)
	task.Action = constant.TaskActionTextToVideo
	task.FailReason = "https://upstream.invalid/private-video.mp4"
	require.NoError(t, model.DB.Save(task).Error)
	require.NoError(t, model.DB.Model(&model.User{}).
		Where("id = ?", task.UserId).
		Update("status", common.UserStatusDisabled).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(middleware.TaskArtifactAccessContextKey, true)
	c.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)

	NewHTTP().TaskArtifactContent(c)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
}

func TestTaskArtifactAccessRejectsAmbiguousHistoricalTaskID(t *testing.T) {
	task := setupGenericTaskTest(t)
	task.Action = constant.TaskActionTextToVideo
	task.FailReason = "https://first-upstream.invalid/video.mp4"
	require.NoError(t, model.DB.Save(task).Error)
	require.NoError(t, model.DB.Create(&model.User{
		Id: 8, Username: "other-artifact-owner", Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, Group: "default", AffCode: "artifact-owner-8",
	}).Error)
	require.NoError(t, model.DB.Create(&model.Task{
		TaskID: task.TaskID, Platform: task.Platform, UserId: 8, ChannelId: task.ChannelId,
		Action: constant.TaskActionTextToVideo, Status: model.TaskStatusSuccess,
		FailReason: "https://second-upstream.invalid/video.mp4",
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(middleware.TaskArtifactAccessContextKey, true)
	c.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)

	NewHTTP().TaskArtifactContent(c)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "upstream.invalid")
}

func TestLegacyVideoArtifactContentUsesGetResultURL(t *testing.T) {
	task := setupGenericTaskTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bytes=0-3", r.Header.Get("Range"))
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-3/4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("data"))
	}))
	defer upstream.Close()
	allowPrivateTaskMediaTest(t)

	task.Action = constant.TaskActionTextToVideo
	task.PrivateData.ResultURL = upstream.URL
	task.FailReason = "https://stale.invalid/legacy-fallback.mp4"
	require.NoError(t, model.DB.Save(task).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(middleware.TaskArtifactAccessContextKey, true)
	c.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)
	c.Request.Header.Set("Range", "bytes=0-3")

	NewHTTP().TaskArtifactContent(c)

	assert.Equal(t, http.StatusPartialContent, recorder.Code)
	assert.Equal(t, "data", recorder.Body.String())
	assert.Equal(t, "bytes 0-3/4", recorder.Header().Get("Content-Range"))
}

func TestDisabledArtifactStorePreservesPluginUpstreamContent(t *testing.T) {
	task := setupGenericTaskTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "provider-key", r.Header.Get("x-goog-api-key"))
		assert.Equal(t, "bytes=0-13", r.Header.Get("Range"))
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Range", "bytes 0-13/14")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("artifact-bytes"))
	}))
	defer upstream.Close()
	allowPrivateTaskMediaTest(t)
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })

	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", task.ChannelId).Updates(map[string]any{
		"type":     constant.ChannelTypeGemini,
		"key":      "provider-key",
		"base_url": upstream.URL,
	}).Error)
	task.Platform = constant.TaskPlatform("google")
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{TaskPlugin: &model.TaskPluginSnapshot{
		Key: "google", Name: "Google Veo (Gemini API)", Version: "1.0.0", APIVersion: 1,
	}}
	task.SetData(map[string]any{"response": map[string]any{
		"generateVideoResponse": map[string]any{
			"generatedVideos": []any{map[string]any{"video": map[string]any{"uri": upstream.URL}}},
		},
	}})
	require.NoError(t, model.DB.Save(task).Error)
	require.False(t, service.GetTaskArtifactStore().Enabled())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(middleware.TaskArtifactAccessContextKey, true)
	c.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID+"/artifacts/video/content", nil)
	c.Request.Header.Set("Range", "bytes=0-13")

	NewHTTP().TaskArtifactContent(c)

	assert.Equal(t, http.StatusPartialContent, recorder.Code)
	assert.Equal(t, "artifact-bytes", recorder.Body.String())
	assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "bytes 0-13/14", recorder.Header().Get("Content-Range"))
}

func TestOpenRouterSyncWithoutStoredResultDoesNotAdvertiseArtifacts(t *testing.T) {
	task := setupGenericTaskTest(t)
	task.PrivateData.TokenId = 1
	task.PrivateData.OpenRouterAudio = &model.OpenRouterAudioTask{ResultReady: true, ResultMimeType: "audio/pcm", ResultSize: 16}
	for _, status := range []model.TaskStatus{model.TaskStatusInProgress, model.TaskStatusSuccess} {
		task.Status = status
		require.NoError(t, model.DB.Save(task).Error)
		artifacts, err := domain.ProjectArtifacts(record.ToDomain(task), taskplugins.Artifacts{})
		require.NoError(t, err)
		assert.Empty(t, artifacts)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("id", task.UserId)
		c.Set("token_id", 1)
		c.Params = gin.Params{{Key: "key", Value: task.TaskID}, {Key: "artifact_key", Value: "result"}}
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID+"/artifacts/result/content", nil)
		NewHTTP().TaskArtifactContent(c)
		assert.Equal(t, http.StatusNotFound, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "artifact_not_found")
	}
}

func TestOpenRouterAudioArtifactsRemainAccessibleWhileBillingUnresolved(t *testing.T) {
	task := setupGenericTaskTest(t)
	// Registered first: restore the startup backend after t.Setenv restores env.
	t.Cleanup(func() { require.NoError(t, service.InitTaskArtifactStore()) })
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0700))
	t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeFilesystem)
	t.Setenv(system_setting.TaskArtifactStoreRootEnv, root)
	require.NoError(t, service.InitTaskArtifactStore())
	previousSecret := common.CryptoSecret
	common.CryptoSecret = "audio-artifact-access-fixture"
	t.Cleanup(func() { common.CryptoSecret = previousSecret })
	payload := "persisted audio"
	ref, err := service.GetTaskArtifactStore().Persist(t.Context(), task, types.TaskArtifact{Key: "result", Type: "audio", MimeType: "audio/pcm"}, strings.NewReader(payload))
	require.NoError(t, err)
	task.PrivateData.OpenRouterAudio = &model.OpenRouterAudioTask{ResultReady: true, ResultMimeType: ref.MimeType, ResultSize: ref.Size, ResultSHA256: ref.SHA256}
	// A stale upstream URL must never be used to serve a host-owned result.
	task.PrivateData.ResultURL = "https://must-not-fetch.invalid/private"
	for _, status := range []model.TaskStatus{model.TaskStatusInProgress, model.TaskStatusFailure} {
		task.Status = status
		require.NoError(t, model.DB.Save(task).Error)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Set("id", task.UserId)
		c.Params = gin.Params{{Key: "key", Value: task.TaskID}}
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID+"/artifacts", nil)
		NewHTTP().GetTaskArtifacts(c)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Artifacts []list_task_artifacts.Artifact `json:"artifacts"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.Len(t, response.Artifacts, 1)
		assert.Equal(t, "result", response.Artifacts[0].Key)
		assert.Equal(t, "audio/pcm", response.Artifacts[0].MimeType)
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			recorder = httptest.NewRecorder()
			c, _ = gin.CreateTestContext(recorder)
			c.Set("id", task.UserId)
			c.Params = gin.Params{{Key: "key", Value: task.TaskID}, {Key: "artifact_key", Value: "result"}}
			c.Request = httptest.NewRequest(method, "/v1/tasks/"+task.TaskID+"/artifacts/result/content", nil)
			NewHTTP().TaskArtifactContent(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			if method == http.MethodGet {
				assert.Equal(t, payload, recorder.Body.String())
			} else {
				assert.Empty(t, recorder.Body.String())
			}
		}
	}
	require.NoError(t, os.Remove(filepath.Join(root, ref.ObjectKey)))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", task.UserId)
	c.Params = gin.Params{{Key: "key", Value: task.TaskID}, {Key: "artifact_key", Value: "result"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.TaskID+"/artifacts/result/content", nil)
	NewHTTP().TaskArtifactContent(c)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "artifact_storage_unavailable")
}

func readArtifactTaskForTest(c *gin.Context, taskID string) (*domain.Task, bool, error) {
	reader := gormtasks.Reader{Store: gormtasks.NewStore(func() *gorm.DB { return model.DB })}
	return domain.ResolveTaskAccess(reader, transport.ReadTaskAccess(c, middleware.IsTaskArtifactAccess(c)), taskID)
}
