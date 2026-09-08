package controller

import (
	"encoding/json"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func ProvisionGetAPIUser(c *gin.Context) {
	raw, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	var fields map[string]json.RawMessage
	if e != nil || common.HasDuplicateJSONKeys(raw) || common.Unmarshal(raw, &fields) != nil || len(fields) != 3 {
		writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
		return
	}
	for _, n := range []string{"username", "password", "display_name"} {
		if common.GetJsonType(fields[n]) != "string" {
			writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
			return
		}
	}
	var req model.GetAPICreateUserRequest
	if common.Unmarshal(raw, &req) != nil || strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
		return
	}
	u := model.User{Username: req.Username, Password: req.Password, DisplayName: req.DisplayName}
	if common.Validate.Struct(&u) != nil {
		writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
		return
	}
	cred, err := model.ProvisionGetAPIUser(c.GetInt("role"), req)
	if err != nil {
		writeGetAPIError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": cred})
	c.Set("getapi_target_user_id", cred.UserID)
}
func InitializeGetAPIPAT(c *gin.Context) {
	raw, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if e != nil {
		writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
		return
	}
	var fields map[string]json.RawMessage
	if common.HasDuplicateJSONKeys(raw) || common.Unmarshal(raw, &fields) != nil || len(fields) < 1 || len(fields) > 2 {
		writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
		return
	}
	for k := range fields {
		if k != "expected_username" && k != "apply" {
			writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
			return
		}
	}
	if common.GetJsonType(fields["expected_username"]) != "string" || (fields["apply"] != nil && common.GetJsonType(fields["apply"]) != "boolean") {
		writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
		return
	}
	id, e := strconv.Atoi(c.Param("user_id"))
	if e != nil || id <= 0 {
		writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
		return
	}
	var req struct {
		ExpectedUsername string `json:"expected_username"`
		Apply            bool   `json:"apply"`
	}
	if common.Unmarshal(raw, &req) != nil || strings.TrimSpace(req.ExpectedUsername) == "" {
		writeGetAPIError(c, model.ErrGetAPIInvalidRequest)
		return
	}
	result, err := model.InitializeGetAPIPAT(model.GetAPIInitializePATRequest{UserID: id, ExpectedUsername: req.ExpectedUsername, Apply: req.Apply})
	if err != nil {
		writeGetAPIError(c, err)
		return
	}
	status := http.StatusOK
	if result.Outcome == "issued" {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"success": true, "data": result})
	c.Set("getapi_target_user_id", id)
}
func writeGetAPIError(c *gin.Context, err error) {
	status := http.StatusServiceUnavailable
	code := model.ErrGetAPICredentialUnavailable.Error()
	switch {
	case errors.Is(err, model.ErrGetAPIInvalidRequest):
		status = http.StatusBadRequest
		code = err.Error()
	case errors.Is(err, model.ErrGetAPICapabilityDenied), errors.Is(err, model.ErrGetAPITargetRoleDenied):
		status = http.StatusForbidden
		code = err.Error()
	case errors.Is(err, model.ErrGetAPIAccountNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		status = http.StatusNotFound
		code = model.ErrGetAPIAccountNotFound.Error()
	case errors.Is(err, model.ErrGetAPICreateConflict), errors.Is(err, model.ErrGetAPIUsernameMismatch):
		status = http.StatusConflict
		code = err.Error()
	}
	c.JSON(status, gin.H{"success": false, "code": code, "message": "GetAPI request could not be completed"})
}
