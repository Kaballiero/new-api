package usage

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	auditrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/audit/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func ensureLogRequestId(log *model.Log) {
	if log != nil && log.RequestId == "" {
		log.RequestId = common.NewRequestId()
	}
}

func (w Writer) createLog(log *model.Log) error {
	ensureLogRequestId(log)
	return w.env.LogDB().Create(log).Error
}

func (w Writer) RecordLog(userId int, logType int, content string) {
	if logType == record.LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := w.env.GetUsernameById(userId, false)
	log := &model.Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := w.createLog(log)
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo stores operator metadata under other.admin_info and
// an optional, user-visible operation descriptor under other.op for localization.
func (w Writer) RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo *auditrecord.AuditAdminInfo, operation *auditrecord.AuditOperation, request ...*gin.Context) {
	if logType == record.LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := w.env.GetUsernameById(userId, false)
	log := &model.Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(request) > 0 && request[0] != nil {
		log.RequestId = request[0].GetString(common.RequestIdKey)
	}
	if adminInfo != nil || operation != nil {
		data, err := common.Marshal(auditrecord.AuditOther{AdminInfo: adminInfo, Op: operation})
		if err != nil {
			common.SysError("failed to encode log admin info: " + err.Error())
			return
		}
		log.Other = string(data)
	}
	if err := w.createLog(log); err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

func (w Writer) RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := w.env.GetUsernameById(userId, false)
	other := new(record.LogOther)
	other.MergeAdmin(map[string]any{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	})
	log := &model.Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      record.LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     other.JSONString(),
	}
	err := w.createLog(log)
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

// WriteErrorLog is the plain technical writer shared with usage recording.
func (w Writer) WriteErrorLog(observation LogObservation, userId int, channelId int, modelName, tokenName, content string,
	tokenId, useTimeSeconds int, isStream bool, group string, other *record.LogOther) {
	logger.LogInfo(observation.Context, fmt.Sprintf(
		"record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s",
		userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username, requestId, upstreamRequestId := observation.Identity()
	otherStr := other.JSONString()
	// Check whether this user records client IPs.
	needRecordIp := false
	if settingMap, err := w.env.GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &model.Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             record.LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return observation.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := w.createLog(log)
	if err != nil {
		logger.LogError(observation.Context, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int              `json:"channel_id"`
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	ModelName        string           `json:"model_name"`
	TokenName        string           `json:"token_name"`
	Quota            int              `json:"quota"`
	Content          string           `json:"content"`
	TokenId          int              `json:"token_id"`
	UseTimeSeconds   int              `json:"use_time_seconds"`
	IsStream         bool             `json:"is_stream"`
	Group            string           `json:"group"`
	Other            *record.LogOther `json:"other"`
}

func (w Writer) RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	w.WriteConsumeLog(ObserveLogRequest(c), userId, params,
		func() bool { return common.LogConsumeEnabled }, func() bool { return common.DataExportEnabled })
}

// WriteConsumeLog preserves late policy reads without recovering an HTTP context.
func (w Writer) WriteConsumeLog(observation LogObservation, userId int, params RecordConsumeLogParams, consumeEnabled, exportEnabled func() bool) {
	if !consumeEnabled() {
		return
	}
	logger.LogInfo(observation.Context, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username, requestId, upstreamRequestId := observation.Identity()
	createdAt := common.GetTimestamp()
	otherStr := params.Other.JSONString()
	// Check whether this user records client IPs.
	needRecordIp := false
	if settingMap, err := w.env.GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &model.Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        createdAt,
		Type:             record.LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return observation.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := w.createLog(log)
	if err != nil {
		logger.LogError(observation.Context, "failed to record log: "+err.Error())
	}
	if exportEnabled() {
		LogQuotaData(record.QuotaDataLogParams{
			UserID:    userId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			TokenUsed: params.PromptTokens + params.CompletionTokens,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  common.NodeName,
		})
	}
}

type RecordTaskBillingLogParams struct {
	UserId    int
	LogType   int
	Content   string
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     *record.LogOther
	NodeName  string // The originating task node; fall back to the current node when empty.
}

func (w Writer) RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == record.LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	if err := w.RecordTaskBillingLogWithError(params); err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
}

// RecordTaskBillingLogWithError persists required financial evidence even when
// ordinary consume logging is disabled.
func (w Writer) RecordTaskBillingLogWithError(params RecordTaskBillingLogParams) error {
	var user model.User
	_ = w.env.DB().Select("username").Where("id = ?", params.UserId).First(&user).Error
	username := user.Username
	tokenName := ""
	if params.TokenId > 0 {
		var token model.Token
		_ = w.env.DB().Select("name").Where("id = ?", params.TokenId).First(&token).Error
		tokenName = token.Name
	}
	createdAt := common.GetTimestamp()
	log := &model.Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: createdAt,
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     params.Other.JSONString(),
	}
	err := w.createLog(log)
	if err != nil {
		return err
	}
	if params.LogType == record.LogTypeConsume && common.DataExportEnabled {
		nodeName := params.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		LogQuotaData(record.QuotaDataLogParams{
			UserID:    params.UserId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  nodeName,
		})
	}
	return nil
}

// Writer persists log evidence with late database and user-setting reads.
type Writer struct{ env WriterEnvironment }
type WriterEnvironment struct {
	DB, LogDB       func() *gorm.DB
	GetUsernameById func(int, bool) (string, error)
	GetUserSetting  func(int, bool) (dto.UserSetting, error)
}

func NewWriter(env WriterEnvironment) Writer { return Writer{env: env} }
