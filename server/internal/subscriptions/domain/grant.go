package domain

import (
	"errors"
	"strings"
	"time"
)

// GrantTx preserves purchase counting, group changes and insertion in the owner's active transaction.
type GrantTx interface {
	CountPurchases(userID int, planID int) (int64, error)
	DatabaseTime() int64
	UserGroup(userID int) (string, error)
	SetUserGroup(userID int, group string) error
	InsertSubscription(subscription *UserSubscription) error
}

// GroupTx provides the locked group snapshot and the exact competing-upgrade lookup.
type GroupTx interface {
	UserGroup(userID int) (string, error)
	SetUserGroup(userID int, group string) error
	ActiveUpgrade(userID int, now int64, excludeID int) (bool, error)
}

func GrantSubscription(tx GrantTx, userId int, plan *SubscriptionPlan, source string, appNow func() int64) (*UserSubscription, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}
	if plan == nil || plan.Id == 0 {
		return nil, errors.New("invalid plan")
	}
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	if plan.MaxPurchasePerUser > 0 {
		count, err := tx.CountPurchases(userId, plan.Id)
		if err != nil {
			return nil, err
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			return nil, ErrPurchaseLimit
		}
	}
	nowUnix := tx.DatabaseTime()
	now := time.Unix(nowUnix, 0)
	endUnix, err := PlanEndTime(now, plan)
	if err != nil {
		return nil, err
	}
	resetBase := now
	nextReset := NextResetTime(resetBase, plan, endUnix)
	lastReset := int64(0)
	if nextReset > 0 {
		lastReset = now.Unix()
	}
	upgradeGroup := strings.TrimSpace(plan.UpgradeGroup)
	prevGroup := ""
	if upgradeGroup != "" {
		currentGroup, err := tx.UserGroup(userId)
		if err != nil {
			return nil, err
		}
		if currentGroup != upgradeGroup {
			prevGroup = currentGroup
			if err := tx.SetUserGroup(userId, upgradeGroup); err != nil {
				return nil, err
			}
		}
	}
	allowWalletOverflow := true
	if plan.AllowWalletOverflow != nil {
		allowWalletOverflow = *plan.AllowWalletOverflow
	}
	sub := &UserSubscription{
		UserId:              userId,
		PlanId:              plan.Id,
		AmountTotal:         plan.TotalAmount,
		AmountUsed:          0,
		StartTime:           now.Unix(),
		EndTime:             endUnix,
		Status:              "active",
		Source:              source,
		LastResetTime:       lastReset,
		NextResetTime:       nextReset,
		UpgradeGroup:        upgradeGroup,
		PrevUserGroup:       prevGroup,
		DowngradeGroup:      strings.TrimSpace(plan.DowngradeGroup),
		AllowWalletOverflow: allowWalletOverflow,
		CreatedAt:           appNow(),
		UpdatedAt:           appNow(),
	}
	if err := tx.InsertSubscription(sub); err != nil {
		return nil, err
	}
	return sub, nil
}

func DowngradeGroup(tx GroupTx, sub *UserSubscription, now int64) (string, error) {
	if tx == nil || sub == nil {
		return "", errors.New("invalid downgrade args")
	}
	downgradeGroup := strings.TrimSpace(sub.DowngradeGroup)
	upgradeGroup := strings.TrimSpace(sub.UpgradeGroup)
	// Nothing to do if neither an explicit downgrade target nor an upgrade snapshot exists.
	if downgradeGroup == "" && upgradeGroup == "" {
		return "", nil
	}
	currentGroup, err := tx.UserGroup(sub.UserId)
	if err != nil {
		return "", err
	}
	// If another active upgraded subscription exists, keep the current group.
	active, err := tx.ActiveUpgrade(sub.UserId, now, sub.Id)
	if err == nil && active {
		return "", nil
	}

	// Determine the downgrade target: an explicit downgrade group takes precedence,
	// otherwise revert to the group held before purchase (legacy behavior).
	target := downgradeGroup
	if target == "" {
		// Legacy behavior: only revert when the subscription actually elevated the user.
		if currentGroup != upgradeGroup {
			return "", nil
		}
		target = strings.TrimSpace(sub.PrevUserGroup)
	}
	if target == "" || target == currentGroup {
		return "", nil
	}
	if err := tx.SetUserGroup(sub.UserId, target); err != nil {
		return "", err
	}
	return target, nil
}
