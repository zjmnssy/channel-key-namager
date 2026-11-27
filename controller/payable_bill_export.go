package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// writePayableBillExport writes the header and the rows into an xlsx download. The columns listed in
// amountColumns (1-based) get the six-decimal number style on every row where the cell holds a number.
func writePayableBillExport(c *gin.Context, filename string, headerKeys []string, rows [][]any, amountColumns []int) {
	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	sheet := file.GetSheetName(0)
	amountStyle, err := file.NewStyle(&excelize.Style{CustomNumFmt: new("0.000000")})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	header := make([]any, len(headerKeys))
	for i, key := range headerKeys {
		header[i] = i18n.T(c, key)
	}
	if err := file.SetSheetRow(sheet, "A1", &header); err != nil {
		common.ApiError(c, err)
		return
	}
	for i, cells := range rows {
		start, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := file.SetSheetRow(sheet, start, &cells); err != nil {
			common.ApiError(c, err)
			return
		}
		for _, column := range amountColumns {
			if _, isNumber := cells[column-1].(float64); !isNumber {
				continue
			}
			cell, _ := excelize.CoordinatesToCellName(column, i+2)
			if err := file.SetCellStyle(sheet, cell, cell, amountStyle); err != nil {
				common.ApiError(c, err)
				return
			}
		}
	}

	c.Header("Content-Type", receivableBillExportContentType)
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Status(http.StatusOK)
	if err := file.Write(c.Writer); err != nil {
		common.SysError(common.LogText("Failed to write payable bill export: %v", err))
	}
}

