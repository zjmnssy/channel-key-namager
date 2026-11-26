package model

import (
	"gorm.io/gorm"
)

const (
	// 结算方式（payable_settlement_methods.method）：没有记录的提供商按渠道明细结算
	PayableSettlementChannel = "channel"
	PayableSettlementUsage   = "usage"
)

// PayableSettlementHook 只供测试：结算语句执行完、事务提交前调用，生产中为 nil。
var PayableSettlementHook func(siteID, billDate int)

// PayableSettlementMethod 是一个提供商的结算方式，每个提供商至多一行；没有记录的按渠道明细结算。
type PayableSettlementMethod struct {
	Id          int    `json:"id" gorm:"primaryKey"`
	ProviderId  int    `json:"provider_id" gorm:"not null;uniqueIndex:idx_payable_settlement_methods_provider"`
	Method      string `json:"method" gorm:"size:16;not null"`
	CreatedTime int64  `json:"created_time"`
	UpdatedTime int64  `json:"updated_time"`
}

// PayableUsageSettlement 是某站点某天按使用明细结算的记录，每个站点每天至多一行：该站点该天的每日账单写成成功时，
// 按此刻的规则、结算方式与提供商的应付折扣保存。bill_date 就是每日账单的北京日期 YYYYMMDD。
type PayableUsageSettlement struct {
	Id              int      `json:"id" gorm:"primaryKey"`
	SiteId          int      `json:"site_id" gorm:"not null;uniqueIndex:idx_payable_usage_settlements_site_date,priority:1"`
	BillDate        int      `json:"bill_date" gorm:"not null;uniqueIndex:idx_payable_usage_settlements_site_date,priority:2;index:idx_payable_usage_settlements_bill_date"`
	ProviderId      int      `json:"provider_id" gorm:"not null"`
	ProviderName    string   `json:"provider_name" gorm:"size:64"` // 用户名快照
	Quota           int64    `json:"quota" gorm:"not null"`        // 取自这次成功的每日账单
	QuotaPerUnit    int64    `json:"quota_per_unit"`
	PayableDiscount *float64 `json:"payable_discount"` // 提供商此刻的应付折扣快照，未设置为空
	CreatedTime     int64    `json:"created_time"`
	UpdatedTime     int64    `json:"updated_time"`
}

// payableWholeSite 是一个整站站点：未删除的站点上有一条有效的“全部渠道”规则。
type payableWholeSite struct {
	SiteID   int
	SiteName string
	Provider User // 规则的提供商，只有 id、username、payable_discount
	Conflict bool // 站点上有指定渠道规则把渠道指给了此刻是渠道供应商、但不是 Provider 的用户
}

// payableWholeSites 返回未删除站点上有效的“全部渠道”规则（remote_channel_id = 0、rule_type = all），siteIDs 为空时不筛选，按站点 ID 升序。
// 规则的提供商必须此刻是渠道供应商（角色 5、未软删除，含已禁用），与拉取时 ResolvePayableProviders 一致，不是的规则丢掉。
// 指给 Provider 自己的指定渠道规则不算冲突；指给已不是供应商的用户的，拉取时会被跳过，也不算冲突。
// 规则、站点名称、指定渠道规则、供应商各一次批量查询，不按站点逐个查。
func payableWholeSites(db *gorm.DB, siteIDs []int) ([]payableWholeSite, error) {
	wholeSites := []payableWholeSite{}
	query := db.Model(&PayableProviderOverride{}).Select("site_id", "provider_id").
		Where("remote_channel_id = 0 AND rule_type = ?", PayableRuleAll)
	if len(siteIDs) > 0 {
		query = query.Where("site_id IN ?", siteIDs)
	}
	var siteRules []PayableProviderOverride
	if err := query.Order("site_id").Find(&siteRules).Error; err != nil {
		return nil, err
	}
	if len(siteRules) == 0 {
		return wholeSites, nil
	}

	ruleSiteIDs := make([]int, 0, len(siteRules))
	siteProviders := make(map[int]int, len(siteRules))
	providerIDs := make([]int, 0, len(siteRules))
	for _, rule := range siteRules {
		ruleSiteIDs = append(ruleSiteIDs, rule.SiteId)
		siteProviders[rule.SiteId] = rule.ProviderId
		providerIDs = append(providerIDs, rule.ProviderId)
	}
	var sites []ThirdPartySite
	if err := db.Select("id", "name").Where("id IN ?", ruleSiteIDs).Find(&sites).Error; err != nil {
		return nil, err
	}
	siteNames := make(map[int]string, len(sites))
	for _, site := range sites {
		siteNames[site.Id] = site.Name
	}
	var channelRules []PayableProviderOverride
	if err := db.Select("site_id", "provider_id").Where("site_id IN ? AND remote_channel_id > 0", ruleSiteIDs).Find(&channelRules).Error; err != nil {
		return nil, err
	}
	for _, rule := range channelRules {
		providerIDs = append(providerIDs, rule.ProviderId)
	}
	suppliers, err := payableSuppliersIn(db, providerIDs)
	if err != nil {
		return nil, err
	}

	conflicts := map[int]bool{}
	for _, rule := range channelRules {
		if rule.ProviderId != siteProviders[rule.SiteId] && hasSupplier(suppliers, rule.ProviderId) {
			conflicts[rule.SiteId] = true
		}
	}
	for _, rule := range siteRules {
		name, live := siteNames[rule.SiteId]
		provider, valid := suppliers[rule.ProviderId]
		if !live || !valid {
			continue
		}
		wholeSites = append(wholeSites, payableWholeSite{SiteID: rule.SiteId, SiteName: name, Provider: provider, Conflict: conflicts[rule.SiteId]})
	}
	return wholeSites, nil
}

