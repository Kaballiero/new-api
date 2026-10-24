package controller

import (
	"sync"

	"github.com/gin-gonic/gin"

	subscriptionsapp "github.com/QuantumNous/new-api/internal/app/subscriptions"
)

var subscriptionsHTTP = sync.OnceValue(subscriptionsapp.NewHTTP)

func GetSubscriptionPlans(c *gin.Context)              { subscriptionsHTTP().ListPlans.Handle(c) }
func GetSubscriptionSelf(c *gin.Context)               { subscriptionsHTTP().Own.Handle(c) }
func UpdateSubscriptionPreference(c *gin.Context)      { subscriptionsHTTP().Preference.Handle(c) }
func SubscriptionRequestBalancePay(c *gin.Context)     { subscriptionsHTTP().Balance.Handle(c) }
func AdminListSubscriptionPlans(c *gin.Context)        { subscriptionsHTTP().ListAllPlans.Handle(c) }
func AdminCreateSubscriptionPlan(c *gin.Context)       { subscriptionsHTTP().CreatePlan.Handle(c) }
func AdminUpdateSubscriptionPlan(c *gin.Context)       { subscriptionsHTTP().UpdatePlan.Handle(c) }
func AdminUpdateSubscriptionPlanStatus(c *gin.Context) { subscriptionsHTTP().PlanStatus.Handle(c) }
func AdminBindSubscription(c *gin.Context)             { subscriptionsHTTP().Bind.Handle(c) }
func AdminCreateUserSubscription(c *gin.Context)       { subscriptionsHTTP().Bind.CreateForUser(c) }
func AdminListUserSubscriptions(c *gin.Context)        { subscriptionsHTTP().UserSubscriptions.Handle(c) }
func AdminResetUserSubscriptionsByPlan(c *gin.Context) { subscriptionsHTTP().Reset.ForUser(c) }
func AdminResetPlanSubscriptions(c *gin.Context)       { subscriptionsHTTP().Reset.ForPlan(c) }
func AdminInvalidateUserSubscription(c *gin.Context)   { subscriptionsHTTP().Invalidate.Handle(c) }
func AdminDeleteUserSubscription(c *gin.Context)       { subscriptionsHTTP().Delete.Handle(c) }

var subscriptionProviders = sync.OnceValue(subscriptionsapp.NewProviders)
