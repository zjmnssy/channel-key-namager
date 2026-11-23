package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
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

// receivableRequest 以超级管理员身份直接调用处理函数，header 为空时不加请求头。
func receivableRequest(t *testing.T, method, target string, header http.Header, params gin.Params, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(method, target, nil)
	for name, values := range header {
		for _, value := range values {
			c.Request.Header.Add(name, value)
		}
	}
	c.Params = params
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root")
	c.Set("session_id", "dashboard-session")
	c.Set("auth_version", int64(1))
	c.Set("session_version", int64(1))
	handler(c)
	return response
}

type receivableReply struct {
	Success    bool            `json:"success"`
	MessageKey string          `json:"message_key"`
	Data       json.RawMessage `json:"data"`
}

func receivableReplyOf(t *testing.T, response *httptest.ResponseRecorder) receivableReply {
	t.Helper()
	var reply receivableReply
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &reply))
	return reply
}

// receivableListOf 解码列表响应，返回总数和各行。
func receivableListOf(t *testing.T, response *httptest.ResponseRecorder) (int, []map[string]any) {
	t.Helper()
	reply := receivableReplyOf(t, response)
	require.True(t, reply.Success, response.Body.String())
	var page struct {
		Total int              `json:"total"`
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, common.Unmarshal(reply.Data, &page))
	return page.Total, page.Items
}

// receivableRowKey 把站点和日期写成 "站点id/日期"，用来比较行的顺序。
func receivableRowKey(siteID, date int) string {
	return strconv.Itoa(siteID) + "/" + strconv.Itoa(date)
}

func receivableRowKeys(items []map[string]any) []string {
	keys := make([]string, len(items))
	for i, item := range items {
		keys[i] = receivableRowKey(int(item["site_id"].(float64)), int(item["bill_date"].(float64)))
	}
	return keys
}

// receivableSeedListBills 写入列表和导出测试共用的数据：A 站 1122 成功、1121 重试中、1031 成功，已删除的 B 站 1122 失败。
func receivableSeedListBills(t *testing.T, db *gorm.DB) (siteA, siteB *model.ThirdPartySite) {
	t.Helper()
	siteA = receivableSeedSite(t, db, "rb-list-a", "https://rb-list-a.example.com", 1)
	siteB = receivableSeedSite(t, db, "rb-list-b", "https://rb-list-b.example.com", 1)
	require.Less(t, siteA.Id, siteB.Id)
	require.NoError(t, db.Delete(siteB).Error)
	const badGateway = `{"key":"Site request failed: {{detail}}","params":{"detail":"HTTP 502"}}`
	for _, bill := range []*model.ReceivableBill{
		{SiteId: siteA.Id, BillDate: 20261122, SiteName: siteA.Name, SiteBaseURL: siteA.BaseURL, Quota: 61728394, QuotaPerUnit: 500000, ReceivableDiscount: 0.85,
			Status: model.ReceivableBillStatusSuccess, Attempts: 1, LeaseUntil: 99, SyncedAt: 1795366805},
		{SiteId: siteA.Id, BillDate: 20261121, SiteName: siteA.Name, Status: model.ReceivableBillStatusRetrying, Attempts: 3, LastError: badGateway},
		{SiteId: siteB.Id, BillDate: 20261122, SiteName: siteB.Name, Status: model.ReceivableBillStatusFailed, Attempts: 5,
			LastError: `{"key":"The site has been deleted"}`},
		{SiteId: siteA.Id, BillDate: 20261031, SiteName: siteA.Name, Quota: 500000, QuotaPerUnit: 500000, Status: model.ReceivableBillStatusSuccess},
	} {
		require.NoError(t, db.Create(bill).Error)
	}
	return siteA, siteB
}

