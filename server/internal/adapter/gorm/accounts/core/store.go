// Package core implements account SQL and shared quota operations behind explicit capabilities.
package core

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	accountcache "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/counters"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
	"strings"
)

type Environment struct {
	DB          func() *gorm.DB
	GroupColumn func() string
	Cache       accountcache.Store
}
type Store struct {
	env     Environment
	queries record.Queries
}

func NewStore(env Environment) Store {
	return Store{env: env, queries: record.NewQueries(env.DB, env.GroupColumn)}
}

var ErrDatabase = errors.New("database error")
var userBindColumns = map[string]bool{"github_id": true, "discord_id": true, "oidc_id": true, "linux_do_id": true, "wechat_id": true}

func AllowedBindColumn(column string) bool { return userBindColumns[column] }
func (store Store) UpdateUserSetting(userId int, setting dto.UserSetting) error {
	if userId == 0 {
		return errors.New("id 为空！")
	}
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		return err
	}
	settingValue := string(settingBytes)
	if err = store.env.DB().Model(&record.User{}).Where("id = ?", userId).Update("setting", settingValue).Error; err != nil {
		return err
	}
	return store.env.Cache.UpdateUserSettingCache(userId, settingValue)
}

// UpdateUserBindColumn 第三方账号绑定字段的专用更新。
// 绑定操作必须只写绑定列：若改为“读取完整用户 → 改一个字段 → 整体更新”，
// 读快照期间并发发生的封禁、降权或分组变更会被旧快照覆盖恢复。
// 角色、状态、分组只允许通过各自带锁/CAS 的专用方法修改。
func (store Store) UpdateUserBindColumn(userId int, column string, value string) error {
	if userId <= 0 {
		return errors.New("id 为空！")
	}
	if !userBindColumns[column] {
		return fmt.Errorf("invalid user bind column: %s", column)
	}
	return store.env.DB().Model(&record.User{}).Where("id = ?", userId).Update(column, value).Error
}

// CheckUserExistOrDeleted check if user exist or deleted, if not exist, return false, nil, if deleted or exist, return true, nil
func (store Store) CheckUserExistOrDeleted(username string, email string) (bool, error) {
	var user record.User

	// err := store.env.DB().Unscoped().First(&user, "username = ? or email = ?", username, email).Error
	// check email if empty
	var err error
	email = NormalizeEmail(email)
	if email == "" {
		err = store.env.DB().Unscoped().First(&user, "username = ?", username).Error
	} else {
		err = store.env.DB().Unscoped().First(&user, "username = ? or LOWER(email) = ?", username, email).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// not exist, return false, nil
			return false, nil
		}
		// other error, return false, err
		return false, err
	}
	// exist, return true, nil
	return true, nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (store Store) emailQuery(tx *gorm.DB, email string) *gorm.DB {
	if tx == nil {
		tx = store.env.DB()
	}
	return tx.Unscoped().Model(&record.User{}).Where("LOWER(email) = ?", NormalizeEmail(email))
}

func (store Store) CountUsersByEmail(email string) (int64, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return 0, nil
	}
	var count int64
	err := store.emailQuery(store.env.DB(), email).Count(&count).Error
	return count, err
}

func (store Store) IsEmailAvailable(email string, excludeUserID int) (bool, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return true, nil
	}
	query := store.emailQuery(store.env.DB(), email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count == 0, nil
}

func (store Store) EnsureEmailAvailable(email string, excludeUserID int) error {
	available, err := store.IsEmailAvailable(email, excludeUserID)
	if err != nil {
		return err
	}
	if !available {
		return domain.ErrEmailAlreadyTaken
	}
	return nil
}

// withNormalizedEmailLock serializes concurrent writers that target the same
// normalized email inside tx, so a "check then write" sequence cannot be raced
// by two transactions. It must be called inside an active transaction; the lock
// is scoped to that transaction and released on commit/rollback.
//
//   - PostgreSQL: transaction-level advisory lock keyed by the normalized email.
//   - MySQL (default REPEATABLE READ): a locking read that takes a next-key/gap
//     lock on the email index, blocking concurrent inserts of the same value.
//   - SQLite: no explicit lock; the single-writer model already serializes the
//     write, so a racing second write fails instead of duplicating.
//
// An empty email is allowed to repeat and needs no serialization.
func (store Store) WithNormalizedEmailLock(tx *gorm.DB, email string, fn func(tx *gorm.DB) error) error {
	if err := store.LockNormalizedEmail(tx, email); err != nil {
		return err
	}
	return fn(tx)
}

func (store Store) LockNormalizedEmail(tx *gorm.DB, email string) error {
	email = NormalizeEmail(email)
	if email == "" {
		return nil
	}
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", email).Error; err != nil {
			return err
		}
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		var ids []int
		if err := tx.Raw("SELECT id FROM users WHERE email = ? FOR UPDATE", email).Scan(&ids).Error; err != nil {
			return err
		}
	}
	return nil
}

