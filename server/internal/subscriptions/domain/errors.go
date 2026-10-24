package domain

import "errors"

var ErrInvalidUserID = errors.New("无效的用户ID")

var (
	ErrInvalidID             = errors.New("无效的ID")
	ErrTitleRequired         = errors.New("套餐标题不能为空")
	ErrNegativePrice         = errors.New("价格不能为负数")
	ErrHighPrice             = errors.New("价格不能超过9999")
	ErrNegativePurchaseLimit = errors.New("购买上限不能为负数")
	ErrNegativeTotal         = errors.New("总额度不能为负数")
	ErrUpgradeGroupMissing   = errors.New("升级分组不存在")
	ErrDowngradeGroupMissing = errors.New("降级分组不存在")
	ErrInvalidResetSeconds   = errors.New("自定义重置周期需大于0秒")
)

var ErrPurchaseLimit = errors.New("已达到该套餐购买上限")

var (
	ErrNoActivePlan       = errors.New("该用户没有有效的此套餐订阅")
	ErrOrderNotFound      = errors.New("subscription order not found")
	ErrOrderStatusInvalid = errors.New("subscription order status invalid")
	ErrProviderMismatch   = errors.New("payment method mismatch")
)

var (
	ErrPlanDisabled          = errors.New("套餐未启用")
	ErrBalanceNegativePrice  = errors.New("套餐价格不能为负数")
	ErrBalanceDisallowed     = errors.New("该套餐不允许使用余额兑换")
	ErrInsufficientBalance   = errors.New("余额不足")
	ErrInvalidQuotaUnit      = errors.New("额度单位配置错误")
	ErrInvalidSubscriptionID = errors.New("无效的订阅ID")
)
