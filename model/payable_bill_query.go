package model

import (
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

// PayableUsageRow 是一行明细；Counted 表示所属进度已成功（完整拉取），Manual 表示该站点渠道有手动指定。
type PayableUsageRow struct {
	PayableChannelUsage
	Counted bool `json:"counted" gorm:"-"`
	Manual  bool `json:"manual" gorm:"-"`
}

// PayableProviderBill 是某天某提供商的账单汇总。折扣的统计只看增量非 0 的行：增量为 0 的行不计金额，折扣也无所谓。
type PayableProviderBill struct {
	BillDate        int
	ProviderId      int
	ProviderName    string // 组内 id 最大的一行的快照
	DeltaQuota      int64
	QuotaPerUnit    int64
	Discount        *float64         // 组内增量非 0 的行折扣全部相同且非空时有值
	DiscountMixed   bool             // 组内增量非 0 的行出现 >= 2 种非空折扣
	DiscountMissing bool             // 组内有增量非 0 而折扣为空的行
	PayableAmount   *decimal.Decimal // 有折扣行的 金额×折扣 之和；增量全为 0 时为 0；增量非 0 的行都没有折扣时 nil
}

// PayableProviderOption 是提供商筛选和手动指定里的一个渠道供应商。
type PayableProviderOption struct {
	Id              int      `json:"id"`
	Username        string   `json:"username"`
	Disabled        bool     `json:"disabled"`
	PayableDiscount *float64 `json:"payable_discount"`
}

// payableCompleteUsages 返回只含完整拉取行（所属进度为 success）并按 filter 筛选的明细查询。
func payableCompleteUsages(filter PayableUsageFilter) *gorm.DB {
	query := DB.Model(&PayableChannelUsage{}).
		Where("bill_date >= ? AND bill_date <= ?", filter.StartDate, filter.EndDate).
		Where("pull_site_id IN (?)", DB.Model(&PayablePullSite{}).Select("id").Where("status = ?", PayablePullSuccess))
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
	for i, usage := range usages {
		rows[i].PayableChannelUsage = usage
		pullIDs = append(pullIDs, usage.PullSiteId)
		siteIDs = append(siteIDs, usage.SiteId)
		channelIDs = append(channelIDs, usage.RemoteChannelId)
	}
	// 去重后再绑定：导出（limit <= 0）的行数可达数万，每行一个参数会超出各库的绑定参数上限。
	for _, ids := range [][]int{pullIDs, siteIDs, channelIDs} {
		slices.Sort(ids)
	}
	pullIDs, siteIDs, channelIDs = slices.Compact(pullIDs), slices.Compact(siteIDs), slices.Compact(channelIDs)
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
	for i := range rows {
		rows[i].Counted = counted[rows[i].PullSiteId]
		rows[i].Manual = manual[[2]int{rows[i].SiteId, rows[i].RemoteChannelId}]
	}
	return rows, total, nil
}

// ListPayableProviderBills 返回按 (账单日期, 提供商) 汇总的账单，按账单日期倒序、提供商 id 升序，同时返回组数。
// 只汇总完整拉取且增量非空的行。limit <= 0 时返回全部组。
func ListPayableProviderBills(filter PayableUsageFilter, offset, limit int) ([]PayableProviderBill, int64, error) {
	var total int64
	groups := payableCompleteUsages(filter).Where("delta_quota IS NOT NULL").Select("bill_date, provider_id").Group("bill_date, provider_id")
	if err := DB.Table("(?) AS g", groups).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	pageQuery := payableCompleteUsages(filter).Where("delta_quota IS NOT NULL").
		Select("bill_date, provider_id, SUM(delta_quota) AS delta_quota").
		Group("bill_date, provider_id").Order("bill_date desc, provider_id asc")
	if limit > 0 {
		pageQuery = pageQuery.Offset(offset).Limit(limit)
	}
	var bills []PayableProviderBill
	if err := pageQuery.Scan(&bills).Error; err != nil {
		return nil, 0, err
	}
	if len(bills) == 0 {
		return bills, total, nil
	}

	index := make(map[[2]int]int, len(bills))
	dates := make([]int, 0, len(bills))
	providerIDs := make([]int, 0, len(bills))
	for i, bill := range bills {
		index[[2]int{bill.BillDate, bill.ProviderId}] = i
		dates = append(dates, bill.BillDate)
		providerIDs = append(providerIDs, bill.ProviderId)
	}
	// 日期和提供商各自去重，绑定参数个数不随组数增长。
	slices.Sort(dates)
	slices.Sort(providerIDs)
	dates, providerIDs = slices.Compact(dates), slices.Compact(providerIDs)
	var usages []PayableChannelUsage
	err := payableCompleteUsages(filter).Where("delta_quota IS NOT NULL").
		Where("bill_date IN ? AND provider_id IN ?", dates, providerIDs).Order("id asc").Find(&usages).Error
	if err != nil {
		return nil, 0, err
	}
	distinct := make([]map[float64]bool, len(bills))
	for _, usage := range usages {
		i, ok := index[[2]int{usage.BillDate, usage.ProviderId}]
		if !ok {
			continue
		}
		bill := &bills[i]
		bill.ProviderName = usage.ProviderName // id 升序遍历，最后留下的是组内 id 最大的行
		bill.QuotaPerUnit = usage.QuotaPerUnit
		// 增量为 0 的行不计金额，它的折扣也不影响"多个"、"未设置"。
		if *usage.DeltaQuota == 0 {
			continue
		}
		if usage.PayableDiscount == nil {
			bill.DiscountMissing = true
			continue
		}
		if distinct[i] == nil {
			distinct[i] = map[float64]bool{}
		}
		distinct[i][*usage.PayableDiscount] = true
		amount := ReceivableAmount(*usage.DeltaQuota, usage.QuotaPerUnit).Mul(decimal.NewFromFloat(*usage.PayableDiscount))
		if bill.PayableAmount != nil {
			amount = bill.PayableAmount.Add(amount)
		}
		bill.PayableAmount = &amount
	}
	for i := range bills {
		bill := &bills[i]
		bill.DiscountMixed = len(distinct[i]) >= 2
		if len(distinct[i]) == 1 && !bill.DiscountMissing {
			for discount := range distinct[i] {
				bill.Discount = &discount
			}
		}
		// 组内增量全为 0：没有要付的钱，应付金额为 0，没有折扣可显示
		if bill.PayableAmount == nil && !bill.DiscountMissing {
			zero := decimal.Zero
			bill.PayableAmount = &zero
		}
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

// GetPayableUsageRow 返回一行明细及其 counted、manual 标记；明细不存在时返回 ErrPayableUsageNotFound。
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
	row.Counted, row.Manual = pulls > 0, overrides > 0
	return row, nil
}
