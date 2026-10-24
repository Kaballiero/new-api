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
