package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupReceivableBillTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupThirdPartySiteTest(t)
	originalOptions := service.ReceivableBillClientOptions
	service.ReceivableBillClientOptions = service.ChannelPushClientOptions{Pace: 0, Timeout: 2 * time.Second, MaxRetryAfter: 30 * time.Minute}
	t.Cleanup(func() { service.ReceivableBillClientOptions = originalOptions })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.ReceivableBill{}, &model.ReceivableBillHour{}, &model.AuditLog{}))
	clearTables := func() {
		for _, table := range []any{&model.ReceivableBillHour{}, &model.ReceivableBill{}, &model.AuditLog{}, &model.User{}} {
			// A fresh session per table: GORM reuses the Statement of a stored session, so every Delete would hit the first table.
			require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(table).Error)
		}
	}
	clearTables()
	t.Cleanup(clearTables)
	return db
}

func receivableSeedSite(t *testing.T, db *gorm.DB, name, baseURL string, created int64) *model.ThirdPartySite {
	t.Helper()
	site := channelPushSeedSite(t, db, name)
	require.NoError(t, db.Model(site).Updates(map[string]any{"base_url": baseURL, "created_time": created}).Error)
	site.BaseURL, site.CreatedTime = baseURL, created
	return site
}

func TestReceivableBillCalendar(t *testing.T) {
	// 1795363200 = 2026-11-22T16:00:00Z，即北京 11-23 0 点
	assert.Equal(t, 20261122, model.ReceivableBillDate(time.Unix(1795363199, 0).UTC()))
	assert.Equal(t, 20261123, model.ReceivableBillDate(time.Unix(1795363200, 0).UTC()))
	start, ok := model.ReceivableBillDayStart(20261121)
	require.True(t, ok)
	assert.Equal(t, int64(1795190400), start.Unix())
	for _, bad := range []int{20261131, 20260229, 2026113, 0} {
		_, ok := model.ReceivableBillDayStart(bad)
		assert.False(t, ok, bad)
	}
	assert.Equal(t, "123.456788", model.ReceivableAmount(61728394, 500000).StringFixed(6))
	assert.Equal(t, "1.000000", model.ReceivableAmount(500000, 0).StringFixed(6))
}

func TestReceivableBillRowsAreUniquePerSiteAndPeriod(t *testing.T) {
	db := setupReceivableBillTest(t)
	siteA := receivableSeedSite(t, db, "rb-a", "https://rb-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "rb-b", "https://rb-b.example.com", 1)

	require.NoError(t, model.InsertReceivableRow(&model.ReceivableBill{SiteId: siteA.Id, BillDate: 20261122, Quota: 1}))
	require.NoError(t, model.InsertReceivableRow(&model.ReceivableBill{SiteId: siteA.Id, BillDate: 20261121}))
	require.NoError(t, model.InsertReceivableRow(&model.ReceivableBill{SiteId: siteA.Id, BillDate: 20261122, Quota: 2}))
	require.NoError(t, model.InsertReceivableRow(&model.ReceivableBill{SiteId: siteB.Id, BillDate: 20261122}))
	var stored model.ReceivableBill
	require.NoError(t, db.Where("site_id = ? AND bill_date = ?", siteA.Id, 20261122).First(&stored).Error)
	assert.Equal(t, int64(1), stored.Quota)
	coverage, err := model.ReceivableRowCoverage[model.ReceivableBill]()
	require.NoError(t, err)
	assert.Equal(t, model.ReceivableCoverage{FirstPeriod: 20261121, PeriodCount: 2}, coverage[siteA.Id])
	assert.Equal(t, model.ReceivableCoverage{FirstPeriod: 20261122, PeriodCount: 1}, coverage[siteB.Id])

	require.NoError(t, model.InsertReceivableRow(&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: 1795363200, Quota: 1}))
	require.NoError(t, model.InsertReceivableRow(&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: 1795359600}))
	require.NoError(t, model.InsertReceivableRow(&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: 1795363200, Quota: 2}))
	require.NoError(t, model.InsertReceivableRow(&model.ReceivableBillHour{SiteId: siteB.Id, BillHour: 1795363200}))
	var storedHour model.ReceivableBillHour
	require.NoError(t, db.Where("site_id = ? AND bill_hour = ?", siteA.Id, 1795363200).First(&storedHour).Error)
	assert.Equal(t, int64(1), storedHour.Quota)
	hourCoverage, err := model.ReceivableRowCoverage[model.ReceivableBillHour]()
	require.NoError(t, err)
	assert.Equal(t, model.ReceivableCoverage{FirstPeriod: 1795359600, PeriodCount: 2}, hourCoverage[siteA.Id])
	assert.Equal(t, model.ReceivableCoverage{FirstPeriod: 1795363200, PeriodCount: 1}, hourCoverage[siteB.Id])
}

