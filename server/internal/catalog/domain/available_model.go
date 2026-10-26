package domain

import "errors"

type AvailableModel struct {
	Id                     string
	Object                 string
	Created                int
	OwnedBy                string
	SupportedEndpointTypes []string
}

var ErrUserGroup = errors.New("get user group failed")
