package admin_query_user_logs

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/transport"

	"github.com/gin-gonic/gin"
)

const (
	maxLogQueryBodyBytes = 256 << 10
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Get(c *gin.Context) {
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
	h.respond(c, request)
}

func (h Handler) Post(c *gin.Context) {
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
	h.respond(c, request)
}

func (h Handler) respond(c *gin.Context, request AdminLogQueryRequest) {
	page := common.PageInfo{Page: request.Page, PageSize: request.PageSize}
	query := domain.LogQuery{UserIDs: request.UserIDs, Kind: request.Type,
		StartTimestamp: request.StartTimestamp, EndTimestamp: request.EndTimestamp, ModelName: request.ModelName,
		Page: request.Page, Count: page.PageSize}
	result, err := h.usecase.Execute(Input{Query: query, Audience: transport.Audience(c)})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidLogQuery) {
			common.ApiErrorI18nStatusCode(c, 400, "invalid_params", i18n.MsgInvalidParams)
			return
		}
		common.ApiErrorStatusCode(c, 500, "internal_error", err)
		return
	}
	page.SetTotal(int(result.Total))
	page.SetItems(result.Items)
	common.ApiSuccess(c, page)
}