func TestReceivableBillLeaseAdmitsOneSyncAtATime(t *testing.T) {
	db := setupReceivableBillTest(t)
	site := receivableSeedSite(t, db, "rb-lease", "https://rb-lease.example.com", 1)
	row := &model.ReceivableBill{SiteId: site.Id, BillDate: 20261122, Status: model.ReceivableBillStatusRetrying}
	require.NoError(t, model.InsertReceivableRow(row))
	reload := func() model.ReceivableBill {
		var stored model.ReceivableBill
		require.NoError(t, db.First(&stored, row.Id).Error)
		return stored
	}

	lease, ok, err := model.ClaimReceivableRow[model.ReceivableBill](row.Id, 1000)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int64(1300), lease)
	_, ok, err = model.ClaimReceivableRow[model.ReceivableBill](row.Id, 1000)
	require.NoError(t, err)
	assert.False(t, ok)

	finished, err := model.FinishReceivableRow[model.ReceivableBill](row.Id, 1299, map[string]any{"status": model.ReceivableBillStatusSuccess})
	require.NoError(t, err)
	assert.False(t, finished)
	assert.Equal(t, model.ReceivableBillStatusRetrying, reload().Status)
	finished, err = model.FinishReceivableRow[model.ReceivableBill](row.Id, 1300, map[string]any{"status": model.ReceivableBillStatusSuccess})
	require.NoError(t, err)
	assert.True(t, finished)
	assert.Equal(t, int64(0), reload().LeaseUntil)
	assert.Equal(t, model.ReceivableBillStatusSuccess, reload().Status)

	_, ok, err = model.ClaimReceivableRow[model.ReceivableBill](row.Id, 1000)
	require.NoError(t, err)
	assert.True(t, ok)
	_, ok, err = model.ClaimReceivableRow[model.ReceivableBill](row.Id, 1300)
	require.NoError(t, err)
	assert.False(t, ok)
	lease, ok, err = model.ClaimReceivableRow[model.ReceivableBill](row.Id, 1301)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int64(1601), lease)
	finished, err = model.FinishReceivableRow[model.ReceivableBill](row.Id, 1300, map[string]any{"status": model.ReceivableBillStatusFailed})
	require.NoError(t, err)
	assert.False(t, finished)

	// 扫描只认领仍在重试且到期的行：并发的手动重新同步可能已让它变成成功，只有手动重新同步能让成功行离开成功
	for _, tc := range []struct {
		name        string
		status      string
		nextRetryAt int64
		claimed     bool
	}{
		{"成功", model.ReceivableBillStatusSuccess, 0, false},
		{"重试中但未到期", model.ReceivableBillStatusRetrying, 1001, false},
		{"重试中且到期", model.ReceivableBillStatusRetrying, 1000, true},
		{"失败", model.ReceivableBillStatusFailed, 0, false},
	} {
		require.NoError(t, db.Model(&model.ReceivableBill{}).Where("id = ?", row.Id).
			Updates(map[string]any{"status": tc.status, "next_retry_at": tc.nextRetryAt, "lease_until": 0}).Error)
		lease, ok, err := model.ClaimDueReceivableRow[model.ReceivableBill](row.Id, 1000)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.claimed, ok, tc.name)
		if tc.claimed {
			assert.Equal(t, int64(1300), lease, tc.name)
		}
	}
}

