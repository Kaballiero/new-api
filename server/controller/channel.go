package controller

import (
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/preview_channel_models"

	channeladapter "github.com/QuantumNous/new-api/internal/adapter/channels"

	"github.com/QuantumNous/new-api/internal/app"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

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

type OpenAIModel = channeladapter.OpenAIModel
type OpenAIModelsResponse = channeladapter.OpenAIModelsResponse

func GetChannelOps(c *gin.Context) { app.NewGetChannelOpsHTTP(c, channelEffects()).Handle(c) }

func GetAllChannels(c *gin.Context) { app.NewGetAllChannelsHTTP(c, channelEffects()).Handle(c) }

func FetchUpstreamModels(c *gin.Context) {
	app.NewFetchUpstreamModelsHTTP(c, channelEffects()).Handle(c)
}

func FixChannelsAbilities(c *gin.Context) {
	app.NewFixChannelsAbilitiesHTTP(c, channelEffects()).Handle(c)
}

func SearchChannels(c *gin.Context) { app.NewSearchChannelsHTTP(c, channelEffects()).Handle(c) }

func GetChannel(c *gin.Context) { app.NewGetChannelHTTP(c, channelEffects()).Handle(c) }

// GetChannelKey 获取渠道密钥（需要通过安全验证中间件）
// 此函数依赖 SecureVerificationRequired 中间件，确保用户已通过安全验证
func GetChannelKey(c *gin.Context) { app.NewGetChannelKeyHTTP(c, channelEffects()).Handle(c) }

// validateChannel 通用的渠道校验函数
func validateChannel(channel *model.Channel, isAdd bool) error {
	value := repository.ToDomain(channel)
	err := domain.ValidateChannel(value, isAdd, repository.NewStore())
	if channel != nil {
		*channel = *repository.FromDomain(value)
	}
	return err
}

func RefreshCodexChannelCredential(c *gin.Context) {
	app.NewRefreshCodexChannelCredentialHTTP(c, channelEffects()).Handle(c)
}

type AddChannelRequest struct {
	Mode                      string                `json:"mode"`
	MultiKeyMode              constant.MultiKeyMode `json:"multi_key_mode"`
	BatchAddSetKeyPrefix2Name bool                  `json:"batch_add_set_key_prefix_2_name"`
	Channel                   *model.Channel        `json:"channel"`
}

func AddChannel(c *gin.Context) { app.NewAddChannelHTTP(c, channelEffects()).Handle(c) }

func DeleteChannel(c *gin.Context) { app.NewDeleteChannelHTTP(c, channelEffects()).Handle(c) }

func DeleteDisabledChannel(c *gin.Context) {
	app.NewDeleteDisabledChannelHTTP(c, channelEffects()).Handle(c)
}

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

func DisableTagChannels(c *gin.Context) { app.NewDisableTagChannelsHTTP(c, channelEffects()).Handle(c) }

func EnableTagChannels(c *gin.Context) { app.NewEnableTagChannelsHTTP(c, channelEffects()).Handle(c) }

func EditTagChannels(c *gin.Context) { app.NewEditTagChannelsHTTP(c, channelEffects()).Handle(c) }

type ChannelBatch struct {
	Ids []int   `json:"ids"`
	Tag *string `json:"tag"`
}

func DeleteChannelBatch(c *gin.Context) { app.NewDeleteChannelBatchHTTP(c, channelEffects()).Handle(c) }

type PatchChannel struct {
	model.Channel
	MultiKeyMode *string `json:"multi_key_mode"`
	KeyMode      *string `json:"key_mode"` // 多key模式下密钥覆盖或者追加
}

type ChannelStatusRequest struct {
	Status int `json:"status"`
}

type ChannelStatusBatchRequest struct {
	Ids    []int `json:"ids"`
	Status int   `json:"status"`
}

func UpdateChannel(c *gin.Context) { app.NewUpdateChannelHTTP(c, channelEffects()).Handle(c) }

func UpdateChannelStatus(c *gin.Context) {
	app.NewUpdateChannelStatusHTTP(c, channelEffects()).Handle(c)
}

func BatchUpdateChannelStatus(c *gin.Context) {
	app.NewBatchUpdateChannelStatusHTTP(c, channelEffects()).Handle(c)
}

func isManageableChannelStatus(status int) bool {
	return status == common.ChannelStatusEnabled || status == common.ChannelStatusManuallyDisabled
}

// equalStringPtr 比较两个 *string 是否相等（均为 nil 视为相等）。

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

func buildAdvancedCustomModelPreviewChannel(req FetchModelsRequest) (*model.Channel, error) {
	value, err := app.NewPreviewChannelModelsUsecase().Preview(preview_channel_models.FetchModelsRequest(req))
	return repository.FromDomain(value), err
}

func FetchModels(c *gin.Context) { app.NewFetchModelsHTTP(c, channelEffects()).Handle(c) }

func BatchSetChannelTag(c *gin.Context) { app.NewBatchSetChannelTagHTTP(c, channelEffects()).Handle(c) }

func GetTagModels(c *gin.Context) { app.NewGetTagModelsHTTP(c, channelEffects()).Handle(c) }

// CopyChannel handles cloning an existing channel with its key.
// POST /api/channel/copy/:id
// Optional query params:
//
//	suffix         - string appended to the original name (default "_复制")
//	reset_balance  - bool, when true will reset balance & used_quota to 0 (default true)
func CopyChannel(c *gin.Context) { app.NewCopyChannelHTTP(c, channelEffects()).Handle(c) }

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

// ManageMultiKeys handles multi-key management operations
func ManageMultiKeys(c *gin.Context) { app.NewManageMultiKeysHTTP(c, channelEffects()).Handle(c) }

// OllamaPullModel 拉取 Ollama 模型
func OllamaPullModel(c *gin.Context) { app.NewOllamaPullModelHTTP(c, channelEffects()).Handle(c) }

// OllamaPullModelStream 流式拉取 Ollama 模型
func OllamaPullModelStream(c *gin.Context) {
	app.NewOllamaPullModelStreamHTTP(c, channelEffects()).Handle(c)
}

// OllamaDeleteModel 删除 Ollama 模型
func OllamaDeleteModel(c *gin.Context) { app.NewOllamaDeleteModelHTTP(c, channelEffects()).Handle(c) }

// OllamaVersion 获取 Ollama 服务版本信息
func OllamaVersion(c *gin.Context) { app.NewOllamaVersionHTTP(c, channelEffects()).Handle(c) }
