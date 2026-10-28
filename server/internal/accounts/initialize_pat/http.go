package initialize_pat

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	scenario   Usecase
	writeError func(*gin.Context, error)
}

func NewHTTP(scenario Usecase, writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{scenario: scenario, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	raw, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if e != nil {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	var fields map[string]json.RawMessage
	if common.HasDuplicateJSONKeys(raw) || common.Unmarshal(raw, &fields) != nil || len(fields) < 1 || len(fields) > 2 {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	for k := range fields {
		if k != "expected_username" && k != "apply" {
			h.writeError(c, domain.ErrGetAPIInvalidRequest)
			return
		}
	}
	if common.GetJsonType(fields["expected_username"]) != "string" || (fields["apply"] != nil && common.GetJsonType(fields["apply"]) != "boolean") {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	id, e := strconv.Atoi(c.Param("user_id"))
	if e != nil || id <= 0 {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	var req struct {
		ExpectedUsername string `json:"expected_username"`
		Apply            bool   `json:"apply"`
	}
	if common.Unmarshal(raw, &req) != nil || strings.TrimSpace(req.ExpectedUsername) == "" {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	result, err := h.scenario.Execute(Input{UserID: id, ExpectedUsername: req.ExpectedUsername, Apply: req.Apply})
	if err != nil {
		h.writeError(c, err)
		return
	}
	status := http.StatusOK
	if result.Outcome == "issued" {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"success": true, "data": result})
	c.Set("getapi_target_user_id", id)
}
