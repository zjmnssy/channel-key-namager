package model

import (
	"errors"
	"maps"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const (
	ReceivableBillStatusSuccess  = "success"
	ReceivableBillStatusRetrying = "retrying"
	ReceivableBillStatusFailed   = "failed"

	ReceivableBillQuotaPerUnit int64 = 500000
	ReceivableBillLeaseSeconds int64 = 300
)

var (
	ErrReceivableBillNotFound    = errors.New("Bill does not exist")
	ErrReceivableBillSiteDeleted = errors.New("The site has been deleted")
	ErrReceivableBillSyncing     = errors.New("This bill is being synced, please try again later")
)

// ReceivableBillLocation 是应收账单使用的北京时区，后端不得用 time.Local。
var ReceivableBillLocation = time.FixedZone("UTC+8", 8*60*60)

// ReceivableBill 是某站点某个北京日期的日账单。
type ReceivableBill struct {
	Id                 int     `json:"id"`
	SiteId             int     `json:"site_id" gorm:"not null;uniqueIndex:idx_receivable_bills_site_date,priority:1"`
	BillDate           int     `json:"bill_date" gorm:"not null;uniqueIndex:idx_receivable_bills_site_date,priority:2;index:idx_receivable_bills_bill_date"`
	SiteName           string  `json:"site_name" gorm:"size:128"`
	SiteBaseURL        string  `json:"site_base_url" gorm:"column:site_base_url;size:1024"`
	Quota              int64   `json:"quota"`
	QuotaPerUnit       int64   `json:"quota_per_unit"`
	ReceivableDiscount float64 `json:"receivable_discount"`
	Status             string  `json:"status" gorm:"size:16;index:idx_receivable_bills_retry,priority:1"`
	Attempts           int     `json:"attempts"`
	LastError          string  `json:"-" gorm:"type:text"` // common.Message 的 JSON，没有错误时为 ""
	SyncedAt           int64   `json:"synced_at"`
	NextRetryAt        int64   `json:"next_retry_at" gorm:"index:idx_receivable_bills_retry,priority:2"`
	LeaseUntil         int64   `json:"-"`
	CreatedTime        int64   `json:"created_time"`
	UpdatedTime        int64   `json:"updated_time"`
}

// ReceivableBillHour 是某站点某个整点小时（Unix 秒）的小时数据。
type ReceivableBillHour struct {
	Id           int    `json:"id"`
	SiteId       int    `json:"site_id" gorm:"not null;uniqueIndex:idx_receivable_bill_hours_site_hour,priority:1"`
	BillHour     int64  `json:"bill_hour" gorm:"not null;uniqueIndex:idx_receivable_bill_hours_site_hour,priority:2;index:idx_receivable_bill_hours_bill_hour"`
	Quota        int64  `json:"quota"`
	QuotaPerUnit int64  `json:"quota_per_unit"`
	Status       string `json:"status" gorm:"size:16;index:idx_receivable_bill_hours_retry,priority:1"`
	Attempts     int    `json:"attempts"`
	LastError    string `json:"-" gorm:"type:text"` // common.Message 的 JSON，没有错误时为 ""
	SyncedAt     int64  `json:"synced_at"`
	NextRetryAt  int64  `json:"next_retry_at" gorm:"index:idx_receivable_bill_hours_retry,priority:2"`
	LeaseUntil   int64  `json:"-"`
	CreatedTime  int64  `json:"created_time"`
	UpdatedTime  int64  `json:"updated_time"`
}

type ReceivableRow interface {
	ReceivableBill | ReceivableBillHour
}

type ReceivableCoverage struct {
	FirstPeriod int64
	PeriodCount int64
}

// ReceivableBillDate 返回 t 的北京日期 YYYYMMDD。
func ReceivableBillDate(t time.Time) int {
	year, month, day := t.In(ReceivableBillLocation).Date()
	return year*10000 + int(month)*100 + day
}

// ReceivableBillDayStart 返回 YYYYMMDD 日期的北京 0 点，不是真实日期时返回 false。
func ReceivableBillDayStart(date int) (time.Time, bool) {
	year, month, day := date/10000, time.Month(date/100%100), date%100
	if year < 1000 || year > 9999 {
		return time.Time{}, false
	}
	start := time.Date(year, month, day, 0, 0, 0, 0, ReceivableBillLocation)
	if start.Year() != year || start.Month() != month || start.Day() != day {
		return time.Time{}, false
	}
	return start, true
}

// ReceivableAmount 把额度换算为金额，quotaPerUnit 不为正时按 ReceivableBillQuotaPerUnit 计算。
func ReceivableAmount(quota, quotaPerUnit int64) decimal.Decimal {
	if quotaPerUnit <= 0 {
		quotaPerUnit = ReceivableBillQuotaPerUnit
	}
	return decimal.NewFromInt(quota).Div(decimal.NewFromInt(quotaPerUnit))
}

// ReceivableAmountAfterDiscount 返回应收金额：额度换算的金额（折前）× 应收折扣，不做舍入。
func ReceivableAmountAfterDiscount(quota, quotaPerUnit int64, discount float64) decimal.Decimal {
	return ReceivableAmount(quota, quotaPerUnit).Mul(decimal.NewFromFloat(discount))
}

// ClaimReceivableRow 认领一行的同步租约，只有租约已过期且本次抢到时 ok 才为 true。
func ClaimReceivableRow[T ReceivableRow](id int, now int64) (lease int64, ok bool, err error) {
	lease = now + ReceivableBillLeaseSeconds
	result := DB.Model(new(T)).Where("id = ? AND lease_until < ?", id, now).Update("lease_until", lease)
	if result.Error != nil {
		return 0, false, result.Error
	}
	return lease, result.RowsAffected == 1, nil
}

// ClaimDueReceivableRow 是扫描用的认领：除租约外还要求这一行仍在重试中且已到重试时间。
// 扫描读到到期行与认领之间，手动重新同步可能已让它变成成功，而只有手动重新同步能让成功行离开成功。
func ClaimDueReceivableRow[T ReceivableRow](id int, now int64) (lease int64, ok bool, err error) {
	lease = now + ReceivableBillLeaseSeconds
	result := DB.Model(new(T)).
		Where("id = ? AND lease_until < ? AND status = ? AND next_retry_at <= ?", id, now, ReceivableBillStatusRetrying, now).
		Update("lease_until", lease)
	if result.Error != nil {
		return 0, false, result.Error
	}
	return lease, result.RowsAffected == 1, nil
}

// FinishReceivableRow 在仍持有租约时写入 updates 并释放租约，返回是否写入。
// 日账单写成成功时同一事务结算（settlePayableUsageDay）：租约已失效、什么都没写时不结算；结算失败时这次写入一起回滚，
// 日账单保持原状，租约到期后由原有的重试逻辑处理。
func FinishReceivableRow[T ReceivableRow](id int, lease int64, updates map[string]any) (bool, error) {
	now := common.GetTimestamp()
	values := make(map[string]any, len(updates)+2)
	maps.Copy(values, updates)
	values["lease_until"] = int64(0)
	values["updated_time"] = now
	_, isDaily := any(new(T)).(*ReceivableBill)
	if !isDaily || updates["status"] != ReceivableBillStatusSuccess {
		result := DB.Model(new(T)).Where("id = ? AND lease_until = ?", id, lease).Updates(values)
		if result.Error != nil {
			return false, result.Error
		}
		return result.RowsAffected == 1, nil
	}
	finished := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&ReceivableBill{}).Where("id = ? AND lease_until = ?", id, lease).Updates(values)
		if result.Error != nil || result.RowsAffected != 1 {
			return result.Error
		}
		var bill ReceivableBill
		if err := tx.Select("site_id", "bill_date", "quota", "quota_per_unit").Where("id = ?", id).First(&bill).Error; err != nil {
			return err
		}
		finished = true
		return settlePayableUsageDay(tx, bill.SiteId, bill.BillDate, bill.Quota, bill.QuotaPerUnit, now)
	})
	if err != nil {
		return false, err
	}
	return finished, nil
}

