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

// payableUsageSettled 是“按使用明细结算的站点天”（设计 5.4）：使用明细结算记录 s 连上同站点同日期、当前状态为成功的每日账单 b。
// 每日账单当前不是成功（重新同步后在重试中或失败）的记录不算，这一天回到渠道明细结算。不带 Select，由调用方选列；
// 提供商账单的两部分、渠道消费明细的“不计入”标记与导出都从这里判断，保证口径一致。
func payableUsageSettled() *gorm.DB {
	return DB.Table("payable_usage_settlements AS s").
		Joins("JOIN receivable_bills AS b ON b.site_id = s.site_id AND b.bill_date = s.bill_date AND b.status = ?", ReceivableBillStatusSuccess)
}

// payableUsageSettledDays 返回 siteIDs 与 dates 组合中按使用明细结算的站点天，以 [站点 ID, 账单日期] 为键。
// 调用方传入去重后的站点和日期，绑定参数个数不随明细行数增长。
func payableUsageSettledDays(siteIDs, dates []int) (map[[2]int]bool, error) {
	var days []struct {
		SiteId   int
		BillDate int
	}
	err := payableUsageSettled().Select("s.site_id, s.bill_date").Where("s.site_id IN ? AND s.bill_date IN ?", siteIDs, dates).Scan(&days).Error
	if err != nil {
		return nil, err
	}
	settled := make(map[[2]int]bool, len(days))
	for _, day := range days {
		settled[[2]int{day.SiteId, day.BillDate}] = true
	}
	return settled, nil
}

// CountPayableUnsyncedUsageSites 返回设计 5.5 的 N：筛选日期、筛选站点内，按此刻的规则与结算方式应按使用明细结算（整站站点、没有冲突、
// 规则的提供商按使用明细结算），但每日账单存在且不是成功（重试中或失败）的站点天数，这些站点天暂按渠道明细结算。
// 每日账单行还没有生成的日期、已删除的站点不计；与 CountPayablePullGaps 一样不看提供商筛选。
func CountPayableUnsyncedUsageSites(filter PayableUsageFilter) (int64, error) {
	wholeSites, err := payableWholeSites(DB, filter.SiteIDs)
	if err != nil {
		return 0, err
	}
	providerIDs := make([]int, 0, len(wholeSites))
	for _, site := range wholeSites {
		providerIDs = append(providerIDs, site.Provider.Id)
	}
	methods, err := payableSettlementMethods(DB, providerIDs)
	if err != nil {
		return 0, err
	}
	siteIDs := make([]int, 0, len(wholeSites))
	for _, site := range wholeSites {
		if !site.Conflict && methods[site.Provider.Id] == PayableSettlementUsage {
			siteIDs = append(siteIDs, site.SiteID)
		}
	}
	if len(siteIDs) == 0 {
		return 0, nil
	}
	var count int64
	err = DB.Model(&ReceivableBill{}).
		Where("site_id IN ? AND bill_date >= ? AND bill_date <= ? AND status <> ?", siteIDs, filter.StartDate, filter.EndDate, ReceivableBillStatusSuccess).
		Count(&count).Error
	return count, err
}
