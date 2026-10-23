package domain

import (
	"errors"
	"strings"
)

var (
	ErrPagination = errors.New("invalid audit pagination")
	ErrFilters    = errors.New("invalid audit filters")
	ErrTimeRange  = errors.New("invalid audit time range")
	ErrResult     = errors.New("invalid audit result")
)

type Query struct {
	UserId                                                   int
	Username, Category, TokenRef, ExcludeTokenRef, RequestId string
	StartTimestamp, EndTimestamp                             int64
	Success                                                  *bool
	Offset, Limit                                            int
}

type VisibilityPolicy struct {
	ActorRoles               []int
	HideRoot, HidePrivileged bool
}

func EventVisibility(viewer int, roles Roles, self bool) VisibilityPolicy {
	policy := VisibilityPolicy{}
	if viewer < roles.Root {
		policy.ActorRoles = []int{roles.Common, roles.Admin}
	}
	policy.HideRoot = self || viewer < roles.Root
	policy.HidePrivileged = self || viewer < roles.Admin
	return policy
}

func ValidatePagination(page, size int) error {
	if page < 1 || size < 1 || page > 100000000 {
		return ErrPagination
	}
	return nil
}

func ValidateFilters(query Query) error {
	category := query.Category
	if category != "" && category != AuditCategoryLogin && category != AuditCategorySecurity &&
		category != AuditCategoryOperation && category != AuditCategoryAccessToken {
		return ErrFilters
	}
	for _, value := range []string{query.TokenRef, query.ExcludeTokenRef} {
		if value != "" && (len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "") {
			return ErrFilters
		}
	}
	return nil
}
