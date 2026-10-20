package authorization

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/authorization/record"
	"github.com/QuantumNous/new-api/internal/authorization/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) Store { return Store{db: db} }
func (store Store) UpsertRole(spec domain.RoleSpec) error {
	role := record.AuthzRole{
		Key:         spec.Key,
		Name:        spec.Name,
		Description: spec.Description,
		BuiltIn:     spec.BuiltIn,
		Enabled:     true,
		Sort:        spec.Sort,
	}
	if err := store.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name",
			"description",
			"built_in",
			"enabled",
			"sort",
		}),
	}).Create(&role).Error; err != nil {
		return err
	}
	return nil
}
func (store Store) ResetRolePolicies(subjects []string) error {
	return store.db.Where("ptype = ? AND v0 IN ?", "p", subjects).Delete(&record.CasbinRule{}).Error
}
func (store Store) RemoveResource(userID int, resource string) error {
	return store.db.Where("ptype = ? AND v0 = ? AND v1 = ?", "p", domain.UserSubject(userID), resource).Delete(&record.CasbinRule{}).Error
}
func (store Store) InsertOverrides(userID int, policies []domain.OverridePolicy) error {
	rules := make([]record.CasbinRule, 0, len(policies))
	for _, policy := range policies {
		rules = append(rules, newRule("p", []string{domain.UserSubject(userID), policy.Resource, policy.Action, policy.Effect}))
	}
	return store.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rules).Error
}