type fakeStatRequest struct {
	Method        string
	Path          string
	Query         url.Values
	Authorization string
	User          string
}

type fakeStatSite struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []fakeStatRequest
	respond  func(start, end int64) (status int, body string, header http.Header)
	// onRequest runs after the request is recorded and before respond; it may block.
	onRequest func(r *http.Request)
}

func newFakeStatSite(t *testing.T) *fakeStatSite {
	t.Helper()
	site := &fakeStatSite{respond: func(start, end int64) (int, string, http.Header) {
		return http.StatusOK, `{"success":true,"data":{"quota":500000}}`, nil
	}}
	site.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		start, _ := strconv.ParseInt(query.Get("start_timestamp"), 10, 64)
		end, _ := strconv.ParseInt(query.Get("end_timestamp"), 10, 64)
		site.mu.Lock()
		site.requests = append(site.requests, fakeStatRequest{
			Method: r.Method, Path: r.URL.Path, Query: query,
			Authorization: r.Header.Get("Authorization"), User: r.Header.Get("New-Api-User"),
		})
		respond, onRequest := site.respond, site.onRequest
		site.mu.Unlock()
		if onRequest != nil {
			onRequest(r)
		}
		status, body, header := respond(start, end)
		for name, values := range header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(site.server.Close)
	return site
}

// setRespond replaces the reply under the lock the handler reads it with.
func (s *fakeStatSite) setRespond(respond func(start, end int64) (int, string, http.Header)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.respond = respond
}

func (s *fakeStatSite) recorded() []fakeStatRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]fakeStatRequest(nil), s.requests...)
}

// windows returns the requested [start, end] pairs in request order.
func (s *fakeStatSite) windows() [][2]int64 {
	var windows [][2]int64
	for _, request := range s.recorded() {
		start, _ := strconv.ParseInt(request.Query.Get("start_timestamp"), 10, 64)
		end, _ := strconv.ParseInt(request.Query.Get("end_timestamp"), 10, 64)
		windows = append(windows, [2]int64{start, end})
	}
	return windows
}

