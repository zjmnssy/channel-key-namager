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
