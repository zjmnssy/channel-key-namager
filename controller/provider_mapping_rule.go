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

// providerMappingRuleTypes 是映射规则的三种类型。
var providerMappingRuleTypes = []string{model.PayableRuleAll, model.PayableRuleUnresolved, model.PayableRuleChannel}

// providerMappingErrors 是规则接口的业务错误与审计里对应的 failure_reason；错误文本就是回给控制台的文案键。
var providerMappingErrors = []struct {
	err    error
	reason string
}{
	{model.ErrThirdPartySiteNotFound, "site_not_found"},
	{model.ErrPayableProviderInvalid, "invalid_provider"},
	{model.ErrPayableRuleNotFound, "rule_not_found"},
	{model.ErrPayableSiteRuleExists, "site_rule_exists"},
	{model.ErrPayableChannelRuleExists, "channel_rule_exists"},
	{model.ErrPayableChannelNotPulled, "channel_not_pulled"},
}

// validProviderMappingRule 报告新建或预览的规则是否合法：站点与提供商 ID 为正，类型三选一，指定渠道必须带站点渠道 ID，
// 站点级规则不能带。model 不校验类型与站点渠道的搭配，这里是唯一的关口。
func validProviderMappingRule(input model.PayableRuleInput) bool {
	if input.SiteID <= 0 || input.ProviderID <= 0 || !slices.Contains(providerMappingRuleTypes, input.RuleType) {
		return false
	}
	if input.RuleType == model.PayableRuleChannel {
		return input.RemoteChannelID > 0
	}
	return input.RemoteChannelID == 0
}

// checkProviderMappingApply 校验回改的账单日期范围，apply 为空表示不回改。起止须是有效日期且起 ≤ 止，否则回 Invalid parameters；
// 首尾都算超过 92 天时回 Date range cannot exceed 92 days。不合法时已回复错误并返回 false。
func checkProviderMappingApply(c *gin.Context, apply *model.PayableRuleApply) bool {
	if apply == nil {
		return true
	}
	start, startOK := model.ReceivableBillDayStart(apply.StartDate)
	end, endOK := model.ReceivableBillDayStart(apply.EndDate)
	if !startOK || !endOK || apply.StartDate > apply.EndDate {
		common.ApiErrorT(c, "Invalid parameters")
		return false
	}
	// 起始日往后数 92 天是第一个超出范围的日期
	if !end.Before(start.AddDate(0, 0, model.PayableRuleMaxApplyDays)) {
		common.ApiErrorT(c, "Date range cannot exceed 92 days")
		return false
	}
	return true
}

// respondProviderMappingError 回复规则接口的错误并返回审计的 failure_reason：业务错误回它的文案，其他错误原样回复，记为 database_error。
func respondProviderMappingError(c *gin.Context, err error) string {
	for _, known := range providerMappingErrors {
		if errors.Is(err, known.err) {
			common.ApiErrorT(c, known.err.Error())
			return known.reason
		}
	}
	common.ApiError(c, err)
	return "database_error"
}

// recordProviderMappingAudit 记录一次规则的新建、编辑或删除，成功与失败都记，并标记已记录，让兜底审计跳过这次请求。
func recordProviderMappingAudit(c *gin.Context, action string, params model.AuditFields, success bool) {
	content := auditContentEN(action, params)
	if !success {
		content = "Failed to change the provider mapping rule"
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params,
		auditOperatorInfo(c), &model.AuditRequestInfo{
			Method: c.Request.Method, Route: c.FullPath(), Status: c.Writer.Status(), Success: success,
		}, c)
	markAuditLogged(c)
}

// auditProviderMappingRule 把规则的 ID、站点、类型与站点渠道写进审计参数。
func auditProviderMappingRule(params model.AuditFields, row model.PayableRuleRow) {
	params["rule_id"], params["site_id"], params["site_name"] = row.Id, row.SiteId, row.SiteName
	params["rule_type"], params["remote_channel_id"], params["remote_channel_name"] = row.RuleType, row.RemoteChannelId, row.RemoteChannelName
}