func TestReceivableStatAttempt(t *testing.T) {
	const remoteFailure = "Site request failed: {{detail}}"
	const noQuota = "Site returned no valid quota"
	quotaBody := func(quota string) string { return `{"success":true,"data":{"quota":` + quota + `}}` }
	json200 := func(body string) func() (int, string, http.Header) {
		return func() (int, string, http.Header) { return http.StatusOK, body, nil }
	}
	tests := []struct {
		name      string
		reply     func() (int, string, http.Header)
		quota     int64
		key       string
		detail    string
		retryable bool
		wait      time.Duration
		cancel    string // "before" cancels the context before the call, "during" once the site has the request
	}{
		{name: "quota", reply: json200(quotaBody("61728394")), quota: 61728394},
		{name: "zero", reply: json200(quotaBody("0"))},
		{name: "refusal", reply: json200(`{"success":false,"message":"无权进行此操作"}`), key: remoteFailure, detail: "无权进行此操作"},
		{name: "fractional", reply: json200(quotaBody("1.5")), key: noQuota},
		{name: "negative", reply: json200(quotaBody("-1")), key: noQuota},
		{name: "string", reply: json200(quotaBody(`"12"`)), key: noQuota},
		{name: "missing", reply: json200(`{"success":true,"data":{}}`), key: noQuota},
		{name: "null data", reply: json200(`{"success":true,"data":null}`), key: noQuota},
		{name: "not json", reply: json200("<html>"), key: remoteFailure, detail: "invalid response body", retryable: true},
		{name: "bad gateway", reply: func() (int, string, http.Header) { return http.StatusBadGateway, "", nil }, key: remoteFailure, detail: "HTTP 502", retryable: true},
		{name: "rate limited", reply: func() (int, string, http.Header) {
			return http.StatusTooManyRequests, "", http.Header{"Retry-After": {"120"}}
		}, key: remoteFailure, detail: "HTTP 429", retryable: true, wait: 120 * time.Second},
		{name: "rate limit capped", reply: func() (int, string, http.Header) {
			return http.StatusTooManyRequests, "", http.Header{"Retry-After": {"7200"}}
		}, key: remoteFailure, detail: "HTTP 429", retryable: true, wait: 30 * time.Minute},
		{name: "unauthorized", reply: func() (int, string, http.Header) {
			return http.StatusUnauthorized, `{"success":false,"message":"未登录"}`, nil
		}, key: remoteFailure, detail: "未登录"},
		{name: "redirect", reply: func() (int, string, http.Header) {
			return http.StatusFound, "", http.Header{"Location": {"https://example.com"}}
		}, key: remoteFailure, detail: "HTTP 302"},
		{name: "context cancelled", cancel: "before", key: remoteFailure, retryable: true},
		{name: "cancelled mid request", cancel: "during", key: remoteFailure, retryable: true},
		{name: "long reason", reply: json200(`{"success":false,"message":"` + strings.Repeat("错", 600) + `"}`), key: remoteFailure, detail: strings.Repeat("错", 500)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupReceivableBillTest(t)
			fake := newFakeStatSite(t)
			if tt.reply != nil {
				fake.respond = func(start, end int64) (int, string, http.Header) { return tt.reply() }
			}
			site := receivableSeedSite(t, db, "rb-stat", fake.server.URL, 1)

			client := service.NewChannelPushClient(site, service.ChannelPushClientOptions{Timeout: 2 * time.Second, MaxRetryAfter: 30 * time.Minute})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch tt.cancel {
			case "before":
				cancel()
			case "during":
				fake.onRequest = func(r *http.Request) {
					cancel()
					select {
					case <-r.Context().Done():
					case <-time.After(5 * time.Second):
					}
				}
			}
			got := client.ReadLogStatQuota(ctx, 1795190400, 1795276800)

			assert.Equal(t, tt.quota, got.Quota)
			assert.Equal(t, tt.retryable, got.Retryable)
			assert.Equal(t, tt.wait, got.RetryAfter)
			if tt.key == "" {
				assert.Nil(t, got.Failure)
			} else {
				require.NotNil(t, got.Failure)
				assert.Equal(t, tt.key, got.Failure.Key)
				if tt.detail == "" && tt.cancel != "" {
					assert.NotEmpty(t, got.Failure.Params["detail"])
				} else if tt.detail == "" {
					assert.Empty(t, got.Failure.Params)
				} else {
					assert.Equal(t, tt.detail, got.Failure.Params["detail"])
				}
			}

			requests := fake.recorded()
			if tt.cancel == "before" {
				assert.Empty(t, requests)
				return
			}
			require.Len(t, requests, 1)
			assert.Equal(t, http.MethodGet, requests[0].Method)
			assert.Equal(t, "/api/log/stat", requests[0].Path)
			assert.Equal(t, url.Values{"p": {"1"}, "page_size": {"1"}, "start_timestamp": {"1795190400"}, "end_timestamp": {"1795276800"}}, requests[0].Query)
			assert.Equal(t, "Bearer remote-access-token", requests[0].Authorization)
			assert.Equal(t, "7", requests[0].User)
			assert.Equal(t, [][2]int64{{1795190400, 1795276800}}, fake.windows())
		})
	}
}

// receivableScan runs one scan at the Unix second now and returns the windows the
// fake site received during it. now is a UTC time, proving that nothing depends on
// the server's time zone.
func receivableScan(fake *fakeStatSite, now int64) [][2]int64 {
	before := len(fake.windows())
	service.RunReceivableBillScan(context.Background(), time.Unix(now, 0).UTC())
	return fake.windows()[before:]
}

// receivableDailyReply answers the daily windows with the given reply and every hour
// with a success.
func receivableDailyReply(status int, body string, header http.Header) func(start, end int64) (int, string, http.Header) {
	return func(start, end int64) (int, string, http.Header) {
		if end-start != 86400 {
			return http.StatusOK, `{"success":true,"data":{"quota":500000}}`, nil
		}
		return status, body, header
	}
}

