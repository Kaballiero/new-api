package controller

import (
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"

	"github.com/gin-gonic/gin"
)

func GetSubscriptionPlans(c *gin.Context) { httpapi.Subscriptions().ListPlans.Handle(c) }

func GetSubscriptionSelf(c *gin.Context) { httpapi.Subscriptions().Own.Handle(c) }

func UpdateSubscriptionPreference(c *gin.Context) { httpapi.Subscriptions().Preference.Handle(c) }

func SubscriptionRequestBalancePay(c *gin.Context) { httpapi.Subscriptions().Balance.Handle(c) }

func AdminListSubscriptionPlans(c *gin.Context) { httpapi.Subscriptions().ListAllPlans.Handle(c) }

func AdminCreateSubscriptionPlan(c *gin.Context) { httpapi.Subscriptions().CreatePlan.Handle(c) }

func AdminUpdateSubscriptionPlan(c *gin.Context) { httpapi.Subscriptions().UpdatePlan.Handle(c) }

func AdminUpdateSubscriptionPlanStatus(c *gin.Context) { httpapi.Subscriptions().PlanStatus.Handle(c) }

func AdminBindSubscription(c *gin.Context) { httpapi.Subscriptions().Bind.Handle(c) }

func AdminCreateUserSubscription(c *gin.Context) { httpapi.Subscriptions().Bind.CreateForUser(c) }

func AdminListUserSubscriptions(c *gin.Context) { httpapi.Subscriptions().UserSubscriptions.Handle(c) }

func AdminResetUserSubscriptionsByPlan(c *gin.Context) { httpapi.Subscriptions().Reset.ForUser(c) }

func AdminResetPlanSubscriptions(c *gin.Context) { httpapi.Subscriptions().Reset.ForPlan(c) }

func AdminInvalidateUserSubscription(c *gin.Context) { httpapi.Subscriptions().Invalidate.Handle(c) }

func AdminDeleteUserSubscription(c *gin.Context) { httpapi.Subscriptions().Delete.Handle(c) }
