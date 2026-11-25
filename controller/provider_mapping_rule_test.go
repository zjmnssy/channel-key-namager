package controller

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
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

func providerMappingSeedUsage(t *testing.T, db *gorm.DB, pull *model.PayablePullSite, channelID int, providerID int, source string, discount *float64) *model.PayableChannelUsage {
	t.Helper()
	usage := &model.PayableChannelUsage{TaskId: pull.TaskId, PullSiteId: pull.Id, BillDate: pull.BillDate, SiteId: pull.SiteId,
		SiteName: "site-" + strconv.Itoa(pull.SiteId), RemoteChannelId: channelID, RemoteChannelName: "ch-" + strconv.Itoa(channelID),
		UsedQuota: 10, DeltaQuota: payablePtr(int64(1)), ProviderId: providerID, ProviderName: "p" + strconv.Itoa(providerID),
		ProviderSource: source, PayableDiscount: discount, SyncedAt: 5, CreatedTime: 5, UpdatedTime: 5}
	require.NoError(t, db.Create(usage).Error)
	return usage
}

// providerMappingUsages 按 id 读出全部明细，用于逐字段比较回改前后。
func providerMappingUsages(t *testing.T, db *gorm.DB) map[int]model.PayableChannelUsage {
	t.Helper()
	var rows []model.PayableChannelUsage
	require.NoError(t, db.Find(&rows).Error)
	usages := make(map[int]model.PayableChannelUsage, len(rows))
	for _, row := range rows {
		usages[row.Id] = row
	}
	return usages
}

// providerMappingApplied 是一行明细被规则回改后应有的样子：只改提供商、名称快照、来源、折扣快照（取提供商此刻的折扣）与更新时间，
// 用量、增量、站点与渠道名称快照、同步时间、创建时间都不变。
func providerMappingApplied(row model.PayableChannelUsage, provider *model.User, source string, now int64) model.PayableChannelUsage {
	row.ProviderId, row.ProviderName, row.ProviderSource = provider.Id, provider.Username, source
	row.PayableDiscount, row.UpdatedTime = provider.PayableDiscount, now
	return row
}

func TestProviderMappingApplyScope(t *testing.T) {
	const now = 1000
	scope := &model.PayableRuleApply{StartDate: 20261101, EndDate: 20261102}
	cases := []struct {
		name               string
		ruleType           string
		channel            int    // 指定渠道规则的站点渠道
		target             string // 规则的提供商：s1 或 s4
		apply              bool
		invalidateChannel7 bool  // 先把渠道 7 的指定渠道规则改为 s2，再把 s2 改成普通用户
		changed            []int // 站点 A 在 20261101、20261102 两天被改的渠道
		source             string
	}{
		{name: "all", ruleType: model.PayableRuleAll, target: "s4", apply: true, changed: []int{1, 2, 4, 5, 6}, source: model.PayableProviderRuleAll},
		{name: "unresolved", ruleType: model.PayableRuleUnresolved, target: "s4", apply: true, changed: []int{1, 4}, source: model.PayableProviderRuleUnresolved},
		{name: "channel", ruleType: model.PayableRuleChannel, channel: 3, target: "s4", apply: true, changed: []int{3}, source: model.PayableProviderManual},
		{name: "invalid channel rule", ruleType: model.PayableRuleAll, target: "s4", apply: true, invalidateChannel7: true,
			changed: []int{1, 2, 4, 5, 6, 7}, source: model.PayableProviderRuleAll},
		{name: "provider without discount", ruleType: model.PayableRuleAll, target: "s1", apply: true, changed: []int{1, 4, 5, 8}, source: model.PayableProviderRuleAll},
		{name: "no apply", ruleType: model.PayableRuleAll, target: "s4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupPayableBillTest(t)
			s1 := channelPushSeedSupplier(t, db, "pm-scope-s1")
			s2 := channelPushSeedSupplier(t, db, "pm-scope-s2")
			s4 := channelPushSeedSupplier(t, db, "pm-scope-s4")
			require.NoError(t, db.Model(s4).Update("payable_discount", 0.7).Error)
			s4.PayableDiscount = payablePtr(0.7)
			siteA := receivableSeedSite(t, db, "pm-scope-a", "https://pm-scope-a.example.com", 1)
			siteB := receivableSeedSite(t, db, "pm-scope-b", "https://pm-scope-b.example.com", 1)
			// 每个账单日期一个任务；站点 A 在 20261102 拉取失败：不计入账单的行也要回改
			task1 := payableSeedTask(t, db, 20261101, model.PayableTaskSuccess)
			task2 := payableSeedTask(t, db, 20261102, model.PayableTaskPartialFailed)
			task3 := payableSeedTask(t, db, 20261103, model.PayableTaskSuccess)
			pulls := []*model.PayablePullSite{
				payableSeedPull(t, db, task1, siteA.Id, model.PayablePullSuccess),
				payableSeedPull(t, db, task2, siteA.Id, model.PayablePullFailed),
				payableSeedPull(t, db, task3, siteA.Id, model.PayablePullSuccess),
				payableSeedPull(t, db, task1, siteB.Id, model.PayablePullSuccess),
				payableSeedPull(t, db, task2, siteB.Id, model.PayablePullSuccess),
			}
			rows := []struct {
				channel, provider int
				source            string
				discount          *float64
			}{
				{1, 0, model.PayableProviderUnknown, nil},
				{2, s1.Id, model.PayableProviderParsed, nil},
				{3, s2.Id, model.PayableProviderManual, nil},
				{4, s2.Id, model.PayableProviderRuleUnresolved, nil},
				{5, s2.Id, model.PayableProviderRuleAll, nil},
				{6, s1.Id, model.PayableProviderInherited, nil},
				{7, 0, model.PayableProviderUnknown, nil},                // 站点 A 有一条有效的指定渠道规则
				{8, s4.Id, model.PayableProviderParsed, payablePtr(0.5)}, // 提供商已是目标
			}
			for _, pull := range pulls {
				for _, row := range rows {
					providerMappingSeedUsage(t, db, pull, row.channel, row.provider, row.source, row.discount)
				}
			}
			channel7 := providerMappingSeedRule(t, db, siteA.Id, 7, s4.Id, model.PayableRuleChannel)
			if tc.invalidateChannel7 {
				require.NoError(t, db.Model(channel7).Update("provider_id", s2.Id).Error)
				require.NoError(t, db.Model(s2).Update("role", common.RoleCommonUser).Error)
			}
			target := map[string]*model.User{"s1": s1, "s4": s4}[tc.target]
			var want [][3]int // (账单日期, 站点, 渠道)
			for _, billDate := range []int{20261101, 20261102} {
				for _, channel := range tc.changed {
					want = append(want, [3]int{billDate, siteA.Id, channel})
				}
			}
			input := model.PayableRuleInput{SiteID: siteA.Id, RemoteChannelID: tc.channel, ProviderID: target.Id, RuleType: tc.ruleType}
			if tc.apply {
				input.Apply = scope
				// 预览与回改共用条件：预览的行数就是随后实际修改的行数
				count, err := model.CountPayableRuleApply(model.PayableProviderOverride{SiteId: siteA.Id, RemoteChannelId: tc.channel,
					ProviderId: target.Id, RuleType: tc.ruleType}, *scope)
				require.NoError(t, err)
				assert.EqualValues(t, len(want), count)
			}
			before := providerMappingUsages(t, db)

			_, applied, err := model.CreatePayableRule(input, now)
			require.NoError(t, err)
			assert.EqualValues(t, len(want), applied)
			var changed [][3]int
			for id, row := range providerMappingUsages(t, db) {
				expected := before[id]
				if !assert.ObjectsAreEqual(expected, row) {
					changed = append(changed, [3]int{row.BillDate, row.SiteId, row.RemoteChannelId})
					expected = providerMappingApplied(expected, target, tc.source, now)
				}
				assert.Equal(t, expected, row) // 改了的行只改提供商相关的五个字段，没改的行逐字段不变
			}
			assert.ElementsMatch(t, want, changed)
		})
	}
}

