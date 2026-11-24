package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	receivableBillMaxConcurrentSites = 4
	// receivableBillRowsPerSite bounds the attempts one site gets in a scan.
	receivableBillRowsPerSite = 60
	// receivableBillIssueTime is how long after Beijing midnight yesterday's bill falls due.
	receivableBillIssueTime = time.Hour
	// receivableBillHourDelay is how long after an hour ends it is read, leaving the
	// site time to write the hour's logs: a successful hour is never read again.
	receivableBillHourDelay  = 5 * time.Minute
	receivableBillMaxBackoff = 30 * time.Minute
	// receivableBillDay is the length of a Beijing day, which has no daylight saving.
	receivableBillDay = 24 * time.Hour
)

// RunReceivableBillScan makes one scan at now and returns once every site is done.
// It first fails the retrying rows of deleted sites, then gives every live NewAPI
// site its turn, at most receivableBillMaxConcurrentSites at a time.
func RunReceivableBillScan(ctx context.Context, now time.Time) {
	defer func() {
		if recovered := recover(); recovered != nil {
			common.SysError(common.LogText("receivable bill scan panicked: %v", recovered))
		}
	}()

	if err := model.FailReceivableRowsOfDeletedSites(model.DB); err != nil {
		common.SysError(common.LogText("receivable bill scan could not fail the rows of deleted sites: %v", err))
	}
	sites, err := model.ListReceivableSyncSites()
	var daily, hourly map[int]model.ReceivableCoverage
	if err == nil {
		daily, err = model.ReceivableRowCoverage[model.ReceivableBill]()
	}
	if err == nil {
		hourly, err = model.ReceivableRowCoverage[model.ReceivableBillHour]()
	}
	if err != nil {
		common.SysError(common.LogText("receivable bill scan failed: %v", err))
		return
	}

	var wg sync.WaitGroup
	slots := make(chan struct{}, receivableBillMaxConcurrentSites)
	for _, site := range sites {
		slots <- struct{}{}
		wg.Add(1)
		gopool.Go(func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					common.SysError(common.LogText("receivable bill site sync panicked: site=%d error=%v", site.Id, recovered))
				}
				<-slots
				wg.Done()
			}()
			syncReceivableBillSite(ctx, now, site, daily[site.Id], hourly[site.Id])
		})
	}
	wg.Wait()
}

// receivableSiteTurn is one site's turn in a scan. It makes at most
// receivableBillRowsPerSite attempts and ends at the first failed one, so a broken
// site gets one failing request per scan and cannot hold the scan up with one
// timeout after another. What is left waits for the next scan.
type receivableSiteTurn struct {
	ctx    context.Context
	now    int64
	site   *model.ThirdPartySite
	client *ChannelPushClient
	left   int // attempts the turn may still make
}

// syncReceivableBillSite gives one site its turn: missing days, due daily retries,
// missing hours, then due hourly retries, each in ascending order.
func syncReceivableBillSite(ctx context.Context, now time.Time, site *model.ThirdPartySite, daily, hourly model.ReceivableCoverage) {
	turn := &receivableSiteTurn{
		ctx:    ctx,
		now:    now.Unix(),
		site:   site,
		client: NewChannelPushClient(site, ReceivableBillClientOptions),
		left:   receivableBillRowsPerSite,
	}
	today := receivableBeijingDay(now)
	createdDay := today // a site without a creation time counts as created today
	if site.CreatedTime > 0 {
		createdDay = receivableBeijingDay(time.Unix(site.CreatedTime, 0))
	}

	lastDay := receivableLatestBillDay(now)
	// Bills start with the site's first bill. A site without one starts with the later
	// of its creation day and the latest day due, so days before this feature are not
	// filled in.
	firstDay := lastDay
	if createdDay.After(lastDay) {
		firstDay = createdDay
	}
	if daily.PeriodCount > 0 {
		start, ok := model.ReceivableBillDayStart(int(daily.FirstPeriod))
		if !ok {
			turn.fail(fmt.Errorf("invalid bill date %d", daily.FirstPeriod))
			return
		}
		firstDay = start
	}

	// Hours run from the site's first hour, or else from the later of its creation day
	// and today, to the last hour that ended at least receivableBillHourDelay ago.
	lastHour := receivableLatestBillHour(now)
	firstHour := today
	if createdDay.After(today) {
		firstHour = createdDay
	}
	if hourly.PeriodCount > 0 {
		firstHour = time.Unix(hourly.FirstPeriod, 0)
	}

	if turn.syncDays(firstDay, lastDay, daily.PeriodCount) {
		turn.syncHours(firstHour, lastHour, hourly.PeriodCount)
	}
}