// ExportPayableUsages writes the channel usage details that match the list filter into an xlsx file.
func ExportPayableUsages(c *gin.Context) {
	filter, ok := parsePayableUsageFilter(c)
	if !ok {
		return
	}
	usages, _, err := model.ListPayableUsages(filter, 0, 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// The platform channel comes from the same views as the list, so the page and the file agree.
	views, err := newPayableUsageViews(usages)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	headerKeys := []string{
		i18n.MsgPayableBillExportTaskID, i18n.MsgPayableBillExportBillDate, i18n.MsgPayableBillExportSiteID,
		i18n.MsgPayableBillExportSiteName, i18n.MsgPayableBillExportLocalChannelID, i18n.MsgPayableBillExportLocalChannelName,
		i18n.MsgPayableBillExportRemoteChannelID, i18n.MsgPayableBillExportRemoteChannelName, i18n.MsgPayableBillExportRemoteChannelStatus,
		i18n.MsgPayableBillExportRemoteChannelStatusType, i18n.MsgPayableBillExportRemoteChannelGroup, i18n.MsgPayableBillExportDeltaAmount,
		i18n.MsgPayableBillExportUsedAmount, i18n.MsgPayableBillExportDeltaNote, i18n.MsgPayableBillExportProvider,
		i18n.MsgPayableBillExportProviderSource, i18n.MsgPayableBillExportSyncedAt, i18n.MsgPayableBillExportCounted,
	}
	deltaKeys := map[string]string{
		model.PayableDeltaFirstSync:  i18n.MsgPayableBillDeltaFirstSync,
		model.PayableDeltaNewChannel: i18n.MsgPayableBillDeltaNewChannel,
		model.PayableDeltaRollback:   i18n.MsgPayableBillDeltaRollback,
	}
	// The sources of both site-level rules read "Rule".
	sourceKeys := map[string]string{
		model.PayableProviderManual:         i18n.MsgPayableBillSourceManual,
		model.PayableProviderParsed:         i18n.MsgPayableBillSourceParsed,
		model.PayableProviderInherited:      i18n.MsgPayableBillSourceInherited,
		model.PayableProviderUnknown:        i18n.MsgPayableBillSourceUnknown,
		model.PayableProviderRuleAll:        i18n.MsgPayableBillSourceRule,
		model.PayableProviderRuleUnresolved: i18n.MsgPayableBillSourceRule,
	}
	rows := make([][]any, len(views))
	for i, usage := range views {
		cells := make([]any, len(headerKeys))
		date, _ := model.ReceivableBillDayStart(usage.BillDate)
		cells[0], cells[1] = usage.TaskId, date.Format("2006-01-02")
		cells[2], cells[3] = usage.SiteId, usage.SiteName
		// Cells the page shows as a dash stay empty.
		if usage.LocalChannelId > 0 {
			cells[4] = usage.LocalChannelId
			cells[5] = i18n.T(c, i18n.MsgPayableBillLocalChannelMissing)
			if usage.LocalChannelName != nil {
				cells[5] = *usage.LocalChannelName
			}
		}
		cells[6], cells[7] = usage.RemoteChannelId, usage.RemoteChannelName
		if usage.RemoteChannelStatus != nil {
			switch *usage.RemoteChannelStatus {
			case common.ChannelStatusEnabled:
				cells[8] = i18n.T(c, i18n.MsgPayableBillChannelEnabled)
			case common.ChannelStatusManuallyDisabled:
				cells[8], cells[9] = i18n.T(c, i18n.MsgPayableBillChannelDisabled), i18n.T(c, i18n.MsgPayableBillChannelManuallyDisabled)
			case common.ChannelStatusAutoDisabled:
				cells[8], cells[9] = i18n.T(c, i18n.MsgPayableBillChannelDisabled), i18n.T(c, i18n.MsgPayableBillChannelAutoDisabled)
			default:
				cells[8] = i18n.T(c, i18n.MsgPayableBillChannelStatusUnknown)
			}
		}
		cells[10] = usage.RemoteChannelGroup
		if usage.DeltaQuota != nil {
			cells[11] = model.ReceivableAmount(*usage.DeltaQuota, usage.QuotaPerUnit).Round(6).InexactFloat64()
		}
		cells[12] = model.ReceivableAmount(usage.UsedQuota, usage.QuotaPerUnit).Round(6).InexactFloat64()
		if key, ok := deltaKeys[usage.DeltaFlag]; ok {
			cells[13] = i18n.T(c, key)
		}
		if usage.ProviderId == 0 {
			cells[14] = i18n.T(c, i18n.MsgPayableBillProviderUnknown)
		} else {
			cells[14] = usage.ProviderName
		}
		sourceKey, ok := sourceKeys[usage.ProviderSource]
		if !ok {
			sourceKey = i18n.MsgPayableBillSourceUnknown
		}
		cells[15] = i18n.T(c, sourceKey)
		if usage.SyncedAt != 0 {
			cells[16] = time.Unix(usage.SyncedAt, 0).In(model.ReceivableBillLocation).Format("2006-01-02 15:04:05")
		}
		// Counted only when the pull is complete and the site-day is not settled by usage details, as in the provider bills.
		cells[17] = i18n.T(c, i18n.MsgPayableBillCountedNo)
		if usage.Counted && !usage.UsageSettled {
			cells[17] = i18n.T(c, i18n.MsgPayableBillCountedYes)
		}
		rows[i] = cells
	}
	writePayableBillExport(c, "payable-channel-usages.xlsx", headerKeys, rows, []int{12, 13})
}

// ExportPayableProviderBills writes the provider bills that match the list filter into an xlsx file.
func ExportPayableProviderBills(c *gin.Context) {
	filter, ok := parsePayableUsageFilter(c)
	if !ok {
		return
	}
	bills, _, err := model.ListPayableProviderBills(filter, 0, 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	headerKeys := []string{
		i18n.MsgPayableBillExportBillDate, i18n.MsgPayableBillExportProvider, i18n.MsgPayableBillExportAmount,
		i18n.MsgPayableBillExportDiscount, i18n.MsgPayableBillExportPayableAmount, i18n.MsgPayableBillExportNote,
	}
	rows := make([][]any, len(bills))
	for i, bill := range bills {
		cells := make([]any, len(headerKeys))
		date, _ := model.ReceivableBillDayStart(bill.BillDate)
		cells[0] = date.Format("2006-01-02")
		cells[1] = bill.ProviderName
		if bill.ProviderId == 0 {
			cells[1] = i18n.T(c, i18n.MsgPayableBillProviderUnknown)
		}
		cells[2] = model.ReceivableAmount(bill.DeltaQuota, bill.QuotaPerUnit).Round(6).InexactFloat64()
		// The page checks "mixed" before "missing", and so does the export.
		switch {
		case bill.DiscountMixed:
			cells[3] = i18n.T(c, i18n.MsgPayableBillDiscountMixed)
		case bill.DiscountMissing:
			cells[3] = i18n.T(c, i18n.MsgPayableBillDiscountMissing)
		case bill.Discount != nil:
			cells[3] = *bill.Discount
		}
		if bill.PayableAmount != nil {
			cells[4] = bill.PayableAmount.Round(6).InexactFloat64()
		}
		// Like the "incomplete" mark on the page: rows without a discount add nothing to the payable amount.
		if bill.DiscountMissing {
			cells[5] = i18n.T(c, i18n.MsgPayableBillPayableIncomplete)
		}
		rows[i] = cells
	}
	writePayableBillExport(c, "payable-bills.xlsx", headerKeys, rows, []int{3, 5})
}
