package accountsadapter

import "github.com/QuantumNous/new-api/model"

type AccountModelGroupReader struct{}

func (AccountModelGroupReader) AccountGroup(userID int) (string, error) {
	user, err := model.GetUserCache(userID)
	if err != nil {
		return "", err
	}
	return user.Group, nil
}