// receivableLatestBillDay returns the latest day whose bill is due at now. Yesterday's
// bill falls due at 1:00; before that the latest due is the day before's.
func receivableLatestBillDay(now time.Time) time.Time {
	today := receivableBeijingDay(now)
	lastDay := today.AddDate(0, 0, -1)
	if now.Before(today.Add(receivableBillIssueTime)) {
		lastDay = lastDay.AddDate(0, 0, -1)
	}
	return lastDay
}

// receivableLatestBillHour returns the latest hour that ended at least
// receivableBillHourDelay before now.
func receivableLatestBillHour(now time.Time) time.Time {
	return now.Add(-time.Hour - receivableBillHourDelay).Truncate(time.Hour)
}

// receivableBeijingDay returns the Beijing midnight that starts t's day.
func receivableBeijingDay(t time.Time) time.Time {
	t = t.In(model.ReceivableBillLocation)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, model.ReceivableBillLocation)
}

// syncDays inserts the bills of the days from firstDay to lastDay that have none yet,
// then retries the due bills. rows is how many bills the site has. It reports
// whether the turn goes on.
func (turn *receivableSiteTurn) syncDays(firstDay, lastDay time.Time, rows int64) bool {
	// A site with a bill for every day from its first one has none missing.
	if !firstDay.After(lastDay) && rows != int64(lastDay.Sub(firstDay)/receivableBillDay)+1 {
		dates, err := model.ReceivableRowPeriods[model.ReceivableBill](turn.site.Id,
			int64(model.ReceivableBillDate(firstDay)), int64(model.ReceivableBillDate(lastDay)))
		if err != nil {
			return turn.fail(err)
		}
		for day := firstDay; !day.After(lastDay); day = day.AddDate(0, 0, 1) {
			if _, found := slices.BinarySearch(dates, int64(model.ReceivableBillDate(day))); found {
				continue
			}
			if !turn.insertDay(day) {
				return false
			}
		}
	}

	bills, err := model.DueReceivableRows[model.ReceivableBill](turn.site.Id, turn.now, turn.left)
	if err != nil {
		return turn.fail(err)
	}
	for _, bill := range bills {
		if !turn.retryDay(bill) {
			return false
		}
	}
	return true
}

// syncHours inserts the hours from firstHour to lastHour that have no row yet, then
// retries the due hours. rows is how many hours the site has. It reports whether
// the turn goes on.
func (turn *receivableSiteTurn) syncHours(firstHour, lastHour time.Time, rows int64) bool {
	if !firstHour.After(lastHour) && rows != int64(lastHour.Sub(firstHour)/time.Hour)+1 {
		hours, err := model.ReceivableRowPeriods[model.ReceivableBillHour](turn.site.Id, firstHour.Unix(), lastHour.Unix())
		if err != nil {
			return turn.fail(err)
		}
		for hour := firstHour; !hour.After(lastHour); hour = hour.Add(time.Hour) {
			if _, found := slices.BinarySearch(hours, hour.Unix()); found {
				continue
			}
			if !turn.insertHour(hour) {
				return false
			}
		}
	}

	due, err := model.DueReceivableRows[model.ReceivableBillHour](turn.site.Id, turn.now, turn.left)
	if err != nil {
		return turn.fail(err)
	}
	for _, row := range due {
		if !turn.retryHour(row) {
			return false
		}
	}
	return true
}

// insertDay reads a day that has no bill yet and inserts its bill. The first write
// takes the site's name and address even when it fails, so a bill that never
// succeeds still has a name to show.
func (turn *receivableSiteTurn) insertDay(day time.Time) bool {
	attempt := turn.read(day, receivableBillDay)
	result := newReceivableSyncResult(attempt, 1, turn.now)
	bill := &model.ReceivableBill{
		SiteId:       turn.site.Id,
		BillDate:     model.ReceivableBillDate(day),
		SiteName:     turn.site.Name,
		SiteBaseURL:  turn.site.BaseURL,
		Quota:        result.Quota,
		QuotaPerUnit: result.QuotaPerUnit,
		Status:       result.Status,
		Attempts:     result.Attempts,
		LastError:    result.LastError,
		SyncedAt:     result.SyncedAt,
		NextRetryAt:  result.NextRetryAt,
		CreatedTime:  turn.now,
		UpdatedTime:  turn.now,
	}
	if result.Status == model.ReceivableBillStatusSuccess {
		bill.ReceivableDiscount = turn.site.ReceivableDiscount
	}
	if err := model.InsertReceivableRow(bill); err != nil {
		return turn.fail(err)
	}
	return turn.proceed(attempt)
}