func TestReceivableBillList(t *testing.T) {
	db := setupReceivableBillTest(t)
	siteA, siteB := receivableSeedListBills(t, db)
	a1122, a1121, b1122 := receivableRowKey(siteA.Id, 20261122), receivableRowKey(siteA.Id, 20261121), receivableRowKey(siteB.Id, 20261122)
	list := func(query string) *httptest.ResponseRecorder {
		return receivableRequest(t, http.MethodGet, "/api/receivable_bill/daily?"+query, nil, nil, GetReceivableBills)
	}

	response := list("start_date=20261101&end_date=20261123&p=1&page_size=10")
	total, items := receivableListOf(t, response)
	assert.Equal(t, 3, total)
	require.Equal(t, []string{a1122, b1122, a1121}, receivableRowKeys(items))
	assert.Equal(t, "123.456788", items[0]["amount"])
	assert.Nil(t, items[0]["error"])
	assert.Equal(t, false, items[0]["site_deleted"])
	assert.Nil(t, items[2]["amount"])
	assert.EqualValues(t, 3, items[2]["attempts"])
	assert.Equal(t, map[string]any{"key": "Site request failed: {{detail}}", "params": map[string]any{"detail": "HTTP 502"}}, items[2]["error"])
	assert.Equal(t, true, items[1]["site_deleted"])
	for _, secret := range []string{"remote-access-token", "lease_until", "last_error"} {
		assert.NotContains(t, response.Body.String(), secret)
	}

	_, items = receivableListOf(t, list("start_date=20261101&end_date=20261123&site_ids="+strconv.Itoa(siteB.Id)))
	assert.Equal(t, []string{b1122}, receivableRowKeys(items))
	_, items = receivableListOf(t, list("start_date=20261101&end_date=20261123&statuses=success,failed"))
	assert.Equal(t, []string{a1122, b1122}, receivableRowKeys(items))
	total, items = receivableListOf(t, list("start_date=20261101&end_date=20261123&page_size=1&p=2"))
	assert.Equal(t, 3, total)
	assert.Equal(t, []string{b1122}, receivableRowKeys(items))

	for _, query := range []string{
		"start_date=20261101",
		"start_date=20261131&end_date=20261123",
		"start_date=20261124&end_date=20261123",
		"start_date=20261101&end_date=20261123&site_ids=x",
		"start_date=20261101&end_date=20261123&statuses=ok",
	} {
		reply := receivableReplyOf(t, list(query))
		assert.False(t, reply.Success, query)
		assert.Equal(t, "Invalid parameters", reply.MessageKey, query)
	}
}

func TestReceivableBillSiteOptions(t *testing.T) {
	db := setupReceivableBillTest(t)
	siteA := receivableSeedSite(t, db, "rb-opt-a", "https://rb-opt-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "rb-opt-b", "https://rb-opt-b.example.com", 1)
	siteC := receivableSeedSite(t, db, "rb-opt-c", "https://rb-opt-c.example.com", 1)
	require.NoError(t, db.Delete(siteB).Error)

	response := receivableRequest(t, http.MethodGet, "/api/receivable_bill/sites", nil, nil, GetReceivableBillSites)
	reply := receivableReplyOf(t, response)
	require.True(t, reply.Success, response.Body.String())
	var options []model.ReceivableBillSiteOption
	require.NoError(t, common.Unmarshal(reply.Data, &options))
	// 在线站点在前，已删除的在后
	assert.Equal(t, []model.ReceivableBillSiteOption{
		{Id: siteA.Id, Name: siteA.Name}, {Id: siteC.Id, Name: siteC.Name}, {Id: siteB.Id, Name: siteB.Name, Deleted: true},
	}, options)
	assert.NotContains(t, response.Body.String(), "api_key")
	assert.NotContains(t, response.Body.String(), "remote-access-token")
}

// receivableAudits 按写入顺序返回重新同步审计日志的结果、参数和内容。
func receivableAudits(t *testing.T) (success []bool, params []map[string]any, contents []string) {
	t.Helper()
	var logs []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action = ?", "receivable_bill.sync").Order("id").Find(&logs).Error)
	for _, log := range logs {
		require.NotNil(t, log.Other.Op)
		encoded, err := common.Marshal(log.Other.Op.Params)
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, common.Unmarshal(encoded, &decoded))
		success, params, contents = append(success, log.Success), append(params, decoded), append(contents, log.Content)
	}
	return success, params, contents
}

