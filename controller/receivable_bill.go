package controller

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// receivableBillView 是一行日账单的接口形态：账单字段加上由 last_error 解析出的错误，
// 以及只有成功的行才有的金额（折前）和应收金额（金额 × 应收折扣）。
type receivableBillView struct {
	model.ReceivableBill
	Amount           *string         `json:"amount"`
	ReceivableAmount *string         `json:"receivable_amount"`
	Error            *common.Message `json:"error"`
	SiteDeleted      bool            `json:"site_deleted"`
}

func newReceivableBillView(bill *model.ReceivableBill, deleted bool) receivableBillView {
	view := receivableBillView{ReceivableBill: *bill, SiteDeleted: deleted}
	if bill.Status == model.ReceivableBillStatusSuccess {
		amount := model.ReceivableAmount(bill.Quota, bill.QuotaPerUnit).StringFixed(6)
		receivable := model.ReceivableAmountAfterDiscount(bill.Quota, bill.QuotaPerUnit, bill.ReceivableDiscount).StringFixed(6)
		view.Amount, view.ReceivableAmount = &amount, &receivable
	}
	if bill.LastError != "" {
		var message common.Message
		if err := common.UnmarshalJsonStr(bill.LastError, &message); err == nil {
			view.Error = &message
		}
	}
	return view
}

// parseReceivableIDList 解析逗号分隔的 id 列表（site_ids、provider_ids），空串返回 nil，含小于 minID 的整数或非整数时返回 false。
func parseReceivableIDList(raw string, minID int) ([]int, bool) {
	if raw == "" {
		return nil, true
	}
	var ids []int
	for part := range strings.SplitSeq(raw, ",") {
		id, err := strconv.Atoi(part)
		if err != nil || id < minID {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

// parseReceivableDateRange 读取并校验 start_date、end_date（有效的 YYYYMMDD 且 start <= end），不合法时已回复错误并返回 false。
func parseReceivableDateRange(c *gin.Context) (startDate, endDate int, ok bool) {
	startDate, startErr := strconv.Atoi(c.Query("start_date"))
	endDate, endErr := strconv.Atoi(c.Query("end_date"))
	_, startOK := model.ReceivableBillDayStart(startDate)
	_, endOK := model.ReceivableBillDayStart(endDate)
	if startErr != nil || endErr != nil || !startOK || !endOK || startDate > endDate {
		common.ApiErrorT(c, "Invalid parameters")
		return 0, 0, false
	}
	return startDate, endDate, true
}

// parseReceivableBillFilter 读取列表和导出共用的筛选参数，参数不合法时已回复错误并返回 false。
func parseReceivableBillFilter(c *gin.Context) (model.ReceivableBillFilter, bool) {
	var filter model.ReceivableBillFilter
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
	filter.SiteIDs = siteIDs
	if statuses := c.Query("statuses"); statuses != "" {
		valid := []string{model.ReceivableBillStatusSuccess, model.ReceivableBillStatusRetrying, model.ReceivableBillStatusFailed}
		for part := range strings.SplitSeq(statuses, ",") {
			if !slices.Contains(valid, part) {
				common.ApiErrorT(c, "Invalid parameters")
				return filter, false
			}
			filter.Statuses = append(filter.Statuses, part)
		}
	}
	return filter, true
}

// deletedReceivableBillSites 返回已删除站点的 id 集合。
func deletedReceivableBillSites() (map[int]bool, error) {
	options, err := model.ListReceivableBillSiteOptions()
	if err != nil {
		return nil, err
	}
	deleted := make(map[int]bool)
	for _, option := range options {
		if option.Deleted {
			deleted[option.Id] = true
		}
	}
	return deleted, nil
}

func GetReceivableBills(c *gin.Context) {
	filter, ok := parseReceivableBillFilter(c)
	if !ok {
		return
	}
	pageInfo := common.GetPageQuery(c)
	bills, total, err := model.ListReceivableBills(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	deleted, err := deletedReceivableBillSites()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]receivableBillView, len(bills))
	for i, bill := range bills {
		views[i] = newReceivableBillView(bill, deleted[bill.SiteId])
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(views)
	common.ApiSuccess(c, pageInfo)
}

func GetReceivableBillTrend(c *gin.Context) {
	siteIDs, ok := parseReceivableIDList(c.Query("site_ids"), 1)
	if !ok {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	// A date that is not a number stays 0, which the service rejects with the other bad dates.
	date, _ := strconv.Atoi(c.Query("date"))
	startDate, _ := strconv.Atoi(c.Query("start_date"))
	endDate, _ := strconv.Atoi(c.Query("end_date"))
	trend, err := service.BuildReceivableBillTrend(service.ReceivableTrendQuery{
		View: c.Query("view"), Date: date, StartDate: startDate, EndDate: endDate, SiteIDs: siteIDs,
	}, time.Now())
	if errors.Is(err, service.ErrReceivableTrendInvalidQuery) || errors.Is(err, service.ErrReceivableTrendRangeTooLong) {
		common.ApiErrorT(c, err.Error())
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, trend)
}

func GetReceivableBillSites(c *gin.Context) {
	options, err := model.ListReceivableBillSiteOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, options)
}

func ResyncReceivableBill(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}

	const action = "receivable_bill.sync"
	params := model.AuditFields{"bill_id": id}
	success := false
	defer func() {
		content := auditContentEN(action, params)
		if !success {
			content = "Failed receivable bill resync"
		}
		model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params,
			auditOperatorInfo(c), &model.AuditRequestInfo{
				Method: c.Request.Method, Route: c.FullPath(), Status: c.Writer.Status(), Success: success,
			}, c)
		markAuditLogged(c)
	}()

	// 管理员确认后关闭页面不应取消同步，否则已成功的行会被写成重试中。
	before, after, err := service.ResyncReceivableBill(context.WithoutCancel(c.Request.Context()), id)
	if before.Id != 0 {
		params["site_id"] = before.SiteId
		params["site_name"] = before.SiteName
		params["bill_date"] = before.BillDate
		params["previous_status"] = before.Status
		if before.Status == model.ReceivableBillStatusSuccess {
			params["previous_quota"] = before.Quota
		}
	}
	switch {
	case errors.Is(err, model.ErrReceivableBillNotFound):
		params["failure_reason"] = "not_found"
		common.ApiErrorT(c, err.Error())
		return
	case errors.Is(err, model.ErrReceivableBillSiteDeleted):
		params["failure_reason"] = "site_deleted"
		common.ApiErrorT(c, err.Error())
		return
	case errors.Is(err, model.ErrReceivableBillSyncing):
		params["failure_reason"] = "syncing"
		common.ApiErrorT(c, err.Error())
		return
	case err != nil:
		params["failure_reason"] = "database_error"
		common.ApiError(c, err)
		return
	}

	params["status"] = after.Status
	if after.Status == model.ReceivableBillStatusSuccess {
		params["quota"] = after.Quota
	}
	success = after.Status == model.ReceivableBillStatusSuccess
	common.ApiSuccess(c, newReceivableBillView(after, false))
}
