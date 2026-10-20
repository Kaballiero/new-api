package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	"github.com/QuantumNous/new-api/internal/catalog/insert_vendor_record"
	"github.com/QuantumNous/new-api/internal/catalog/update_vendor_record"
)

// Vendor 用于存储供应商信息，供模型引用
// Name 唯一，用于在模型中关联
// Icon 采用 @lobehub/icons 的图标名，前端可直接渲染
// Status 预留字段，1 表示启用
// 本表同样遵循 3NF 设计范式

type Vendor record.Vendor

// Insert 创建新的供应商记录
func (value *Vendor) Insert() error {
	native := (*record.Vendor)(value)
	input := record.ToVendor(native)
	defer func() { saved := native.DeletedAt; *native = *record.FromVendor(input); native.DeletedAt = saved }()
	return insert_vendor_record.NewUsecase(catalogMetadataWriteBridge{}, common.GetTimestamp, RefreshPricing).Execute(input)
}

// IsVendorNameDuplicated 检查供应商名称是否重复（排除自身 ID）
func IsVendorNameDuplicated(id int, name string) (bool, error) {
	return NewCatalogPersistence().IsVendorNameDuplicated(id, name)
}

// Update 更新供应商记录
func (value *Vendor) Update() error {
	native := (*record.Vendor)(value)
	input := record.ToVendor(native)
	defer func() { saved := native.DeletedAt; *native = *record.FromVendor(input); native.DeletedAt = saved }()
	return update_vendor_record.NewUsecase(catalogMetadataWriteBridge{}, common.GetTimestamp, RefreshPricing).Execute(input)
}

// Delete rejects referenced vendors rather than leaving orphaned model records.
func (v *Vendor) Delete() error { return DeleteVendors([]int{v.Id}) }

// GetVendorByID 根据 ID 获取供应商
func GetVendorByID(id int) (*Vendor, error) {
	v, e := NewCatalogPersistence().GetVendorByID(id)
	if e != nil {
		return nil, e
	}
	value := (*Vendor)(v)
	value.Version = VendorRecordVersion(value)
	return value, nil
}

// GetAllVendors 获取全部供应商（分页）
func GetAllVendors(offset int, limit int) ([]*Vendor, error) {
	vendors, _, err := SearchVendors("", offset, limit)
	return vendors, err
}

// SearchVendors filters persisted vendor records and counts actual model assignments.
func SearchVendors(keyword string, offset, limit int, association ...string) ([]*Vendor, int64, error) {
	values, n, e := NewCatalogPersistence().SearchVendors(keyword, offset, limit, association...)
	if values == nil {
		return nil, n, e
	}
	out := make([]*Vendor, len(values))
	for i, v := range values {
		out[i] = (*Vendor)(v)
		out[i].Version = VendorRecordVersion(out[i])
	}
	return out, n, e
}