// InsertReceivableRow 插入一行，(site_id, 周期) 已存在时什么都不做。
// 日账单写成成功时同一事务结算（settlePayableUsageDay）：这一行已被别处插入、什么都没写时不结算，由写入的一方负责；
// 结算失败时这一行也不插入，由之后的扫描再补。
func InsertReceivableRow[T ReceivableRow](row *T) error {
	bill, isDaily := any(row).(*ReceivableBill)
	if !isDaily || bill.Status != ReceivableBillStatusSuccess {
		return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(bill)
		if result.Error != nil || result.RowsAffected != 1 {
			return result.Error
		}
		return settlePayableUsageDay(tx, bill.SiteId, bill.BillDate, bill.Quota, bill.QuotaPerUnit, common.GetTimestamp())
	})
}

// receivablePeriodColumn 返回 T 存放周期的列名。
func receivablePeriodColumn[T ReceivableRow]() string {
	if _, isDaily := any(new(T)).(*ReceivableBill); isDaily {
		return "bill_date"
	}
	return "bill_hour"
}

// ReceivableRowCoverage 返回每个站点的最早周期和行数。
func ReceivableRowCoverage[T ReceivableRow]() (map[int]ReceivableCoverage, error) {
	periodColumn := receivablePeriodColumn[T]()
	var rows []struct {
		SiteId      int
		FirstPeriod int64
		PeriodCount int64
	}
	err := DB.Model(new(T)).Select("site_id, MIN(" + periodColumn + ") AS first_period, COUNT(*) AS period_count").
		Group("site_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	coverage := make(map[int]ReceivableCoverage, len(rows))
	for _, row := range rows {
		coverage[row.SiteId] = ReceivableCoverage{FirstPeriod: row.FirstPeriod, PeriodCount: row.PeriodCount}
	}
	return coverage, nil
}

// ListReceivableSyncSites 返回需要同步账单的站点：未删除、类型为 NewAPI，带 ApiKey，读取时不写 SQL 日志。
func ListReceivableSyncSites() ([]*ThirdPartySite, error) {
	var sites []*ThirdPartySite
	err := DB.Session(&gorm.Session{Logger: logger.Discard}).Where("type = ?", ThirdPartySiteTypeNewAPI).Order("id").Find(&sites).Error
	return sites, err
}

// ReceivableRowPeriods 返回站点在 [from, to] 内已有行的周期，升序。
func ReceivableRowPeriods[T ReceivableRow](siteID int, from, to int64) ([]int64, error) {
	periodColumn := receivablePeriodColumn[T]()
	var periods []int64
	err := DB.Model(new(T)).Where("site_id = ? AND "+periodColumn+" >= ? AND "+periodColumn+" <= ?", siteID, from, to).
		Order(periodColumn).Pluck(periodColumn, &periods).Error
	return periods, err
}

// DueReceivableRows 返回站点已到重试时间、且没有人在同步的重试中的行，按下次重试时间、id 升序，最多 limit 行。
func DueReceivableRows[T ReceivableRow](siteID int, now int64, limit int) ([]T, error) {
	var rows []T
	err := DB.Where("site_id = ? AND status = ? AND next_retry_at <= ? AND lease_until < ?", siteID, ReceivableBillStatusRetrying, now, now).
		Order("next_retry_at, id").Limit(limit).Find(&rows).Error
	return rows, err
}

// FailReceivableRowsOfDeletedSites 把已删除站点在两张表中重试中的行改为失败，原因是站点已删除。
// 每轮扫描也会先调用它：删除站点时仍在进行的同步把重试中写了回来，下一轮会再改为失败。
func FailReceivableRowsOfDeletedSites(tx *gorm.DB) error {
	lastError, err := common.Marshal(common.NewMessage(ErrReceivableBillSiteDeleted.Error()))
	if err != nil {
		return err
	}
	updates := map[string]any{
		"status":        ReceivableBillStatusFailed,
		"last_error":    string(lastError),
		"next_retry_at": 0,
		"updated_time":  common.GetTimestamp(),
	}
	for _, table := range []any{&ReceivableBill{}, &ReceivableBillHour{}} {
		// 用 IN 已删除的站点，不用 NOT IN 在线的站点：没有在线站点时 GORM 会生成 NOT IN (NULL)，一行都不匹配。
		deletedSites := tx.Unscoped().Model(&ThirdPartySite{}).Select("id").Where("deleted_at IS NOT NULL")
		err := tx.Model(table).Where("status = ? AND site_id IN (?)", ReceivableBillStatusRetrying, deletedSites).Updates(updates).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// RetryFailedReceivableHoursOfDay 把站点在北京日 dayStart（Unix 秒）内失败的小时行改为重试中、立即重试，尝试次数重新计。
// 重新同步某天的账单时，这一天里失败的小时数据也一并重试。
func RetryFailedReceivableHoursOfDay(siteID int, dayStart, now int64) error {
	updates := map[string]any{
		"status":        ReceivableBillStatusRetrying,
		"attempts":      0,
		"next_retry_at": 0,
		"updated_time":  now,
	}
	return DB.Model(&ReceivableBillHour{}).
		Where("site_id = ? AND status = ? AND bill_hour >= ? AND bill_hour < ?",
			siteID, ReceivableBillStatusFailed, dayStart, dayStart+int64(24*60*60)).
		Updates(updates).Error
}

// retryFailedReceivableRowsOfSite 把站点在两张表中失败的行改为重试中、立即重试，尝试次数重新计。
// 编辑站点多半修好了失败的原因（ApiKey、地址或权限）。
func retryFailedReceivableRowsOfSite(tx *gorm.DB, siteID int, now int64) error {
	updates := map[string]any{
		"status":        ReceivableBillStatusRetrying,
		"attempts":      0,
		"next_retry_at": 0,
		"updated_time":  now,
	}
	for _, table := range []any{&ReceivableBill{}, &ReceivableBillHour{}} {
		err := tx.Model(table).Where("site_id = ? AND status = ?", siteID, ReceivableBillStatusFailed).Updates(updates).Error
		if err != nil {
			return err
		}
	}
	return nil
}
