package model

// Binary keys preserve exact identity, including case and trailing spaces, on
// every dialect regardless of the database's default text collation.
type GetAPIQuotaCredit struct {
	ID           int    `gorm:"primaryKey"`
	OperationKey []byte `gorm:"size:800;not null;uniqueIndex"`
	UserID       int    `gorm:"not null"`
	Quota        int    `gorm:"not null"`
	CreatedAt    int64  `gorm:"not null"`
}

func (GetAPIQuotaCredit) TableName() string { return "getapi_quota_credits" }