func TestProviderMappingCreateValidation(t *testing.T) {
	db := setupPayableBillTest(t)
	supplier := channelPushSeedSupplier(t, db, "pm-create-s")
	plain := &model.User{Username: "pm-create-u", Role: common.RoleCommonUser, Status: 1, AffCode: "pm-create-u"}
	require.NoError(t, db.Create(plain).Error)
	site := receivableSeedSite(t, db, "pm-create-a", "https://pm-create-a.example.com", 1)
	other := receivableSeedSite(t, db, "pm-create-b", "https://pm-create-b.example.com", 1)
	deleted := receivableSeedSite(t, db, "pm-create-gone", "https://pm-create-gone.example.com", 1)
	require.NoError(t, db.Delete(deleted).Error)
	task := payableSeedTask(t, db, 20261101, model.PayableTaskSuccess)
	pull := payableSeedPull(t, db, task, site.Id, model.PayablePullSuccess)
	providerMappingSeedUsage(t, db, pull, 1, 0, model.PayableProviderUnknown, nil)
	providerMappingSeedUsage(t, db, pull, 2, 0, model.PayableProviderUnknown, nil)
	// 渠道 9 只在另一个站点拉取过
	providerMappingSeedUsage(t, db, payableSeedPull(t, db, task, other.Id, model.PayablePullSuccess), 9, 0, model.PayableProviderUnknown, nil)

	// 站点级规则可以和任意多条指定渠道规则并存
	for _, input := range []model.PayableRuleInput{
		{SiteID: site.Id, RuleType: model.PayableRuleAll, ProviderID: supplier.Id},
		{SiteID: site.Id, RuleType: model.PayableRuleChannel, RemoteChannelID: 1, ProviderID: supplier.Id},
		{SiteID: site.Id, RuleType: model.PayableRuleChannel, RemoteChannelID: 2, ProviderID: supplier.Id},
	} {
		rule, applied, err := model.CreatePayableRule(input, 100)
		require.NoError(t, err)
		assert.Equal(t, model.PayableProviderOverride{Id: rule.Id, SiteId: site.Id, RemoteChannelId: input.RemoteChannelID,
			ProviderId: supplier.Id, RuleType: input.RuleType, CreatedTime: 100, UpdatedTime: 100}, rule)
		assert.Zero(t, applied)
	}

	var rulesBefore, rulesAfter int64
	require.NoError(t, db.Model(&model.PayableProviderOverride{}).Count(&rulesBefore).Error)
	usages := providerMappingUsages(t, db)
	scope := &model.PayableRuleApply{StartDate: 20261101, EndDate: 20261101}
	for _, tc := range []struct {
		name  string
		input model.PayableRuleInput
		want  error
	}{
		{"missing site", model.PayableRuleInput{SiteID: deleted.Id + 1, RuleType: model.PayableRuleAll, ProviderID: supplier.Id}, model.ErrThirdPartySiteNotFound},
		{"deleted site", model.PayableRuleInput{SiteID: deleted.Id, RuleType: model.PayableRuleAll, ProviderID: supplier.Id}, model.ErrThirdPartySiteNotFound},
		{"not a supplier", model.PayableRuleInput{SiteID: other.Id, RuleType: model.PayableRuleAll, ProviderID: plain.Id}, model.ErrPayableProviderInvalid},
		{"channel pulled only at another site", model.PayableRuleInput{SiteID: site.Id, RuleType: model.PayableRuleChannel,
			RemoteChannelID: 9, ProviderID: supplier.Id}, model.ErrPayableChannelNotPulled},
		{"unresolved beside all", model.PayableRuleInput{SiteID: site.Id, RuleType: model.PayableRuleUnresolved, ProviderID: supplier.Id},
			model.ErrPayableSiteRuleExists},
		{"second all", model.PayableRuleInput{SiteID: site.Id, RuleType: model.PayableRuleAll, ProviderID: supplier.Id}, model.ErrPayableSiteRuleExists},
		{"second channel rule", model.PayableRuleInput{SiteID: site.Id, RuleType: model.PayableRuleChannel, RemoteChannelID: 1,
			ProviderID: supplier.Id}, model.ErrPayableChannelRuleExists},
	} {
		tc.input.Apply = scope
		_, applied, err := model.CreatePayableRule(tc.input, 200)
		assert.ErrorIs(t, err, tc.want, tc.name)
		assert.Zero(t, applied, tc.name)
	}
	require.NoError(t, db.Model(&model.PayableProviderOverride{}).Count(&rulesAfter).Error)
	assert.Equal(t, rulesBefore, rulesAfter)
	assert.Equal(t, usages, providerMappingUsages(t, db)) // 被拒的新建不回改

	// 预览同样要求提供商是渠道供应商
	_, err := model.CountPayableRuleApply(model.PayableProviderOverride{SiteId: other.Id, RuleType: model.PayableRuleAll, ProviderId: plain.Id}, *scope)
	assert.ErrorIs(t, err, model.ErrPayableProviderInvalid)
}

func TestProviderMappingUpdateAndDelete(t *testing.T) {
	db := setupPayableBillTest(t)
	s1 := channelPushSeedSupplier(t, db, "pm-edit-s1")
	s2 := channelPushSeedSupplier(t, db, "pm-edit-s2")
	require.NoError(t, db.Model(s2).Update("payable_discount", 0.6).Error)
	s2.PayableDiscount = payablePtr(0.6)
	plain := &model.User{Username: "pm-edit-u", Role: common.RoleCommonUser, Status: 1, AffCode: "pm-edit-u"}
	require.NoError(t, db.Create(plain).Error)
	site := receivableSeedSite(t, db, "pm-edit-a", "https://pm-edit-a.example.com", 1)
	gone := receivableSeedSite(t, db, "pm-edit-gone", "https://pm-edit-gone.example.com", 1)
	task1 := payableSeedTask(t, db, 20261101, model.PayableTaskSuccess)
	task2 := payableSeedTask(t, db, 20261102, model.PayableTaskSuccess)
	manualRow := providerMappingSeedUsage(t, db, payableSeedPull(t, db, task1, site.Id, model.PayablePullSuccess), 5, s1.Id, model.PayableProviderManual, nil)
	unknownRow := providerMappingSeedUsage(t, db, payableSeedPull(t, db, task2, site.Id, model.PayablePullSuccess), 5, 0, model.PayableProviderUnknown, nil)
	goneRow := providerMappingSeedUsage(t, db, payableSeedPull(t, db, task1, gone.Id, model.PayablePullSuccess), 5, 0, model.PayableProviderUnknown, nil)
	// 升级前的指定渠道规则：rule_type 为 NULL
	old := providerMappingSeedRule(t, db, site.Id, 5, s1.Id, "")
	require.NoError(t, db.Model(old).Update("rule_type", nil).Error)
	siteRule := providerMappingSeedRule(t, db, gone.Id, 0, s1.Id, model.PayableRuleAll)
	require.NoError(t, db.Delete(gone).Error)
	stored := func(id int) model.PayableProviderOverride {
		var rule model.PayableProviderOverride
		require.NoError(t, db.First(&rule, id).Error)
		return rule
	}

	// 只改提供商：返回改前与改后，明细不变，旧行的类型仍是指定渠道
	usages := providerMappingUsages(t, db)
	before, after, applied, err := model.UpdatePayableRule(old.Id, s2.Id, nil, 200)
	require.NoError(t, err)
	assert.Equal(t, model.PayableProviderOverride{Id: old.Id, SiteId: site.Id, RemoteChannelId: 5, ProviderId: s1.Id, CreatedTime: 1, UpdatedTime: 1}, before)
	assert.Equal(t, model.PayableProviderOverride{Id: old.Id, SiteId: site.Id, RemoteChannelId: 5, ProviderId: s2.Id, CreatedTime: 1, UpdatedTime: 200}, after)
	assert.Equal(t, model.PayableRuleChannel, after.Type())
	assert.Equal(t, after, stored(old.Id))
	assert.Zero(t, applied)
	assert.Equal(t, usages, providerMappingUsages(t, db))

	// 提供商不变也能保存，只做一次回改：该渠道范围内的行都改为 manual，包括以前手动设置成别人的行
	scope := &model.PayableRuleApply{StartDate: 20261101, EndDate: 20261102}
	_, after, applied, err = model.UpdatePayableRule(old.Id, s2.Id, scope, 300)
	require.NoError(t, err)
	assert.Equal(t, s2.Id, after.ProviderId)
	assert.EqualValues(t, 2, applied)
	usages = providerMappingUsages(t, db)
	assert.Equal(t, providerMappingApplied(*manualRow, s2, model.PayableProviderManual, 300), usages[manualRow.Id])
	assert.Equal(t, providerMappingApplied(*unknownRow, s2, model.PayableProviderManual, 300), usages[unknownRow.Id])
	assert.Equal(t, *goneRow, usages[goneRow.Id])

	// 规则不存在；提供商不是供应商时规则与明细都不变
	_, _, _, err = model.UpdatePayableRule(siteRule.Id+1, s1.Id, nil, 400)
	assert.ErrorIs(t, err, model.ErrPayableRuleNotFound)
	_, _, _, err = model.UpdatePayableRule(old.Id, plain.Id, scope, 400)
	assert.ErrorIs(t, err, model.ErrPayableProviderInvalid)
	assert.Equal(t, after, stored(old.Id))
	assert.Equal(t, usages, providerMappingUsages(t, db))

	// 站点已删除的规则也能编辑（回改历史账单的归属）
	_, after, applied, err = model.UpdatePayableRule(siteRule.Id, s2.Id, scope, 500)
	require.NoError(t, err)
	assert.Equal(t, s2.Id, after.ProviderId)
	assert.EqualValues(t, 1, applied)
	assert.Equal(t, providerMappingApplied(*goneRow, s2, model.PayableProviderRuleAll, 500), providerMappingUsages(t, db)[goneRow.Id])

	// 删除（站点已删除的规则也能删）：返回被删的规则，明细不变；再删报不存在
	usages = providerMappingUsages(t, db)
	for _, rule := range []model.PayableProviderOverride{stored(old.Id), stored(siteRule.Id)} {
		removed, err := model.DeletePayableRule(rule.Id)
		require.NoError(t, err)
		assert.Equal(t, rule, removed)
		_, err = model.DeletePayableRule(rule.Id)
		assert.ErrorIs(t, err, model.ErrPayableRuleNotFound)
	}
	assert.Equal(t, usages, providerMappingUsages(t, db))
	var rules int64
	require.NoError(t, db.Model(&model.PayableProviderOverride{}).Count(&rules).Error)
	assert.Zero(t, rules)
}

