package controller

import (
	"errors"
	"net/http"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func GetModelPricingConfig(c *gin.Context) {
	snapshot, err := model.GetModelPricingSnapshot(c.QueryArray("model"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, snapshot)
}

func UpdateModelPricingConfig(c *gin.Context) {
	var request struct {
		Changes []model.ModelPricingChange `json:"changes"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.UpdateModelPricing(request.Changes); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrModelPricingConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	names := make([]string, 0, len(request.Changes))
	for _, change := range request.Changes {
		names = append(names, change.ModelName)
	}
	recordManageAudit(c, "model.pricing.update", map[string]any{"models": names})
	common.ApiSuccess(c, gin.H{"updated_models": names})
}

type effectivePricingPreviewResponse struct {
	effectivePricingResponse
	AvailableUserGroups []string `json:"available_user_groups"`
}

// GetEffectivePricingPreview projects a configured user group without changing
// the caller's identity or persisted group. The option route requires root access.
func GetEffectivePricingPreview(c *gin.Context) {
	configuredGroups := ratio_setting.GetGroupRatioCopy()
	availableGroups := make([]string, 0, len(configuredGroups))
	for group := range configuredGroups {
		if group != "" && group != "auto" {
			availableGroups = append(availableGroups, group)
		}
	}
	slices.Sort(availableGroups)
	userGroup, supplied := c.Request.URL.Query()["user_group"]
	var group string
	if supplied {
		if len(userGroup) != 1 || !slices.Contains(availableGroups, userGroup[0]) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user pricing group"})
			return
		}
		group = userGroup[0]
	} else {
		user, err := model.GetUserCache(c.GetInt("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to resolve user pricing group"})
			return
		}
		group = user.Group
	}
	basis, err := service.CurrentBillingFXBasis()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
		return
	}
	response := effectivePricingPreviewResponse{
		effectivePricingResponse: buildEffectivePricingResponse(group, basis, model.GetPricing()),
		AvailableUserGroups:      availableGroups,
	}
	common.ApiSuccess(c, response)
}
