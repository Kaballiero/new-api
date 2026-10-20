package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type Store struct {
	db             func() *gorm.DB
	keyColumn      func() string
	invalidate     func(string) error
	invalidateMany func([]Token) error
}

func NewStore(db func() *gorm.DB, keyColumn func() string, invalidate func(string) error, invalidateMany func([]Token) error) Store {
	return Store{db: db, keyColumn: keyColumn, invalidate: invalidate, invalidateMany: invalidateMany}
}

func (store Store) Insert(token *Token) error {
	var err error
	err = store.db().Create(token).Error
	return err
}

func (store Store) Update(token *Token) (err error) {
	// 写库前失效缓存并设置 fence，防止并发读者把过期快照重新写回缓存。
	if cacheErr := store.invalidate(token.Key); cacheErr != nil {
		common.SysLog("failed to invalidate token cache before update: " + cacheErr.Error())
	}
	return store.db().Model(token).Select("name", "status", "expired_time", "remain_quota", "unlimited_quota",
		"model_limits_enabled", "model_limits", "allow_ips", "group", "cross_group_retry", "auto_groups").Updates(token).Error
}

func (store Store) UpdateStatus(token *Token) (err error) {
	if cacheErr := store.invalidate(token.Key); cacheErr != nil {
		common.SysLog("failed to invalidate token cache before status update: " + cacheErr.Error())
	}
	// This can update zero values
	return store.db().Model(token).Select("accessed_time", "status").Updates(token).Error
}

func (store Store) Delete(token *Token) (err error) {
	if cacheErr := store.invalidate(token.Key); cacheErr != nil {
		common.SysLog("failed to invalidate token cache before delete: " + cacheErr.Error())
	}
	return store.db().Delete(token).Error
}

func (store Store) List(userId int, startIdx int, num int) ([]*Token, error) {
	var tokens []*Token
	var err error
	err = store.db().Where("user_id = ?", userId).Order("id desc").Limit(num).Offset(startIdx).Find(&tokens).Error
	return tokens, err
}

func (store Store) Get(id int, userId int) (*Token, error) {
	if id == 0 || userId == 0 {
		return nil, errors.New("id 或 userId 为空！")
	}
	token := Token{Id: id, UserId: userId}
	var err error = nil
	err = store.db().First(&token, "id = ? and user_id = ?", id, userId).Error
	return &token, err
}

func (store Store) GetByID(id int) (*Token, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	token := Token{Id: id}
	var err error = nil
	err = store.db().First(&token, "id = ?", id).Error
	return &token, err
}

func (store Store) Count(userId int) (int64, error) {
	var total int64
	err := store.db().Model(&Token{}).Where("user_id = ?", userId).Count(&total).Error
	return total, err
}

func (store Store) DeleteBatch(ids []int, userId int) (int, error) {
	if len(ids) == 0 {
		return 0, errors.New("ids 不能为空！")
	}

	tx := store.db().Begin()

	var tokens []Token
	if err := tx.Where("user_id = ? AND id IN (?)", userId, ids).Find(&tokens).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := store.invalidateMany(tokens); err != nil {
		common.SysLog("failed to invalidate token cache before batch delete: " + err.Error())
	}

	if err := tx.Where("user_id = ? AND id IN (?)", userId, ids).Delete(&Token{}).Error; err != nil {
		tx.Rollback()
		return 0, err
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}

	return len(tokens), nil
}

func (store Store) KeysBatch(ids []int, userId int) ([]Token, error) {
	var tokens []Token
	err := store.db().Select("id", store.keyColumn()).
		Where("user_id = ? AND id IN (?)", userId, ids).
		Find(&tokens).Error
	return tokens, err
}

// GetForDeletion preserves the legacy owner-scoped struct query.
func (store Store) GetForDeletion(id, userID int) (*Token, error) {
	token := Token{Id: id, UserId: userID}
	err := store.db().Where(token).First(&token).Error
	return &token, err
}
