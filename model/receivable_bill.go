package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm/clause"
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

// ClaimReceivableRow 认领一行的同步租约，只有租约已过期且本次抢到时 ok 才为 true。
func ClaimReceivableRow[T ReceivableRow](id int, now int64) (lease int64, ok bool, err error) {
	lease = now + ReceivableBillLeaseSeconds
	result := DB.Model(new(T)).Where("id = ? AND lease_until < ?", id, now).Update("lease_until", lease)
	if result.Error != nil {
		return 0, false, result.Error
	}
	return lease, result.RowsAffected == 1, nil
}

// FinishReceivableRow 在仍持有租约时写入 updates 并释放租约，返回是否写入。
func FinishReceivableRow[T ReceivableRow](id int, lease int64, updates map[string]any) (bool, error) {
	values := make(map[string]any, len(updates)+2)
	for column, value := range updates {
		values[column] = value
	}
	values["lease_until"] = int64(0)
	values["updated_time"] = common.GetTimestamp()
	result := DB.Model(new(T)).Where("id = ? AND lease_until = ?", id, lease).Updates(values)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// InsertReceivableRow 插入一行，(site_id, 周期) 已存在时什么都不做。
func InsertReceivableRow[T ReceivableRow](row *T) error {
	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}

// ReceivableRowCoverage 返回每个站点的最早周期和行数。
func ReceivableRowCoverage[T ReceivableRow]() (map[int]ReceivableCoverage, error) {
	periodColumn := "bill_hour"
	if _, isDaily := any(new(T)).(*ReceivableBill); isDaily {
		periodColumn = "bill_date"
	}
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
