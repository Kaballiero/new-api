package availability

import "errors"

type Model struct {
	Id                     string
	Object                 string
	Created                int
	OwnedBy                string
	SupportedEndpointTypes []string
}

var ErrUserGroup = errors.New("get user group failed")
