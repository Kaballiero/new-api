package controller

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	maxLogQueryUserIDs    = 16384
	maxLogQueryBodyBytes  = 256 << 10
	maxLogQueryModelBytes = 16 << 10
)

type AdminLogQueryRequest struct {
	UserIDs        []int  `json:"user_ids"`
	Type           int    `json:"type"`
	StartTimestamp int64  `json:"start_timestamp"`
	EndTimestamp   int64  `json:"end_timestamp"`
	ModelName      string `json:"model_name"`
	Page           int    `json:"p"`
	PageSize       int    `json:"page_size"`
}

func GetAdminUserLogs(c *gin.Context) {
	request := AdminLogQueryRequest{Page: 1, PageSize: common.ItemsPerPage}
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 32)
	if err != nil || userID <= 0 {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	request.UserIDs = []int{int(userID)}
	for _, parameter := range []struct {
		name   string
		target *int
	}{{"type", &request.Type}, {"p", &request.Page}, {"page_size", &request.PageSize}} {
		if values, supplied := query[parameter.name]; supplied {
			value, parseErr := strconv.Atoi(values[0])
			if parseErr != nil || len(values) != 1 {
				common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
				return
			}
			*parameter.target = value
		}
	}
	for _, parameter := range []struct {
		name   string
		target *int64
	}{{"start_timestamp", &request.StartTimestamp}, {"end_timestamp", &request.EndTimestamp}} {
		if values, supplied := query[parameter.name]; supplied {
			value, parseErr := strconv.ParseInt(values[0], 10, 64)
			if parseErr != nil || len(values) != 1 {
				common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
				return
			}
			*parameter.target = value
		}
	}
	request.ModelName = query.Get("model_name")
	readAdminUserLogs(c, request)
}

func QueryAdminLogs(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxLogQueryBodyBytes+1))
	if err != nil || len(body) > maxLogQueryBodyBytes || !utf8.Valid(body) {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	request := AdminLogQueryRequest{Page: 1, PageSize: common.ItemsPerPage}
	var fields map[string]json.RawMessage
	if common.Unmarshal(body, &fields) != nil || fields == nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
			return
		}
	}
	if common.Unmarshal(body, &request) != nil || request.UserIDs == nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	readAdminUserLogs(c, request)
}

func readAdminUserLogs(c *gin.Context, request AdminLogQueryRequest) {
	maxInt := int(^uint(0) >> 1)
	if len(request.UserIDs) > maxLogQueryUserIDs || request.Page <= 0 || request.PageSize < 1 || request.PageSize > 100 ||
		request.Page-1 > (maxInt-request.PageSize)/request.PageSize ||
		request.Type < model.LogTypeUnknown || request.Type > model.LogTypeLogin ||
		request.StartTimestamp < 0 || request.EndTimestamp < 0 ||
		(request.StartTimestamp != 0 && request.EndTimestamp != 0 && request.StartTimestamp > request.EndTimestamp) ||
		len(request.ModelName) > maxLogQueryModelBytes || !utf8.ValidString(request.ModelName) {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	if err := model.ValidateLogModelFilter(request.ModelName); err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	userIDs := make([]int, 0, len(request.UserIDs))
	seen := make(map[int]struct{}, len(request.UserIDs))
	for _, userID := range request.UserIDs {
		if userID <= 0 || userID > math.MaxInt32 {
			common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
			return
		}
		if _, exists := seen[userID]; !exists {
			seen[userID] = struct{}{}
			userIDs = append(userIDs, userID)
		}
	}
	page := common.PageInfo{Page: request.Page, PageSize: request.PageSize}
	logs, total, err := model.GetLogsByUserIDs(userIDs, request.Type, request.StartTimestamp, request.EndTimestamp, request.ModelName, page.GetStartIdx(), page.PageSize)
	if err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	if c.GetInt("role") < common.RoleRootUser {
		model.FormatAdminLogs(logs)
	} else {
		model.FormatRootLogs(logs)
	}
	page.SetTotal(int(total))
	page.SetItems(logs)
	common.ApiSuccess(c, page)
}