func TestReceivableBillResync(t *testing.T) {
	db := setupReceivableBillTest(t)
	fake := newFakeStatSite(t)
	site := receivableSeedSite(t, db, "站点A-旧", fake.server.URL, 1)
	bill := &model.ReceivableBill{SiteId: site.Id, BillDate: 20261122, SiteName: "站点A-旧", SiteBaseURL: "https://old.example.com",
		Quota: 1000000, QuotaPerUnit: 500000, ReceivableDiscount: 0.85, Status: model.ReceivableBillStatusSuccess, Attempts: 4}
	require.NoError(t, db.Create(bill).Error)
	hourOfDay := &model.ReceivableBillHour{SiteId: site.Id, BillHour: 1795287600, Status: model.ReceivableBillStatusFailed, Attempts: 5}
	hourOfOtherDay := &model.ReceivableBillHour{SiteId: site.Id, BillHour: 1795201200, Status: model.ReceivableBillStatusFailed, Attempts: 5}
	require.NoError(t, db.Create(hourOfDay).Error)
	require.NoError(t, db.Create(hourOfOtherDay).Error)
	require.NoError(t, db.Model(site).Updates(map[string]any{"name": "站点A-新", "receivable_discount": 0.9}).Error)
	fake.setRespond(func(start, end int64) (int, string, http.Header) {
		return http.StatusOK, `{"success":true,"data":{"quota":2500000}}`, nil
	})
	resync := func(id string) *httptest.ResponseRecorder {
		return receivableRequest(t, http.MethodPost, "/api/receivable_bill/daily/"+id+"/sync", nil,
			gin.Params{{Key: "id", Value: id}}, ResyncReceivableBill)
	}
	billID := strconv.Itoa(bill.Id)
	viewOf := func(response *httptest.ResponseRecorder) map[string]any {
		reply := receivableReplyOf(t, response)
		require.True(t, reply.Success, response.Body.String())
		var view map[string]any
		require.NoError(t, common.Unmarshal(reply.Data, &view))
		return view
	}

	// 1. 成功：取站点当前的快照，次数重新计，同一天失败的小时行回到重试中
	view := viewOf(resync(billID))
	assert.Equal(t, model.ReceivableBillStatusSuccess, view["status"])
	assert.Equal(t, "5.000000", view["amount"])
	assert.Equal(t, "站点A-新", view["site_name"])
	assert.Equal(t, 0.9, view["receivable_discount"])
	assert.EqualValues(t, 1, view["attempts"])
	assert.Equal(t, [][2]int64{{1795276800, 1795363200}}, fake.windows())
	hour := receivableHourOf(t, db, site.Id, hourOfDay.BillHour)
	assert.Equal(t, model.ReceivableBillStatusRetrying, hour.Status)
	assert.Zero(t, hour.Attempts)
	assert.Zero(t, hour.NextRetryAt)
	other := receivableHourOf(t, db, site.Id, hourOfOtherDay.BillHour)
	assert.Equal(t, model.ReceivableBillStatusFailed, other.Status)
	assert.Equal(t, 5, other.Attempts)
	success, params, contents := receivableAudits(t)
	require.Len(t, success, 1)
	assert.True(t, success[0])
	assert.Equal(t, "Resynced receivable bill #"+billID, contents[0])
	assert.EqualValues(t, bill.Id, params[0]["bill_id"])
	assert.EqualValues(t, 20261122, params[0]["bill_date"])
	assert.Equal(t, "success", params[0]["previous_status"])
	assert.EqualValues(t, 1000000, params[0]["previous_quota"])
	assert.Equal(t, "success", params[0]["status"])
	assert.EqualValues(t, 2500000, params[0]["quota"])

	// 2. 远端返回 502：重试中，库里保留上一次成功的额度
	fake.setRespond(func(start, end int64) (int, string, http.Header) { return http.StatusBadGateway, "", nil })
	view = viewOf(resync(billID))
	assert.Equal(t, model.ReceivableBillStatusRetrying, view["status"])
	assert.Nil(t, view["amount"])
	assert.EqualValues(t, 1, view["attempts"])
	assert.EqualValues(t, 60, view["next_retry_at"].(float64)-view["synced_at"].(float64))
	assert.Equal(t, int64(2500000), receivableBillOf(t, db, site.Id, 20261122).Quota)
	success, params, _ = receivableAudits(t)
	require.Len(t, success, 2)
	assert.False(t, success[1])
	assert.Equal(t, "retrying", params[1]["status"])
	assert.NotContains(t, params[1], "quota")
	assert.EqualValues(t, 2500000, params[1]["previous_quota"])

	// 3. 租约被占用：不发请求，行不变
	_, claimed, err := model.ClaimReceivableRow[model.ReceivableBill](bill.Id, time.Now().Unix())
	require.NoError(t, err)
	require.True(t, claimed)
	requests := len(fake.windows())
	held := receivableBillOf(t, db, site.Id, 20261122)
	reply := receivableReplyOf(t, resync(billID))
	assert.False(t, reply.Success)
	assert.Equal(t, "This bill is being synced, please try again later", reply.MessageKey)
	assert.Len(t, fake.windows(), requests)
	assert.Equal(t, held, receivableBillOf(t, db, site.Id, 20261122))
	success, params, _ = receivableAudits(t)
	require.Len(t, success, 3)
	assert.False(t, success[2])
	assert.Equal(t, "syncing", params[2]["failure_reason"])
	require.NoError(t, db.Model(&model.ReceivableBill{}).Where("id = ?", bill.Id).Update("lease_until", 0).Error)

	// 4. 站点已删除：不发请求
	require.NoError(t, db.Delete(site).Error)
	reply = receivableReplyOf(t, resync(billID))
	assert.False(t, reply.Success)
	assert.Equal(t, "The site has been deleted", reply.MessageKey)
	assert.Len(t, fake.windows(), requests)
	success, params, _ = receivableAudits(t)
	require.Len(t, success, 4)
	assert.Equal(t, "site_deleted", params[3]["failure_reason"])

	// 5. 账单不存在：审计只有 bill_id 和 failure_reason
	reply = receivableReplyOf(t, resync("999999"))
	assert.False(t, reply.Success)
	assert.Equal(t, "Bill does not exist", reply.MessageKey)
	_, params, _ = receivableAudits(t)
	require.Len(t, params, 5)
	assert.Equal(t, map[string]any{"bill_id": float64(999999), "failure_reason": "not_found"}, params[4])

	// 6. id 不是数字
	reply = receivableReplyOf(t, resync("abc"))
	assert.False(t, reply.Success)
	assert.Equal(t, "Invalid ID", reply.MessageKey)
}