func receivableBillOf(t *testing.T, db *gorm.DB, siteID, date int) model.ReceivableBill {
	t.Helper()
	var bill model.ReceivableBill
	require.NoError(t, db.Where("site_id = ? AND bill_date = ?", siteID, date).First(&bill).Error)
	return bill
}

func receivableHourOf(t *testing.T, db *gorm.DB, siteID int, hour int64) model.ReceivableBillHour {
	t.Helper()
	var row model.ReceivableBillHour
	require.NoError(t, db.Where("site_id = ? AND bill_hour = ?", siteID, hour).First(&row).Error)
	return row
}

// receivableLastError decodes last_error, which holds the failure message as JSON.
func receivableLastError(t *testing.T, lastError string) common.Message {
	t.Helper()
	var message common.Message
	require.NoError(t, common.UnmarshalJsonStr(lastError, &message))
	return message
}

func TestReceivableBillScanCreatesDueRows(t *testing.T) {
	db := setupReceivableBillTest(t)
	fake := newFakeStatSite(t)
	site := receivableSeedSite(t, db, "rb-due", fake.server.URL, 1793462400) // 北京 2026-11-01 0 点

	// 北京 11-23 00:59：昨天的账单 1 点才出，只出前天 11-21 的；今天 0 点那个小时还没结束
	assert.Equal(t, [][2]int64{{1795190400, 1795276800}}, receivableScan(fake, 1795366740))
	// 01:00：出昨天 11-22 的，11-21 已有，不再请求
	assert.Equal(t, [][2]int64{{1795276800, 1795363200}}, receivableScan(fake, 1795366800))
	// 02:05：0 点和 1 点两个小时都已结束 5 分钟
	assert.Equal(t, [][2]int64{{1795363200, 1795366800}, {1795366800, 1795370400}}, receivableScan(fake, 1795370700))

	for _, date := range []int{20261121, 20261122} {
		bill := receivableBillOf(t, db, site.Id, date)
		assert.Equal(t, model.ReceivableBillStatusSuccess, bill.Status, date)
		assert.Equal(t, "1.000000", model.ReceivableAmount(bill.Quota, bill.QuotaPerUnit).StringFixed(6), date)
		assert.Equal(t, 0.85, bill.ReceivableDiscount, date)
		assert.Equal(t, site.Name, bill.SiteName, date)
		assert.Equal(t, site.BaseURL, bill.SiteBaseURL, date)
	}
}

func TestReceivableBillScanFillsGapsAfterDowntime(t *testing.T) {
	db := setupReceivableBillTest(t)
	fake := newFakeStatSite(t)
	site := receivableSeedSite(t, db, "rb-gap", fake.server.URL, 1793462400)
	require.NoError(t, db.Create(&model.ReceivableBill{SiteId: site.Id, BillDate: 20261118, Status: model.ReceivableBillStatusSuccess, Attempts: 1}).Error)
	require.NoError(t, db.Create(&model.ReceivableBillHour{SiteId: site.Id, BillHour: 1795104000, Status: model.ReceivableBillStatusSuccess, Attempts: 1}).Error)

	days := [][2]int64{{1795017600, 1795104000}, {1795104000, 1795190400}, {1795190400, 1795276800}, {1795276800, 1795363200}}
	var hours [][2]int64 // 11-20 01:00 到 11-23 00:00；11-20 0 点已有
	for start := int64(1795107600); start <= 1795363200; start += 3600 {
		hours = append(hours, [2]int64{start, start + 3600})
	}
	require.Len(t, hours, 72)

	// 北京 11-23 01:30：先补 11-19 到 11-22 的日账单，本轮剩下的 56 行补小时；
	// 两轮都不请求已有的 11-18 日账单和 11-20 0 点的小时
	assert.Equal(t, append(days, hours[:56]...), receivableScan(fake, 1795368600))
	assert.Equal(t, hours[56:], receivableScan(fake, 1795368660))

	var dayCount, hourCount int64
	require.NoError(t, db.Model(&model.ReceivableBill{}).Where("site_id = ? AND status = ?", site.Id, model.ReceivableBillStatusSuccess).Count(&dayCount).Error)
	require.NoError(t, db.Model(&model.ReceivableBillHour{}).Where("site_id = ? AND status = ?", site.Id, model.ReceivableBillStatusSuccess).Count(&hourCount).Error)
	assert.Equal(t, int64(5), dayCount)
	assert.Equal(t, int64(73), hourCount)
}

