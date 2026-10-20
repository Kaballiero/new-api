package model

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"gorm.io/gorm"
)

func accountQueries() record.Queries {
	return record.NewQueries(func() *gorm.DB { return DB }, func() string { return commonGroupCol })
}
func accountQueryUsers(rows []*record.User) []*User {
	if rows == nil {
		return nil
	}
	users := make([]*User, len(rows))
	for i, user := range rows {
		users[i] = (*User)(user)
	}
	return users
}
