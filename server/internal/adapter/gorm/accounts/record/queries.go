package model

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	accountdomain "github.com/QuantumNous/new-api/internal/accounts/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var userSortColumns = map[string]string{
	"id":            "id",
	"username":      "username",
	"quota":         "quota",
	"group":         "group",
	"created_at":    "created_at",
	"last_login_at": "last_login_at",
}

type UserSortOptions struct {
	SortBy    string
	SortOrder string
}

func NewUserSortOptions(sortBy string, sortOrder string) UserSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := userSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = "id"
		normalizedSortOrder = "desc"
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}

	return UserSortOptions{
		SortBy:    normalizedSortBy,
		SortOrder: normalizedSortOrder,
	}
}

func (options UserSortOptions) Apply(query *gorm.DB) *gorm.DB {
	columnName, ok := userSortColumns[options.SortBy]
	if !ok {
		columnName = "id"
	}
	q := query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: columnName},
		Desc:   options.SortOrder != "asc",
	})
	if columnName != "id" {
		q = q.Order(clause.OrderByColumn{
			Column: clause.Column{Name: "id"},
			Desc:   true,
		})
	}
	return q
}

func resolveUserSortOptions(sortOptions []UserSortOptions) UserSortOptions {
	if len(sortOptions) == 0 {
		return NewUserSortOptions("", "")
	}
	return sortOptions[0]
}

// User if you add sensitive fields, don't forget to clean them in setupLogin function.
// Otherwise, the sensitive information will be saved on local storage in plain text!
type UserBatchRow struct {
	Id           int            `json:"id"`
	Role         int            `json:"-"`
	Status       int            `json:"status"`
	Quota        int            `json:"quota"`
	UsedQuota    int            `json:"used_quota"`
	RequestCount int            `json:"request_count"`
	Group        string         `json:"group"`
	DeletedAt    gorm.DeletedAt `json:"DeletedAt"`
}
type Queries struct {
	db          func() *gorm.DB
	groupColumn func() string
}

func NewQueries(db func() *gorm.DB, groupColumn func() string) Queries {
	return Queries{db: db, groupColumn: groupColumn}
}
func (queries Queries) GetMaxUserId() int {
	var user User
	queries.db().Unscoped().Last(&user)
	return user.Id
}
func (queries Queries) GetAllUsers(pageInfo *common.PageInfo, sortOptions ...UserSortOptions) (users []*User, total int64, err error) {
	// Start transaction
	tx := queries.db().Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get total count within transaction
	err = tx.Unscoped().Model(&User{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated users within same transaction
	order := resolveUserSortOptions(sortOptions)
	err = order.Apply(tx.Unscoped()).Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Omit("password", "access_token").Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}
func (queries Queries) SearchUsers(keyword string, group string, role *int, status *int, startIdx int, num int, sortOptions ...UserSortOptions) ([]*User, int64, error) {
	var users []*User
	var total int64
	var err error

	// Begin the transaction
	tx := queries.db().Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Base query
	query := tx.Unscoped().Model(&User{})

	// Search conditions
	likeCondition := "username LIKE ? OR email LIKE ? OR display_name LIKE ?"
	likeArgs := []any{"%" + keyword + "%", "%" + keyword + "%", "%" + keyword + "%"}

	// Try the keyword as an integer id
	keywordInt, err := strconv.Atoi(keyword)
	if err == nil {
		// A numeric keyword also matches the id
		likeCondition = "id = ? OR " + likeCondition
		likeArgs = append([]any{keywordInt}, likeArgs...)
	}

	query = query.Where("("+likeCondition+")", likeArgs...)
	if group != "" {
		query = query.Where(queries.groupColumn()+" = ?", group)
	}
	if role != nil {
		query = query.Where("role = ?", *role)
	}
	if status != nil {
		if *status == -1 {
			query = query.Where("deleted_at IS NOT NULL")
		} else {
			query = query.Where("deleted_at IS NULL").Where("status = ?", *status)
		}
	}

	// Total count
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Page of results
	order := resolveUserSortOptions(sortOptions)
	err = order.Apply(query.Omit("password", "access_token")).Limit(num).Offset(startIdx).Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit the transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}
func (queries Queries) GetUserById(id int, selectAll bool) (*User, error) {
	if id == 0 {
		return nil, accountdomain.ErrAccountIDRequired
	}
	user := User{Id: id}
	var err error = nil
	if selectAll {
		err = queries.db().First(&user, "id = ?", id).Error
	} else {
		err = queries.db().Omit("password", "access_token").First(&user, "id = ?", id).Error
	}
	return &user, err
}
func (queries Queries) GetUsersBatch(ids []int) ([]UserBatchRow, error) {
	var users []UserBatchRow
	err := queries.db().Model(&User{}).
		Select("id", "role", "status", "quota", "used_quota", "request_count", "group", "deleted_at").
		Where("id IN ?", ids).
		Find(&users).Error
	return users, err
}
func (queries Queries) GetSelfUserById(id int) (*User, error) {
	if id == 0 {
		return nil, accountdomain.ErrAccountIDRequired
	}
	var profile struct {
		User
		HasPassword bool `gorm:"column:has_password"`
	}
	err := queries.db().Model(&User{}).Select([]string{
		"id", "username", "display_name", "role", "status", "email",
		"github_id", "discord_id", "oidc_id", "wechat_id", "telegram_id",
		"group", "quota", "used_quota", "request_count", "aff_code", "aff_count",
		"aff_quota", "aff_history", "inviter_id", "linux_do_id", "setting",
		"stripe_customer", "auth_version",
		"CASE WHEN password <> '' THEN 1 ELSE 0 END AS has_password",
	}).First(&profile, "id = ?", id).Error
	profile.User.HasPassword = profile.HasPassword
	return &profile.User, err
}
func (queries Queries) GetUserIdByAffCode(affCode string) (int, error) {
	if affCode == "" {
		return 0, errors.New("affiliate code is empty")
	}
	var user User
	err := queries.db().Select("id").First(&user, "aff_code = ?", affCode).Error
	return user.Id, err
}