func TestProviderMappingApplyInBatches(t *testing.T) {
	db := setupPayableBillTest(t)
	supplier := channelPushSeedSupplier(t, db, "pm-batch-s")
	site := receivableSeedSite(t, db, "pm-batch-c", "https://pm-batch-c.example.com", 1)
	pull := payableSeedPull(t, db, payableSeedTask(t, db, 20261101, model.PayableTaskSuccess), site.Id, model.PayablePullSuccess)
	rows := make([]*model.PayableChannelUsage, 2500) // 每批 1000 行，要更新 3 批
	for i := range rows {
		rows[i] = &model.PayableChannelUsage{TaskId: pull.TaskId, PullSiteId: pull.Id, BillDate: pull.BillDate, SiteId: site.Id,
			RemoteChannelId: i + 1, UsedQuota: 10, ProviderSource: model.PayableProviderUnknown}
	}
	require.NoError(t, db.CreateInBatches(rows, 500).Error)

	_, applied, err := model.CreatePayableRule(model.PayableRuleInput{SiteID: site.Id, RuleType: model.PayableRuleAll, ProviderID: supplier.Id,
		Apply: &model.PayableRuleApply{StartDate: 20261101, EndDate: 20261101}}, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 2500, applied)
	var changed int64
	require.NoError(t, db.Model(&model.PayableChannelUsage{}).Where("provider_id = ? AND provider_source = ? AND updated_time = ?",
		supplier.Id, model.PayableProviderRuleAll, 100).Count(&changed).Error)
	assert.EqualValues(t, 2500, changed)
}

func TestProviderMappingApplyIsAtomic(t *testing.T) {
	db := setupPayableBillTest(t)
	s1 := channelPushSeedSupplier(t, db, "pm-atomic-s1")
	s2 := channelPushSeedSupplier(t, db, "pm-atomic-s2")
	site := receivableSeedSite(t, db, "pm-atomic", "https://pm-atomic.example.com", 1)
	pull := payableSeedPull(t, db, payableSeedTask(t, db, 20261101, model.PayableTaskSuccess), site.Id, model.PayablePullSuccess)
	usage := providerMappingSeedUsage(t, db, pull, 1, 0, model.PayableProviderUnknown, nil)
	scope := &model.PayableRuleApply{StartDate: 20261101, EndDate: 20261101}
	// 回改明细时数据库报错：规则与明细要么都改，要么都不改
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:pm-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "payable_channel_usages" {
			tx.AddError(errors.New("boom"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove("test:pm-fail") })

	_, applied, err := model.CreatePayableRule(model.PayableRuleInput{SiteID: site.Id, RuleType: model.PayableRuleAll,
		ProviderID: s1.Id, Apply: scope}, 100)
	assert.ErrorContains(t, err, "boom")
	assert.Zero(t, applied)
	var rules int64
	require.NoError(t, db.Model(&model.PayableProviderOverride{}).Count(&rules).Error)
	assert.Zero(t, rules)

	// 编辑同样：回改失败时规则的提供商不变
	rule, _, err := model.CreatePayableRule(model.PayableRuleInput{SiteID: site.Id, RuleType: model.PayableRuleAll, ProviderID: s1.Id}, 100)
	require.NoError(t, err)
	_, _, applied, err = model.UpdatePayableRule(rule.Id, s2.Id, scope, 200)
	assert.ErrorContains(t, err, "boom")
	assert.Zero(t, applied)
	var stored model.PayableProviderOverride
	require.NoError(t, db.First(&stored, rule.Id).Error)
	assert.Equal(t, rule, stored)
	var after model.PayableChannelUsage
	require.NoError(t, db.First(&after, usage.Id).Error)
	assert.Equal(t, *usage, after)
}

func TestProviderMappingConcurrentCreate(t *testing.T) {
	db := setupPayableBillTest(t)
	supplier := channelPushSeedSupplier(t, db, "pm-race-s")
	site := receivableSeedSite(t, db, "pm-race", "https://pm-race.example.com", 1)
	input := model.PayableRuleInput{SiteID: site.Id, RuleType: model.PayableRuleAll, ProviderID: supplier.Id}
	results := make(chan error, 2)
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		// 夹具的 SQLite 连接不带 _txlock=immediate 与 busy_timeout：两个写事务重叠时，后升级写锁的一方直接得到 SQLITE_BUSY，
		// 不会排队等待。所以这里先后新建两次，第二次由插入前的查重挡下。
		for range 2 {
			_, _, err := model.CreatePayableRule(input, 100)
			results <- err
		}
	} else {
		// MySQL、PostgreSQL 上两个事务真并发：都查过没有已有规则、都要插入时才一起放行，
		// 后插入的一方必须被唯一约束挡下并得到同样的提示，而不是数据库错误或存成两条。
		var arrived atomic.Int32
		bothArrived := make(chan struct{})
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:pm-race", func(tx *gorm.DB) {
			if tx.Statement.Table != "payable_provider_overrides" {
				return
			}
			if arrived.Add(1) == 2 {
				close(bothArrived)
			}
			select {
			case <-bothArrived:
			case <-time.After(10 * time.Second): // 另一方没走到插入时不一直等，由下面的断言报错
			}
		}))
		t.Cleanup(func() { _ = db.Callback().Create().Remove("test:pm-race") })
		start := make(chan struct{})
		var workers sync.WaitGroup
		for range 2 {
			workers.Go(func() {
				<-start
				_, _, err := model.CreatePayableRule(input, 100)
				results <- err
			})
		}
		close(start)
		workers.Wait()
	}
	close(results)
	var failures []error
	for err := range results {
		if err != nil {
			failures = append(failures, err)
		}
	}
	require.Len(t, failures, 1)
	assert.ErrorIs(t, failures[0], model.ErrPayableSiteRuleExists)
	var rules int64
	require.NoError(t, db.Model(&model.PayableProviderOverride{}).Count(&rules).Error)
	assert.EqualValues(t, 1, rules)
}