func TestReceivableBillRetryBackoff(t *testing.T) {
	db := setupReceivableBillTest(t)
	fake := newFakeStatSite(t)
	site := receivableSeedSite(t, db, "rb-backoff", fake.server.URL, 1795276800) // 北京 11-22 0 点
	const t0 = int64(1795368600)                                                 // 北京 11-23 01:30
	bill := func() model.ReceivableBill { return receivableBillOf(t, db, site.Id, 20261122) }

	// 失败后结束本轮，所以 11-23 0 点的小时这一轮没有请求
	fake.setRespond(receivableDailyReply(http.StatusBadGateway, "", nil))
	assert.Equal(t, [][2]int64{{1795276800, 1795363200}}, receivableScan(fake, t0))
	got := bill()
	assert.Equal(t, model.ReceivableBillStatusRetrying, got.Status)
	assert.Equal(t, 1, got.Attempts)
	assert.Equal(t, t0+60, got.NextRetryAt)
	assert.Equal(t, common.Message{Key: "Site request failed: {{detail}}", Params: map[string]any{"detail": "HTTP 502"}},
		receivableLastError(t, got.LastError))

	// 还没到重试时间：只请求小时
	assert.Equal(t, [][2]int64{{1795363200, 1795366800}}, receivableScan(fake, t0+59))

	assert.Equal(t, [][2]int64{{1795276800, 1795363200}}, receivableScan(fake, t0+60))
	got = bill()
	assert.Equal(t, 2, got.Attempts)
	assert.Equal(t, t0+180, got.NextRetryAt)

	// Retry-After 比 4 分钟的退避长，按它等
	fake.setRespond(receivableDailyReply(http.StatusTooManyRequests, "", http.Header{"Retry-After": {"600"}}))
	now := got.NextRetryAt
	receivableScan(fake, now)
	got = bill()
	assert.Equal(t, 3, got.Attempts)
	assert.Equal(t, now+600, got.NextRetryAt)

	// Retry-After 最多按 30 分钟算
	fake.setRespond(receivableDailyReply(http.StatusTooManyRequests, "", http.Header{"Retry-After": {"7200"}}))
	now = got.NextRetryAt
	receivableScan(fake, now)
	got = bill()
	assert.Equal(t, 4, got.Attempts)
	assert.Equal(t, now+1800, got.NextRetryAt)

	fake.setRespond(receivableDailyReply(http.StatusOK, `{"success":true,"data":{"quota":61728394}}`, nil))
	now = got.NextRetryAt
	receivableScan(fake, now)
	got = bill()
	assert.Equal(t, model.ReceivableBillStatusSuccess, got.Status)
	assert.Equal(t, 5, got.Attempts)
	assert.Zero(t, got.NextRetryAt)
	assert.Empty(t, got.LastError)
	assert.Equal(t, int64(61728394), got.Quota)
	assert.Equal(t, int64(500000), got.QuotaPerUnit)

	// 第 6 次失败的退避是 32 分钟，封顶为 30 分钟
	require.NoError(t, db.Create(&model.ReceivableBill{SiteId: site.Id, BillDate: 20261121, Status: model.ReceivableBillStatusRetrying, Attempts: 5}).Error)
	fake.setRespond(receivableDailyReply(http.StatusBadGateway, "", nil))
	now += 60
	assert.Equal(t, [][2]int64{{1795190400, 1795276800}}, receivableScan(fake, now))
	capped := receivableBillOf(t, db, site.Id, 20261121)
	assert.Equal(t, 6, capped.Attempts)
	assert.Equal(t, now+1800, capped.NextRetryAt)
}

