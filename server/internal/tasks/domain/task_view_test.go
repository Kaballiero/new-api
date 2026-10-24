package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

func TestTaskLogDTOSeparatesUserAdminAndRootDetails(t *testing.T) {
	task := &domain.Task{
		TaskID:   "task_public",
		Platform: "document-parser",
		PrivateData: domain.TaskPrivateData{
			Key:            "channel-secret-canary",
			UpstreamTaskID: "upstream-private",
			NodeName:       "node-a",
			Execution: &domain.TaskExecutionSnapshot{
				RequestID:   "request-public",
				RequestPath: "/v1/documents",
				TaskPlugin: &domain.TaskPluginSnapshot{
					Key:     "document-parser",
					Name:    "Document Parser",
					Version: "1.2.3",
					Author: &domain.TaskPluginAuthorSnapshot{
						Name: "Community Author",
						URL:  "https://plugins.example/author",
					},
					APIVersion: 1,
					Generation: 42,
				},
			},
		},
	}

	userView := domain.ProjectTaskView(task, domain.Visibility{Admin: common.RoleCommonUser >= common.RoleAdminUser, Root: common.RoleCommonUser >= common.RoleRootUser}, nil)
	assert.Nil(t, userView.AdminInfo)
	assert.Nil(t, userView.RootInfo)

	adminView := domain.ProjectTaskView(task, domain.Visibility{Admin: common.RoleAdminUser >= common.RoleAdminUser, Root: common.RoleAdminUser >= common.RoleRootUser}, nil)
	require.NotNil(t, adminView.AdminInfo)
	require.NotNil(t, adminView.AdminInfo.TaskPlugin)
	assert.Equal(t, "document-parser", adminView.AdminInfo.TaskPlugin.Key)
	assert.Equal(t, "Document Parser", adminView.AdminInfo.TaskPlugin.Name)
	assert.Equal(t, "1.2.3", adminView.AdminInfo.TaskPlugin.Version)
	require.NotNil(t, adminView.AdminInfo.TaskPlugin.Author)
	assert.Equal(t, "Community Author", adminView.AdminInfo.TaskPlugin.Author.Name)
	assert.Equal(t, "https://plugins.example/author", adminView.AdminInfo.TaskPlugin.Author.URL)
	assert.Equal(t, "request-public", adminView.AdminInfo.RequestID)
	assert.Equal(t, "/v1/documents", adminView.AdminInfo.RequestPath)
	assert.Nil(t, adminView.RootInfo)

	rootView := domain.ProjectTaskView(task, domain.Visibility{Admin: common.RoleRootUser >= common.RoleAdminUser, Root: common.RoleRootUser >= common.RoleRootUser}, nil)
	require.NotNil(t, rootView.AdminInfo)
	require.NotNil(t, rootView.RootInfo)
	require.NotNil(t, rootView.RootInfo.TaskPlugin)
	assert.Equal(t, 1, rootView.RootInfo.TaskPlugin.APIVersion)
	assert.Equal(t, uint64(42), rootView.RootInfo.TaskPlugin.Generation)
	assert.Equal(t, "upstream-private", rootView.RootInfo.UpstreamTaskID)
	assert.Equal(t, "node-a", rootView.RootInfo.NodeName)

	adminJSON, err := common.Marshal(adminView)
	require.NoError(t, err)
	assert.NotContains(t, string(adminJSON), "channel-secret-canary")
	assert.NotContains(t, string(adminJSON), "upstream-private")

	rootJSON, err := common.Marshal(rootView)
	require.NoError(t, err)
	assert.NotContains(t, string(rootJSON), "channel-secret-canary")
	assert.Contains(t, string(rootJSON), "upstream-private")
}

