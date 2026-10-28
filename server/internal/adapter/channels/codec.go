package channels

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/update_channel"
)

// ChannelPatchCodec retains the wire snapshot and overlay semantics of channel edits.
type ChannelPatchCodec struct{ record.Codec }

func (ChannelPatchCodec) Snapshot(channel *domain.Channel) (domain.Channel, error) {
	data, err := common.Marshal(channel)
	if err != nil {
		return domain.Channel{}, err
	}
	var snapshot domain.Channel
	err = common.Unmarshal(data, &snapshot)
	return snapshot, err
}
func (ChannelPatchCodec) DecodePatch(raw []byte, patch *update_channel.PatchChannel) error {
	return common.Unmarshal(raw, patch)
}
