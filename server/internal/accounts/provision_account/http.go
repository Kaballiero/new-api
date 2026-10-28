package provision_account

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/dto"

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
	var fields map[string]json.RawMessage
	if e != nil || common.HasDuplicateJSONKeys(raw) || common.Unmarshal(raw, &fields) != nil || len(fields) < 3 || len(fields) > 5 {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	for name := range fields {
		if name != "username" && name != "password" && name != "display_name" && name != "group" && name != "initial_quota" {
			h.writeError(c, domain.ErrGetAPIInvalidRequest)
			return
		}
	}
	for _, n := range []string{"username", "password", "display_name"} {
		if common.GetJsonType(fields[n]) != "string" {
			h.writeError(c, domain.ErrGetAPIInvalidRequest)
			return
		}
	}
	if value, exists := fields["group"]; exists {
		if common.GetJsonType(value) != "string" {
			h.writeError(c, domain.ErrGetAPIInvalidRequest)
			return
		}
		var group string
		if common.Unmarshal(value, &group) != nil || strings.TrimSpace(group) == "" {
			h.writeError(c, domain.ErrGetAPIInvalidRequest)
			return
		}
	}
	var req GetAPICreateUserRequest
	if value, exists := fields["initial_quota"]; exists && common.GetJsonType(value) != "number" {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	if common.Unmarshal(raw, &req) != nil || strings.TrimSpace(req.Username) == "" || req.Password == "" {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	u := dto.User{Username: req.Username, Password: req.Password, DisplayName: req.DisplayName}
	if common.Validate.Struct(&u) != nil {
		h.writeError(c, domain.ErrGetAPIInvalidRequest)
		return
	}
	cred, err := h.scenario.Execute(c.GetInt("role"), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": cred})
	c.Set("getapi_target_user_id", cred.UserID)
}
