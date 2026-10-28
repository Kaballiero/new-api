package tasks

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

func MidjourneyToDomain(task *record.Midjourney) *domain.Midjourney {
	if task == nil {
		return nil
	}
	return &domain.Midjourney{
		Id:               task.Id,
		Code:             task.Code,
		UserId:           task.UserId,
		Action:           task.Action,
		MjId:             task.MjId,
		Prompt:           task.Prompt,
		PromptEn:         task.PromptEn,
		Description:      task.Description,
		State:            task.State,
		SubmitTime:       task.SubmitTime,
		StartTime:        task.StartTime,
		FinishTime:       task.FinishTime,
		ImageUrl:         task.ImageUrl,
		VideoUrl:         task.VideoUrl,
		VideoUrls:        task.VideoUrls,
		Status:           task.Status,
		Progress:         task.Progress,
		FailReason:       task.FailReason,
		ChannelId:        task.ChannelId,
		Quota:            task.Quota,
		Buttons:          task.Buttons,
		Properties:       task.Properties,
		TokenId:          task.TokenId,
		BillingChannelId: task.BillingChannelId,
	}
}

func MidjourneyFromDomain(task *domain.Midjourney) *record.Midjourney {
	if task == nil {
		return nil
	}
	return &record.Midjourney{
		Id:               task.Id,
		Code:             task.Code,
		UserId:           task.UserId,
		Action:           task.Action,
		MjId:             task.MjId,
		Prompt:           task.Prompt,
		PromptEn:         task.PromptEn,
		Description:      task.Description,
		State:            task.State,
		SubmitTime:       task.SubmitTime,
		StartTime:        task.StartTime,
		FinishTime:       task.FinishTime,
		ImageUrl:         task.ImageUrl,
		VideoUrl:         task.VideoUrl,
		VideoUrls:        task.VideoUrls,
		Status:           task.Status,
		Progress:         task.Progress,
		FailReason:       task.FailReason,
		ChannelId:        task.ChannelId,
		Quota:            task.Quota,
		Buttons:          task.Buttons,
		Properties:       task.Properties,
		TokenId:          task.TokenId,
		BillingChannelId: task.BillingChannelId,
	}
}