func TestReceivableBillNonRetryableFailureStops(t *testing.T) {
	db := setupReceivableBillTest(t)
	fake := newFakeStatSite(t)
	fake.setRespond(receivableDailyReply(http.StatusOK, `{"success":false,"message":"无权进行此操作"}`, nil))
	site := receivableSeedSite(t, db, "rb-refused", fake.server.URL, 1795276800) // 北京 11-22 0 点
	const now = int64(1795368600)                                                // 北京 11-23 01:30

	// 失败后结束本轮，所以这一轮没有小时请求
	assert.Equal(t, [][2]int64{{1795276800, 1795363200}}, receivableScan(fake, now))
	bill := receivableBillOf(t, db, site.Id, 20261122)
	assert.Equal(t, model.ReceivableBillStatusFailed, bill.Status)
	assert.Equal(t, 1, bill.Attempts)
	assert.Zero(t, bill.NextRetryAt)
	assert.Equal(t, "无权进行此操作", receivableLastError(t, bill.LastError).Params["detail"])

	// 一小时后：失败的账单不再请求，0 点和 1 点的小时照常补上
	assert.Equal(t, [][2]int64{{1795363200, 1795366800}, {1795366800, 1795370400}}, receivableScan(fake, now+3600))
	assert.Equal(t, model.ReceivableBillStatusFailed, receivableBillOf(t, db, site.Id, 20261122).Status)
}