// payableSettlementMethods 返回 providerIDs 已保存的结算方式，以提供商 ID 为键；没有记录的不在结果里，调用方按渠道明细结算处理。
func payableSettlementMethods(db *gorm.DB, providerIDs []int) (map[int]string, error) {
	methods := map[int]string{}
	if len(providerIDs) == 0 {
		return methods, nil
	}
	var rows []PayableSettlementMethod
	if err := db.Select("provider_id", "method").Where("provider_id IN ?", providerIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		methods[row.ProviderId] = row.Method
	}
	return methods, nil
}

// settlePayableUsageDay 在把站点 siteID 在 billDate 的每日账单写成成功的事务 tx 里结算这一天（设计 5.2、5.3），quota、quotaPerUnit 取自这张日账单。
// 站点是整站站点、没有冲突、且规则的提供商按使用明细结算时，保存（新建或覆盖）这一天的记录，折扣取提供商此刻的应付折扣（可为空）；
// 否则删除这一天已有的记录（若有），这一天按渠道明细结算。
// 先普通查询出已有记录，再按主键更新或删除、没有时新建；不按 (site_id, bill_date) 直接 DELETE 或 upsert：MySQL 可重复读下，
// 前者对不存在的键留下间隙锁，后者对已存在的键留下临键锁，事务提交前会挡住键落在这个间隙里的其他站点同一天的结算。
func settlePayableUsageDay(tx *gorm.DB, siteID, billDate int, quota, quotaPerUnit, now int64) error {
	wholeSites, err := payableWholeSites(tx, []int{siteID})
	if err != nil {
		return err
	}
	var provider *User
	if len(wholeSites) == 1 && !wholeSites[0].Conflict {
		methods, err := payableSettlementMethods(tx, []int{wholeSites[0].Provider.Id})
		if err != nil {
			return err
		}
		if methods[wholeSites[0].Provider.Id] == PayableSettlementUsage {
			provider = &wholeSites[0].Provider
		}
	}

	var existing PayableUsageSettlement
	if err := tx.Select("id").Where("site_id = ? AND bill_date = ?", siteID, billDate).Limit(1).Find(&existing).Error; err != nil {
		return err
	}
	switch {
	case provider != nil && existing.Id != 0:
		err = tx.Model(&PayableUsageSettlement{}).Where("id = ?", existing.Id).Updates(map[string]any{
			"provider_id": provider.Id, "provider_name": provider.Username, "quota": quota, "quota_per_unit": quotaPerUnit,
			"payable_discount": provider.PayableDiscount, "updated_time": now,
		}).Error
	case provider != nil:
		err = tx.Create(&PayableUsageSettlement{SiteId: siteID, BillDate: billDate, ProviderId: provider.Id, ProviderName: provider.Username,
			Quota: quota, QuotaPerUnit: quotaPerUnit, PayableDiscount: provider.PayableDiscount, CreatedTime: now, UpdatedTime: now}).Error
	case existing.Id != 0:
		err = tx.Delete(&PayableUsageSettlement{}, existing.Id).Error
	}
	if err != nil {
		return err
	}
	if PayableSettlementHook != nil {
		PayableSettlementHook(siteID, billDate)
	}
	return nil
}