// retryDay retries a due bill once it holds the bill's lease; a bill someone else
// is syncing is left to them.
func (turn *receivableSiteTurn) retryDay(bill model.ReceivableBill) bool {
	day, ok := model.ReceivableBillDayStart(bill.BillDate)
	if !ok {
		return turn.fail(fmt.Errorf("bill %d has invalid date %d", bill.Id, bill.BillDate))
	}
	lease, claimed, err := model.ClaimDueReceivableRow[model.ReceivableBill](bill.Id, turn.now)
	if err != nil {
		return turn.fail(err)
	}
	if !claimed {
		return true
	}
	attempt := turn.read(day, receivableBillDay)
	result := newReceivableSyncResult(attempt, bill.Attempts+1, turn.now)
	if _, err := model.FinishReceivableRow[model.ReceivableBill](bill.Id, lease, receivableBillUpdates(result, turn.site)); err != nil {
		return turn.fail(err)
	}
	return turn.proceed(attempt)
}

// insertHour reads an hour that has no row yet and inserts its row.
func (turn *receivableSiteTurn) insertHour(hour time.Time) bool {
	attempt := turn.read(hour, time.Hour)
	result := newReceivableSyncResult(attempt, 1, turn.now)
	err := model.InsertReceivableRow(&model.ReceivableBillHour{
		SiteId:       turn.site.Id,
		BillHour:     hour.Unix(),
		Quota:        result.Quota,
		QuotaPerUnit: result.QuotaPerUnit,
		Status:       result.Status,
		Attempts:     result.Attempts,
		LastError:    result.LastError,
		SyncedAt:     result.SyncedAt,
		NextRetryAt:  result.NextRetryAt,
		CreatedTime:  turn.now,
		UpdatedTime:  turn.now,
	})
	if err != nil {
		return turn.fail(err)
	}
	return turn.proceed(attempt)
}

// retryHour retries a due hour once it holds the row's lease; a row someone else is
// syncing is left to them.
func (turn *receivableSiteTurn) retryHour(row model.ReceivableBillHour) bool {
	lease, claimed, err := model.ClaimDueReceivableRow[model.ReceivableBillHour](row.Id, turn.now)
	if err != nil {
		return turn.fail(err)
	}
	if !claimed {
		return true
	}
	attempt := turn.read(time.Unix(row.BillHour, 0), time.Hour)
	result := newReceivableSyncResult(attempt, row.Attempts+1, turn.now)
	if _, err := model.FinishReceivableRow[model.ReceivableBillHour](row.Id, lease, result.updates()); err != nil {
		return turn.fail(err)
	}
	return turn.proceed(attempt)
}

// read sends the one request of an attempt for the window [start, start+length],
// counted against the turn.
func (turn *receivableSiteTurn) read(start time.Time, length time.Duration) ReceivableStatAttempt {
	turn.left--
	return turn.client.ReadLogStatQuota(turn.ctx, start.Unix(), start.Add(length).Unix())
}

// proceed reports whether the turn goes on after a saved attempt: not after a failed
// one, retryable or not, and not once the turn has made all its attempts.
func (turn *receivableSiteTurn) proceed(attempt ReceivableStatAttempt) bool {
	return attempt.Failure == nil && turn.left > 0
}

// fail logs a database error, which ends the turn.
func (turn *receivableSiteTurn) fail(err error) bool {
	common.SysError(common.LogText("receivable bill sync failed: site=%d error=%v", turn.site.Id, err))
	return false
}

// receivableSyncResult is what one attempt writes to its row.
type receivableSyncResult struct {
	Status       string
	Quota        int64 // Quota and QuotaPerUnit are set on success only
	QuotaPerUnit int64
	Attempts     int
	LastError    string // the failure message as JSON, "" on success
	SyncedAt     int64
	NextRetryAt  int64 // 0 unless retrying
}

// newReceivableSyncResult turns one read of the site at now into the new state of
// its row. attempts counts this attempt and sets the backoff of a retryable failure.
func newReceivableSyncResult(attempt ReceivableStatAttempt, attempts int, now int64) receivableSyncResult {
	result := receivableSyncResult{Attempts: attempts, SyncedAt: now}
	if attempt.Failure == nil {
		result.Status = model.ReceivableBillStatusSuccess
		result.Quota = attempt.Quota
		result.QuotaPerUnit = model.ReceivableBillQuotaPerUnit
		return result
	}
	// A message with string params always encodes.
	lastError, _ := common.Marshal(attempt.Failure)
	result.LastError = string(lastError)
	if !attempt.Retryable {
		result.Status = model.ReceivableBillStatusFailed
		return result
	}
	result.Status = model.ReceivableBillStatusRetrying
	result.NextRetryAt = now + int64(syncRetryDelay(attempts, attempt.RetryAfter)/time.Second)
	return result
}

