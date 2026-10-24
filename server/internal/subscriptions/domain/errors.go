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

var (
	ErrStripePriceMissing    = errors.New("该套餐未配置 StripePriceId")
	ErrStripeConfig          = errors.New("Stripe 未配置或密钥无效")
	ErrStripeWebhook         = errors.New("Stripe Webhook 未配置")
	ErrCreemProductMissing   = errors.New("该套餐未配置 CreemProductId")
	ErrCreemWebhook          = errors.New("Creem Webhook 未配置")
	ErrPancakeProductMissing = errors.New("该套餐未配置 WaffoPancakeProductId")
	ErrPancakeConfig         = errors.New("Waffo Pancake 未配置或密钥无效")
	ErrUserMissing           = errors.New("用户不存在")
	ErrPriceLow              = errors.New("套餐金额过低")
	ErrPayMethod             = errors.New("支付方式不存在")
	ErrCallbackConfig        = errors.New("回调地址配置错误")
	ErrEpayConfig            = errors.New("当前管理员未配置支付信息")
	ErrCreateOrder           = errors.New("创建订单失败")
	ErrCheckout              = errors.New("拉起支付失败")
)

var (
	ErrProductName    = errors.New("套餐名称不能为空")
	ErrProductAmount  = errors.New("套餐价格不能为空")
	ErrPancakeBinding = errors.New("Waffo Pancake 未完成配置，请先在支付设置中完成网关绑定")
	ErrCreateProduct  = errors.New("创建套餐产品失败")
	ErrProductCatalog = errors.New("拉取产品列表失败")
)
