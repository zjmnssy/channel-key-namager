package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
// provider_ids 允许 0（未知提供商）。
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
	DeltaAmount *string `json:"delta_amount"`
	UsedAmount  string  `json:"used_amount"`
}

type payableProviderBillView struct {
	BillDate        int      `json:"bill_date"`
	ProviderId      int      `json:"provider_id"`
	ProviderName    string   `json:"provider_name"`
	Amount          string   `json:"amount"`
	Discount        *float64 `json:"discount"`
	DiscountMixed   bool     `json:"discount_mixed"`
	DiscountMissing bool     `json:"discount_missing"`
	PayableAmount   *string  `json:"payable_amount"`
}

func newPayableUsageView(row model.PayableUsageRow) payableUsageView {
	view := payableUsageView{PayableUsageRow: row,
		UsedAmount: model.ReceivableAmount(row.UsedQuota, row.QuotaPerUnit).StringFixed(6)}
	if row.DeltaQuota != nil {
		delta := model.ReceivableAmount(*row.DeltaQuota, row.QuotaPerUnit).StringFixed(6)
		view.DeltaAmount = &delta
	}
	return view
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
	views := make([]payableUsageView, len(rows))
	for i, row := range rows {
		views[i] = newPayableUsageView(row)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(views)
	common.ApiSuccess(c, pageInfo)
}

func GetPayableProviderBills(c *gin.Context) {
	filter, ok := parsePayableUsageFilter(c)
	if !ok {
		return
	}
	pageInfo := common.GetPageQuery(c)
	bills, total, err := model.ListPayableProviderBills(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	incomplete, err := model.CountIncompletePayablePulls(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]payableProviderBillView, len(bills))
	for i, bill := range bills {
		views[i] = payableProviderBillView{
			BillDate: bill.BillDate, ProviderId: bill.ProviderId, ProviderName: bill.ProviderName,
			Amount:   model.ReceivableAmount(bill.DeltaQuota, bill.QuotaPerUnit).StringFixed(6),
			Discount: bill.Discount, DiscountMixed: bill.DiscountMixed, DiscountMissing: bill.DiscountMissing,
		}
		if bill.PayableAmount != nil {
			payable := bill.PayableAmount.StringFixed(6)
			views[i].PayableAmount = &payable
		}
	}
	common.ApiSuccess(c, gin.H{
		"page": pageInfo.GetPage(), "page_size": pageInfo.GetPageSize(), "total": total, "items": views,
		"incomplete_site_count": incomplete,
	})
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
	var input struct {
		ProviderId int `json:"provider_id"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || input.ProviderId < 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}

	action := "payable_bill.set_provider"
	if input.ProviderId == 0 {
		action = "payable_bill.clear_provider"
	}
	params := model.AuditFields{"usage_id": id, "provider_id": input.ProviderId}
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

	before, after, err := model.SetPayableUsageProvider(id, input.ProviderId, common.GetTimestamp())
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
	params["provider_name"] = after.ProviderName
	params["discount"] = after.PayableDiscount

	row, err := model.GetPayableUsageRow(id)
	if err != nil {
		params["failure_reason"] = "database_error"
		common.ApiError(c, err)
		return
	}
	success = true
	common.ApiSuccess(c, newPayableUsageView(row))
}
