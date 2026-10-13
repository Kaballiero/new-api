package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

var errPricingUsingGroup = errors.New("invalid using_group")

func expectedPricingRevision(values url.Values) (string, error) {
	entries, exists := values["expected_revision"]
	if !exists {
		return "", nil
	}
	if len(entries) != 1 || entries[0] == "" || len(entries[0]) > 256 {
		return "", fmt.Errorf("invalid expected_revision")
	}
	return entries[0], nil
}

func pricingRevisionMatches(c *gin.Context, expected, actual string) bool {
	if expected == "" || expected == actual {
		return true
	}
	c.JSON(http.StatusConflict, gin.H{"success": false, "code": "pricing_catalog_revision_mismatch", "message": "Pricing catalog revision changed"})
	return false
}

func consumerPricingRevision(source string, context effectivePricingPageContext) (string, error) {
	value, err := common.Marshal(struct {
		Source, UserGroup, UsingGroup string
		AutoGroups                    []string
		UsableGroups                  map[string]string
		GroupRatios                   map[string]float64
		FXFactor                      float64
	}{source, context.metadata.UserGroup, context.usingGroup, context.metadata.AutoGroups, context.usableGroups, context.groupRatios, context.fxFactor})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:]), nil
}

func loadConsumerPricingContext(userGroup, usingGroup string) (effectivePricingPageContext, error) {
	context := effectivePricingPageContext{
		metadata:     consumerPricingResponse{SchemaVersion: "v2", UserGroup: userGroup, Currency: "RUB", AutoGroups: service.GetUserAutoGroup(userGroup), Data: []consumerPricingModel{}},
		usableGroups: service.GetUserUsableGroups(userGroup), groupRatios: make(map[string]float64), usingGroup: usingGroup,
	}
	if usingGroup != "" {
		if _, allowed := context.usableGroups[usingGroup]; !allowed {
			return context, errPricingUsingGroup
		}
	}
	basis, err := service.CurrentBillingFXBasis()
	if err != nil {
		return context, err
	}
	context.fxFactor = basis.Factor
	source := ""
	context.pricing, context.vendors, source, err = model.GetPricingCatalogueSnapshot()
	if err != nil {
		return context, fmt.Errorf("failed to resolve pricing catalog revision")
	}
	for group := range context.usableGroups {
		context.groupRatios[group] = service.GetUserGroupRatio(userGroup, group)
	}
	if usingGroup != "" {
		context.usableGroups = map[string]string{usingGroup: context.usableGroups[usingGroup]}
	}
	context.metadata.CatalogRevision, err = consumerPricingRevision(source, context)
	return context, err
}
