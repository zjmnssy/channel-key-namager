package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// parsePayableMessage 解析存成 JSON 的 common.Message，空串或无法解析时返回 nil。
func parsePayableMessage(raw string) *common.Message {
	if raw == "" {
		return nil
	}
	var message common.Message
	if err := common.UnmarshalJsonStr(raw, &message); err != nil {
		return nil
	}
	return &message
}

// parsePayableUsageFilter 读取明细和提供商账单共用的筛选参数，参数不合法时已回复错误并返回 false。
// provider_ids 允许 0（未知提供商）。渠道供应商只能看提供商是本人的行：提供商固定为本人，带站点或提供商筛选的请求一律不合法，
// 以免按站点筛选试出各行属于哪个站点。
func parsePayableUsageFilter(c *gin.Context) (model.PayableUsageFilter, bool) {
	var filter model.PayableUsageFilter
	startDate, endDate, ok := parseReceivableDateRange(c)
	if !ok {
		return filter, false
	}
	filter.StartDate, filter.EndDate = startDate, endDate

	siteIDs, ok := parseReceivableIDList(c.Query("site_ids"), 1)
	if !ok {
		common.ApiErrorT(c, "Invalid parameters")
		return filter, false
	}
	providerIDs, ok := parseReceivableIDList(c.Query("provider_ids"), 0)
	if !ok {
		common.ApiErrorT(c, "Invalid parameters")
		return filter, false
	}
	if c.GetInt("role") == common.RoleChannelSupplierUser {
		if len(siteIDs) > 0 || len(providerIDs) > 0 {
			common.ApiErrorT(c, "Invalid parameters")
			return filter, false
		}
		filter.ProviderIDs = []int{c.GetInt("id")}
		return filter, true
	}
	filter.SiteIDs, filter.ProviderIDs = siteIDs, providerIDs
	return filter, true
}

type payablePullTaskView struct {
	model.PayablePullTaskSummary
	DeltaAmount *string `json:"delta_amount"`
	UsedAmount  *string `json:"used_amount"`
}

type payablePullSiteView struct {
	model.PayablePullSite
	Error *common.Message `json:"error"`
	Note  *common.Message `json:"note"`
}

type payableUsageView struct {
	model.PayableUsageRow
	DeltaAmount      *string `json:"delta_amount"`
	UsedAmount       string  `json:"used_amount"`
	LocalChannelId   int     `json:"local_channel_id"`   // 站点渠道名称结尾推送标记里的平台渠道 ID；没有本站点的标记时为 0
	LocalChannelName *string `json:"local_channel_name"` // 平台渠道当前的名称；没有平台渠道 ID 或平台上已没有这个渠道时为 null
}

// payableSupplierUsageView 是渠道供应商看到的一行明细：明细视图去掉站点 ID、名称、地址与拉取进度 ID（同一进度的行属于同一个站点）。
// 字段逐个列出，明细以后新增的字段不会自动给渠道供应商。
type payableSupplierUsageView struct {
	Id                  int      `json:"id"`
	TaskId              int      `json:"task_id"`
	BillDate            int      `json:"bill_date"`
	RemoteChannelId     int      `json:"remote_channel_id"`
	RemoteChannelName   string   `json:"remote_channel_name"`
	RemoteChannelStatus *int     `json:"remote_channel_status"`
	RemoteChannelGroup  string   `json:"remote_channel_group"`
	UsedQuota           int64    `json:"used_quota"`
	DeltaQuota          *int64   `json:"delta_quota"`
	DeltaFlag           string   `json:"delta_flag"`
	QuotaPerUnit        int64    `json:"quota_per_unit"`
	ProviderId          int      `json:"provider_id"`
	ProviderName        string   `json:"provider_name"`
	ProviderSource      string   `json:"provider_source"`
	PayableDiscount     *float64 `json:"payable_discount"`
	SyncedAt            int64    `json:"synced_at"`
	CreatedTime         int64    `json:"created_time"`
	UpdatedTime         int64    `json:"updated_time"`
	Counted             bool     `json:"counted"`
	Manual              bool     `json:"manual"`
	UsageSettled        bool     `json:"usage_settled"`
	DeltaAmount         *string  `json:"delta_amount"`
	UsedAmount          string   `json:"used_amount"`
	LocalChannelId      int      `json:"local_channel_id"`
	LocalChannelName    *string  `json:"local_channel_name"`
}

