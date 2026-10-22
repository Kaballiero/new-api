package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	keydomain "github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetAllLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	logs, total, err := model.GetAllLogs(logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), channel, group, requestId, upstreamRequestId)
	if err != nil {
		writeLogQueryError(c, err)
		return
	}
	if c.GetInt("role") < common.RoleRootUser {
		model.FormatAdminLogs(logs)
	} else {
		model.FormatRootLogs(logs)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func GetUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId := c.GetInt("id")
	tokenId := 0
	if tokenIdQuery, supplied := c.GetQuery("token_id"); supplied {
		var err error
		tokenId, err = strconv.Atoi(tokenIdQuery)
		if err != nil || tokenId <= 0 {
			common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
			return
		}
		if _, err = model.GetTokenByIds(tokenId, userId); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "token_not_found", i18n.MsgTokenNotExists)
				return
			}
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
			return
		}
	}
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	logs, total, err := model.GetUserLogsByTokenId(userId, tokenId, logType, startTimestamp, endTimestamp, modelName, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), group, requestId, upstreamRequestId)
	if err != nil {
		writeLogQueryError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

func GetLogByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		c.JSON(200, gin.H{
			"success": false,
			"message": "无效的令牌",
		})
		return
	}
	logs, err := model.GetLogByTokenId(tokenId)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetLogsStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	stat, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		writeLogQueryError(c, err)
		return
	}
	common.ApiSuccess(c, stat)
}

func GetLogsSelfStat(c *gin.Context) {
	username := c.GetString("username")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	stat, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		writeLogQueryError(c, err)
		return
	}
	common.ApiSuccess(c, stat)
}

// writeLogQueryError translates the shared search validator errors at the log
// HTTP boundary, preserving the existing response for other storage errors.
func writeLogQueryError(c *gin.Context, err error) {
	var key string
	switch {
	case errors.Is(err, keydomain.ErrSearchConsecutiveWildcards):
		key = i18n.MsgTokenSearchConsecutiveWildcards
	case errors.Is(err, keydomain.ErrSearchTooManyWildcards):
		key = i18n.MsgTokenSearchTooManyWildcards
	case errors.Is(err, keydomain.ErrSearchKeywordTooShort):
		key = i18n.MsgTokenSearchKeywordTooShort
	default:
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	lang := i18n.GetLangFromContext(c)
	if lang == "zh-TW" {
		// These log errors were Chinese literals on origin/main for both
		// Chinese locales. Keep those bytes using the existing zh-CN text.
		lang = "zh-CN"
	}
	common.ApiErrorMsgStatusCode(c, http.StatusInternalServerError, "internal_error", i18n.Translate(lang, key))
}
