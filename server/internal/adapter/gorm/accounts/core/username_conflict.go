package core

import (
	"errors"
	"strings"

	sqlitedriver "github.com/glebarez/go-sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

func IsUsernameUniqueViolation(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062 && strings.Contains(strings.ToLower(mysqlErr.Message), "username")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && (strings.Contains(strings.ToLower(pgErr.ConstraintName), "username") || strings.Contains(strings.ToLower(pgErr.Detail), "username"))
	}
	var sqliteErr *sqlitedriver.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code() == 2067 && strings.Contains(strings.ToLower(err.Error()), "username")
	}
	return false
}