func TestReceivableBillTrend(t *testing.T) {
	db := setupReceivableBillTest(t)
	siteA := receivableSeedSite(t, db, "rb-trend-a", "https://rb-trend-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "rb-trend-b", "https://rb-trend-b.example.com", 1)
	siteC := receivableSeedSite(t, db, "rb-trend-c", "https://rb-trend-c.example.com", 1)
	siteD := receivableSeedSite(t, db, "rb-trend-d", "https://rb-trend-d.example.com", 1)
	siteE := receivableSeedSite(t, db, "rb-trend-e", "https://rb-trend-e.example.com", 1)
	require.NoError(t, db.Delete(siteB).Error)
	require.NoError(t, db.Delete(siteC).Error)
	require.NoError(t, db.Delete(siteE).Error)

	hour := func(date, clock int) int64 {
		day, ok := model.ReceivableBillDayStart(date)
		require.True(t, ok)
		return day.Add(time.Duration(clock) * time.Hour).Unix()
	}
	success, retrying := model.ReceivableBillStatusSuccess, model.ReceivableBillStatusRetrying
	for _, row := range []any{
		&model.ReceivableBill{SiteId: siteA.Id, BillDate: 20261121, Quota: 1000000, QuotaPerUnit: 500000, Status: success},
		&model.ReceivableBill{SiteId: siteA.Id, BillDate: 20261122, Status: retrying},
		&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: hour(20261122, 0), Quota: 500000, QuotaPerUnit: 500000, Status: success},
		&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: hour(20261122, 1), Status: retrying},
		&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: hour(20261122, 3), Quota: 250000, QuotaPerUnit: 500000, Status: success},
		&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: hour(20261123, 0), Quota: 500000, QuotaPerUnit: 500000, Status: success},
		&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: hour(20261123, 1), Quota: 500000, QuotaPerUnit: 500000, Status: success},
		&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: hour(20261123, 2), Quota: 500000, QuotaPerUnit: 500000, Status: success},
		&model.ReceivableBill{SiteId: siteB.Id, BillDate: 20261122, Quota: 500000, QuotaPerUnit: 500000, Status: success},
		&model.ReceivableBillHour{SiteId: siteB.Id, BillHour: hour(20261122, 2), Quota: 500000, QuotaPerUnit: 500000, Status: success},
		// A's bill of 11-21 succeeded, so its hours that day are not read. E was deleted on 11-21, which has no bill.
		&model.ReceivableBillHour{SiteId: siteA.Id, BillHour: hour(20261121, 5), Quota: 500000, QuotaPerUnit: 500000, Status: success},
		&model.ReceivableBillHour{SiteId: siteE.Id, BillHour: hour(20261121, 6), Quota: 250000, QuotaPerUnit: 500000, Status: success},
		&model.ReceivableBillHour{SiteId: siteE.Id, BillHour: hour(20261121, 7), Quota: 250000, QuotaPerUnit: 500000, Status: success},
	} {
		require.NoError(t, db.Create(row).Error)
	}
	now := time.Unix(1795401000, 0).UTC() // Beijing 11-23 10:30
	str := func(s string) *string { return &s }

	trend, err := service.BuildReceivableBillTrend(service.ReceivableTrendQuery{View: "day", Date: 20261122}, now)
	require.NoError(t, err)
	require.Len(t, trend.Labels, 24)
	assert.Equal(t, "00:00", trend.Labels[0])
	assert.Equal(t, "23:00", trend.Labels[23])
	require.Len(t, trend.Series, 3)
	a, b, d := trend.Series[0], trend.Series[1], trend.Series[2]
	assert.Equal(t, []int{siteA.Id, siteB.Id, siteD.Id}, []int{a.SiteID, b.SiteID, d.SiteID})
	assert.Equal(t, siteA.Name, a.SiteName)
	assert.False(t, a.Deleted)
	assert.True(t, b.Deleted)
	assert.Equal(t, str("1.000000"), a.Amounts[0])
	assert.Nil(t, a.Amounts[1])
	assert.Equal(t, str("0.500000"), a.Amounts[3])
	assert.Equal(t, str("1.000000"), b.Amounts[2])
	for _, amount := range d.Amounts {
		assert.Nil(t, amount)
	}
	assert.NotContains(t, a.Provisional, true)
	assert.Equal(t, 22, trend.MissingCount)

	// Today: the hours after the last one read (09:00) are neither shown nor missing.
	trend, err = service.BuildReceivableBillTrend(service.ReceivableTrendQuery{View: "day", Date: 20261123}, now)
	require.NoError(t, err)
	require.Len(t, trend.Series, 2)
	assert.Equal(t, siteA.Id, trend.Series[0].SiteID)
	assert.Equal(t, 7, trend.MissingCount)

	var hoursRead int64
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("receivable_trend_test:count_hours", func(tx *gorm.DB) {
		if tx.Statement.Table == "receivable_bill_hours" {
			hoursRead += tx.Statement.RowsAffected
		}
	}))
	trend, err = service.BuildReceivableBillTrend(service.ReceivableTrendQuery{View: "month", StartDate: 20261120, EndDate: 20261123}, now)
	require.NoError(t, db.Callback().Query().Remove("receivable_trend_test:count_hours"))
	require.NoError(t, err)
	// Only the hours of A on 11-22 and 11-23 (5) and of E on 11-21 (2) are read: not the hours of days with a successful bill.
	assert.EqualValues(t, 7, hoursRead)
	assert.Equal(t, []string{"2026-11-20", "2026-11-21", "2026-11-22", "2026-11-23"}, trend.Labels)
	require.Len(t, trend.Series, 4)
	assert.Equal(t, []*string{nil, str("2.000000"), str("1.500000"), str("3.000000")}, trend.Series[0].Amounts)
	assert.Equal(t, []bool{false, false, true, true}, trend.Series[0].Provisional)
	assert.Equal(t, []*string{nil, nil, str("1.000000"), nil}, trend.Series[1].Amounts)
	assert.Equal(t, []bool{false, false, false, false}, trend.Series[1].Provisional)
	// E, deleted before its bill of 11-21 was due, shows its hours of that day.
	assert.Equal(t, siteE.Id, trend.Series[3].SiteID)
	assert.Equal(t, []*string{nil, str("1.000000"), nil, nil}, trend.Series[3].Amounts)
	assert.Equal(t, []bool{false, true, false, false}, trend.Series[3].Provisional)
	assert.Zero(t, trend.MissingCount)

	// site_ids picks the series itself and skips ids that do not exist.
	trend, err = service.BuildReceivableBillTrend(service.ReceivableTrendQuery{
		View: "month", StartDate: 20261120, EndDate: 20261123, SiteIDs: []int{siteD.Id, 99999},
	}, now)
	require.NoError(t, err)
	require.Len(t, trend.Series, 1)
	assert.Equal(t, siteD.Id, trend.Series[0].SiteID)
	for _, amount := range trend.Series[0].Amounts {
		assert.Nil(t, amount)
	}
	assert.Zero(t, trend.MissingCount)

	trend, err = service.BuildReceivableBillTrend(service.ReceivableTrendQuery{View: "day", Date: 20261122, SiteIDs: []int{99999}}, now)
	require.NoError(t, err)
	assert.Empty(t, trend.Series)

	trendRequest := func(query string) receivableReply {
		return receivableReplyOf(t, receivableRequest(t, http.MethodGet, "/api/receivable_bill/trend?"+query, nil, nil, GetReceivableBillTrend))
	}
	reply := trendRequest("view=month&start_date=20261120&end_date=20261123&site_ids=" + strconv.Itoa(siteA.Id))
	require.True(t, reply.Success)
	assert.Contains(t, string(reply.Data), `"missing_count":0`)
	for _, query := range []string{
		"view=year&date=20261122",
		"view=day&date=20261131",
		"view=day",
		"view=month&start_date=20261123&end_date=20261120",
		"view=day&date=20261122&site_ids=x",
	} {
		reply := trendRequest(query)
		assert.False(t, reply.Success, query)
		assert.Equal(t, "Invalid parameters", reply.MessageKey, query)
	}
	reply = trendRequest("view=month&start_date=20251101&end_date=20261123")
	assert.False(t, reply.Success)
	assert.Equal(t, "Date range cannot exceed 366 days", reply.MessageKey)
}

