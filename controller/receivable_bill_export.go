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

const receivableBillExportContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// ExportReceivableBills writes the daily bills that match the list filter into an xlsx file, with
// the header and the status and failure texts in the language of the reader.
func ExportReceivableBills(c *gin.Context) {
	filter, ok := parseReceivableBillFilter(c)
	if !ok {
		return
	}
	bills, _, err := model.ListReceivableBills(filter, 0, 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	file := excelize.NewFile()
	defer func() { _ = file.Close() }()
	sheet := file.GetSheetName(0)
	amountStyle, err := file.NewStyle(&excelize.Style{CustomNumFmt: new("0.000000")})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	headers := []string{
		i18n.MsgReceivableBillExportBillDate, i18n.MsgReceivableBillExportSiteName, i18n.MsgReceivableBillExportSiteURL,
		i18n.MsgReceivableBillExportAmount, i18n.MsgReceivableBillExportDiscount, i18n.MsgReceivableBillExportStatus,
		i18n.MsgReceivableBillExportFailureReason, i18n.MsgReceivableBillExportSyncedAt,
	}
	for i, key := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := file.SetCellStr(sheet, cell, i18n.T(c, key)); err != nil {
			common.ApiError(c, err)
			return
		}
	}

	statusKeys := map[string]string{
		model.ReceivableBillStatusSuccess:  i18n.MsgReceivableBillStatusSuccess,
		model.ReceivableBillStatusRetrying: i18n.MsgReceivableBillStatusRetrying,
		model.ReceivableBillStatusFailed:   i18n.MsgReceivableBillStatusFailed,
	}
	for i, bill := range bills {
		row := i + 2
		cells := make([]any, len(headers))
		date, _ := model.ReceivableBillDayStart(bill.BillDate)
		cells[0] = date.Format("2006-01-02")
		cells[1], cells[2] = bill.SiteName, bill.SiteBaseURL
		if bill.Status == model.ReceivableBillStatusSuccess {
			cells[3] = model.ReceivableAmount(bill.Quota, bill.QuotaPerUnit).Round(6).InexactFloat64()
			cells[4] = bill.ReceivableDiscount
		}
		cells[5] = i18n.T(c, statusKeys[bill.Status])
		if message := newReceivableBillView(bill, false).Error; message != nil {
			switch message.Key {
			case "Site request failed: {{detail}}":
				detail, _ := message.Params["detail"].(string)
				cells[6] = i18n.T(c, i18n.MsgReceivableBillErrorRequestFailed, map[string]any{"Detail": detail})
			case "Site returned no valid quota":
				cells[6] = i18n.T(c, i18n.MsgReceivableBillErrorNoValidQuota)
			case "The site has been deleted":
				cells[6] = i18n.T(c, i18n.MsgReceivableBillErrorSiteDeleted)
			default:
				cells[6] = message.Key
			}
		}
		if bill.SyncedAt != 0 {
			cells[7] = time.Unix(bill.SyncedAt, 0).In(model.ReceivableBillLocation).Format("2006-01-02 15:04:05")
		}
		start, _ := excelize.CoordinatesToCellName(1, row)
		if err := file.SetSheetRow(sheet, start, &cells); err != nil {
			common.ApiError(c, err)
			return
		}
		if bill.Status == model.ReceivableBillStatusSuccess {
			cell, _ := excelize.CoordinatesToCellName(4, row)
			if err := file.SetCellStyle(sheet, cell, cell, amountStyle); err != nil {
				common.ApiError(c, err)
				return
			}
		}
	}

	c.Header("Content-Type", receivableBillExportContentType)
	c.Header("Content-Disposition", `attachment; filename="receivable-bills.xlsx"`)
	c.Status(http.StatusOK)
	if err := file.Write(c.Writer); err != nil {
		common.SysError(common.LogText("Failed to write receivable bill export: %v", err))
	}
}
