package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"

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

// InsertWithUniqueName inserts token unless another key of the same user that
// is not deleted has the same name (domain.NameTaken); then it returns
// domain.ErrNameDuplicate. The check and the insert share one transaction
// that holds the owner lock.
func (store Store) InsertWithUniqueName(token *Token) error {
	return store.lockOwner(token.UserId, func(tx Store) error {
		if err := tx.requireUniqueName(token.UserId, 0, token.Name); err != nil {
			return err
		}
		return tx.Insert(token)
	})
}

// UpdateWithUniqueName is Update that also writes the name, with the name rule
// of InsertWithUniqueName; the key itself does not occupy its name.
func (store Store) UpdateWithUniqueName(token *Token) error {
	return store.lockOwner(token.UserId, func(tx Store) error {
		if err := tx.requireUniqueName(token.UserId, token.Id, token.Name); err != nil {
			return err
		}
		return tx.update(token, true)
	})
}

// lockOwner calls run in a transaction that serializes it with every other
// lockOwner transaction of the same user.
func (store Store) lockOwner(userID int, run func(Store) error) error {
	return store.db().Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "sqlite" {
			// SQLite has no row locks: take the database-wide write lock before the first read.
			if err := tx.Model(&Token{}).Where("1 = 0").Update("id", gorm.Expr("id")).Error; err != nil {
				return err
			}
		} else {
			var ids []int
			if err := locking.LockForUpdate(tx.Table("users")).Where("id = ?", userID).Pluck("id", &ids).Error; err != nil {
				return err
			}
		}
		scoped := store
		scoped.db = func() *gorm.DB { return tx }
		return run(scoped)
	})
}

func (store Store) requireUniqueName(userID, exceptID int, name string) error {
	var names []string
	if err := store.db().Model(&Token{}).Where("user_id = ? AND id <> ?", userID, exceptID).Pluck("name", &names).Error; err != nil {
		return err
	}
	if domain.NameTaken(names, name) {
		return domain.ErrNameDuplicate
	}
	return nil
}

// Update writes the editable fields of token except its name: only
// UpdateWithUniqueName renames, so a concurrent edit cannot restore a stale
// name past the uniqueness check.
func (store Store) Update(token *Token) error { return store.update(token, false) }

func (store Store) update(token *Token, rename bool) error {
	// Invalidate the cache and set the fence before writing so that concurrent
	// readers cannot write a stale snapshot back into the cache.
	if cacheErr := store.invalidate(token.Key); cacheErr != nil {
		common.SysLog("failed to invalidate token cache before update: " + cacheErr.Error())
	}
	fields := []any{"expired_time", "remain_quota", "unlimited_quota", "model_limits_enabled", "model_limits", "allow_ips", "group", "cross_group_retry", "auto_groups"}
	if rename {
		fields = append(fields, "name")
	}
	return store.db().Model(token).Select("status", fields...).Updates(token).Error
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

// ListAfter returns up to limit keys of the user, newest first, with ids below
// afterID; afterID 0 starts from the newest key.
func (store Store) ListAfter(userId, afterID, limit int) ([]*Token, error) {
	query := store.db().Where("user_id = ?", userId)
	if afterID > 0 {
		query = query.Where("id < ?", afterID)
	}
	var tokens []*Token
	err := query.Order("id desc").Limit(limit).Find(&tokens).Error
	return tokens, err
}

// ListByIDs returns the keys with the given ids of any user, newest first.
func (store Store) ListByIDs(ids []int) ([]*Token, error) {
	var tokens []*Token
	err := store.db().Where("id IN (?)", ids).Order("id desc").Find(&tokens).Error
	return tokens, err
}

func (store Store) Get(id int, userId int) (*Token, error) {
	if id == 0 || userId == 0 {
		return nil, domain.ErrKeyIDRequired
	}
	token := Token{Id: id, UserId: userId}
	var err error = nil
	err = store.db().First(&token, "id = ? and user_id = ?", id, userId).Error
	return &token, err
}

func (store Store) GetByID(id int) (*Token, error) {
	if id == 0 {
		return nil, errors.New("api key id is empty")
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
		return 0, errors.New("api key ids are empty")
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