func (store Store) GetMaxUserId() int { return store.queries.GetMaxUserId() }

func (store Store) GetAllUsers(pageInfo *common.PageInfo, sortOptions ...record.UserSortOptions) (users []*record.User, total int64, err error) {
	rows, total, err := store.queries.GetAllUsers(pageInfo, sortOptions...)
	return rows, total, err
}

func (store Store) SearchUsers(keyword string, group string, role *int, status *int, startIdx int, num int, sortOptions ...record.UserSortOptions) ([]*record.User, int64, error) {
	rows, total, err := store.queries.SearchUsers(keyword, group, role, status, startIdx, num, sortOptions...)
	return rows, total, err
}

func (store Store) GetUserById(id int, selectAll bool) (*record.User, error) {
	user, err := store.queries.GetUserById(id, selectAll)
	return (*record.User)(user), err
}

func (store Store) GetUsersBatch(ids []int) ([]record.UserBatchRow, error) {
	return store.queries.GetUsersBatch(ids)
}

// GetSelfUserById reads dashboard profile data and password existence in one
// query. The password hash and management access token are never selected.
func (store Store) GetSelfUserById(id int) (*record.User, error) {
	user, err := store.queries.GetSelfUserById(id)
	return (*record.User)(user), err
}

func (store Store) GetUserIdByAffCode(affCode string) (int, error) {
	return store.queries.GetUserIdByAffCode(affCode)
}

func (store Store) InviteUser(inviterId int) error {
	result := store.env.DB().Model(&record.User{}).Where("id = ?", inviterId).Updates(map[string]any{
		"aff_count":   gorm.Expr("aff_count + ?", 1),
		"aff_quota":   gorm.Expr("aff_quota + ?", common.QuotaForInviter),
		"aff_history": gorm.Expr("aff_history + ?", common.QuotaForInviter),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (store Store) EnsureEmailAvailableWithTx(tx *gorm.DB, email string, excludeUserID int) error {
	email = NormalizeEmail(email)
	if email == "" {
		return nil
	}
	query := store.emailQuery(tx, email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return domain.ErrEmailAlreadyTaken
	}
	return nil
}

// EnsureUsernameAvailableWithTx checks that username is not taken by any user
// other than excludeUserID (pass 0 for insert). Uses Unscoped so soft-deleted
// users still block reuse — the unique index covers them, matching the email
// helper's behavior. Works on SQLite/MySQL/PostgreSQL.
func (store Store) EnsureUsernameAvailableWithTx(tx *gorm.DB, username string, excludeUserID int) error {
	if tx == nil {
		tx = store.env.DB()
	}
	query := tx.Unscoped().Model(&record.User{}).Where("username = ?", username)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return domain.ErrUsernameAlreadyTaken
	}
	return nil
}

func (store Store) FillUserById(user *record.User) error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	store.env.DB().Where(record.User{Id: user.Id}).First(user)
	return nil
}

func (store Store) FillUserByGitHubId(user *record.User) error {
	if user.GitHubId == "" {
		return errors.New("GitHub id 为空！")
	}
	store.env.DB().Where(record.User{GitHubId: user.GitHubId}).First(user)
	return nil
}

// UpdateGitHubId updates the user's GitHub ID (used for migration from login to numeric ID)
func (store Store) UpdateGitHubId(user *record.User, newGitHubId string) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}
	return store.env.DB().Model(user).Update("github_id", newGitHubId).Error
}

func (store Store) FillUserByDiscordId(user *record.User) error {
	if user.DiscordId == "" {
		return errors.New("discord id 为空！")
	}
	store.env.DB().Where(record.User{DiscordId: user.DiscordId}).First(user)
	return nil
}

func (store Store) FillUserByOidcId(user *record.User) error {
	if user.OidcId == "" {
		return errors.New("oidc id 为空！")
	}
	store.env.DB().Where(record.User{OidcId: user.OidcId}).First(user)
	return nil
}

func (store Store) FillUserByWeChatId(user *record.User) error {
	if user.WeChatId == "" {
		return errors.New("WeChat id 为空！")
	}
	store.env.DB().Where(record.User{WeChatId: user.WeChatId}).First(user)
	return nil
}

func (store Store) IsEmailAlreadyTaken(email string) bool {
	count, err := store.CountUsersByEmail(email)
	return err == nil && count > 0
}

func (store Store) GetUniqueUserByEmail(email string) (*record.User, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, domain.ErrEmailNotFound
	}
	var users []record.User
	if err := store.env.DB().Where("LOWER(email) = ?", email).Limit(2).Find(&users).Error; err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, domain.ErrEmailNotFound
	case 1:
		return &users[0], nil
	default:
		return nil, domain.ErrEmailAmbiguous
	}
}

