package controller

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// payableSettlementMethods 是结算方式的两个取值。
var payableSettlementMethods = []string{model.PayableSettlementChannel, model.PayableSettlementUsage}

// GetPayableSettlementMethods 分页返回渠道供应商的结算方式，按提供商 ID 升序；provider_ids、methods 是逗号分隔的筛选条件。
func GetPayableSettlementMethods(c *gin.Context) {
	providerIDs, ok := parseReceivableIDList(c.Query("provider_ids"), 1)
	if !ok {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	filter := model.PayableSettlementFilter{ProviderIDs: providerIDs}
	if methods := c.Query("methods"); methods != "" {
		for method := range strings.SplitSeq(methods, ",") {
			if !slices.Contains(payableSettlementMethods, method) {
				common.ApiErrorT(c, "Invalid parameters")
				return
			}
			filter.Methods = append(filter.Methods, method)
		}
	}
	pageInfo := common.GetPageQuery(c)
	rows, total, err := model.ListPayableSettlementMethods(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

// SetPayableSettlementMethod 设置一个渠道供应商的结算方式，与当前相同时也保存（只更新更新时间）。只影响之后同步成功的每日账单。
func SetPayableSettlementMethod(c *gin.Context) {
	providerID, err := strconv.Atoi(c.Param("provider_id"))
	if err != nil || providerID <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	// 缺少 method 或为 null 时指针为 nil：与解码失败一样在处理函数记审计之前返回，由兜底审计记录。
	var input struct {
		Method *string `json:"method"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || input.Method == nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	method := *input.Method

	const action = "payable_bill.set_settlement_method"
	params := model.AuditFields{"provider_id": providerID, "settlement_method": method}
	success := false
	defer func() {
		content := auditContentEN(action, params)
		if !success {
			content = "Failed to set the settlement method"
		}
		model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params,
			auditOperatorInfo(c), &model.AuditRequestInfo{
				Method: c.Request.Method, Route: c.FullPath(), Status: c.Writer.Status(), Success: success,
			}, c)
		markAuditLogged(c)
	}()

	if !slices.Contains(payableSettlementMethods, method) {
		params["failure_reason"] = "invalid_method"
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	now := common.GetTimestamp()
	previous, provider, err := model.SetPayableSettlementMethod(providerID, method, now)
	switch {
	case errors.Is(err, model.ErrPayableProviderInvalid):
		params["failure_reason"] = "invalid_provider"
		common.ApiErrorT(c, err.Error())
		return
	case err != nil:
		params["failure_reason"] = "database_error"
		common.ApiError(c, err)
		return
	}
	success = true
	params["provider_name"] = provider.Username
	params["previous_settlement_method"] = previous
	common.ApiSuccess(c, gin.H{"provider_id": providerID, "method": method, "updated_time": now})
}
