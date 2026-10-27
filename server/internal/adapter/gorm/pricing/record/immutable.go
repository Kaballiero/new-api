package record

type CatalogSource struct {
	ID             string `gorm:"primaryKey;size:64"`
	Kind           string `gorm:"size:32;not null"`
	BaseURL        string `gorm:"type:text;not null"`
	Revision       string `gorm:"size:64;not null"`
	RemoteRevision string `gorm:"size:64;not null"`
	Generation     int64  `gorm:"not null"`
	CheckedAt      int64  `gorm:"not null"`
	FetchedAt      string `gorm:"size:64;not null"`
}
type CatalogRevision struct {
	SourceID       string `gorm:"primaryKey;size:64"`
	Hash           string `gorm:"primaryKey;size:64"`
	Manifest       string `gorm:"not null"`
	RemoteRevision string `gorm:"size:64;not null"`
	CreatedAt      int64  `gorm:"not null"`
}
type CatalogDocument struct {
	SourceID string `gorm:"primaryKey;size:64"`
	Hash     string `gorm:"primaryKey;size:64"`
	ModelID  string `gorm:"not null"`
	Payload  string `gorm:"not null"`
}