func TestTaskLogDTODoesNotInventHistoricalPluginProvenance(t *testing.T) {
	task := &domain.Task{
		TaskID:   "task_without_snapshot",
		Platform: "document-parser",
	}

	adminView := domain.ProjectTaskView(task, domain.Visibility{Admin: common.RoleAdminUser >= common.RoleAdminUser, Root: common.RoleAdminUser >= common.RoleRootUser}, nil)

	assert.Nil(t, adminView.AdminInfo)
	assert.Nil(t, adminView.RootInfo)
}

func TestTaskLogDTOReplacesLegacyVideoURLWithAvailabilityFlag(t *testing.T) {
	task := &domain.Task{
		TaskID:     "task_legacy_video",
		Platform:   "jimeng",
		Action:     constant.TaskActionTextToVideo,
		Status:     domain.TaskStatusSuccess,
		FailReason: "https://private-upstream.invalid/video.mp4?signature=secret",
	}

	view := domain.ProjectTaskView(task, domain.Visibility{Admin: common.RoleCommonUser >= common.RoleAdminUser, Root: common.RoleCommonUser >= common.RoleRootUser}, nil)
	assert.True(t, view.LegacyVideoAvailable)
	assert.Empty(t, view.ResultURL)
	assert.Empty(t, view.FailReason)
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-upstream.invalid")
	assert.NotContains(t, string(encoded), "result_url")
	assert.Contains(t, string(encoded), "legacy_video_available")
}

func TestTaskLogDTOKeepsFailureReasonAndDoesNotMarkPluginTaskLegacy(t *testing.T) {
	failed := &domain.Task{
		TaskID:     "task_failed",
		Platform:   "jimeng",
		Action:     constant.TaskActionTextToVideo,
		Status:     domain.TaskStatusFailure,
		FailReason: "provider rejected the request",
	}
	failedView := domain.ProjectTaskView(failed, domain.Visibility{Admin: common.RoleCommonUser >= common.RoleAdminUser, Root: common.RoleCommonUser >= common.RoleRootUser}, nil)
	assert.Equal(t, "provider rejected the request", failedView.FailReason)
	assert.False(t, failedView.LegacyVideoAvailable)

	pluginTask := &domain.Task{
		TaskID:     "task_plugin_video",
		Platform:   "community-video",
		Action:     constant.TaskActionTextToVideo,
		Status:     domain.TaskStatusSuccess,
		FailReason: "https://stale-upstream.invalid/plugin-video.mp4",
		PrivateData: domain.TaskPrivateData{
			ResultURL: "https://private-upstream.invalid/plugin-video.mp4",
			Execution: &domain.TaskExecutionSnapshot{
				TaskPlugin: &domain.TaskPluginSnapshot{Key: "community-video"},
			},
		},
	}
	pluginView := domain.ProjectTaskView(pluginTask, domain.Visibility{Admin: common.RoleCommonUser >= common.RoleAdminUser, Root: common.RoleCommonUser >= common.RoleRootUser}, nil)
	assert.False(t, pluginView.LegacyVideoAvailable)
	assert.Empty(t, pluginView.ResultURL)
	assert.Empty(t, pluginView.FailReason)
}

func TestProjectedTaskArtifactValidationRejectsAmbiguousIdentity(t *testing.T) {
	validated, err := domain.ValidateArtifacts([]domain.Artifact{
		{Key: "video-main", Type: "video", MimeType: "video/mp4"},
		{Key: "cover.main", Type: "image", MimeType: "image/png"},
	})
	require.NoError(t, err)
	require.Len(t, validated, 2)
	assert.Equal(t, "video-main", validated[0].Key)

	for _, artifacts := range [][]domain.Artifact{
		{{Key: "../video", Type: "video"}},
		{{Key: "video/0", Type: "video"}},
		{{Key: "video-main", Type: "video"}, {Key: "video-main", Type: "image"}},
		{{Key: "video-main", Type: "unknown"}},
		{{Key: "video-main", Type: "video", MimeType: "video/mp4\r\nX-Test: injected"}},
	} {
		_, err := domain.ValidateArtifacts(artifacts)
		assert.ErrorIs(t, err, domain.ErrArtifactPlugin)
	}
}
