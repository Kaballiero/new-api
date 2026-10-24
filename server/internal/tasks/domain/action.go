package domain

import "github.com/QuantumNous/new-api/constant"

type TaskPlatform = constant.TaskPlatform

const (
	TaskPlatformSuno                = constant.TaskPlatformSuno
	TaskPlatformMidjourney          = constant.TaskPlatformMidjourney
	TaskPlatformOpenRouterAudio     = constant.TaskPlatformOpenRouterAudio
	TaskPlatformOpenRouterImage     = constant.TaskPlatformOpenRouterImage
	TaskPlatformOpenRouterChatImage = constant.TaskPlatformOpenRouterChatImage
	TaskPlatformOpenRouterRerank    = constant.TaskPlatformOpenRouterRerank
	TaskActionImageToVideo          = constant.TaskActionImageToVideo
	TaskActionTextToVideo           = constant.TaskActionTextToVideo
	TaskActionFirstTailToVideo      = constant.TaskActionFirstTailToVideo
	TaskActionReferenceToVideo      = constant.TaskActionReferenceToVideo
	TaskActionRemix                 = constant.TaskActionRemix
)