func (store Store) IsWeChatIdAlreadyTaken(wechatId string) bool {
	return store.env.DB().Unscoped().Where("wechat_id = ?", wechatId).Find(&record.User{}).RowsAffected == 1
}

func (store Store) IsGitHubIdAlreadyTaken(githubId string) bool {
	return store.env.DB().Unscoped().Where("github_id = ?", githubId).Find(&record.User{}).RowsAffected == 1
}

func (store Store) IsDiscordIdAlreadyTaken(discordId string) bool {
	return store.env.DB().Unscoped().Where("discord_id = ?", discordId).Find(&record.User{}).RowsAffected == 1
}

func (store Store) IsOidcIdAlreadyTaken(oidcId string) bool {
	return store.env.DB().Where("oidc_id = ?", oidcId).Find(&record.User{}).RowsAffected == 1
}

func (store Store) IsTelegramIdAlreadyTaken(telegramId string) bool {
	return store.env.DB().Unscoped().Where("telegram_id = ?", telegramId).Find(&record.User{}).RowsAffected == 1
}

func (store Store) IsAdmin(userId int) bool {
	if userId == 0 {
		return false
	}
	var user record.User
	err := store.env.DB().Where("id = ?", userId).Select("role").Find(&user).Error
	if err != nil {
		common.SysLog("no such user " + err.Error())
		return false
	}
	return user.Role >= common.RoleAdminUser
}

// GetUserQuota gets quota from Redis first, falls back to DB if needed
func (store Store) GetUserQuota(id int, fromDB bool) (quota int, err error) {
	if !fromDB && common.RedisEnabled {
		return store.env.Cache.GetUserQuotaCache(id)
	}
	err = store.env.DB().Model(&record.User{}).Where("id = ?", id).Select("quota").Find(&quota).Error
	if err != nil {
		return 0, err
	}

	return quota, nil
}

func (store Store) GetUserUsedQuota(id int) (quota int, err error) {
	err = store.env.DB().Model(&record.User{}).Where("id = ?", id).Select("used_quota").Find(&quota).Error
	return quota, err
}

