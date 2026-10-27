package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
	"sync"
)

var billingCompatibilityHTTP = sync.OnceValue(app.NewBillingCompatibilityHTTP)

// BRIDGE: dashboard subscription routes retain their handler name; remove when the router binds the owning HTTP handler directly.
func GetSubscription(c *gin.Context) { billingCompatibilityHTTP().Subscription.Handle(c) }

// BRIDGE: dashboard usage routes retain their handler name; remove when the router binds the owning HTTP handler directly.
func GetUsage(c *gin.Context) { billingCompatibilityHTTP().Usage.Handle(c) }
