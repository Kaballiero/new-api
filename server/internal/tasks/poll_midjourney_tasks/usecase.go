package poll_midjourney_tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Summary is the result recorded on a midjourney_poll system task
// row, summarizing one polling pass.
type Summary struct {
	UnfinishedTasks int `json:"unfinished_tasks"`
	ChannelsScanned int `json:"channels_scanned"`
	NullTasksFailed int `json:"null_tasks_failed"`
}

// Execute performs one Midjourney polling pass synchronously.
// It honors ctx cancellation (the system-task runner cancels it when the lease
// is lost) and, when report is non-nil, reports progress as (processedChannels,
// totalChannels) so the system task surfaces a percentage.
func (u *Usecase) Execute(ctx context.Context, report func(processed, total int)) (Summary, error) {
	summary := Summary{}
	if ctx == nil {
		ctx = context.Background()
	}

	tasks := u.store.Unfinished()
	if len(tasks) == 0 {
		return summary, nil
	}
	summary.UnfinishedTasks = len(tasks)

	u.diagnostics.Info(ctx, fmt.Sprintf("检测到未完成的任务数有: %v", len(tasks)))
	taskChannelM := make(map[int][]string)
	taskM := make(map[string]*domain.Midjourney)
	nullTaskIds := make([]int, 0)
	for _, task := range tasks {
		if task.MjId == "" {
			// Collect unfinished rows with no upstream identity.
			nullTaskIds = append(nullTaskIds, task.Id)
			continue
		}
		taskM[task.MjId] = task
		taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], task.MjId)
	}
	if len(nullTaskIds) > 0 {
		summary.NullTasksFailed = len(nullTaskIds)
		err := u.store.BulkUpdateByIDs(nullTaskIds, map[string]any{
			"status":   "FAILURE",
			"progress": "100%",
		})
		if err != nil {
			u.diagnostics.Error(ctx, fmt.Sprintf("Fix null mj_id task error: %v", err))
		} else {
			u.diagnostics.Info(ctx, fmt.Sprintf("Fix null mj_id task success: %v", nullTaskIds))
		}
	}
	if len(taskChannelM) == 0 {
		return summary, nil
	}

	totalChannels := len(taskChannelM)
	processedChannels := 0
	for channelId, taskIds := range taskChannelM {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedChannels, totalChannels)
		}
		processedChannels++
		summary.ChannelsScanned++
		u.diagnostics.Info(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
		if len(taskIds) == 0 {
			continue
		}
		midjourneyChannel, err := u.channels.Get(channelId)
		if err != nil {
			u.diagnostics.Error(ctx, fmt.Sprintf("CacheGetChannel: %v", err))
			err := u.store.BulkUpdateByTaskIDs(taskIds, map[string]any{
				"fail_reason": fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelId),
				"status":      "FAILURE",
				"progress":    "100%",
			})
			if err != nil {
				u.diagnostics.Info(ctx, fmt.Sprintf("UpdateMidjourneyTask error: %v", err))
			}
			continue
		}
		responseItems, err := u.client.Fetch(ctx, midjourneyChannel, taskIds)
		if err != nil {
			continue
		}

		for _, responseItem := range responseItems {
			task := taskM[responseItem.MjId]
			if task == nil {
				u.diagnostics.Warn(ctx, fmt.Sprintf("Midjourney task response ignored: unknown mj_id=%s", responseItem.MjId))
				continue
			}

			useTime := (u.now().UnixNano() / int64(time.Millisecond)) - task.SubmitTime
			// A task older than one hour fails unless its saved progress is complete.
			if useTime > 3600000 && task.Progress != "100%" {
				responseItem.FailReason = "上游任务超时（超过1小时）"
				responseItem.Status = "FAILURE"
			}
			if !u.needsUpdate(task, responseItem) {
				continue
			}
			preStatus := task.Status
			task.Code = 1
			task.Progress = responseItem.Progress
			task.PromptEn = responseItem.PromptEn
			task.State = responseItem.State
			task.SubmitTime = responseItem.SubmitTime
			task.StartTime = responseItem.StartTime
			task.FinishTime = responseItem.FinishTime
			task.ImageUrl = responseItem.ImageUrl
			task.Status = responseItem.Status
			task.FailReason = responseItem.FailReason
			if responseItem.Properties != nil {
				propertiesStr, _ := u.codec.Marshal(responseItem.Properties)
				task.Properties = string(propertiesStr)
			}
			if responseItem.Buttons != nil {
				buttonStr, _ := u.codec.Marshal(responseItem.Buttons)
				task.Buttons = string(buttonStr)
			}
			// Apply the primary video URL.
			task.VideoUrl = responseItem.VideoUrl

			// Persist the additional video URL list as JSON.
			if responseItem.VideoUrls != nil && len(responseItem.VideoUrls) > 0 {
				videoUrlsStr, err := u.codec.Marshal(responseItem.VideoUrls)
				if err != nil {
					u.diagnostics.Error(ctx, fmt.Sprintf("序列化 VideoUrls 失败: %v", err))
					task.VideoUrls = "[]" // Preserve the empty-array fallback on encoding failure.
				} else {
					task.VideoUrls = string(videoUrlsStr)
				}
			} else {
				task.VideoUrls = "" // Clear the field when the upstream list is empty.
			}

			shouldReturnQuota := false
			if (task.Progress != "100%" && responseItem.FailReason != "") || (task.Progress == "100%" && task.Status == "FAILURE") {
				u.diagnostics.Info(ctx, task.MjId+" 构建失败，"+task.FailReason)
				task.Progress = "100%"
				if task.Quota != 0 {
					shouldReturnQuota = true
				}
			}
			won, err := u.store.Transition(task, preStatus)
			if err != nil {
				u.diagnostics.Error(ctx, "UpdateMidjourneyTask task error: "+err.Error())
			} else if won && shouldReturnQuota {
				u.refund.RefundMidjourney(ctx, task, "构图失败")
			}
		}
	}
	if report != nil && (ctx == nil || ctx.Err() == nil) {
		report(totalChannels, totalChannels)
	}
	return summary, nil
}

func (u *Usecase) needsUpdate(oldTask *domain.Midjourney, newTask Result) bool {
	if oldTask.Code != 1 {
		return true
	}
	if oldTask.Progress != newTask.Progress {
		return true
	}
	if oldTask.PromptEn != newTask.PromptEn {
		return true
	}
	if oldTask.State != newTask.State {
		return true
	}
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.ImageUrl != newTask.ImageUrl {
		return true
	}
	if oldTask.Status != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.Progress != "100%" && newTask.FailReason != "" {
		return true
	}
	// Compare the primary video URL.
	if oldTask.VideoUrl != newTask.VideoUrl {
		return true
	}
	// Compare the serialized additional video URLs.
	if newTask.VideoUrls != nil && len(newTask.VideoUrls) > 0 {
		newVideoUrlsStr, _ := u.codec.Marshal(newTask.VideoUrls)
		if oldTask.VideoUrls != string(newVideoUrlsStr) {
			return true
		}
	} else if oldTask.VideoUrls != "" {
		// An empty incoming list clears a previously saved list.
		return true
	}

	return false
}
