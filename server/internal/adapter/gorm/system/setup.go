package system

import "github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"

func (s Store) GetSetup() *record.Setup {
	var setup record.Setup
	if s.DB().First(&setup).Error != nil {
		return nil
	}
	return &setup
}
func (s Store) CreateSetup(version string, at int64) error {
	return s.DB().Create(&record.Setup{Version: version, InitializedAt: at}).Error
}
func (s Store) SetupTime() *int64 {
	v := s.GetSetup()
	if v == nil {
		return nil
	}
	return &v.InitializedAt
}
