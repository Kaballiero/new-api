package accountsadapter

import accountcache "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"

type AccountModelGroupReader struct{ Cache accountcache.Store }

func (store AccountModelGroupReader) AccountGroup(userID int) (string, error) {
	user, err := store.Cache.GetUserCache(userID)
	if err != nil {
		return "", err
	}
	return user.Group, nil
}