func TestReceivableBillSiteEditAndDeleteHooks(t *testing.T) {
	db := setupReceivableBillTest(t)
	fakeA, fakeB := newFakeStatSite(t), newFakeStatSite(t)
	siteA := receivableSeedSite(t, db, "rb-hook-a", fakeA.server.URL, 1795276800)
	siteB := receivableSeedSite(t, db, "rb-hook-b", fakeB.server.URL, 1795276800)
	const now = int64(1795368600) // 北京 11-23 01:30
	const refused = `{"key":"Site request failed: {{detail}}","params":{"detail":"无权进行此操作"}}`
	const badGateway = `{"key":"Site request failed: {{detail}}","params":{"detail":"HTTP 502"}}`
	for _, row := range []any{
		&model.ReceivableBill{SiteId: siteA.Id, BillDate: 20261121, SiteName: siteA.Name, SiteBaseURL: siteA.BaseURL,
			Status: model.ReceivableBillStatusFailed, Attempts: 3, LastError: refused},
		&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: 1795363200, Status: model.ReceivableBillStatusFailed, Attempts: 3, LastError: refused},
		&model.ReceivableBill{SiteId: siteA.Id, BillDate: 20261122, SiteName: siteA.Name, SiteBaseURL: siteA.BaseURL,
			Quota: 1000000, QuotaPerUnit: 500000, ReceivableDiscount: 0.85, Status: model.ReceivableBillStatusSuccess, Attempts: 1},
		&model.ReceivableBill{SiteId: siteB.Id, BillDate: 20261121, SiteName: siteB.Name, SiteBaseURL: siteB.BaseURL,
			Status: model.ReceivableBillStatusFailed, Attempts: 3, LastError: refused},
	} {
		require.NoError(t, db.Create(row).Error)
	}

	// 编辑 A：A 失败的两行转为重试中，次数重新计，立即重试；B 的行和 A 成功账单的快照不变
	_, err := model.UpdateThirdPartySite(siteA.Id, model.ThirdPartySiteInput{Name: "rb-hook-a-renamed", Type: model.ThirdPartySiteTypeNewAPI,
		BaseURL: siteA.BaseURL, RemoteUserID: siteA.RemoteUserID, ReceivableDiscount: 0.9})
	require.NoError(t, err)
	failedBill := receivableBillOf(t, db, siteA.Id, 20261121)
	assert.Equal(t, model.ReceivableBillStatusRetrying, failedBill.Status)
	assert.Zero(t, failedBill.Attempts)
	assert.Zero(t, failedBill.NextRetryAt)
	failedHour := receivableHourOf(t, db, siteA.Id, 1795363200)
	assert.Equal(t, model.ReceivableBillStatusRetrying, failedHour.Status)
	assert.Zero(t, failedHour.Attempts)
	assert.Zero(t, failedHour.NextRetryAt)
	billB := receivableBillOf(t, db, siteB.Id, 20261121)
	assert.Equal(t, model.ReceivableBillStatusFailed, billB.Status)
	assert.Equal(t, 3, billB.Attempts)
	snapshot := receivableBillOf(t, db, siteA.Id, 20261122)
	assert.Equal(t, "rb-hook-a", snapshot.SiteName)
	assert.Equal(t, 0.85, snapshot.ReceivableDiscount)

	// 删除 B：钩子只处理重试中的行，B 失败的行保持原样
	require.NoError(t, model.DeleteThirdPartySite(siteB.Id))
	billB = receivableBillOf(t, db, siteB.Id, 20261121)
	assert.Equal(t, model.ReceivableBillStatusFailed, billB.Status)
	assert.Equal(t, refused, billB.LastError)

	// 只有 A 在线：A 的两行同步成功，快照取编辑后的名称和折扣
	assert.Equal(t, [][2]int64{{1795190400, 1795276800}, {1795363200, 1795366800}}, receivableScan(fakeA, now))
	assert.Empty(t, fakeB.windows())
	failedBill = receivableBillOf(t, db, siteA.Id, 20261121)
	assert.Equal(t, model.ReceivableBillStatusSuccess, failedBill.Status)
	assert.Equal(t, "rb-hook-a-renamed", failedBill.SiteName)
	assert.Equal(t, 0.9, failedBill.ReceivableDiscount)
	assert.Equal(t, model.ReceivableBillStatusSuccess, receivableHourOf(t, db, siteA.Id, 1795363200).Status)

	// 删除 A：A 在两张表中重试中的行转为失败，原因是站点已删除；之后的扫描不再请求 A
	require.NoError(t, db.Create(&model.ReceivableBill{SiteId: siteA.Id, BillDate: 20261120, Status: model.ReceivableBillStatusRetrying,
		Attempts: 2, LastError: badGateway, NextRetryAt: now + 120}).Error)
	require.NoError(t, db.Create(&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: 1795359600, Status: model.ReceivableBillStatusRetrying,
		Attempts: 2, LastError: badGateway, NextRetryAt: now + 120}).Error)
	require.NoError(t, model.DeleteThirdPartySite(siteA.Id))
	siteDeleted := common.Message{Key: "The site has been deleted"}
	retrying := receivableBillOf(t, db, siteA.Id, 20261120)
	assert.Equal(t, model.ReceivableBillStatusFailed, retrying.Status)
	assert.Zero(t, retrying.NextRetryAt)
	assert.Equal(t, siteDeleted, receivableLastError(t, retrying.LastError))
	retryingHour := receivableHourOf(t, db, siteA.Id, 1795359600)
	assert.Equal(t, model.ReceivableBillStatusFailed, retryingHour.Status)
	assert.Zero(t, retryingHour.NextRetryAt)
	assert.Equal(t, siteDeleted, receivableLastError(t, retryingHour.LastError))
	assert.Empty(t, receivableScan(fakeA, now+60))

	// 删除时正在进行的同步把重试中写了回来：下一轮扫描再转为失败
	require.NoError(t, db.Model(&model.ReceivableBill{}).Where("id = ?", retrying.Id).Updates(map[string]any{
		"status": model.ReceivableBillStatusRetrying, "attempts": 3, "last_error": badGateway, "next_retry_at": now + 240}).Error)
	assert.Empty(t, receivableScan(fakeA, now+120))
	retrying = receivableBillOf(t, db, siteA.Id, 20261120)
	assert.Equal(t, model.ReceivableBillStatusFailed, retrying.Status)
	assert.Zero(t, retrying.NextRetryAt)
	assert.Equal(t, siteDeleted, receivableLastError(t, retrying.LastError))
}