func TestReceivableBillExport(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := setupReceivableBillTest(t)
	siteA, siteB := receivableSeedListBills(t, db)
	export := func(query, language string) *httptest.ResponseRecorder {
		header := http.Header{}
		header.Set("Accept-Language", language)
		return receivableRequest(t, http.MethodGet, "/api/receivable_bill/daily/export?"+query, header, nil, ExportReceivableBills)
	}
	rowsOf := func(response *httptest.ResponseRecorder) (*excelize.File, [][]string) {
		t.Helper()
		require.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", response.Header().Get("Content-Type"))
		assert.Equal(t, `attachment; filename="receivable-bills.xlsx"`, response.Header().Get("Content-Disposition"))
		file, err := excelize.OpenReader(bytes.NewReader(response.Body.Bytes()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = file.Close() })
		rows, err := file.GetRows(file.GetSheetName(0), excelize.Options{RawCellValue: true})
		require.NoError(t, err)
		return file, rows
	}

	file, rows := rowsOf(export("start_date=20261101&end_date=20261123", "zh-CN"))
	sheet := file.GetSheetName(0)
	require.Len(t, rows, 4)
	assert.Equal(t, []string{"账单日期", "站点名称", "站点URL", "金额(折前)($)", "应收折扣", "同步结果", "失败原因", "同步时间"}, rows[0])
	assert.Equal(t, []string{"2026-11-22", siteA.Name, siteA.BaseURL, "123.456788", "0.85", "成功", "", "2026-11-23 01:00:05"}, rows[1])
	// Rows without a success carry no amount or discount, and a bill that never synced has no sync time.
	assert.Equal(t, []string{"2026-11-22", siteB.Name, "", "", "", "失败", "站点已删除"}, rows[2])
	assert.Equal(t, []string{"2026-11-21", siteA.Name, "", "", "", "重试中", "站点请求失败：HTTP 502"}, rows[3])

	cellType, err := file.GetCellType(sheet, "D2")
	require.NoError(t, err)
	assert.Contains(t, []excelize.CellType{excelize.CellTypeUnset, excelize.CellTypeNumber}, cellType)
	styleID, err := file.GetCellStyle(sheet, "D2")
	require.NoError(t, err)
	style, err := file.GetStyle(styleID)
	require.NoError(t, err)
	require.NotNil(t, style.CustomNumFmt)
	assert.Equal(t, "0.000000", *style.CustomNumFmt)
	cellType, err = file.GetCellType(sheet, "E2")
	require.NoError(t, err)
	assert.Contains(t, []excelize.CellType{excelize.CellTypeUnset, excelize.CellTypeNumber}, cellType)
	cellType, err = file.GetCellType(sheet, "A2")
	require.NoError(t, err)
	assert.NotContains(t, []excelize.CellType{excelize.CellTypeUnset, excelize.CellTypeNumber}, cellType, "the bill date is text")

	// A language the backend does not support is written in common.DefaultLanguage.
	originalLanguage := common.DefaultLanguage
	common.DefaultLanguage = "en"
	t.Cleanup(func() { common.DefaultLanguage = originalLanguage })
	_, rows = rowsOf(export("start_date=20261101&end_date=20261123", "fr"))
	assert.Equal(t, "Bill Date", rows[0][0])
	assert.Equal(t, "Success", rows[1][5])
	assert.Equal(t, "The site has been deleted", rows[2][6])
	assert.Equal(t, "Site request failed: HTTP 502", rows[3][6])

	// An empty result still downloads a file that holds only the header.
	_, rows = rowsOf(export("start_date=20261101&end_date=20261123&statuses=success&site_ids="+strconv.Itoa(siteB.Id), "zh-CN"))
	assert.Len(t, rows, 1)

	reply := receivableReplyOf(t, export("end_date=20261123", "zh-CN"))
	assert.False(t, reply.Success)
	assert.Equal(t, "Invalid parameters", reply.MessageKey)
}