func TestProviderMappingApplyAndPulls(t *testing.T) {
	db := setupPayableBillTest(t)
	target := channelPushSeedSupplier(t, db, "pm-pull-s")
	other := channelPushSeedSupplier(t, db, "pm-pull-o")
	siteA := receivableSeedSite(t, db, "pm-pull-a", "https://pm-pull-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pm-pull-b", "https://pm-pull-b.example.com", 1)
	task := payableSeedTask(t, db, 20261101, model.PayableTaskSuccess)
	later := payableSeedTask(t, db, 20261105, model.PayableTaskRunning)
	pullA := payableSeedPull(t, db, task, siteA.Id, model.PayablePullSuccess)
	pullB := payableSeedPull(t, db, task, siteB.Id, model.PayablePullSuccess)
	pullLater := payableSeedPull(t, db, later, siteA.Id, model.PayablePullPending)
	existing := providerMappingSeedUsage(t, db, pullA, 1, 0, model.PayableProviderUnknown, nil)
	scope := &model.PayableRuleApply{StartDate: 20261101, EndDate: 20261102}
	t.Cleanup(func() { model.PayableRuleApplyHook = nil })

	// 补扫：事务提交之后、补扫之前写入的行里，只有站点 A 在范围内的旧结果被补改；
	// 开始前已有的行（id 不大于开始时的最大 id）不归补扫管
	var late []*model.PayableChannelUsage
	model.PayableRuleApplyHook = func(stage string) {
		if stage != "after_commit" {
			return
		}
		late = []*model.PayableChannelUsage{
			providerMappingSeedUsage(t, db, pullA, 2, 0, model.PayableProviderUnknown, nil),
			providerMappingSeedUsage(t, db, pullA, 3, other.Id, model.PayableProviderManual, nil),
			providerMappingSeedUsage(t, db, pullB, 2, 0, model.PayableProviderUnknown, nil),
			providerMappingSeedUsage(t, db, pullLater, 2, 0, model.PayableProviderUnknown, nil),
		}
		require.NoError(t, db.Save(existing).Error) // 事务里已改过的行再写回旧结果
	}
	_, applied, err := model.CreatePayableRule(model.PayableRuleInput{SiteID: siteA.Id, RuleType: model.PayableRuleAll,
		ProviderID: target.Id, Apply: scope}, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 2, applied) // 事务里改的 1 行 + 补扫改的 1 行
	usages := providerMappingUsages(t, db)
	require.Len(t, late, 4)
	assert.Equal(t, providerMappingApplied(*late[0], target, model.PayableProviderRuleAll, 100), usages[late[0].Id])
	for _, row := range []*model.PayableChannelUsage{late[1], late[2], late[3], existing} {
		assert.Equal(t, *row, usages[row.Id])
	}

	// 锁：回改未提交时，其他站点的拉取照常写入（按主键分批更新，不像一条范围 UPDATE 那样在 MySQL 上留下间隙锁）
	t.Run("other sites write while the backfill is uncommitted", func(t *testing.T) {
		if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			t.Skip("SQLite 同时只有一个写事务，回改提交前其他连接的写入本来就要等")
		}
		timeout := "SET SESSION innodb_lock_wait_timeout = 3"
		if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
			timeout = "SET lock_timeout = '3s'"
		}
		siteC := receivableSeedSite(t, db, "pm-pull-c", "https://pm-pull-c.example.com", 1)
		providerMappingSeedUsage(t, db, payableSeedPull(t, db, task, siteC.Id, model.PayablePullSuccess), 1, 0, model.PayableProviderUnknown, nil)
		model.PayableRuleApplyHook = func(stage string) {
			if stage != "before_commit" {
				return
			}
			// 回改事务占着自己的连接，Connection 从连接池另取一条；被锁住时 3 秒后报错，而不是一直等
			err := model.DB.Connection(func(conn *gorm.DB) error {
				if err := conn.Exec(timeout).Error; err != nil {
					return err
				}
				return conn.Create(&model.PayableChannelUsage{TaskId: task.Id, PullSiteId: pullB.Id, BillDate: task.BillDate,
					SiteId: siteB.Id, RemoteChannelId: 9, UsedQuota: 10, ProviderSource: model.PayableProviderUnknown}).Error
			})
			assert.NoError(t, err)
		}
		_, applied, err := model.CreatePayableRule(model.PayableRuleInput{SiteID: siteC.Id, RuleType: model.PayableRuleAll,
			ProviderID: target.Id, Apply: scope}, 200)
		require.NoError(t, err)
		assert.EqualValues(t, 1, applied)
		var written int64
		require.NoError(t, db.Model(&model.PayableChannelUsage{}).Where("pull_site_id = ? AND remote_channel_id = ?", pullB.Id, 9).
			Count(&written).Error)
		assert.EqualValues(t, 1, written)
	})
}

// providerMappingSeedNamedUsage 写一行指定渠道名的明细。
func providerMappingSeedNamedUsage(t *testing.T, db *gorm.DB, pull *model.PayablePullSite, channelID int, name string) {
	t.Helper()
	usage := providerMappingSeedUsage(t, db, pull, channelID, 0, model.PayableProviderUnknown, nil)
	require.NoError(t, db.Model(usage).Update("remote_channel_name", name).Error)
}

func providerMappingRuleIDs(rows []model.PayableRuleRow) []int {
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	return ids
}

func TestProviderMappingRuleList(t *testing.T) {
	db := setupPayableBillTest(t)
	s1 := channelPushSeedSupplier(t, db, "pm-list-s1")
	s2 := channelPushSeedSupplier(t, db, "pm-list-s2")
	s3 := channelPushSeedSupplier(t, db, "pm-list-s3")
	s4 := channelPushSeedSupplier(t, db, "pm-list-s4 用户名")
	s5 := channelPushSeedSupplier(t, db, "pm-list-s5")
	siteA := receivableSeedSite(t, db, "pm-list-a", "https://pm-list-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pm-list-b", "https://pm-list-b.example.com", 1)
	require.NoError(t, db.Delete(siteB).Error)

	rule1 := providerMappingSeedRule(t, db, siteA.Id, 5, s1.Id, "") // 旧行：rule_type 为空
	rule2 := providerMappingSeedRule(t, db, siteA.Id, 0, s2.Id, model.PayableRuleUnresolved)
	rule3 := providerMappingSeedRule(t, db, siteB.Id, 0, s3.Id, model.PayableRuleAll)
	rule4 := providerMappingSeedRule(t, db, siteA.Id, 6, s4.Id, model.PayableRuleChannel)
	rule5 := providerMappingSeedRule(t, db, siteA.Id, 7, s5.Id, model.PayableRuleChannel)
	require.NoError(t, db.Model(s3).Update("role", common.RoleCommonUser).Error)
	require.NoError(t, db.Delete(s4).Error)
	require.NoError(t, db.Unscoped().Delete(s5).Error)

	// 渠道 5 前后两天名称不同，渠道 6、7 没有明细
	older := payableSeedPull(t, db, payableSeedTask(t, db, 20261030, model.PayableTaskSuccess), siteA.Id, model.PayablePullSuccess)
	newer := payableSeedPull(t, db, payableSeedTask(t, db, 20261101, model.PayableTaskSuccess), siteA.Id, model.PayablePullSuccess)
	providerMappingSeedNamedUsage(t, db, older, 5, "ch5-old")
	providerMappingSeedNamedUsage(t, db, newer, 5, "ch5-new")

	rows, total, err := model.ListPayableRules(model.PayableRuleFilter{}, 0, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 5, total)
	assert.Equal(t, []int{rule5.Id, rule4.Id, rule3.Id, rule2.Id, rule1.Id}, providerMappingRuleIDs(rows))
	byID := map[int]model.PayableRuleRow{}
	for _, row := range rows {
		byID[row.Id] = row
	}
	assert.Equal(t, model.PayableRuleRow{Id: rule1.Id, SiteId: siteA.Id, SiteName: siteA.Name, RuleType: model.PayableRuleChannel,
		RemoteChannelId: 5, RemoteChannelName: "ch5-new", ProviderId: s1.Id, ProviderName: s1.Username, ProviderValid: true,
		CreatedTime: 1, UpdatedTime: 1}, byID[rule1.Id])
	assert.Equal(t, model.PayableRuleUnresolved, byID[rule2.Id].RuleType)
	assert.True(t, byID[rule2.Id].ProviderValid)
	assert.Equal(t, model.PayableRuleAll, byID[rule3.Id].RuleType)
	assert.True(t, byID[rule3.Id].SiteDeleted)
	assert.Equal(t, siteB.Name, byID[rule3.Id].SiteName)
	assert.False(t, byID[rule3.Id].ProviderValid)
	assert.Equal(t, s3.Username, byID[rule3.Id].ProviderName)
	assert.False(t, byID[rule4.Id].SiteDeleted)
	assert.False(t, byID[rule4.Id].ProviderValid)
	assert.Equal(t, "pm-list-s4 用户名", byID[rule4.Id].ProviderName)
	assert.Empty(t, byID[rule4.Id].RemoteChannelName)
	assert.False(t, byID[rule5.Id].ProviderValid)
	assert.Equal(t, "#"+strconv.Itoa(s5.Id), byID[rule5.Id].ProviderName)

	// 筛选
	for _, tc := range []struct {
		name   string
		filter model.PayableRuleFilter
		ids    []int
	}{
		{"site", model.PayableRuleFilter{SiteIDs: []int{siteB.Id}}, []int{rule3.Id}},
		{"provider", model.PayableRuleFilter{ProviderIDs: []int{s1.Id, s2.Id}}, []int{rule2.Id, rule1.Id}},
		{"channel", model.PayableRuleFilter{Types: []string{model.PayableRuleChannel}}, []int{rule5.Id, rule4.Id, rule1.Id}},
		{"all", model.PayableRuleFilter{Types: []string{model.PayableRuleAll}}, []int{rule3.Id}},
		{"unresolved and all", model.PayableRuleFilter{Types: []string{model.PayableRuleUnresolved, model.PayableRuleAll}}, []int{rule3.Id, rule2.Id}},
		{"combined", model.PayableRuleFilter{SiteIDs: []int{siteA.Id}, Types: []string{model.PayableRuleChannel}, ProviderIDs: []int{s4.Id}}, []int{rule4.Id}},
	} {
		rows, total, err := model.ListPayableRules(tc.filter, 0, 0)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.ids, providerMappingRuleIDs(rows), tc.name)
		assert.EqualValues(t, len(tc.ids), total, tc.name)
	}

	// 分页：总数不受分页影响
	rows, total, err = model.ListPayableRules(model.PayableRuleFilter{}, 1, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 5, total)
	assert.Equal(t, []int{rule4.Id, rule3.Id}, providerMappingRuleIDs(rows))

	// 单条读取
	row, err := model.GetPayableRuleRow(rule1.Id)
	require.NoError(t, err)
	assert.Equal(t, byID[rule1.Id], row)
	_, err = model.GetPayableRuleRow(rule5.Id + 100)
	assert.ErrorIs(t, err, model.ErrPayableRuleNotFound)
}

