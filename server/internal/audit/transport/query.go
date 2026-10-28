package transport

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/audit/domain"

	"github.com/gin-gonic/gin"
)

func ReadQuery(c *gin.Context, page, size, offset int) (domain.Query, error) {
	query := domain.Query{Username: c.Query("username"), Category: c.Query("category"), TokenRef: c.Query("token_ref"),
		ExcludeTokenRef: c.Query("exclude_token_ref"), RequestId: c.Query("request_id"), Offset: offset, Limit: size}
	if err := domain.ValidatePagination(page, size); err != nil {
		return query, err
	}
	if err := domain.ValidateFilters(query); err != nil {
		return query, err
	}
	for name, target := range map[string]*int64{"start_timestamp": &query.StartTimestamp, "end_timestamp": &query.EndTimestamp} {
		if raw := c.Query(name); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				return query, domain.ErrTimeRange
			}
			*target = parsed
		}
	}
	if query.EndTimestamp > 0 && query.EndTimestamp < query.StartTimestamp {
		return query, domain.ErrTimeRange
	}
	if raw := c.Query("success"); raw != "" {
		if raw != "true" && raw != "false" {
			return query, domain.ErrResult
		}
		success := raw == "true"
		query.Success = &success
	}
	return query, nil
}

func WriteError(c *gin.Context, err error) {
	var key string
	switch {
	case errors.Is(err, domain.ErrPagination):
		key = i18n.MsgAuditInvalidPagination
	case errors.Is(err, domain.ErrFilters):
		key = i18n.MsgAuditInvalidFilters
	case errors.Is(err, domain.ErrTimeRange):
		key = i18n.MsgAuditInvalidTimeRange
	case errors.Is(err, domain.ErrResult):
		key = i18n.MsgAuditInvalidResult
	default:
		common.ApiError(c, err)
		return
	}
	common.ApiErrorMsg(c, i18n.Translate(requestLanguage(c), key))
}

func requestLanguage(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return i18n.DefaultLang
	}
	return i18n.GetLangFromContext(c)
}

// ReadHandlers retains one legacy router entrypoint for the two read scenarios.
type ReadHandlers struct {
	admin, own gin.HandlerFunc
}

func NewReadHandlers(admin, own gin.HandlerFunc) ReadHandlers {
	return ReadHandlers{admin: admin, own: own}
}

func (h ReadHandlers) Handle(c *gin.Context) {
	if c.FullPath() == "/api/audit/self" {
		h.own(c)
		return
	}
	h.admin(c)
}
