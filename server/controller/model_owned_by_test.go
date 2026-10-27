package controller

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelGroupSelectionAndPreferredOwner(t *testing.T) {
	previousMax := setting.GetMaxTokenAutoGroups()
	previousUsable := setting.UserUsableGroups2JSONString()
	previousRatios := ratio_setting.GroupRatio2JSONString()
	previousSelfUse, previousMemory := operation_setting.SelfUseModeEnabled, common.MemoryCacheEnabled
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("1"))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	operation_setting.SelfUseModeEnabled = true
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(fmt.Sprint(previousMax)))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsable))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
		operation_setting.SelfUseModeEnabled = previousSelfUse
		common.MemoryCacheEnabled = previousMemory
	})
	for _, tc := range []struct {
		name, token string
		auto        []string
		restrict    bool
		want        []string
	}{
		{name: "user group", want: []string{"owned-default"}},
		{name: "explicit token group", token: "vip", want: []string{"owned-vip"}},
		{name: "auto snapshot limit", token: "auto", auto: []string{"vip", "default"}, want: []string{"owned-vip"}},
		{name: "auto snapshot loses permission", token: "auto", auto: []string{"vip"}, restrict: true, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupModelListControllerTestDB(t)
			for i, group := range []string{"default", "vip"} {
				channel := model.Channel{Id: i + 1, Name: group, Type: constant.ChannelTypeOpenAI, Key: "fixture", Status: common.ChannelStatusEnabled, Group: group, Models: "owned-" + group}
				require.NoError(t, channelstore.NewChannelPersistence().Insert(&(channel)))
			}
			require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
			if tc.restrict {
				require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
			}
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyTokenGroup, tc.token)
			if tc.auto != nil {
				common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, tc.auto)
			}
			ListModels(ctx, constant.ChannelTypeOpenAI)
			var result listModelsResponse
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success)
			names := []string{}
			for _, v := range result.Data {
				names = append(names, v.Id)
				require.Equal(t, "openai", v.OwnedBy)
			}
			require.Equal(t, tc.want, names)
		})
	}
}