func TestProviderMappingChannelSearch(t *testing.T) {
	db := setupPayableBillTest(t)
	supplier := channelPushSeedSupplier(t, db, "pm-search-s")
	siteA := receivableSeedSite(t, db, "pm-search-a", "https://pm-search-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pm-search-b", "https://pm-search-b.example.com", 1)
	siteC := receivableSeedSite(t, db, "pm-search-c", "https://pm-search-c.example.com", 1)
	siteD := receivableSeedSite(t, db, "pm-search-d", "https://pm-search-d.example.com", 1)
	task1030 := payableSeedTask(t, db, 20261030, model.PayableTaskSuccess)
	task1101 := payableSeedTask(t, db, 20261101, model.PayableTaskSuccess)
	task1102 := payableSeedTask(t, db, 20261102, model.PayableTaskSuccess)
	providerMappingSeedNamedUsage(t, db, payableSeedPull(t, db, task1030, siteA.Id, model.PayablePullSuccess), 99, "az-old")
	pull := payableSeedPull(t, db, task1101, siteA.Id, model.PayablePullSuccess)
	providerMappingSeedNamedUsage(t, db, pull, 10, "Az-GPT 甲")
	providerMappingSeedNamedUsage(t, db, pull, 11, "az_gpt%1")
	providerMappingSeedNamedUsage(t, db, pull, 12, "plain")
	providerMappingSeedNamedUsage(t, db, payableSeedPull(t, db, task1102, siteA.Id, model.PayablePullFailed), 13, "az-only-failed")
	providerMappingSeedNamedUsage(t, db, payableSeedPull(t, db, task1101, siteB.Id, model.PayablePullSuccess), 10, "other")
	// 站点 D 只有失败的拉取；站点 C 从未拉取
	providerMappingSeedNamedUsage(t, db, payableSeedPull(t, db, task1101, siteD.Id, model.PayablePullFailed), 30, "only-failed")
	providerMappingSeedRule(t, db, siteA.Id, 10, supplier.Id, model.PayableRuleChannel)
	providerMappingSeedRule(t, db, siteA.Id, 0, supplier.Id, model.PayableRuleAll)
	providerMappingSeedRule(t, db, siteB.Id, 12, supplier.Id, model.PayableRuleChannel)

	search := func(siteID int, keyword string, limit int) []int {
		options, err := model.SearchPayableChannels(siteID, keyword, limit)
		require.NoError(t, err, keyword)
		ids := make([]int, 0, len(options))
		for _, option := range options {
			ids = append(ids, option.RemoteChannelId)
		}
		return ids
	}
	for _, tc := range []struct {
		keyword string
		ids     []int
	}{
		{"AZ-gpt", []int{10}},
		{"az", []int{10, 11, 13}},
		{"99", []int{99}},
		{"10", []int{10}},
		{"%", []int{11}},
		{"_", []int{11}},
		{"'", nil},
		{"\"", nil},
		{";--", nil},
		{"甲", []int{10}},
		{"  az  ", []int{10, 11, 13}},
		{"", []int{10, 11, 12, 13}},
	} {
		assert.Equal(t, append([]int{}, tc.ids...), search(siteA.Id, tc.keyword, 20), "keyword %q", tc.keyword)
	}
	assert.Equal(t, []int{10, 11}, search(siteA.Id, "", 2))
	assert.Equal(t, []int{10}, search(siteB.Id, "", 20))
	assert.Equal(t, []int{30}, search(siteD.Id, "only", 20))
	assert.Empty(t, search(siteC.Id, "", 20))
	assert.Empty(t, search(siteC.Id, "az", 20))

	// 渠道 ID 精确命中排最前，再接名称匹配；精确命中的名称取最近一天
	providerMappingSeedNamedUsage(t, db, pull, 20, "has 12 inside")
	assert.Equal(t, []int{12, 20}, search(siteA.Id, "12", 20))
	options, err := model.SearchPayableChannels(siteA.Id, "99", 20)
	require.NoError(t, err)
	assert.Equal(t, []model.PayableChannelOption{{RemoteChannelId: 99, RemoteChannelName: "az-old"}}, options)

	// HasRule：指定渠道规则才算，站点级规则与其他站点的规则不影响
	options, err = model.SearchPayableChannels(siteA.Id, "", 20)
	require.NoError(t, err)
	hasRule := map[int]bool{}
	for _, option := range options {
		hasRule[option.RemoteChannelId] = option.HasRule
	}
	assert.Equal(t, map[int]bool{10: true, 11: false, 12: false, 13: false, 20: false}, hasRule)
}

var providerMappingActor = &model.User{Id: 1, Username: "root", Role: common.RoleRootUser}

// providerMappingCall 以超级管理员的控制台会话调用规则接口；id 不为空时作为路由参数 :id。
func providerMappingCall(t *testing.T, method, target, body, id string, handler gin.HandlerFunc) receivableReply {
	t.Helper()
	var params gin.Params
	if id != "" {
		params = gin.Params{{Key: "id", Value: id}}
	}
	return receivableReplyOf(t, channelSupplierRequest(t, method, target, body, providerMappingActor, params, handler))
}

// providerMappingJSON 把值经 JSON 往返成 map。期望的审计参数与读出的审计参数都这样处理，数字同为 float64，可以整体比较。
func providerMappingJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	return decoded
}

type providerMappingAudit struct {
	Action  string
	Success bool
	Content string
	Params  map[string]any
}

