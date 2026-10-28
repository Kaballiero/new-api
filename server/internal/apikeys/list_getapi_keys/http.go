package list_getapi_keys

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) HTTP { return HTTP{reader: reader} }

// Handle serves GET /api/getapi/tokens?user_id=&after_id=&limit= or ?ids=1,2,3.
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	var ok bool
	if input.UserID, ok = queryInt(c, "user_id"); !ok {
		transport.WriteError(c, domain.ErrListQueryInvalid)
		return
	}
	if input.AfterID, ok = queryInt(c, "after_id"); !ok {
		transport.WriteError(c, domain.ErrListQueryInvalid)
		return
	}
	if input.Limit, ok = queryInt(c, "limit"); !ok {
		transport.WriteError(c, domain.ErrListQueryInvalid)
		return
	}
	if raw, present := c.GetQuery("ids"); present {
		input.IDs = []int{}
		for part := range strings.SplitSeq(raw, ",") {
			id, err := strconv.Atoi(part)
			if err != nil {
				transport.WriteError(c, domain.ErrListQueryInvalid)
				return
			}
			input.IDs = append(input.IDs, id)
			if len(input.IDs) > maxItems {
				break
			}
		}
	}
	if input.UserID != nil && *input.UserID > 0 {
		// GetAPIAuth records the integration audit event with this target.
		c.Set("getapi_target_user_id", *input.UserID)
	}
	output, err := NewUsecase(h.reader).Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	if input.IDs != nil {
		common.ApiSuccess(c, Batch{Items: output.Items})
		return
	}
	common.ApiSuccess(c, output)
}

// queryInt returns nil when the parameter is absent and false when it is not an integer.
func queryInt(c *gin.Context, name string) (*int, bool) {
	raw, present := c.GetQuery(name)
	if !present {
		return nil, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, false
	}
	return &value, true
}