// newPayableSupplierUsageViews 把明细视图转成渠道供应商看到的视图。
func newPayableSupplierUsageViews(views []payableUsageView) []payableSupplierUsageView {
	supplierViews := make([]payableSupplierUsageView, len(views))
	for i, view := range views {
		supplierViews[i] = payableSupplierUsageView{
			Id: view.Id, TaskId: view.TaskId, BillDate: view.BillDate,
			RemoteChannelId: view.RemoteChannelId, RemoteChannelName: view.RemoteChannelName,
			RemoteChannelStatus: view.RemoteChannelStatus, RemoteChannelGroup: view.RemoteChannelGroup,
			UsedQuota: view.UsedQuota, DeltaQuota: view.DeltaQuota, DeltaFlag: view.DeltaFlag, QuotaPerUnit: view.QuotaPerUnit,
			ProviderId: view.ProviderId, ProviderName: view.ProviderName, ProviderSource: view.ProviderSource,
			PayableDiscount: view.PayableDiscount, SyncedAt: view.SyncedAt, CreatedTime: view.CreatedTime, UpdatedTime: view.UpdatedTime,
			Counted: view.Counted, Manual: view.Manual, UsageSettled: view.UsageSettled,
			DeltaAmount: view.DeltaAmount, UsedAmount: view.UsedAmount,
			LocalChannelId: view.LocalChannelId, LocalChannelName: view.LocalChannelName,
		}
	}
	return supplierViews
}

type payableProviderBillView struct {
	BillDate         int      `json:"bill_date"`
	ProviderId       int      `json:"provider_id"`
	ProviderName     string   `json:"provider_name"`
	SettlementMethod string   `json:"settlement_method,omitempty"` // 这一行的结算方式：channel 或 usage；渠道供应商的账单不分结算方式，没有这个字段
	Amount           string   `json:"amount"`
	Discount         *float64 `json:"discount"`
	DiscountMixed    bool     `json:"discount_mixed"`
	DiscountMissing  bool     `json:"discount_missing"`
	PayableAmount    *string  `json:"payable_amount"`
}

// newPayableUsageViews 把明细行转成接口视图（导出也用它确定平台渠道）：算出金额，并按站点渠道名称结尾的推送标记填上平台渠道 ID 与它在平台上当前的名称。
// 标记里的站点必须是本行的站点，与渠道推送、提供商解析认定平台渠道的方式相同。
func newPayableUsageViews(rows []model.PayableUsageRow) ([]payableUsageView, error) {
	views := make([]payableUsageView, len(rows))
	localIDs := make([]int, 0, len(rows))
	for i, row := range rows {
		views[i] = payableUsageView{PayableUsageRow: row,
			UsedAmount: model.ReceivableAmount(row.UsedQuota, row.QuotaPerUnit).StringFixed(6)}
		if row.DeltaQuota != nil {
			delta := model.ReceivableAmount(*row.DeltaQuota, row.QuotaPerUnit).StringFixed(6)
			views[i].DeltaAmount = &delta
		}
		if siteID, localID, ok := service.ParseChannelPushMarker(row.RemoteChannelName); ok && siteID == row.SiteId {
			views[i].LocalChannelId = localID
			localIDs = append(localIDs, localID)
		}
	}
	names, err := model.PayableLocalChannelNames(localIDs)
	if err != nil {
		return nil, err
	}
	for i := range views {
		if name, ok := names[views[i].LocalChannelId]; ok {
			views[i].LocalChannelName = &name
		}
	}
	return views, nil
}

func GetPayablePullTasks(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	tasks, total, err := model.ListPayablePullTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]payablePullTaskView, len(tasks))
	for i, task := range tasks {
		views[i].PayablePullTaskSummary = task
		// 任务结束前还有进度在拉取，合计不是最终值，不给金额。
		if task.Status == model.PayableTaskRunning {
			continue
		}
		delta := model.ReceivableAmount(task.DeltaQuota, model.ReceivableBillQuotaPerUnit).StringFixed(6)
		used := model.ReceivableAmount(task.UsedQuota, model.ReceivableBillQuotaPerUnit).StringFixed(6)
		views[i].DeltaAmount, views[i].UsedAmount = &delta, &used
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(views)
	common.ApiSuccess(c, pageInfo)
}

func GetPayablePullTaskSites(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	sites, err := model.ListPayablePullSites(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]payablePullSiteView, len(sites))
	for i, site := range sites {
		views[i] = payablePullSiteView{PayablePullSite: site, Error: parsePayableMessage(site.LastError), Note: parsePayableMessage(site.Note)}
	}
	common.ApiSuccess(c, views)
}

