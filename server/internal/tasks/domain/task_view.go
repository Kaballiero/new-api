package domain

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/constant"
)

type TaskView struct {
	ID                   int64           `json:"id"`
	CreatedAt            int64           `json:"created_at"`
	UpdatedAt            int64           `json:"updated_at"`
	TaskID               string          `json:"task_id"`
	Platform             string          `json:"platform"`
	UserId               int             `json:"user_id"`
	Group                string          `json:"group"`
	ChannelId            int             `json:"channel_id"`
	Quota                int             `json:"quota"`
	Action               string          `json:"action"`
	Status               string          `json:"status"`
	FailReason           string          `json:"fail_reason"`
	ResultURL            string          `json:"result_url,omitempty"` // Task result URL, including video outputs.
	LegacyVideoAvailable bool            `json:"legacy_video_available,omitempty"`
	SubmitTime           int64           `json:"submit_time"`
	StartTime            int64           `json:"start_time"`
	FinishTime           int64           `json:"finish_time"`
	Progress             string          `json:"progress"`
	Properties           any             `json:"properties"`
	Username             string          `json:"username,omitempty"`
	Data                 json.RawMessage `json:"data"`
	AdminInfo            *TaskAdminInfo  `json:"admin_info,omitempty"`
	RootInfo             *TaskRootInfo   `json:"root_info,omitempty"`
}

type TaskPluginInfo struct {
	Key     string                `json:"key"`
	Name    string                `json:"name"`
	Version string                `json:"version,omitempty"`
	Author  *TaskPluginAuthorInfo `json:"author,omitempty"`
}

type TaskPluginAuthorInfo struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type TaskPluginRuntimeInfo struct {
	Key        string `json:"key"`
	Version    string `json:"version"`
	APIVersion int    `json:"api_version"`
	Generation uint64 `json:"generation"`
}

type TaskAdminInfo struct {
	OpenRouterSync map[string]any  `json:"openrouter_sync,omitempty"`
	RequestID      string          `json:"request_id,omitempty"`
	RequestPath    string          `json:"request_path,omitempty"`
	TaskPlugin     *TaskPluginInfo `json:"task_plugin,omitempty"`
}

type TaskRootInfo struct {
	TaskPlugin     *TaskPluginRuntimeInfo `json:"task_plugin,omitempty"`
	UpstreamTaskID string                 `json:"upstream_task_id,omitempty"`
	NodeName       string                 `json:"node_name,omitempty"`
}

// TaskPropertiesView retains the public properties shape without stored codecs.
type TaskPropertiesView struct {
	Input             string `json:"input"`
	UpstreamModelName string `json:"upstream_model_name,omitempty"`
	OriginModelName   string `json:"origin_model_name,omitempty"`
}

type Visibility struct {
	Admin bool
	Root  bool
}

type TaskQuery struct {
	Platform       string
	TaskID         string
	Status         string
	Action         string
	StartTimestamp int64
	EndTimestamp   int64
	ChannelID      string
}
type MidjourneyQuery struct {
	ChannelID      string
	MjID           string
	StartTimestamp string
	EndTimestamp   string
}

// ProjectTaskView applies audience policy after optional username enrichment.
func ProjectTaskView(task *Task, visibility Visibility, projectData func([]byte, string) ([]byte, error)) *TaskView {
	item := &TaskView{
		ID: task.ID, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, TaskID: task.TaskID, Platform: task.Platform,
		UserId: task.UserId, Group: task.Group, ChannelId: task.ChannelId, Quota: task.Quota, Action: constant.NormalizeTaskAction(task.Action),
		Status: string(task.Status), FailReason: task.FailReason, ResultURL: task.GetResultURL(), SubmitTime: task.SubmitTime,
		StartTime: task.StartTime, FinishTime: task.FinishTime, Progress: task.Progress,
		Properties: TaskPropertiesView{task.Properties.Input, task.Properties.UpstreamModelName, task.Properties.OriginModelName},
		Username:   task.Username, Data: task.Data,
	}
	if !visibility.Admin && len(item.Data) > 0 {
		projected, err := projectData(item.Data, task.Properties.OriginModelName)
		if err != nil {
			item.Data = nil
		} else {
			item.Data = projected
		}
	}
	item.LegacyVideoAvailable = LegacyVideoAvailable(task)
	if task.Status == TaskStatusSuccess {
		item.ResultURL = ""
		if FailReasonIsLegacyResultURL(task.FailReason) {
			item.FailReason = ""
		}
	}
	if visibility.Admin {
		info := &TaskAdminInfo{}
		if execution := task.PrivateData.Execution; execution != nil {
			info.RequestID = execution.RequestID
			info.RequestPath = execution.RequestPath
			if snapshot := execution.TaskPlugin; snapshot != nil {
				info.TaskPlugin = &TaskPluginInfo{Key: snapshot.Key, Name: snapshot.Name, Version: snapshot.Version}
				if snapshot.Author != nil {
					info.TaskPlugin.Author = &TaskPluginAuthorInfo{Name: snapshot.Author.Name, URL: snapshot.Author.URL}
				}
			}
		}
		info.OpenRouterSync = OpenRouterSyncAdminView(task)
		if info.RequestID != "" || info.RequestPath != "" || info.TaskPlugin != nil || info.OpenRouterSync != nil {
			item.AdminInfo = info
		}
	}
	if visibility.Root {
		info := &TaskRootInfo{UpstreamTaskID: task.PrivateData.UpstreamTaskID, NodeName: task.PrivateData.NodeName}
		if execution := task.PrivateData.Execution; execution != nil {
			if snapshot := execution.TaskPlugin; snapshot != nil {
				info.TaskPlugin = &TaskPluginRuntimeInfo{
					Key:        snapshot.Key,
					Version:    snapshot.Version,
					APIVersion: snapshot.APIVersion,
					Generation: snapshot.Generation,
				}
			}
		}
		if info.TaskPlugin != nil || info.UpstreamTaskID != "" || info.NodeName != "" {
			item.RootInfo = info
		}
	}
	return item
}
