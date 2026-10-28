package common

import "github.com/gin-gonic/gin"

// BillingSettler is the existing gateway lifecycle contract, implemented by
// relay/billing.Session. Business reservation state itself has no Gin dependency.
type BillingSettler interface {
	// Settle applies the difference between actual quota and the hold.
	Settle(actualQuota int) error
	// Refund queues the remaining hold's refund once, unless already terminal.
	Refund(c *gin.Context)
	// NeedsRefund reports a nonterminal, refundable funding/token hold.
	NeedsRefund() bool
	// GetPreConsumedQuota returns the current hold, including the trusted zero.
	GetPreConsumedQuota() int
	// Reserve raises the hold to a higher target and keeps lower targets intact.
	Reserve(targetQuota int) error
}
