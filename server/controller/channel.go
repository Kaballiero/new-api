package controller

import (
	"github.com/QuantumNous/new-api/constant"
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// SearchChannelsResponse is the response body of GET /api/channel/search —
// paged channels plus per-type counts for the admin UI filter bar. Named
// schema replaces the previous anonymous gin.H{items,total,type_counts}.
type SearchChannelsResponse struct {
	Items      []*model.Channel `json:"items"`
	Total      int              `json:"total"`
	TypeCounts map[int64]int64  `json:"type_counts"`
}

// ChannelKeyResponse is the response body of POST /api/channel/{id}/key.
// Reveals the channel's upstream credentials (gated by SecureVerificationRequired
// middleware). Separate struct so OpenAPI gets a named schema.
type ChannelKeyResponse struct {
	Key string `json:"key"`
}

// ChannelTestResponse is the result of GET /api/channel/test/{id}. The legacy
// shape lived OUTSIDE the standard {success,message,data} envelope (success/
// message/time/error_code at top level); migrating to the envelope so SDK
// types pick it up as $ref instead of anonymous object.
type ChannelTestResponse struct {
	Time      float64 `json:"time"`
	ErrorCode string  `json:"error_code,omitempty"`
}

func GetChannelOps(c *gin.Context) { httpapi.Channels().GetChannelOperations.Handle(c) }

func GetAllChannels(c *gin.Context) { httpapi.Channels().ListChannels.Handle(c) }

func FetchUpstreamModels(c *gin.Context) { httpapi.Channels().FetchUpstreamModels.Handle(c) }

func FixChannelsAbilities(c *gin.Context) { httpapi.Channels().RepairChannelAbilities.Handle(c) }

func SearchChannels(c *gin.Context) { httpapi.Channels().SearchChannels.Handle(c) }

func GetChannel(c *gin.Context) { httpapi.Channels().GetChannel.Handle(c) }

func GetChannelKey(c *gin.Context) { httpapi.Channels().RevealChannelKey.Handle(c) }

func RefreshCodexChannelCredential(c *gin.Context) {
	httpapi.Channels().RefreshCodexCredential.Handle(c)
}

type AddChannelRequest struct {
	Mode                      string                `json:"mode"`
	MultiKeyMode              constant.MultiKeyMode `json:"multi_key_mode"`
	BatchAddSetKeyPrefix2Name bool                  `json:"batch_add_set_key_prefix_2_name"`
	Channel                   *model.Channel        `json:"channel"`
}

func AddChannel(c *gin.Context) { httpapi.Channels().CreateChannel.Handle(c) }

func DeleteChannel(c *gin.Context) { httpapi.Channels().DeleteChannel.Handle(c) }

func DeleteDisabledChannel(c *gin.Context) { httpapi.Channels().DeleteDisabledChannels.Handle(c) }

type ChannelTag struct {
	Tag            string  `json:"tag"`
	NewTag         *string `json:"new_tag"`
	Priority       *int64  `json:"priority"`
	Weight         *uint   `json:"weight"`
	ModelMapping   *string `json:"model_mapping"`
	Models         *string `json:"models"`
	Groups         *string `json:"groups"`
	ParamOverride  *string `json:"param_override"`
	HeaderOverride *string `json:"header_override"`
}

func DisableTagChannels(c *gin.Context) { httpapi.Channels().DisableTagChannels.Handle(c) }

func EnableTagChannels(c *gin.Context) { httpapi.Channels().EnableTagChannels.Handle(c) }

func EditTagChannels(c *gin.Context) { httpapi.Channels().EditTagChannels.Handle(c) }

type ChannelBatch struct {
	Ids []int   `json:"ids"`
	Tag *string `json:"tag"`
}

func DeleteChannelBatch(c *gin.Context) { httpapi.Channels().DeleteChannelsBatch.Handle(c) }

type PatchChannel struct {
	model.Channel
	MultiKeyMode *string `json:"multi_key_mode"`
	KeyMode      *string `json:"key_mode"` // replace or append keys in multi-key mode
}

type ChannelStatusRequest struct {
	Status int `json:"status"`
}

type ChannelStatusBatchRequest struct {
	Ids    []int `json:"ids"`
	Status int   `json:"status"`
}

func UpdateChannel(c *gin.Context) { httpapi.Channels().UpdateChannel.Handle(c) }

func UpdateChannelStatus(c *gin.Context) { httpapi.Channels().UpdateChannelStatus.Handle(c) }

func BatchUpdateChannelStatus(c *gin.Context) { httpapi.Channels().UpdateChannelStatusBatch.Handle(c) }

// FetchModelsRequest supports saved and unsaved model discovery previews.
type FetchModelsRequest struct {
	ChannelID      int     `json:"channel_id"`
	BaseURL        *string `json:"base_url"`
	Type           int     `json:"type"`
	Key            string  `json:"key"`
	AdvancedCustom *string `json:"advanced_custom"`
	HeaderOverride *string `json:"header_override"`
	Proxy          *string `json:"proxy"`
}

func FetchModels(c *gin.Context) { httpapi.Channels().PreviewChannelModels.Handle(c) }

func BatchSetChannelTag(c *gin.Context) { httpapi.Channels().SetChannelTagsBatch.Handle(c) }

func GetTagModels(c *gin.Context) { httpapi.Channels().GetTagModels.Handle(c) }

func CopyChannel(c *gin.Context) { httpapi.Channels().CopyChannel.Handle(c) }

// MultiKeyManageRequest represents the request for multi-key management operations
type MultiKeyManageRequest struct {
	ChannelId int    `json:"channel_id"`
	Action    string `json:"action"`              // "disable_key", "enable_key", "delete_key", "delete_disabled_keys", "get_key_status"
	KeyIndex  *int   `json:"key_index,omitempty"` // for disable_key, enable_key, and delete_key actions
	Page      int    `json:"page,omitempty"`      // for get_key_status pagination
	PageSize  int    `json:"page_size,omitempty"` // for get_key_status pagination
	Status    *int   `json:"status,omitempty"`    // for get_key_status filtering: 1=enabled, 2=manual_disabled, 3=auto_disabled, nil=all
}

// MultiKeyStatusResponse represents the response for key status query
type MultiKeyStatusResponse struct {
	Keys       []KeyStatus `json:"keys"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
	// Statistics
	EnabledCount        int `json:"enabled_count"`
	ManualDisabledCount int `json:"manual_disabled_count"`
	AutoDisabledCount   int `json:"auto_disabled_count"`
}

type KeyStatus struct {
	Index        int    `json:"index"`
	Status       int    `json:"status"` // 1: enabled, 2: disabled
	DisabledTime int64  `json:"disabled_time,omitempty"`
	Reason       string `json:"reason,omitempty"`
	KeyPreview   string `json:"key_preview"` // first 10 chars of key for identification
}

func ManageMultiKeys(c *gin.Context) { httpapi.Channels().ManageMultiKeys.Handle(c) }

func OllamaPullModel(c *gin.Context) { httpapi.Channels().PullOllamaModel.Handle(c) }

func OllamaPullModelStream(c *gin.Context) { httpapi.Channels().StreamOllamaModelPull.Handle(c) }

func OllamaDeleteModel(c *gin.Context) { httpapi.Channels().DeleteOllamaModel.Handle(c) }

func OllamaVersion(c *gin.Context) { httpapi.Channels().GetOllamaVersion.Handle(c) }
