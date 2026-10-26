package prefill

import "errors"

type Group struct {
	Id          int    `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Items       []byte `json:"-"`
	Description string `json:"description,omitempty"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}

var ErrNameAndType = errors.New("prefill name and type are required")
var ErrDuplicate = errors.New("prefill name exists")
var ErrID = errors.New("prefill id is required")
