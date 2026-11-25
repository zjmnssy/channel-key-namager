package controller

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func providerMappingSeedRule(t *testing.T, db *gorm.DB, siteID, channelID, providerID int, ruleType string) *model.PayableProviderOverride {
	t.Helper()
	rule := &model.PayableProviderOverride{SiteId: siteID, RemoteChannelId: channelID, ProviderId: providerID,
		RuleType: ruleType, CreatedTime: 1, UpdatedTime: 1}
	require.NoError(t, db.Create(rule).Error)
	return rule
}

func TestProviderMappingResolutionOrder(t *testing.T) {
	db := setupPayableBillTest(t)
	s1 := channelPushSeedSupplier(t, db, "pm-s1") // 渠道名解析出的供应商
	s2 := channelPushSeedSupplier(t, db, "pm-s2") // 上次的供应商（沿用）
	s3 := channelPushSeedSupplier(t, db, "pm-s3") // 站点级规则
	s4 := channelPushSeedSupplier(t, db, "pm-s4") // 指定渠道规则
	local := channelPushSeedChannel(t, db, s1, "pm-ch1", 1, "m", 1)
	site := receivableSeedSite(t, db, "pm-site", "https://pm.example.com", 1)
	providerMappingSeedRule(t, db, site.Id, 3, s4.Id, model.PayableRuleChannel)
	for channelID, source := range map[int]string{5: model.PayableProviderParsed, 6: model.PayableProviderRuleAll,
		7: model.PayableProviderRuleUnresolved, 8: model.PayableProviderManual} {
		provider := s3.Id
		if source == model.PayableProviderParsed {
			provider = s2.Id
		}
		require.NoError(t, db.Create(&model.PayableChannelUsage{TaskId: 900, PullSiteId: 900, BillDate: 20261121, SiteId: site.Id,
			RemoteChannelId: channelID, ProviderId: provider, ProviderSource: source}).Error)
	}
	channels := []model.PayableRemoteChannel{{ID: 1, LocalChannelID: local.Id}, {ID: 2}, {ID: 3}, {ID: 5}, {ID: 6}, {ID: 7}, {ID: 8}}
	resolve := func() map[int]string {
		providers, err := model.ResolvePayableProviders(site.Id, 20261122, channels)
		require.NoError(t, err)
		got := map[int]string{}
		for id, provider := range providers {
			got[id] = provider.Source + ":" + strconv.Itoa(provider.ID)
		}
		return got
	}
	of := func(source string, user *model.User) string { return source + ":" + strconv.Itoa(user.Id) }
	unknown := model.PayableProviderUnknown + ":0"

	// 没有站点级规则：指定渠道 > 解析 > 沿用；规则行与手动行不沿用
	plain := map[int]string{1: of("parsed", s1), 2: unknown, 3: of("manual", s4), 5: of("inherited", s2), 6: unknown, 7: unknown, 8: unknown}
	assert.Equal(t, plain, resolve())

	// 不能解析：只接住解析和沿用都没有结果的渠道
	rule := providerMappingSeedRule(t, db, site.Id, 0, s3.Id, model.PayableRuleUnresolved)
	fallback := of("rule_unresolved", s3)
	assert.Equal(t, map[int]string{1: of("parsed", s1), 2: fallback, 3: of("manual", s4), 5: of("inherited", s2), 6: fallback, 7: fallback, 8: fallback}, resolve())

	// 全部渠道：连能解析、能沿用的也归规则，指定渠道除外
	require.NoError(t, db.Model(rule).Update("rule_type", model.PayableRuleAll).Error)
	all := of("rule_all", s3)
	assert.Equal(t, map[int]string{1: all, 2: all, 3: of("manual", s4), 5: all, 6: all, 7: all, 8: all}, resolve())

	// 站点改地址后规则仍生效
	require.NoError(t, db.Model(site).Update("base_url", "https://changed.example.com").Error)
	assert.Equal(t, all, resolve()[1])

	// 规则的提供商不再是供应商：跳过这条规则，往下走；恢复后自动恢复
	require.NoError(t, db.Model(s3).Update("role", common.RoleCommonUser).Error)
	assert.Equal(t, plain, resolve())
	require.NoError(t, db.Model(s3).Update("role", common.RoleChannelSupplierUser).Error)
	assert.Equal(t, all, resolve()[2])

	// 指定渠道规则失效：按“全部渠道”走，不再占用该渠道
	require.NoError(t, db.Model(s4).Update("role", common.RoleCommonUser).Error)
	assert.Equal(t, all, resolve()[3])
	require.NoError(t, db.Model(s4).Update("role", common.RoleChannelSupplierUser).Error)

	// 删除站点级规则后回落，不从规则行沿用
	require.NoError(t, db.Delete(rule).Error)
	assert.Equal(t, plain, resolve())
}

func TestPayableRuleTypeOfOldRowsAndSetProvider(t *testing.T) {
	db := setupPayableBillTest(t)
	supplier := channelPushSeedSupplier(t, db, "pm-old")
	site := receivableSeedSite(t, db, "pm-old-site", "https://pm-old.example.com", 1)
	// 升级前的行 rule_type 为空
	old := &model.PayableProviderOverride{SiteId: site.Id, RemoteChannelId: 9, ProviderId: supplier.Id}
	require.NoError(t, db.Omit("rule_type").Create(old).Error)
	require.NoError(t, db.Exec("UPDATE payable_provider_overrides SET rule_type = NULL WHERE id = ?", old.Id).Error)
	var loaded model.PayableProviderOverride
	require.NoError(t, db.First(&loaded, old.Id).Error)
	assert.Equal(t, "", loaded.RuleType)
	assert.Equal(t, model.PayableRuleChannel, loaded.Type())

	task := payableSeedTask(t, db, 20261122, model.PayableTaskSuccess)
	pull := payableSeedPull(t, db, task, site.Id, model.PayablePullSuccess)
	usage := payableSeedUsage(t, db, pull, 9, 100, nil, 0, nil)
	_, _, err := model.SetPayableUsageProvider(usage.Id, supplier.Id, 50)
	require.NoError(t, err)
	require.NoError(t, db.First(&loaded, old.Id).Error)
	assert.Equal(t, model.PayableRuleChannel, loaded.RuleType)
}
