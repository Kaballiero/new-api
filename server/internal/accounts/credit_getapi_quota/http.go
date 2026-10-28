package credit_getapi_quota

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
)

type HTTP struct {
	scenario   Usecase
	writeError func(*gin.Context, error)
}

func NewHTTP(scenario Usecase, writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{scenario: scenario, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || id <= 0 {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	c.Set("getapi_target_user_id", id)
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	var fields map[string]json.RawMessage
	if err != nil || common.HasDuplicateJSONKeys(raw) || common.Unmarshal(raw, &fields) != nil || len(fields) != 2 || common.GetJsonType(fields["operationKey"]) != "string" || common.GetJsonType(fields["quota"]) != "number" {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	var req struct {
		OperationKey string `json:"operationKey"`
		Quota        int    `json:"quota"`
	}
	if common.Unmarshal(raw, &req) != nil {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	result, err := h.scenario.Execute(Input{UserID: id, OperationKey: req.OperationKey, Quota: req.Quota})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