// providerMappingAudits 按写入顺序读出 provider_mapping.* 审计。
func providerMappingAudits(t *testing.T) []providerMappingAudit {
	t.Helper()
	var logs []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action LIKE ?", "provider_mapping.%").Order("id").Find(&logs).Error)
	audits := make([]providerMappingAudit, 0, len(logs))
	for _, log := range logs {
		require.NotNil(t, log.Other.Op)
		audits = append(audits, providerMappingAudit{Action: log.Action, Success: log.Success, Content: log.Content,
			Params: providerMappingJSON(t, log.Other.Op.Params)})
	}
	return audits
}

func TestProviderMappingRuleAPI(t *testing.T) {
	db := setupPayableBillTest(t)
	s1 := channelPushSeedSupplier(t, db, "pm-api-s1")
	s2 := channelPushSeedSupplier(t, db, "pm-api-s2")
	plain := &model.User{Username: "pm-api-u", Role: common.RoleCommonUser, Status: 1, AffCode: "pm-api-u"}
	require.NoError(t, db.Create(plain).Error)
	siteA := receivableSeedSite(t, db, "pm-api-a", "https://pm-api-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pm-api-b", "https://pm-api-b.example.com", 1)
	gone := receivableSeedSite(t, db, "pm-api-gone", "https://pm-api-gone.example.com", 1)
	require.NoError(t, db.Delete(gone).Error)
	task := payableSeedTask(t, db, 20261101, model.PayableTaskSuccess)
	pullA := payableSeedPull(t, db, task, siteA.Id, model.PayablePullSuccess)
	providerMappingSeedUsage(t, db, pullA, 1, 0, model.PayableProviderUnknown, nil)
	providerMappingSeedUsage(t, db, pullA, 2, 0, model.PayableProviderUnknown, nil)
	providerMappingSeedUsage(t, db, pullA, 3, s2.Id, model.PayableProviderParsed, nil)
	// 回改范围前一天的一行，回改不动它
	early := payableSeedPull(t, db, payableSeedTask(t, db, 20260831, model.PayableTaskSuccess), siteA.Id, model.PayablePullSuccess)
	providerMappingSeedUsage(t, db, early, 1, 0, model.PayableProviderUnknown, nil)
	// 站点 B 拉取过 25 个渠道，渠道搜索最多返回 20 个
	pullB := payableSeedPull(t, db, task, siteB.Id, model.PayablePullSuccess)
	for channel := 1; channel <= 25; channel++ {
		providerMappingSeedUsage(t, db, pullB, channel, 0, model.PayableProviderUnknown, nil)
	}

	const rules = "/api/provider_mapping_rule/"
	const start, end = 20260901, 20261201 // 首尾都算，恰好 92 天
	apply := map[string]any{"start_date": start, "end_date": end}
	body := func(fields map[string]any) string {
		encoded, err := common.Marshal(fields)
		require.NoError(t, err)
		return string(encoded)
	}
	type saveResult struct {
		Rule        model.PayableRuleRow `json:"rule"`
		AppliedRows int64                `json:"applied_rows"`
	}
	save := func(method, id string, fields map[string]any, handler gin.HandlerFunc) saveResult {
		reply := providerMappingCall(t, method, rules+id, body(fields), id, handler)
		require.True(t, reply.Success, reply.MessageKey)
		var result saveResult
		require.NoError(t, common.Unmarshal(reply.Data, &result))
		return result
	}
	previewTarget := func(siteID int, ruleType string, channelID, providerID, startDate, endDate int) string {
		return fmt.Sprintf(rules+"preview?site_id=%d&rule_type=%s&remote_channel_id=%d&provider_id=%d&start_date=%d&end_date=%d",
			siteID, ruleType, channelID, providerID, startDate, endDate)
	}
	previewRows := func(target string) int64 {
		reply := providerMappingCall(t, http.MethodGet, target, "", "", PreviewProviderMappingRule)
		require.True(t, reply.Success, reply.MessageKey)
		var result struct {
			Rows int64 `json:"rows"`
		}
		require.NoError(t, common.Unmarshal(reply.Data, &result))
		return result.Rows
	}
	ruleRow := func(id int) model.PayableRuleRow {
		row, err := model.GetPayableRuleRow(id)
		require.NoError(t, err)
		return row
	}
	// changedSince 返回与快照相比改动过的明细行数
	changedSince := func(before map[int]model.PayableChannelUsage) int64 {
		var changed int64
		for id, row := range providerMappingUsages(t, db) {
			if !assert.ObjectsAreEqual(before[id], row) {
				changed++
			}
		}
		return changed
	}
	seen := 0
	// newAudits 返回上次调用之后新写入的 provider_mapping.* 审计
	newAudits := func() []providerMappingAudit {
		audits := providerMappingAudits(t)
		added := audits[seen:]
		seen = len(audits)
		return added
	}

	// 1. 新建“全部渠道”并回改恰好 92 天：预览的行数就是随后实际修改的行数（范围外的行不动），审计记下新规则、名称与回改
	rows := previewRows(previewTarget(siteA.Id, model.PayableRuleAll, 0, s1.Id, start, end))
	assert.EqualValues(t, 3, rows)
	before := providerMappingUsages(t, db)
	result := save(http.MethodPost, "", map[string]any{"site_id": siteA.Id, "rule_type": "all", "provider_id": s1.Id, "apply": apply},
		CreateProviderMappingRule)
	allRule := result.Rule
	assert.Equal(t, model.PayableRuleAll, allRule.RuleType)
	assert.Equal(t, ruleRow(allRule.Id), allRule)
	assert.Equal(t, rows, result.AppliedRows)
	assert.Equal(t, rows, changedSince(before))
	allID := strconv.Itoa(allRule.Id)
	assert.Equal(t, []providerMappingAudit{{Action: "provider_mapping.create", Success: true, Content: "Created provider mapping rule #" + allID,
		Params: providerMappingJSON(t, map[string]any{"rule_id": allRule.Id, "site_id": siteA.Id, "site_name": siteA.Name, "rule_type": "all",
			"remote_channel_id": 0, "remote_channel_name": "", "provider_id": s1.Id, "provider_name": s1.Username,
			"apply_start_date": start, "apply_end_date": end, "applied_rows": 3})}}, newAudits())

	// 2. 新建“指定渠道”不回改：改动 0 行，审计没有回改的字段；另在站点 B 建一条“不能解析”
	before = providerMappingUsages(t, db)
	result = save(http.MethodPost, "", map[string]any{"site_id": siteA.Id, "rule_type": "channel", "remote_channel_id": 2, "provider_id": s2.Id},
		CreateProviderMappingRule)
	channelRule := result.Rule
	assert.Equal(t, model.PayableRuleChannel, channelRule.RuleType)
	assert.Equal(t, ruleRow(channelRule.Id), channelRule)
	assert.Zero(t, result.AppliedRows)
	assert.Zero(t, changedSince(before))
	channelID := strconv.Itoa(channelRule.Id)
	assert.Equal(t, []providerMappingAudit{{Action: "provider_mapping.create", Success: true, Content: "Created provider mapping rule #" + channelID,
		Params: providerMappingJSON(t, map[string]any{"rule_id": channelRule.Id, "site_id": siteA.Id, "site_name": siteA.Name, "rule_type": "channel",
			"remote_channel_id": 2, "remote_channel_name": "ch-2", "provider_id": s2.Id, "provider_name": s2.Username})}}, newAudits())
	unresolvedRule := save(http.MethodPost, "", map[string]any{"site_id": siteB.Id, "rule_type": "unresolved", "provider_id": s1.Id},
		CreateProviderMappingRule).Rule
	require.Len(t, newAudits(), 1)

	// 3. 业务错误：回对应的文案，各记一条失败审计。新建的审计记下请求的字段，编辑的审计还记下原规则
	missingID := unresolvedRule.Id + 100
	for _, tc := range []struct {
		name, method, id string
		fields           map[string]any
		handler          gin.HandlerFunc
		want             string
		params           map[string]any
	}{
		{"site rule exists", http.MethodPost, "", map[string]any{"site_id": siteA.Id, "rule_type": "unresolved", "provider_id": s2.Id, "apply": apply},
			CreateProviderMappingRule, "This site already has a site-level mapping rule",
			map[string]any{"site_id": siteA.Id, "rule_type": "unresolved", "remote_channel_id": 0, "provider_id": s2.Id,
				"apply_start_date": start, "apply_end_date": end, "failure_reason": "site_rule_exists"}},
		{"channel rule exists", http.MethodPost, "", map[string]any{"site_id": siteA.Id, "rule_type": "channel", "remote_channel_id": 2, "provider_id": s1.Id},
			CreateProviderMappingRule, "This site channel already has a mapping rule",
			map[string]any{"site_id": siteA.Id, "rule_type": "channel", "remote_channel_id": 2, "provider_id": s1.Id, "failure_reason": "channel_rule_exists"}},
		{"not a supplier", http.MethodPost, "", map[string]any{"site_id": siteA.Id, "rule_type": "channel", "remote_channel_id": 1, "provider_id": plain.Id},
			CreateProviderMappingRule, "The selected user is not a channel supplier",
			map[string]any{"site_id": siteA.Id, "rule_type": "channel", "remote_channel_id": 1, "provider_id": plain.Id, "failure_reason": "invalid_provider"}},
		{"site not found", http.MethodPost, "", map[string]any{"site_id": gone.Id, "rule_type": "all", "provider_id": s1.Id},
			CreateProviderMappingRule, "Site does not exist",
			map[string]any{"site_id": gone.Id, "rule_type": "all", "remote_channel_id": 0, "provider_id": s1.Id, "failure_reason": "site_not_found"}},
		{"channel not pulled", http.MethodPost, "", map[string]any{"site_id": siteA.Id, "rule_type": "channel", "remote_channel_id": 99, "provider_id": s1.Id},
			CreateProviderMappingRule, "This site channel has not been pulled yet",
			map[string]any{"site_id": siteA.Id, "rule_type": "channel", "remote_channel_id": 99, "provider_id": s1.Id, "failure_reason": "channel_not_pulled"}},
		{"update to a non-supplier", http.MethodPut, allID, map[string]any{"provider_id": plain.Id}, UpdateProviderMappingRule,
			"The selected user is not a channel supplier",
			map[string]any{"rule_id": allRule.Id, "site_id": siteA.Id, "site_name": siteA.Name, "rule_type": "all", "remote_channel_id": 0,
				"remote_channel_name": "", "provider_id": plain.Id, "previous_provider_id": s1.Id, "previous_provider_name": s1.Username,
				"failure_reason": "invalid_provider"}},
		{"update a missing rule", http.MethodPut, strconv.Itoa(missingID), map[string]any{"provider_id": s1.Id}, UpdateProviderMappingRule,
			"Mapping rule does not exist", map[string]any{"rule_id": missingID, "provider_id": s1.Id, "failure_reason": "rule_not_found"}},
	} {
		reply := providerMappingCall(t, tc.method, rules+tc.id, body(tc.fields), tc.id, tc.handler)
		assert.False(t, reply.Success, tc.name)
		assert.Equal(t, tc.want, reply.MessageKey, tc.name)
		action := map[string]string{http.MethodPost: "provider_mapping.create", http.MethodPut: "provider_mapping.update"}[tc.method]
		assert.Equal(t, []providerMappingAudit{{Action: action, Content: "Failed to change the provider mapping rule",
			Params: providerMappingJSON(t, tc.params)}}, newAudits(), tc.name)
	}

	// 4. 参数不合法：在登记审计之前返回（由兜底审计记录），不写 provider_mapping.* 审计，也不改数据。
	// 新建的请求体只有被测的一项不合法：漏过校验就会建出规则或写下失败审计
	var rulesBefore, rulesAfter []model.PayableProviderOverride
	require.NoError(t, db.Order("id").Find(&rulesBefore).Error)
	usagesBefore := providerMappingUsages(t, db)
	channelBody := func(changes map[string]any) string {
		fields := map[string]any{"site_id": siteB.Id, "rule_type": "channel", "remote_channel_id": 5, "provider_id": s1.Id}
		maps.Copy(fields, changes)
		return body(fields)
	}
	const invalid, tooLong = "Invalid parameters", "Date range cannot exceed 92 days"
	for _, tc := range []struct {
		name, method, target, body, id string
		handler                        gin.HandlerFunc
		want                           string
	}{
		{"create: empty body", http.MethodPost, rules, `{}`, "", CreateProviderMappingRule, invalid},
		{"create: not json", http.MethodPost, rules, `not json`, "", CreateProviderMappingRule, invalid},
		{"create: site 0", http.MethodPost, rules, channelBody(map[string]any{"site_id": 0}), "", CreateProviderMappingRule, invalid},
		{"create: unknown type", http.MethodPost, rules, channelBody(map[string]any{"rule_type": "x"}), "", CreateProviderMappingRule, invalid},
		{"create: channel rule without a channel", http.MethodPost, rules, channelBody(map[string]any{"remote_channel_id": 0}), "", CreateProviderMappingRule, invalid},
		{"create: site rule with a channel", http.MethodPost, rules, channelBody(map[string]any{"rule_type": "all"}), "", CreateProviderMappingRule, invalid},
		{"create: provider 0", http.MethodPost, rules, channelBody(map[string]any{"provider_id": 0}), "", CreateProviderMappingRule, invalid},
		{"create: not a date", http.MethodPost, rules, channelBody(map[string]any{"apply": map[string]any{"start_date": 20260230, "end_date": end}}),
			"", CreateProviderMappingRule, invalid},
		{"create: start after end", http.MethodPost, rules, channelBody(map[string]any{"apply": map[string]any{"start_date": 20261102, "end_date": 20261101}}),
			"", CreateProviderMappingRule, invalid},
		{"create: 93 days", http.MethodPost, rules, channelBody(map[string]any{"apply": map[string]any{"start_date": start, "end_date": 20261202}}),
			"", CreateProviderMappingRule, tooLong},
		{"update: invalid id", http.MethodPut, rules + "abc", body(map[string]any{"provider_id": s1.Id}), "abc", UpdateProviderMappingRule, "Invalid ID"},
		{"update: provider 0", http.MethodPut, rules + allID, `{}`, allID, UpdateProviderMappingRule, invalid},
		{"update: 93 days", http.MethodPut, rules + allID, body(map[string]any{"provider_id": s1.Id, "apply": map[string]any{"start_date": start, "end_date": 20261202}}),
			allID, UpdateProviderMappingRule, tooLong},
		{"delete: invalid id", http.MethodDelete, rules + "0", "", "0", DeleteProviderMappingRule, "Invalid ID"},
		{"preview: no parameters", http.MethodGet, rules + "preview", "", "", PreviewProviderMappingRule, invalid},
		{"preview: unknown type", http.MethodGet, previewTarget(siteA.Id, "x", 0, s1.Id, start, end), "", "", PreviewProviderMappingRule, invalid},
		{"preview: channel rule without a channel", http.MethodGet, previewTarget(siteA.Id, model.PayableRuleChannel, 0, s1.Id, start, end),
			"", "", PreviewProviderMappingRule, invalid},
		{"preview: site rule with a channel", http.MethodGet, previewTarget(siteA.Id, model.PayableRuleAll, 3, s1.Id, start, end),
			"", "", PreviewProviderMappingRule, invalid},
		{"preview: channel not a number", http.MethodGet, fmt.Sprintf(rules+"preview?site_id=%d&rule_type=all&remote_channel_id=abc&provider_id=%d&start_date=%d&end_date=%d",
			siteA.Id, s1.Id, start, end), "", "", PreviewProviderMappingRule, invalid},
		{"preview: start after end", http.MethodGet, previewTarget(siteA.Id, model.PayableRuleAll, 0, s1.Id, 20261102, 20261101),
			"", "", PreviewProviderMappingRule, invalid},
		{"preview: 93 days", http.MethodGet, previewTarget(siteA.Id, model.PayableRuleAll, 0, s1.Id, start, 20261202), "", "", PreviewProviderMappingRule, tooLong},
		{"channels: no site", http.MethodGet, rules + "channels?keyword=ch", "", "", SearchProviderMappingChannels, invalid},
		{"channels: site 0", http.MethodGet, rules + "channels?site_id=0", "", "", SearchProviderMappingChannels, invalid},
		{"list: unknown type", http.MethodGet, rules + "?types=channel,x", "", "", GetProviderMappingRules, invalid},
		{"list: invalid site", http.MethodGet, rules + "?site_ids=1,a", "", "", GetProviderMappingRules, invalid},
		{"list: invalid provider", http.MethodGet, rules + "?provider_ids=0", "", "", GetProviderMappingRules, invalid},
	} {
		reply := providerMappingCall(t, tc.method, tc.target, tc.body, tc.id, tc.handler)
		assert.False(t, reply.Success, tc.name)
		assert.Equal(t, tc.want, reply.MessageKey, tc.name)
	}
	assert.Empty(t, newAudits())
	require.NoError(t, db.Order("id").Find(&rulesAfter).Error)
	assert.Equal(t, rulesBefore, rulesAfter)
	assert.Equal(t, usagesBefore, providerMappingUsages(t, db))

	// 5. 编辑：改提供商并回改，审计记下原提供商与新提供商；预览（站点级规则可以不带 remote_channel_id）的行数就是实际修改的行数。
	// 渠道 2 有有效的指定渠道规则，站点级规则跳过它
	rows = previewRows(fmt.Sprintf(rules+"preview?site_id=%d&rule_type=all&provider_id=%d&start_date=%d&end_date=%d", siteA.Id, s2.Id, start, end))
	assert.EqualValues(t, 2, rows)
	before = providerMappingUsages(t, db)
	result = save(http.MethodPut, allID, map[string]any{"provider_id": s2.Id, "apply": apply}, UpdateProviderMappingRule)
	assert.Equal(t, ruleRow(allRule.Id), result.Rule)
	assert.Equal(t, s2.Id, result.Rule.ProviderId)
	assert.Equal(t, rows, result.AppliedRows)
	assert.Equal(t, rows, changedSince(before))
	assert.Equal(t, []providerMappingAudit{{Action: "provider_mapping.update", Success: true, Content: "Updated provider mapping rule #" + allID,
		Params: providerMappingJSON(t, map[string]any{"rule_id": allRule.Id, "site_id": siteA.Id, "site_name": siteA.Name, "rule_type": "all",
			"remote_channel_id": 0, "remote_channel_name": "", "provider_id": s2.Id, "provider_name": s2.Username,
			"previous_provider_id": s1.Id, "previous_provider_name": s1.Username, "apply_start_date": start, "apply_end_date": end,
			"applied_rows": 2})}}, newAudits())

	// 提供商不变也能保存，只做一次回改：渠道 2 那一行改为该规则的提供商
	result = save(http.MethodPut, channelID, map[string]any{"provider_id": s2.Id, "apply": apply}, UpdateProviderMappingRule)
	assert.EqualValues(t, 1, result.AppliedRows)
	audits := newAudits()
	require.Len(t, audits, 1)
	assert.True(t, audits[0].Success)
	assert.EqualValues(t, s2.Id, audits[0].Params["previous_provider_id"])
	assert.EqualValues(t, s2.Id, audits[0].Params["provider_id"])
	assert.EqualValues(t, 1, audits[0].Params["applied_rows"])

	// 6. 列表：按 ID 倒序分页；site_ids、types、provider_ids 都可多选
	type rulePage struct {
		Page     int                    `json:"page"`
		PageSize int                    `json:"page_size"`
		Total    int                    `json:"total"`
		Items    []model.PayableRuleRow `json:"items"`
	}
	list := func(query string) rulePage {
		reply := providerMappingCall(t, http.MethodGet, rules+"?"+query, "", "", GetProviderMappingRules)
		require.True(t, reply.Success, reply.MessageKey)
		var page rulePage
		require.NoError(t, common.Unmarshal(reply.Data, &page))
		return page
	}
	assert.Equal(t, rulePage{Page: 2, PageSize: 1, Total: 3, Items: []model.PayableRuleRow{ruleRow(channelRule.Id)}}, list("p=2&page_size=1"))
	for _, tc := range []struct {
		query string
		ids   []int
	}{
		{"", []int{unresolvedRule.Id, channelRule.Id, allRule.Id}},
		{"types=channel,all", []int{channelRule.Id, allRule.Id}},
		{fmt.Sprintf("site_ids=%d,%d", siteB.Id, gone.Id), []int{unresolvedRule.Id}},
		{fmt.Sprintf("site_ids=%d,%d&types=unresolved,channel&provider_ids=%d,%d", siteA.Id, siteB.Id, s1.Id, s2.Id),
			[]int{unresolvedRule.Id, channelRule.Id}},
	} {
		page := list(tc.query)
		assert.Equal(t, tc.ids, providerMappingRuleIDs(page.Items), tc.query)
		assert.Equal(t, len(tc.ids), page.Total, tc.query)
	}

	// 7. 渠道搜索：按关键字在站点拉取过的渠道里搜，最多返回 20 个，标出已有规则的渠道
	searchChannels := func(query string) []model.PayableChannelOption {
		reply := providerMappingCall(t, http.MethodGet, rules+"channels?"+query, "", "", SearchProviderMappingChannels)
		require.True(t, reply.Success, reply.MessageKey)
		var options []model.PayableChannelOption
		require.NoError(t, common.Unmarshal(reply.Data, &options))
		return options
	}
	options := searchChannels("site_id=" + strconv.Itoa(siteB.Id))
	require.Len(t, options, 20)
	assert.Equal(t, model.PayableChannelOption{RemoteChannelId: 20, RemoteChannelName: "ch-20"}, options[19])
	assert.Equal(t, []model.PayableChannelOption{{RemoteChannelId: 2, RemoteChannelName: "ch-2", HasRule: true}},
		searchChannels(fmt.Sprintf("site_id=%d&keyword=ch-2", siteA.Id)))

	// 8. 删除：data 为 null，审计记下原提供商，明细不变；再删报规则不存在
	usagesBefore = providerMappingUsages(t, db)
	reply := providerMappingCall(t, http.MethodDelete, rules+channelID, "", channelID, DeleteProviderMappingRule)
	require.True(t, reply.Success, reply.MessageKey)
	assert.Equal(t, "null", string(reply.Data))
	assert.Equal(t, []providerMappingAudit{{Action: "provider_mapping.delete", Success: true, Content: "Deleted provider mapping rule #" + channelID,
		Params: providerMappingJSON(t, map[string]any{"rule_id": channelRule.Id, "site_id": siteA.Id, "site_name": siteA.Name, "rule_type": "channel",
			"remote_channel_id": 2, "remote_channel_name": "ch-2", "previous_provider_id": s2.Id, "previous_provider_name": s2.Username})}}, newAudits())
	_, err := model.GetPayableRuleRow(channelRule.Id)
	assert.ErrorIs(t, err, model.ErrPayableRuleNotFound)
	assert.Equal(t, usagesBefore, providerMappingUsages(t, db))
	reply = providerMappingCall(t, http.MethodDelete, rules+channelID, "", channelID, DeleteProviderMappingRule)
	assert.False(t, reply.Success)
	assert.Equal(t, "Mapping rule does not exist", reply.MessageKey)
	assert.Equal(t, []providerMappingAudit{{Action: "provider_mapping.delete", Content: "Failed to change the provider mapping rule",
		Params: providerMappingJSON(t, map[string]any{"rule_id": channelRule.Id, "failure_reason": "rule_not_found"})}}, newAudits())

	// 9. 预览同样要求提供商是渠道供应商
	reply = providerMappingCall(t, http.MethodGet, previewTarget(siteA.Id, model.PayableRuleAll, 0, plain.Id, start, end), "", "",
		PreviewProviderMappingRule)
	assert.False(t, reply.Success)
	assert.Equal(t, "The selected user is not a channel supplier", reply.MessageKey)

	// 10. 其他错误按数据库错误回复，审计记为 database_error，规则不变
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:pm-api-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "payable_provider_overrides" {
			tx.AddError(errors.New("boom"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove("test:pm-api-fail") })
	reply = providerMappingCall(t, http.MethodPut, rules+allID, body(map[string]any{"provider_id": s1.Id}), allID, UpdateProviderMappingRule)
	assert.False(t, reply.Success)
	audits = newAudits()
	require.Len(t, audits, 1)
	assert.False(t, audits[0].Success)
	assert.Equal(t, "database_error", audits[0].Params["failure_reason"])
	assert.Equal(t, s2.Id, ruleRow(allRule.Id).ProviderId)
}
