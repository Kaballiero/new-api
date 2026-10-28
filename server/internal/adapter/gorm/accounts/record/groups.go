package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/change_group"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

type GroupStore struct {
	db      func() *gorm.DB
	refresh func(int) error
}

// RolesByIDs возвращает map от ID пользователя к роли для указанных ID.
// В map входят только существующие пользователи. Отсутствующие ID определяет вызывающий код.
func (s GroupStore) RolesByIDs(ids []int) (map[int]int, error) {
	if len(ids) == 0 {
		return map[int]int{}, nil
	}
	type idRole struct {
		Id   int
		Role int
	}
	var rows []idRole
	if err := s.db().Model(&User{}).Select("id, role").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int]int, len(rows))
	for _, r := range rows {
		out[r.Id] = r.Role
	}
	return out, nil
}

// BatchUpdateUserGroup обновляет столбец `group` указанных пользователей в одной транзакции
// и сбрасывает Redis-кэш каждого пользователя. Для SQLite разбивает ID на части, чтобы не превысить
// ограничение "too many SQL variables". Возвращает число затронутых строк.
//
// Функция НЕ проверяет права по ролям: вызывающий ОБЯЗАН заранее отфильтровать
// ID по роли инициатора запроса (см. слой controller).
func (s GroupStore) UpdateGroup(ids []int, group string) (int64, error) {
	return change_group.NewGroupUpdate(GroupPersistence{db: s.db, refresh: s.refresh}).Execute(ids, group)
}

type GroupPersistence struct {
	db      func() *gorm.DB
	refresh func(int) error
}

func (GroupPersistence) SQLite() bool { return common.UsingMainDatabase(common.DatabaseTypeSQLite) }
func (s GroupPersistence) Begin() change_group.GroupMutation {
	return groupMutation{tx: s.db().Begin()}
}
func (GroupPersistence) RefreshEnabled() bool { return common.RedisEnabled }
func (s GroupPersistence) RefreshGroups(ids []int) {
	gopool.Go(func() {
		for _, id := range ids {
			if err := s.refresh(id); err != nil {
				common.SysError(fmt.Sprintf("failed to refresh batch user group cache for user %d: %s", id, err))
			}
		}
	})
}

type groupMutation struct{ tx *gorm.DB }

func (m groupMutation) Update(ids []int, group string) (int64, error) {
	result := m.tx.Model(&User{}).Where("id IN ?", ids).Updates(map[string]any{"group": group})
	return result.RowsAffected, result.Error
}
func (m groupMutation) Commit() error { return m.tx.Commit().Error }
func (m groupMutation) Rollback()     { m.tx.Rollback() }

func NewGroupStore(db func() *gorm.DB, refresh func(int) error) GroupStore {
	return GroupStore{db: db, refresh: refresh}
}