// syncRetryDelay is how long the n-th failure in a row waits: 1, 2, 4, 8, 16, then
// 30 minutes, and at least as long as a 429 asked for.
func syncRetryDelay(attempts int, retryAfter time.Duration) time.Duration {
	return max(min(time.Minute<<min(max(attempts-1, 0), 5), receivableBillMaxBackoff), retryAfter)
}

// updates returns the result as the columns of an existing row. A failure leaves the
// quota columns alone, so a row that fails after a success keeps its last good
// values; only a success shows an amount.
func (result receivableSyncResult) updates() map[string]any {
	updates := map[string]any{
		"status":        result.Status,
		"attempts":      result.Attempts,
		"last_error":    result.LastError,
		"synced_at":     result.SyncedAt,
		"next_retry_at": result.NextRetryAt,
	}
	if result.Status == model.ReceivableBillStatusSuccess {
		updates["quota"] = result.Quota
		updates["quota_per_unit"] = result.QuotaPerUnit
	}
	return updates
}

// receivableBillUpdates returns the columns a daily bill takes from result; a
// success also takes the site's current name, address and discount.
func receivableBillUpdates(result receivableSyncResult, site *model.ThirdPartySite) map[string]any {
	updates := result.updates()
	if result.Status == model.ReceivableBillStatusSuccess {
		updates["site_name"] = site.Name
		updates["site_base_url"] = site.BaseURL
		updates["receivable_discount"] = site.ReceivableDiscount
	}
	return updates
}

// ResyncReceivableBill 手动重新同步一张日账单：认领该行，向站点读取一次，把结果写回，
// 再让同一天里失败的小时数据重新重试。尝试次数从 1 重新计，成功时取站点当前的名称、地址和折扣。
// before 是认领成功后重读的行，审计据此记录被覆盖的状态和额度；认领前的错误带着认领前读到的行。
// 认领后重读失败时直接返回错误，租约 300 秒后自然过期。after 只在结果已写回时有值，
// 结果可以是成功，也可以是重试中或失败。
func ResyncReceivableBill(ctx context.Context, billID int) (before model.ReceivableBill, after *model.ReceivableBill, err error) {
	err = model.DB.First(&before, billID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return before, nil, model.ErrReceivableBillNotFound
	}
	if err != nil {
		return before, nil, err
	}
	site, err := model.GetChannelPushSite(before.SiteId)
	if errors.Is(err, model.ErrThirdPartySiteNotFound) {
		return before, nil, model.ErrReceivableBillSiteDeleted
	}
	if err != nil {
		return before, nil, err
	}
	day, ok := model.ReceivableBillDayStart(before.BillDate)
	if !ok {
		return before, nil, fmt.Errorf("bill %d has invalid date %d", before.Id, before.BillDate)
	}

	now := time.Now().Unix()
	lease, claimed, err := model.ClaimReceivableRow[model.ReceivableBill](before.Id, now)
	if err != nil {
		return before, nil, err
	}
	if !claimed {
		return before, nil, model.ErrReceivableBillSyncing
	}
	// 认领前读到的行可能已被扫描写成成功，认领后重读才是将被覆盖的值。
	if err := model.DB.First(&before, before.Id).Error; err != nil {
		return before, nil, err
	}
	attempt := NewChannelPushClient(site, ReceivableBillClientOptions).ReadLogStatQuota(ctx, day.Unix(), day.Add(receivableBillDay).Unix())
	result := newReceivableSyncResult(attempt, 1, now)
	finished, err := model.FinishReceivableRow[model.ReceivableBill](before.Id, lease, receivableBillUpdates(result, site))
	if err != nil {
		return before, nil, err
	}
	if !finished {
		return before, nil, model.ErrReceivableBillSyncing
	}
	if err := model.RetryFailedReceivableHoursOfDay(site.Id, day.Unix(), now); err != nil {
		return before, nil, err
	}

	var stored model.ReceivableBill
	if err := model.DB.First(&stored, before.Id).Error; err != nil {
		return before, nil, err
	}
	return before, &stored, nil
}
