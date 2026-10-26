package transport

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain/prefill"
	"github.com/gin-gonic/gin"
)

type PrefillGroup struct {
	Id          int            `json:"id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Items       RawPrefillJSON `json:"items"`
	Description string         `json:"description,omitempty"`
	CreatedTime int64          `json:"created_time"`
	UpdatedTime int64          `json:"updated_time"`
}

// RawPrefillJSON retains scalar, object, array and null JSON without copying through a codec round trip.
type RawPrefillJSON []byte

func (v RawPrefillJSON) MarshalJSON() ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return v, nil
}
func (v *RawPrefillJSON) UnmarshalJSON(raw []byte) error {
	if raw == nil {
		*v = nil
		return nil
	}
	*v = append((*v)[:0], raw...)
	return nil
}
func (v PrefillGroup) Value() prefill.Group {
	return prefill.Group{Id: v.Id, Name: v.Name, Type: v.Type, Items: []byte(v.Items), Description: v.Description, CreatedTime: v.CreatedTime, UpdatedTime: v.UpdatedTime}
}
func PrefillResponse(v prefill.Group) PrefillGroup {
	return PrefillGroup{Id: v.Id, Name: v.Name, Type: v.Type, Items: RawPrefillJSON(v.Items), Description: v.Description, CreatedTime: v.CreatedTime, UpdatedTime: v.UpdatedTime}
}
func PrefillError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, prefill.ErrNameAndType):
		common.ApiErrorMsg(c, "组名称和类型不能为空")
	case errors.Is(err, prefill.ErrDuplicate):
		common.ApiErrorMsg(c, "组名称已存在")
	case errors.Is(err, prefill.ErrID):
		common.ApiErrorMsg(c, "缺少组 ID")
	default:
		common.ApiError(c, err)
	}
}
