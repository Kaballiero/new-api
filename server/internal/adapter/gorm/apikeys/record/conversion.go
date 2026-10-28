package model

import (
	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"gorm.io/gorm"
)

func ToDomain(t *Token) *domain.Token {
	if t == nil {
		return nil
	}
	r := &domain.Token{CacheSchema: t.CacheSchema, Id: t.Id, UserId: t.UserId, Key: t.Key, Status: t.Status, Name: t.Name, CreatedTime: t.CreatedTime, AccessedTime: t.AccessedTime, ExpiredTime: t.ExpiredTime, RemainQuota: t.RemainQuota, BatchHeldQuota: t.BatchHeldQuota, BatchQuotaVersion: t.BatchQuotaVersion, UnlimitedQuota: t.UnlimitedQuota, ModelLimitsEnabled: t.ModelLimitsEnabled, ModelLimits: t.ModelLimits, AllowIps: t.AllowIps, UsedQuota: t.UsedQuota, Group: t.Group, CrossGroupRetry: t.CrossGroupRetry, AutoGroups: t.AutoGroups}
	if t.DeletedAt.Valid {
		v := t.DeletedAt.Time
		r.DeletedAt = &v
	}
	return r
}
func FromDomain(t *domain.Token) *Token {
	r := &Token{CacheSchema: t.CacheSchema, Id: t.Id, UserId: t.UserId, Key: t.Key, Status: t.Status, Name: t.Name, CreatedTime: t.CreatedTime, AccessedTime: t.AccessedTime, ExpiredTime: t.ExpiredTime, RemainQuota: t.RemainQuota, BatchHeldQuota: t.BatchHeldQuota, BatchQuotaVersion: t.BatchQuotaVersion, UnlimitedQuota: t.UnlimitedQuota, ModelLimitsEnabled: t.ModelLimitsEnabled, ModelLimits: t.ModelLimits, AllowIps: t.AllowIps, UsedQuota: t.UsedQuota, Group: t.Group, CrossGroupRetry: t.CrossGroupRetry, AutoGroups: t.AutoGroups}
	if t.DeletedAt != nil {
		r.DeletedAt = gorm.DeletedAt{Time: *t.DeletedAt, Valid: true}
	}
	return r
}
