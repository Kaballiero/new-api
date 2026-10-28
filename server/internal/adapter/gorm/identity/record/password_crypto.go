package model

// LoginEncryptionKey stores internal key material used by the browser login
// protocol. It is deliberately separate from administrator-facing options.
type LoginEncryptionKey struct {
	ID            uint   `json:"-" gorm:"primaryKey"`
	Slot          string `json:"-" gorm:"type:varchar(32);not null;uniqueIndex"`
	PrivateKeyPEM string `json:"-" gorm:"type:text;not null"`
}