func GetPayableUsages(c *gin.Context) {
	filter, ok := parsePayableUsageFilter(c)
	if !ok {
		return
	}
	pageInfo := common.GetPageQuery(c)
	rows, total, err := model.ListPayableUsages(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views, err := newPayableUsageViews(rows)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	if c.GetInt("role") == common.RoleChannelSupplierUser {
		pageInfo.SetItems(newPayableSupplierUsageViews(views))
	} else {
		pageInfo.SetItems(views)
	}
	common.ApiSuccess(c, pageInfo)
}

func GetPayableProviderBills(c *gin.Context) {
	filter, ok := parsePayableUsageFilter(c)
	if !ok {
		return
	}
	// 渠道供应商的账单不分结算方式，同一天的两种合成一行；全平台站点的同步情况也不给渠道供应商。
	supplier := c.GetInt("role") == common.RoleChannelSupplierUser
	filter.MergeMethods = supplier
	pageInfo := common.GetPageQuery(c)
	bills, total, err := model.ListPayableProviderBills(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]payableProviderBillView, len(bills))
	for i, bill := range bills {
		views[i] = payableProviderBillView{
			BillDate: bill.BillDate, ProviderId: bill.ProviderId, ProviderName: bill.ProviderName, SettlementMethod: bill.Method,
			Amount:   model.ReceivableAmount(bill.DeltaQuota, bill.QuotaPerUnit).StringFixed(6),
			Discount: bill.Discount, DiscountMixed: bill.DiscountMixed, DiscountMissing: bill.DiscountMissing,
		}
		if bill.PayableAmount != nil {
			payable := bill.PayableAmount.StringFixed(6)
			views[i].PayableAmount = &payable
		}
	}
	page := gin.H{"page": pageInfo.GetPage(), "page_size": pageInfo.GetPageSize(), "total": total, "items": views}
	if supplier {
		common.ApiSuccess(c, page)
		return
	}
	incomplete, mismatched, err := model.CountPayablePullGaps(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	unsynced, err := model.CountPayableUnsyncedUsageSites(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page["incomplete_site_count"], page["mismatch_site_count"], page["unsynced_usage_site_count"] = incomplete, mismatched, unsynced
	common.ApiSuccess(c, page)
}

func GetPayableOptions(c *gin.Context) {
	sites, err := model.ListReceivableBillSiteOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	providers, err := model.ListPayableProviderOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"sites": sites, "providers": providers})
}

// SetPayableUsageProvider 手动设置一行明细的提供商；provider_id 为 0 表示清除该站点渠道的手动设置。
func SetPayableUsageProvider(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	// 指针区分缺少或为 null 的 provider_id 与显式的 0（清除手动设置）：前者是非法参数，不能当作清除。
	var input struct {
		ProviderId *int `json:"provider_id"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || input.ProviderId == nil || *input.ProviderId < 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	providerID := *input.ProviderId

	action := "payable_bill.set_provider"
	if providerID == 0 {
		action = "payable_bill.clear_provider"
	}
	params := model.AuditFields{"usage_id": id, "provider_id": providerID}
	success := false
	defer func() {
		content := auditContentEN(action, params)
		if !success {
			content = "Failed to set the provider of channel usage"
		}
		model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params,
			auditOperatorInfo(c), &model.AuditRequestInfo{
				Method: c.Request.Method, Route: c.FullPath(), Status: c.Writer.Status(), Success: success,
			}, c)
		markAuditLogged(c)
	}()

	before, after, err := model.SetPayableUsageProvider(id, providerID, common.GetTimestamp())
	if before.Id != 0 {
		params["site_id"] = before.SiteId
		params["site_name"] = before.SiteName
		params["remote_channel_id"] = before.RemoteChannelId
		params["remote_channel_name"] = before.RemoteChannelName
		params["bill_date"] = before.BillDate
		params["previous_provider_id"] = before.ProviderId
		params["previous_provider_name"] = before.ProviderName
		params["previous_discount"] = before.PayableDiscount
	}
	switch {
	case errors.Is(err, model.ErrPayableUsageNotFound):
		params["failure_reason"] = "not_found"
		common.ApiErrorT(c, err.Error())
		return
	case errors.Is(err, model.ErrPayableProviderInvalid):
		params["failure_reason"] = "invalid_provider"
		common.ApiErrorT(c, err.Error())
		return
	case err != nil:
		params["failure_reason"] = "database_error"
		common.ApiError(c, err)
		return
	}
	// 改动已提交：之后重读这一行失败，也不能把审计记成失败。
	success = true
	params["provider_name"] = after.ProviderName
	params["discount"] = after.PayableDiscount

	row, err := model.GetPayableUsageRow(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views, err := newPayableUsageViews([]model.PayableUsageRow{row})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, views[0])
}
