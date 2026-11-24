package controller

import (
	"net/http"
	"strconv"
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

	deltaKeys := map[string]string{
		model.PayableDeltaFirstSync:  i18n.MsgPayableBillDeltaFirstSync,
		model.PayableDeltaNewChannel: i18n.MsgPayableBillDeltaNewChannel,
		model.PayableDeltaRollback:   i18n.MsgPayableBillDeltaRollback,
	}
	sourceKeys := map[string]string{
		model.PayableProviderManual:    i18n.MsgPayableBillSourceManual,
		model.PayableProviderParsed:    i18n.MsgPayableBillSourceParsed,
		model.PayableProviderInherited: i18n.MsgPayableBillSourceInherited,
		model.PayableProviderUnknown:   i18n.MsgPayableBillSourceUnknown,
	}
	rows := make([][]any, len(usages))
	for i, usage := range usages {
		cells := make([]any, 13)
		date, _ := model.ReceivableBillDayStart(usage.BillDate)
		cells[0], cells[1] = strconv.Itoa(usage.TaskId), date.Format("2006-01-02")
		cells[2], cells[3], cells[4], cells[5] = strconv.Itoa(usage.SiteId), usage.SiteName, usage.SiteBaseURL, usage.RemoteChannelName
		if usage.DeltaQuota != nil {
			cells[6] = model.ReceivableAmount(*usage.DeltaQuota, usage.QuotaPerUnit).Round(6).InexactFloat64()
		}
		cells[7] = model.ReceivableAmount(usage.UsedQuota, usage.QuotaPerUnit).Round(6).InexactFloat64()
		if key, ok := deltaKeys[usage.DeltaFlag]; ok {
			cells[8] = i18n.T(c, key)
		}
		if usage.ProviderId == 0 {
			cells[9] = i18n.T(c, i18n.MsgPayableBillProviderUnknown)
		} else {
			cells[9] = usage.ProviderName
		}
		sourceKey, ok := sourceKeys[usage.ProviderSource]
		if !ok {
			sourceKey = i18n.MsgPayableBillSourceUnknown
		}
		cells[10] = i18n.T(c, sourceKey)
		if usage.SyncedAt != 0 {
			cells[11] = time.Unix(usage.SyncedAt, 0).In(model.ReceivableBillLocation).Format("2006-01-02 15:04:05")
		}
		cells[12] = i18n.T(c, i18n.MsgPayableBillCountedNo)
		if usage.Counted {
			cells[12] = i18n.T(c, i18n.MsgPayableBillCountedYes)
		}
		rows[i] = cells
	}
	writePayableBillExport(c, "payable-channel-usages.xlsx", []string{
		i18n.MsgPayableBillExportTaskID, i18n.MsgPayableBillExportBillDate, i18n.MsgPayableBillExportSiteID,
		i18n.MsgPayableBillExportSiteName, i18n.MsgPayableBillExportSiteURL, i18n.MsgPayableBillExportChannelName,
		i18n.MsgPayableBillExportDeltaAmount, i18n.MsgPayableBillExportUsedAmount, i18n.MsgPayableBillExportDeltaNote,
		i18n.MsgPayableBillExportProvider, i18n.MsgPayableBillExportProviderSource, i18n.MsgPayableBillExportSyncedAt,
		i18n.MsgPayableBillExportCounted,
	}, rows, []int{7, 8})
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

	rows := make([][]any, len(bills))
	for i, bill := range bills {
		cells := make([]any, 5)
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
		rows[i] = cells
	}
	writePayableBillExport(c, "payable-bills.xlsx", []string{
		i18n.MsgPayableBillExportBillDate, i18n.MsgPayableBillExportProvider, i18n.MsgPayableBillExportAmount,
		i18n.MsgPayableBillExportDiscount, i18n.MsgPayableBillExportPayableAmount,
	}, rows, []int{3, 5})
}
