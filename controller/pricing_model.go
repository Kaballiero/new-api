package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type consumerPricingModelDetail struct {
	SchemaVersion   string                   `json:"schema_version"`
	CatalogRevision string                   `json:"catalog_revision"`
	UserGroup       string                   `json:"user_group"`
	Currency        string                   `json:"currency"`
	AutoGroups      []string                 `json:"auto_groups"`
	Model           effectivePricingPageItem `json:"model"`
}

func parseEffectivePricingModelQuery(values url.Values) (string, string, error) {
	for key := range values {
		if key != "model_name" && key != "expected_revision" {
			return "", "", fmt.Errorf("unsupported %s", key)
		}
	}
	names := values["model_name"]
	if len(names) != 1 || strings.TrimSpace(names[0]) == "" {
		return "", "", fmt.Errorf("model_name is required")
	}
	expected, err := expectedPricingRevision(values)
	return names[0], expected, err
}

func GetEffectivePricingModel(c *gin.Context) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid pricing query"})
		return
	}
	name, expected, err := parseEffectivePricingModelQuery(values)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	user, err := model.GetUserCache(c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to resolve user pricing group"})
		return
	}
	context, err := loadConsumerPricingContext(user.Group, "")
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
		return
	}
	if !pricingRevisionMatches(c, expected, context.metadata.CatalogRevision) {
		return
	}
	context.userID = c.GetInt("id")
	detail, found := buildEffectivePricingModelDetail(context, name)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "model pricing is unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": detail})
}

func buildEffectivePricingModelDetail(context effectivePricingPageContext, name string) (consumerPricingModelDetail, bool) {
	for _, item := range model.ConsumerPricingCatalogue(context.pricing, context.usableGroups) {
		if item.ModelName != name || len(effectivePricingGroups(item.EnableGroup, context.usableGroups)) == 0 {
			continue
		}
		projected := buildConsumerPricingModel(context.usableGroups, context.groupRatios, context.fxFactor, item, true, pricingUserContext{id: context.userID, group: context.metadata.UserGroup})
		var creator *effectivePricingCreator
		for _, vendor := range context.vendors {
			if item.VendorID != 0 && vendor.ID == item.VendorID {
				if name := trimPricingText(vendor.Name); name != "" {
					creator = &effectivePricingCreator{ID: vendor.ID, Name: name}
				}
				break
			}
		}
		return consumerPricingModelDetail{
			SchemaVersion: context.metadata.SchemaVersion, CatalogRevision: context.metadata.CatalogRevision,
			UserGroup: context.metadata.UserGroup, Currency: context.metadata.Currency, AutoGroups: context.metadata.AutoGroups,
			Model: effectivePricingPageItem{consumerPricingModel: projected, Modality: effectivePricingModality(item), Creator: creator},
		}, true
	}
	return consumerPricingModelDetail{}, false
}
