package model

import (
	"cmp"
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// PayableUsageFilter 是明细、提供商账单和未完成进度计数共用的筛选条件，日期为 YYYYMMDD。
type PayableUsageFilter struct {
	StartDate, EndDate int
	SiteIDs            []int // 空 = 不筛选
	ProviderIDs        []int // 空 = 不筛选；0 表示"未知提供商"
	// MergeMethods 只用于提供商账单：同一天同一提供商的两种结算方式合成一行，行上没有结算方式（渠道供应商查看本人账单时）。
	MergeMethods bool
}

// PayablePullTaskSummary 是任务加上进度和明细汇总，增量和累计和只统计完整拉取的行。
type PayablePullTaskSummary struct {
	PayablePullTask
	FinishedSites int   `json:"finished_sites"`
	RetryingSites int   `json:"retrying_sites"`
	FailedSites   int   `json:"failed_sites"`   // 含 superseded
	MismatchSites int   `json:"mismatch_sites"` // 成功但写入的渠道数少于站点报的总数（补扫后仍对不上）
	ChannelTotal  int   `json:"channel_total"`
	DeltaQuota    int64 `json:"-"`
	UsedQuota     int64 `json:"-"`
}

// PayableUsageRow 是一行明细；Counted 表示所属进度已成功（完整拉取），Manual 表示该站点渠道有手动指定，
// UsageSettled 表示该站点这一天按使用明细结算（payableUsageSettled），这一行不计入提供商账单。
type PayableUsageRow struct {
	PayableChannelUsage
	Counted      bool `json:"counted" gorm:"-"`
	Manual       bool `json:"manual" gorm:"-"`
	UsageSettled bool `json:"usage_settled" gorm:"-"`
}

// PayableProviderBill 是某天某提供商一种结算方式的账单汇总，同一天同一提供商两种结算方式都有时分两行：
// 渠道明细结算的行汇总明细（额度是增量），使用明细结算的行汇总使用明细结算记录。按 MergeMethods 查询时两种合成一行。
// 折扣的统计只看额度非 0 的明细或记录：额度为 0 的不计金额，折扣也无所谓。
type PayableProviderBill struct {
	BillDate        int
	ProviderId      int
	Method          string // 结算方式：PayableSettlementChannel 或 PayableSettlementUsage；合并结算方式的行为空
	ProviderName    string // 组内 id 最大的一行明细（一条记录）的快照
	DeltaQuota      int64  // 组内的额度之和
	QuotaPerUnit    int64
	Discount        *float64         // 组内额度非 0 的行折扣全部相同且非空时有值
	DiscountMixed   bool             // 组内额度非 0 的行出现 >= 2 种非空折扣
	DiscountMissing bool             // 组内有额度非 0 而折扣为空的行
	PayableAmount   *decimal.Decimal // 有折扣行的 金额×折扣 之和；额度全为 0 时为 0；额度非 0 的行都没有折扣时 nil
}

// PayableProviderOption 是提供商筛选和手动指定里的一个渠道供应商。
type PayableProviderOption struct {
	Id              int      `json:"id"`
	Username        string   `json:"username"`
	Disabled        bool     `json:"disabled"`
	PayableDiscount *float64 `json:"payable_discount"`
}

// payableCompleteUsages 返回完整拉取（所属进度为 success）、且不按使用明细结算的明细行，并按 filter 筛选。
// 按使用明细结算的站点天（payableUsageSettled）的明细不论提供商是谁，一律不计入（设计 5.4）。
func payableCompleteUsages(filter PayableUsageFilter) *gorm.DB {
	query := DB.Model(&PayableChannelUsage{}).
		Where("bill_date >= ? AND bill_date <= ?", filter.StartDate, filter.EndDate).
		Where("pull_site_id IN (?)", DB.Model(&PayablePullSite{}).Select("id").Where("status = ?", PayablePullSuccess)).
		Where("NOT EXISTS (?)", payableUsageSettled().Select("1").
			Where("s.site_id = payable_channel_usages.site_id AND s.bill_date = payable_channel_usages.bill_date"))
	if len(filter.SiteIDs) > 0 {
		query = query.Where("site_id IN ?", filter.SiteIDs)
	}
	if len(filter.ProviderIDs) > 0 {
		query = query.Where("provider_id IN ?", filter.ProviderIDs)
	}
	return query
}

// ListPayablePullTasks 按 id 倒序分页返回任务及其汇总，同时返回任务总数。
func ListPayablePullTasks(offset, limit int) ([]PayablePullTaskSummary, int64, error) {
	var total int64
	if err := DB.Model(&PayablePullTask{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var tasks []PayablePullTask
	if err := DB.Order("id desc").Offset(offset).Limit(limit).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}
	summaries := make([]PayablePullTaskSummary, len(tasks))
	index := make(map[int]int, len(tasks))
	taskIDs := make([]int, len(tasks))
	for i, task := range tasks {
		summaries[i].PayablePullTask = task
		index[task.Id] = i
		taskIDs[i] = task.Id
	}
	if len(taskIDs) == 0 {
		return summaries, total, nil
	}

	var statusCounts []struct {
		TaskId int
		Status string
		Count  int
	}
	err := DB.Model(&PayablePullSite{}).Select("task_id, status, COUNT(*) AS count").
		Where("task_id IN ?", taskIDs).Group("task_id, status").Scan(&statusCounts).Error
	if err != nil {
		return nil, 0, err
	}
	for _, row := range statusCounts {
		summary := &summaries[index[row.TaskId]]
		switch row.Status {
		case PayablePullSuccess:
			summary.FinishedSites += row.Count
		case PayablePullRetrying:
			summary.RetryingSites += row.Count
		case PayablePullFailed, PayablePullSuperseded:
			summary.FailedSites += row.Count
		}
	}

	var mismatched []struct {
		TaskId int
		Count  int
	}
	// 成功但写入的渠道数少于站点报的总数：补扫后仍对不上、以已拉到的为准结束的进度。
	err = DB.Model(&PayablePullSite{}).Select("task_id, COUNT(*) AS count").
		Where("task_id IN ? AND status = ? AND channel_count < reported_total", taskIDs, PayablePullSuccess).
		Group("task_id").Scan(&mismatched).Error
	if err != nil {
		return nil, 0, err
	}
	for _, row := range mismatched {
		summaries[index[row.TaskId]].MismatchSites = row.Count
	}

	var totals []struct {
		TaskId int
		Count  int
	}
	err = DB.Model(&PayableChannelUsage{}).Select("task_id, COUNT(*) AS count").
		Where("task_id IN ?", taskIDs).Group("task_id").Scan(&totals).Error
	if err != nil {
		return nil, 0, err
	}
	for _, row := range totals {
		summaries[index[row.TaskId]].ChannelTotal = row.Count
	}

	var sums []struct {
		TaskId     int
		DeltaQuota int64
		UsedQuota  int64
	}
	err = DB.Model(&PayableChannelUsage{}).
		Select("task_id, COALESCE(SUM(delta_quota), 0) AS delta_quota, COALESCE(SUM(used_quota), 0) AS used_quota").
		Where("task_id IN ?", taskIDs).
		Where("pull_site_id IN (?)", DB.Model(&PayablePullSite{}).Select("id").Where("status = ?", PayablePullSuccess)).
		Group("task_id").Scan(&sums).Error
	if err != nil {
		return nil, 0, err
	}
	for _, row := range sums {
		summary := &summaries[index[row.TaskId]]
		summary.DeltaQuota, summary.UsedQuota = row.DeltaQuota, row.UsedQuota
	}
	return summaries, total, nil
}

// ListPayablePullSites 返回任务的各站点进度，按站点 id 升序。
func ListPayablePullSites(taskID int) ([]PayablePullSite, error) {
	var sites []PayablePullSite
	err := DB.Where("task_id = ?", taskID).Order("site_id asc").Find(&sites).Error
	return sites, err
}

// ListPayableUsages 返回筛选后的明细，按账单日期倒序、站点 id、远端渠道 id 升序，同时返回总行数。limit <= 0 时返回全部。
func ListPayableUsages(filter PayableUsageFilter, offset, limit int) ([]PayableUsageRow, int64, error) {
	query := DB.Model(&PayableChannelUsage{}).Where("bill_date >= ? AND bill_date <= ?", filter.StartDate, filter.EndDate)
	if len(filter.SiteIDs) > 0 {
		query = query.Where("site_id IN ?", filter.SiteIDs)
	}
	if len(filter.ProviderIDs) > 0 {
		query = query.Where("provider_id IN ?", filter.ProviderIDs)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	query = query.Order("bill_date desc, site_id asc, remote_channel_id asc")
	if limit > 0 {
		query = query.Offset(offset).Limit(limit)
	}
	var usages []PayableChannelUsage
	if err := query.Find(&usages).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]PayableUsageRow, len(usages))
	if len(usages) == 0 {
		return rows, total, nil
	}

	pullIDs := make([]int, 0, len(usages))
	siteIDs := make([]int, 0, len(usages))
	channelIDs := make([]int, 0, len(usages))
	dates := make([]int, 0, len(usages))
	for i, usage := range usages {
		rows[i].PayableChannelUsage = usage
		pullIDs = append(pullIDs, usage.PullSiteId)
		siteIDs = append(siteIDs, usage.SiteId)
		channelIDs = append(channelIDs, usage.RemoteChannelId)
		dates = append(dates, usage.BillDate)
	}
	// 去重后再绑定：导出（limit <= 0）的行数可达数万，每行一个参数会超出各库的绑定参数上限。
	for _, ids := range [][]int{pullIDs, siteIDs, channelIDs, dates} {
		slices.Sort(ids)
	}
	pullIDs, siteIDs, channelIDs, dates = slices.Compact(pullIDs), slices.Compact(siteIDs), slices.Compact(channelIDs), slices.Compact(dates)
	var successPulls []int
	err := DB.Model(&PayablePullSite{}).Where("id IN ? AND status = ?", pullIDs, PayablePullSuccess).Pluck("id", &successPulls).Error
	if err != nil {
		return nil, 0, err
	}
	counted := make(map[int]bool, len(successPulls))
	for _, id := range successPulls {
		counted[id] = true
	}
	var overrides []PayableProviderOverride
	err = DB.Where("site_id IN ? AND remote_channel_id IN ?", siteIDs, channelIDs).Find(&overrides).Error
	if err != nil {
		return nil, 0, err
	}
	manual := make(map[[2]int]bool, len(overrides))
	for _, override := range overrides {
		manual[[2]int{override.SiteId, override.RemoteChannelId}] = true
	}
	settled, err := payableUsageSettledDays(siteIDs, dates)
	if err != nil {
		return nil, 0, err
	}
	for i := range rows {
		rows[i].Counted = counted[rows[i].PullSiteId]
		rows[i].Manual = manual[[2]int{rows[i].SiteId, rows[i].RemoteChannelId}]
		rows[i].UsageSettled = settled[[2]int{rows[i].SiteId, rows[i].BillDate}]
	}
	return rows, total, nil
}

// PayableLocalChannelNames 返回 ids 中平台上仍存在的渠道的当前名称，以渠道 ID 为键。
func PayableLocalChannelNames(ids []int) (map[int]string, error) {
	names := make(map[int]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	var channels []Channel
	err := DB.Select("id", "name").Where("id IN ?", slices.Compact(slices.Sorted(slices.Values(ids)))).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		names[channel.Id] = channel.Name
	}
	return names, nil
}

// payableBillEntry 是提供商账单组内的一笔：一行渠道明细（额度是增量）或一条使用明细结算记录。
type payableBillEntry struct {
	name         string
	quota        int64
	quotaPerUnit int64
	discount     *float64
}

// sumPayableBill 按组内各笔（id 升序）定下账单的名称、换算率、折扣与应付金额，渠道明细与使用明细结算两部分共用：
// 名称与换算率取最后一笔，即组内 id 最大的一笔；额度为 0 的笔不计金额，它的折扣也不影响“多个”“未设置”；
// 有额度非 0 而折扣为空的笔时“未设置”，应付金额只计有折扣的笔；额度全为 0 时没有要付的钱，应付金额为 0，没有折扣可显示。
func sumPayableBill(bill *PayableProviderBill, entries []payableBillEntry) {
	distinct := map[float64]bool{}
	for _, entry := range entries {
		bill.ProviderName, bill.QuotaPerUnit = entry.name, entry.quotaPerUnit
		if entry.quota == 0 {
			continue
		}
		if entry.discount == nil {
			bill.DiscountMissing = true
			continue
		}
		distinct[*entry.discount] = true
		amount := ReceivableAmount(entry.quota, entry.quotaPerUnit).Mul(decimal.NewFromFloat(*entry.discount))
		if bill.PayableAmount != nil {
			amount = bill.PayableAmount.Add(amount)
		}
		bill.PayableAmount = &amount
	}
	bill.DiscountMixed = len(distinct) >= 2
	if len(distinct) == 1 && !bill.DiscountMissing {
		for discount := range distinct {
			bill.Discount = &discount
		}
	}
	if bill.PayableAmount == nil && !bill.DiscountMissing {
		zero := decimal.Zero
		bill.PayableAmount = &zero
	}
}

// ListPayableProviderBills 返回提供商账单，同时返回行数；limit <= 0 时返回全部行。账单在查询时汇总，分两部分（设计 5.4）：
//   - 使用明细结算：筛选日期、站点、提供商内按使用明细结算的站点天（payableUsageSettled）的记录，按 (账单日期, 提供商) 汇总。
//     记录的提供商不会是 0，提供商筛选含 0 时这一部分也没有提供商 0 的行。
//   - 渠道明细结算：完整拉取、增量非空、且不按使用明细结算的明细（payableCompleteUsages），按 (账单日期, 提供商) 汇总。
//
// 两部分的行合并后按账单日期倒序、提供商 id 升序排列，同一天同一提供商两行时使用明细结算在前，再分页。
// filter.MergeMethods 时同一天同一提供商的两部分先合成一行（结算方式为空）再排序、分页，行内各笔使用明细结算在前、渠道明细结算在后。
// 名称、折扣与应付金额都由 sumPayableBill 汇总；渠道明细结算部分由 SQL 分组求增量之和，只取页内各行的明细来汇总。
func ListPayableProviderBills(filter PayableUsageFilter, offset, limit int) ([]PayableProviderBill, int64, error) {
	var channelBills []PayableProviderBill
	err := payableCompleteUsages(filter).Where("delta_quota IS NOT NULL").
		Select("bill_date, provider_id, SUM(delta_quota) AS delta_quota").Group("bill_date, provider_id").Scan(&channelBills).Error
	if err != nil {
		return nil, 0, err
	}

	settled := payableUsageSettled().Select("s.*").Where("s.bill_date >= ? AND s.bill_date <= ?", filter.StartDate, filter.EndDate)
	if len(filter.SiteIDs) > 0 {
		settled = settled.Where("s.site_id IN ?", filter.SiteIDs)
	}
	if len(filter.ProviderIDs) > 0 {
		settled = settled.Where("s.provider_id IN ?", filter.ProviderIDs)
	}
	var records []PayableUsageSettlement
	if err := settled.Order("s.id").Find(&records).Error; err != nil {
		return nil, 0, err
	}

	// 一行账单：使用明细结算部分的各笔在这里收齐；渠道明细结算部分先只有 SQL 求的增量之和，它的各笔分页后再取。
	type billRow struct {
		bill         PayableProviderBill
		usageEntries []payableBillEntry
		hasChannel   bool
	}
	type billKey struct {
		date, providerID int
		method           string
	}
	var rows []billRow
	rowIndex := map[billKey]int{}
	// rowOf 返回一部分所在的行；合并结算方式时结算方式为空，同一天同一提供商的两部分落到同一行。返回的指针在下一次调用前有效。
	rowOf := func(date, providerID int, method string) *billRow {
		if filter.MergeMethods {
			method = ""
		}
		key := billKey{date, providerID, method}
		i, ok := rowIndex[key]
		if !ok {
			i = len(rows)
			rowIndex[key] = i
			rows = append(rows, billRow{bill: PayableProviderBill{BillDate: date, ProviderId: providerID, Method: method}})
		}
		return &rows[i]
	}
	for _, record := range records {
		row := rowOf(record.BillDate, record.ProviderId, PayableSettlementUsage)
		row.bill.DeltaQuota += record.Quota
		row.usageEntries = append(row.usageEntries, payableBillEntry{name: record.ProviderName, quota: record.Quota,
			quotaPerUnit: record.QuotaPerUnit, discount: record.PayableDiscount})
	}
	for _, channelBill := range channelBills {
		row := rowOf(channelBill.BillDate, channelBill.ProviderId, PayableSettlementChannel)
		row.bill.DeltaQuota += channelBill.DeltaQuota
		row.hasChannel = true
	}
	slices.SortFunc(rows, func(a, b billRow) int {
		// 结算方式倒序（"usage" > "channel"）：同一天同一提供商的两行，使用明细结算在前
		return cmp.Or(cmp.Compare(b.bill.BillDate, a.bill.BillDate), cmp.Compare(a.bill.ProviderId, b.bill.ProviderId),
			cmp.Compare(b.bill.Method, a.bill.Method))
	})
	total := int64(len(rows))
	if limit > 0 {
		start := min(max(offset, 0), len(rows))
		rows = rows[start:min(start+limit, len(rows))]
	}

	index := make(map[[2]int]int, len(rows))
	dates := make([]int, 0, len(rows))
	providerIDs := make([]int, 0, len(rows))
	for i, row := range rows {
		if !row.hasChannel {
			continue
		}
		index[[2]int{row.bill.BillDate, row.bill.ProviderId}] = i
		dates = append(dates, row.bill.BillDate)
		providerIDs = append(providerIDs, row.bill.ProviderId)
	}
	var usages []PayableChannelUsage
	if len(index) > 0 {
		// 日期和提供商各自去重，绑定参数个数不随行数增长。
		slices.Sort(dates)
		slices.Sort(providerIDs)
		dates, providerIDs = slices.Compact(dates), slices.Compact(providerIDs)
		err = payableCompleteUsages(filter).Where("delta_quota IS NOT NULL").
			Where("bill_date IN ? AND provider_id IN ?", dates, providerIDs).Order("id asc").Find(&usages).Error
		if err != nil {
			return nil, 0, err
		}
	}
	channelEntries := make([][]payableBillEntry, len(rows))
	for _, usage := range usages {
		if i, ok := index[[2]int{usage.BillDate, usage.ProviderId}]; ok {
			channelEntries[i] = append(channelEntries[i], payableBillEntry{name: usage.ProviderName, quota: *usage.DeltaQuota,
				quotaPerUnit: usage.QuotaPerUnit, discount: usage.PayableDiscount})
		}
	}
	bills := make([]PayableProviderBill, len(rows))
	for i, row := range rows {
		bills[i] = row.bill
		sumPayableBill(&bills[i], slices.Concat(row.usageEntries, channelEntries[i]))
	}
	return bills, total, nil
}

// CountPayablePullGaps 返回筛选日期内、筛选站点里会让账单不完整的两类进度数：incomplete 是未成功的（含失败、被取代、进行中），
// 它们的明细不计入账单；mismatched 是成功但写入的渠道数少于站点报的总数的（补扫后仍对不上、以已拉到的为准结束）。
func CountPayablePullGaps(filter PayableUsageFilter) (incomplete, mismatched int64, err error) {
	pulls := DB.Model(&PayablePullSite{}).Where("bill_date >= ? AND bill_date <= ?", filter.StartDate, filter.EndDate)
	if len(filter.SiteIDs) > 0 {
		pulls = pulls.Where("site_id IN ?", filter.SiteIDs)
	}
	pulls = pulls.Session(&gorm.Session{}) // 两次计数各自在这些条件上追加状态条件
	if err := pulls.Where("status <> ?", PayablePullSuccess).Count(&incomplete).Error; err != nil {
		return 0, 0, err
	}
	err = pulls.Where("status = ? AND channel_count < reported_total", PayablePullSuccess).Count(&mismatched).Error
	return incomplete, mismatched, err
}

// ListPayableProviderOptions 返回所有渠道供应商（角色 5、未软删除，含已禁用），按 id 升序。
func ListPayableProviderOptions() ([]PayableProviderOption, error) {
	var users []User
	err := DB.Select("id", "username", "status", "payable_discount").
		Where("role = ?", common.RoleChannelSupplierUser).Order("id asc").Find(&users).Error
	if err != nil {
		return nil, err
	}
	options := make([]PayableProviderOption, len(users))
	for i, user := range users {
		options[i] = PayableProviderOption{Id: user.Id, Username: user.Username,
			Disabled: user.Status != common.UserStatusEnabled, PayableDiscount: user.PayableDiscount}
	}
	return options, nil
}

// GetPayableUsageRow 返回一行明细及其 counted、manual、usage_settled 标记；明细不存在时返回 ErrPayableUsageNotFound。
func GetPayableUsageRow(id int) (PayableUsageRow, error) {
	var row PayableUsageRow
	if err := DB.First(&row.PayableChannelUsage, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, ErrPayableUsageNotFound
		}
		return row, err
	}
	var pulls int64
	err := DB.Model(&PayablePullSite{}).Where("id = ? AND status = ?", row.PullSiteId, PayablePullSuccess).Count(&pulls).Error
	if err != nil {
		return row, err
	}
	var overrides int64
	err = DB.Model(&PayableProviderOverride{}).Where("site_id = ? AND remote_channel_id = ?", row.SiteId, row.RemoteChannelId).Count(&overrides).Error
	if err != nil {
		return row, err
	}
	settled, err := payableUsageSettledDays([]int{row.SiteId}, []int{row.BillDate})
	if err != nil {
		return row, err
	}
	row.Counted, row.Manual, row.UsageSettled = pulls > 0, overrides > 0, settled[[2]int{row.SiteId, row.BillDate}]
	return row, nil
}
