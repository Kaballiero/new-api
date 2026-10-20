package personal_access_tokens

import (
	"errors"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/revoke_token"
	"gorm.io/gorm"
)

type Store struct {
	db       func() *gorm.DB
	logQuery func() *gorm.DB
	lock     func(*gorm.DB) *gorm.DB
	now      func() int64
}

func NewStore(db func() *gorm.DB, logQuery func() *gorm.DB, lock func(*gorm.DB) *gorm.DB, now func() int64) Store {
	return Store{db: db, logQuery: logQuery, lock: lock, now: now}
}
func (store Store) IsMissing(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
func (store Store) Account(id int) (domain.Account, error) {
	var user record.User
	if err := store.db().Select("id", "role", "access_token", "access_token_created_at").First(&user, id).Error; err != nil {
		return domain.Account{}, err
	}
	token := ""
	if user.AccessToken != nil {
		token = *user.AccessToken
	}
	return domain.Account{Token: token, Role: user.Role, CreatedAt: user.AccessTokenCreatedAt}, nil
}
func (store Store) LatestUsage(userID int, ref string, roles []int) (domain.Usage, error) {
	var latest domain.Usage
	query := store.logQuery().Select("created_at", "ip").Where("user_id = ? AND token_ref = ? AND category = ?", userID, ref, "access_token")
	if roles != nil {
		query = query.Where("actor_role IN ?", roles)
	}
	err := query.Order("created_at DESC").Order("event_id DESC").Take(&latest).Error
	return latest, err
}
func (store Store) CountToken(key string) (int64, error) {
	var existing int64
	err := store.db().Model(&record.User{}).Where("access_token = ?", key).Count(&existing).Error
	return existing, err
}
func (store Store) Update(id int, token string) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	result := store.db().Model(&record.User{}).Where("id = ?", id).Updates(map[string]any{"access_token": token, "access_token_created_at": store.now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

type transaction struct {
	db   *gorm.DB
	lock func(*gorm.DB) *gorm.DB
}

func (store Store) Transaction(action func(revoke_token.Transaction) error) error {
	return store.db().Transaction(func(tx *gorm.DB) error { return action(transaction{db: tx, lock: store.lock}) })
}
func (tx transaction) LockToken(id int) (string, error) {
	var user record.User
	if err := tx.lock(tx.db).Select("id", "access_token").First(&user, id).Error; err != nil {
		return "", err
	}
	if user.AccessToken == nil {
		return "", nil
	}
	return *user.AccessToken, nil
}
func (tx transaction) Clear(id int) error {
	return tx.db.Model(&record.User{}).Where("id = ?", id).Updates(map[string]any{"access_token": nil, "access_token_created_at": nil}).Error
}
