package model

import "gorm.io/gorm"

// KeyCache retains the host's shared atomic quota cache and hydration rules.
type KeyCache struct {
	Enabled    func() bool
	Get        func(string) (*Token, error)
	Initialize func(Token) (int, error)
}

type KeyReader struct {
	db        func() *gorm.DB
	keyColumn func() string
	lock      func(*gorm.DB) *gorm.DB
	cache     KeyCache
	log       func(string)
}

func NewKeyReader(db func() *gorm.DB, keyColumn func() string, lock func(*gorm.DB) *gorm.DB, cache KeyCache, log func(string)) KeyReader {
	return KeyReader{db: db, keyColumn: keyColumn, lock: lock, cache: cache, log: log}
}

func (reader KeyReader) Read(key string, fromDB bool) (token *Token, err error) {
	if !fromDB && reader.cache.Enabled() {
		token, err := reader.cache.Get(key)
		if err == nil {
			return token, nil
		}
		// A cache miss or failure falls through to the database.
	}
	token = &Token{}
	if !reader.cache.Enabled() {
		err = reader.db().Where(reader.keyColumn()+" = ?", key).First(token).Error
		if err != nil {
			return nil, err
		}
		return token, nil
	}
	err = reader.db().Transaction(func(tx *gorm.DB) error {
		if err := reader.lock(tx).Where(reader.keyColumn()+" = ?", key).First(token).Error; err != nil {
			return err
		}
		// Hydrate a cold cache from the locked snapshot; the host cache only refreshes
		// TTL for an existing hash so an atomic reservation cannot be overwritten.
		if _, cacheErr := reader.cache.Initialize(*token); cacheErr != nil {
			reader.log("failed to init token cache: " + cacheErr.Error())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return token, nil
}