// GetProviderMappingRules 分页返回映射规则，按 ID 倒序；site_ids、types、provider_ids 是逗号分隔的筛选条件。
func GetProviderMappingRules(c *gin.Context) {
	siteIDs, sitesOK := parseReceivableIDList(c.Query("site_ids"), 1)
	providerIDs, providersOK := parseReceivableIDList(c.Query("provider_ids"), 1)
	if !sitesOK || !providersOK {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	filter := model.PayableRuleFilter{SiteIDs: siteIDs, ProviderIDs: providerIDs}
	if types := c.Query("types"); types != "" {
		for ruleType := range strings.SplitSeq(types, ",") {
			if !slices.Contains(providerMappingRuleTypes, ruleType) {
				common.ApiErrorT(c, "Invalid parameters")
				return
			}
			filter.Types = append(filter.Types, ruleType)
		}
	}
	pageInfo := common.GetPageQuery(c)
	rows, total, err := model.ListPayableRules(filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

// CreateProviderMappingRule 新建一条映射规则，可选同时回改所选账单日期内已拉取的明细。
func CreateProviderMappingRule(c *gin.Context) {
	var input model.PayableRuleInput
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || !validProviderMappingRule(input) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !checkProviderMappingApply(c, input.Apply) {
		return
	}

	params := model.AuditFields{"site_id": input.SiteID, "rule_type": input.RuleType, "remote_channel_id": input.RemoteChannelID,
		"provider_id": input.ProviderID}
	if input.Apply != nil {
		params["apply_start_date"], params["apply_end_date"] = input.Apply.StartDate, input.Apply.EndDate
	}
	success := false
	defer func() { recordProviderMappingAudit(c, "provider_mapping.create", params, success) }()

	rule, applied, err := model.CreatePayableRule(input, common.GetTimestamp())
	if err != nil {
		params["failure_reason"] = respondProviderMappingError(c, err)
		return
	}
	// 规则与回改已提交：之后重读规则失败，也不能把审计记成失败。
	success = true
	params["rule_id"] = rule.Id
	if input.Apply != nil {
		params["applied_rows"] = applied
	}
	row, err := model.GetPayableRuleRow(rule.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	auditProviderMappingRule(params, row)
	params["provider_name"] = row.ProviderName
	common.ApiSuccess(c, gin.H{"rule": row, "applied_rows": applied})
}

// UpdateProviderMappingRule 修改规则的提供商，可选同时回改；提供商不变也可以保存，只做回改。站点、类型与站点渠道不能改。
func UpdateProviderMappingRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	var input struct {
		ProviderID int                     `json:"provider_id"`
		Apply      *model.PayableRuleApply `json:"apply"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || input.ProviderID <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !checkProviderMappingApply(c, input.Apply) {
		return
	}

	params := model.AuditFields{"rule_id": id, "provider_id": input.ProviderID}
	if input.Apply != nil {
		params["apply_start_date"], params["apply_end_date"] = input.Apply.StartDate, input.Apply.EndDate
	}
	success := false
	defer func() { recordProviderMappingAudit(c, "provider_mapping.update", params, success) }()

	// 审计里的站点、渠道名称与原提供商取自修改前的规则
	previous, err := model.GetPayableRuleRow(id)
	if err != nil {
		params["failure_reason"] = respondProviderMappingError(c, err)
		return
	}
	auditProviderMappingRule(params, previous)
	params["previous_provider_id"], params["previous_provider_name"] = previous.ProviderId, previous.ProviderName

	_, _, applied, err := model.UpdatePayableRule(id, input.ProviderID, input.Apply, common.GetTimestamp())
	if err != nil {
		params["failure_reason"] = respondProviderMappingError(c, err)
		return
	}
	// 规则与回改已提交：之后重读规则失败，也不能把审计记成失败。
	success = true
	if input.Apply != nil {
		params["applied_rows"] = applied
	}
	row, err := model.GetPayableRuleRow(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	params["provider_name"] = row.ProviderName
	common.ApiSuccess(c, gin.H{"rule": row, "applied_rows": applied})
}

// DeleteProviderMappingRule 删除一条规则：已拉取的明细不变，下一次拉取起不再使用。站点已删除的规则也可以删。
func DeleteProviderMappingRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid ID")
		return
	}
	params := model.AuditFields{"rule_id": id}
	success := false
	defer func() { recordProviderMappingAudit(c, "provider_mapping.delete", params, success) }()

	// 审计里的站点、渠道名称与原提供商取自删除前的规则
	previous, err := model.GetPayableRuleRow(id)
	if err != nil {
		params["failure_reason"] = respondProviderMappingError(c, err)
		return
	}
	auditProviderMappingRule(params, previous)
	params["previous_provider_id"], params["previous_provider_name"] = previous.ProviderId, previous.ProviderName
	if _, err := model.DeletePayableRule(id); err != nil {
		params["failure_reason"] = respondProviderMappingError(c, err)
		return
	}
	success = true
	common.ApiSuccess(c, nil)
}

// PreviewProviderMappingRule 返回按规则回改所选账单日期时会修改的明细行数，参数校验与新建相同，条件与真正回改相同。
// 不要求站点存在：编辑站点已删除的规则时也要预览。
func PreviewProviderMappingRule(c *gin.Context) {
	var query struct {
		SiteID          int    `form:"site_id"`
		RuleType        string `form:"rule_type"`
		RemoteChannelID int    `form:"remote_channel_id"` // 站点级规则可以不带
		ProviderID      int    `form:"provider_id"`
		StartDate       int    `form:"start_date"`
		EndDate         int    `form:"end_date"`
	}
	err := c.ShouldBindQuery(&query)
	input := model.PayableRuleInput{SiteID: query.SiteID, RuleType: query.RuleType, RemoteChannelID: query.RemoteChannelID,
		ProviderID: query.ProviderID, Apply: &model.PayableRuleApply{StartDate: query.StartDate, EndDate: query.EndDate}}
	if err != nil || !validProviderMappingRule(input) {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !checkProviderMappingApply(c, input.Apply) {
		return
	}
	rows, err := model.CountPayableRuleApply(model.PayableProviderOverride{SiteId: input.SiteID, RemoteChannelId: input.RemoteChannelID,
		ProviderId: input.ProviderID, RuleType: input.RuleType}, *input.Apply)
	if err != nil {
		respondProviderMappingError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"rows": rows})
}

// SearchProviderMappingChannels 在站点拉取过的渠道里按名称或站点渠道 ID 搜索，供新建“指定渠道”规则时选择，最多返回 20 个。
func SearchProviderMappingChannels(c *gin.Context) {
	siteID, err := strconv.Atoi(c.Query("site_id"))
	if err != nil || siteID <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	options, err := model.SearchPayableChannels(siteID, c.Query("keyword"), 20)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, options)
}