// GetUserGroup gets group from Redis first, falls back to DB if needed
func (store Store) GetUserGroup(id int, fromDB bool) (group string, err error) {
	defer func() {
		// Update Redis cache asynchronously on successful store.env.DB() read
		if common.RedisEnabled && fromDB && err == nil {
			gopool.Go(func() {
				if err := store.env.Cache.RefreshUserGroupCache(id); err != nil {
					common.SysLog("failed to update user group cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		group, err := store.env.Cache.GetUserGroupCache(id)
		if err == nil {
			return group, nil
		}
		// Don't return error - fall through to store.env.DB()
	}
	fromDB = true
	err = store.env.DB().Model(&record.User{}).Where("id = ?", id).Select(store.env.GroupColumn()).Find(&group).Error
	if err != nil {
		return "", err
	}

	return group, nil
}

// GetUserSetting gets setting from Redis first, falls back to DB if needed
func (store Store) GetUserSetting(id int, fromDB bool) (settingMap dto.UserSetting, err error) {
	var setting string
	defer func() {
		// Update Redis cache asynchronously on successful store.env.DB() read
		if common.RedisEnabled && fromDB && err == nil {
			gopool.Go(func() {
				if err := store.env.Cache.UpdateUserSettingCache(id, setting); err != nil {
					common.SysLog("failed to update user setting cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		setting, err := store.env.Cache.GetUserSettingCache(id)
		if err == nil {
			return setting, nil
		}
		// Don't return error - fall through to store.env.DB()
	}
	fromDB = true
	// can be nil setting
	var safeSetting sql.NullString
	err = store.env.DB().Model(&record.User{}).Where("id = ?", id).Select("setting").Find(&safeSetting).Error
	if err != nil {
		return settingMap, err
	}
	if safeSetting.Valid {
		setting = safeSetting.String
	} else {
		setting = ""
	}
	userBase := &accountcache.UserBase{
		Setting: setting,
	}
	return userBase.GetSetting(), nil
}

func (store Store) IncreaseUserQuota(id int, quota int, db bool) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if err := common.ValidateWalletQuota(quota); err != nil {
		return err
	}
	if !db && common.BatchUpdateEnabled {
		counters.AddNewRecord(counters.BatchUpdateTypeUserQuota, id, quota)
		gopool.Go(func() {
			if err := store.env.Cache.CacheIncrUserQuota(id, int64(quota)); err != nil {
				common.SysLog("failed to increase user quota: " + err.Error())
			}
		})
		return nil
	}
	if err := store.increaseUserQuota(id, quota); err != nil {
		return err
	}
	gopool.Go(func() {
		if err := store.env.Cache.CacheIncrUserQuota(id, int64(quota)); err != nil {
			common.SysLog("failed to increase user quota: " + err.Error())
		}
	})
	return nil
}

func (store Store) increaseUserQuota(id int, quota int) (err error) {
	result := store.env.DB().Model(&record.User{}).
		Where("id = ? AND quota <= ?", id, common.MaxWalletQuota-quota).
		Update("quota", gorm.Expr("quota + ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := store.env.DB().Model(&record.User{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return domain.ErrQuotaLimitExceeded
}

func (store Store) DecreaseUserQuota(id int, quota int, db bool) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	gopool.Go(func() {
		err := store.env.Cache.CacheDecrUserQuota(id, int64(quota))
		if err != nil {
			common.SysLog("failed to decrease user quota: " + err.Error())
		}
	})
	if !db && common.BatchUpdateEnabled {
		counters.AddNewRecord(counters.BatchUpdateTypeUserQuota, id, -quota)
		return nil
	}
	return store.decreaseUserQuota(id, quota)
}

func (store Store) decreaseUserQuota(id int, quota int) (err error) {
	err = store.env.DB().Model(&record.User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota - ?", quota)).Error
	if err != nil {
		return err
	}
	return err
}

func (store Store) GetRootUser() (user *record.User) {
	store.env.DB().Where("role = ?", common.RoleRootUser).First(&user)
	return user
}

func (store Store) UpdateUserLastLoginAt(id int) {
	if err := store.env.DB().Model(&record.User{}).Where("id = ?", id).Update("last_login_at", common.GetTimestamp()).Error; err != nil {
		common.SysLog("failed to update user last_login_at: " + err.Error())
	}
}

func (store Store) UpdateUserUsedQuotaAndRequestCount(id int, quota int) {
	if common.BatchUpdateEnabled {
		counters.AddNewRecord(counters.BatchUpdateTypeUsedQuota, id, quota)
		counters.AddNewRecord(counters.BatchUpdateTypeRequestCount, id, 1)
		return
	}
	store.updateUserUsedQuotaAndRequestCount(id, quota, 1)
}

// UpdateUserUsedQuota adjusts accumulated usage without changing request count.
func (store Store) UpdateUserUsedQuota(id int, quota int) {
	if common.BatchUpdateEnabled {
		counters.AddNewRecord(counters.BatchUpdateTypeUsedQuota, id, quota)
		return
	}
	if err := store.env.DB().Model(&record.User{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
		common.SysLog("failed to update user used quota: " + err.Error())
	}
}

func (store Store) updateUserUsedQuotaAndRequestCount(id int, quota int, count int) {
	err := store.env.DB().Model(&record.User{}).Where("id = ?", id).Updates(
		map[string]any{
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"request_count": gorm.Expr("request_count + ?", count),
		},
	).Error
	if err != nil {
		common.SysLog("failed to update user used quota and request count: " + err.Error())
		return
	}

	//// 更新缓存
	//if err := invalidateUserCache(id); err != nil {
	//	common.SysError("failed to invalidate user cache: " + err.Error())
	//}
}

// GetUsernameById gets username from Redis first, falls back to DB if needed
func (store Store) GetUsernameById(id int, fromDB bool) (username string, err error) {
	defer func() {
		// Update Redis cache asynchronously on successful store.env.DB() read
		if common.RedisEnabled && fromDB && err == nil {
			gopool.Go(func() {
				if err := store.env.Cache.UpdateUserNameCache(id, username); err != nil {
					common.SysLog("failed to update user name cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		username, err := store.env.Cache.GetUserNameCache(id)
		if err == nil {
			return username, nil
		}
		// Don't return error - fall through to store.env.DB()
	}
	fromDB = true
	err = store.env.DB().Model(&record.User{}).Where("id = ?", id).Select("username").Find(&username).Error
	if err != nil {
		return "", err
	}

	return username, nil
}

func (store Store) IsLinuxDOIdAlreadyTaken(linuxDOId string) bool {
	var user record.User
	err := store.env.DB().Unscoped().Where("linux_do_id = ?", linuxDOId).First(&user).Error
	return !errors.Is(err, gorm.ErrRecordNotFound)
}

func (store Store) FillUserByLinuxDOId(user *record.User) error {
	if user.LinuxDOId == "" {
		return errors.New("linux do id is empty")
	}
	err := store.env.DB().Where("linux_do_id = ?", user.LinuxDOId).First(user).Error
	return err
}

func (store Store) FindInsertedUserID(username string) (int, error) {
	var user record.User
	err := store.env.DB().Where("username = ?", username).First(&user).Error
	return user.Id, err
}
