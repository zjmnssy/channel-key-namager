package controller

import (
	"cmp"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPayableBillTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupThirdPartySiteTest(t)
	originalOptions := service.PayableBillClientOptions
	service.PayableBillClientOptions = service.ChannelPushClientOptions{Pace: 0, Timeout: 2 * time.Second, MaxRetryAfter: 30 * time.Minute}
	t.Cleanup(func() { service.PayableBillClientOptions = originalOptions })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.AuditLog{},
		&model.PayablePullTask{}, &model.PayablePullSite{}, &model.PayableChannelUsage{}, &model.PayableProviderOverride{}))
	clearTables := func() {
		for _, table := range []any{&model.PayableProviderOverride{}, &model.PayableChannelUsage{}, &model.PayablePullSite{},
			&model.PayablePullTask{}, &model.AuditLog{}, &model.Channel{}, &model.User{}} {
			// A fresh session per table: GORM reuses the Statement of a stored session, so every Delete would hit the first table.
			require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(table).Error)
		}
	}
	clearTables()
	t.Cleanup(clearTables)
	return db
}

func payableTaskOf(t *testing.T, db *gorm.DB, billDate int) model.PayablePullTask {
	t.Helper()
	var task model.PayablePullTask
	require.NoError(t, db.Where("bill_date = ?", billDate).First(&task).Error)
	return task
}

func payablePullOf(t *testing.T, db *gorm.DB, billDate, siteID int) model.PayablePullSite {
	t.Helper()
	var pull model.PayablePullSite
	require.NoError(t, db.Where("bill_date = ? AND site_id = ?", billDate, siteID).First(&pull).Error)
	return pull
}

func TestPayablePullTaskCreation(t *testing.T) {
	db := setupPayableBillTest(t)
	siteA := receivableSeedSite(t, db, "pb-a", "https://pb-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pb-b", "https://pb-b.example.com", 1)

	created, err := model.CreatePayablePullTask(20261123, []*model.ThirdPartySite{siteA, siteB}, 100)
	require.NoError(t, err)
	assert.True(t, created)
	created, err = model.CreatePayablePullTask(20261123, []*model.ThirdPartySite{siteA, siteB}, 101)
	require.NoError(t, err)
	assert.False(t, created) // bill_date 唯一

	var taskCount, pullCount int64
	require.NoError(t, db.Model(&model.PayablePullTask{}).Count(&taskCount).Error)
	require.NoError(t, db.Model(&model.PayablePullSite{}).Count(&pullCount).Error)
	assert.Equal(t, int64(1), taskCount)
	assert.Equal(t, int64(2), pullCount)
	task := payableTaskOf(t, db, 20261123)
	assert.Equal(t, 2, task.SiteTotal)
	assert.Equal(t, model.PayableTaskRunning, task.Status)
	assert.Zero(t, task.StartedTime) // 开始时间是第一次请求的时刻，建任务时还没有请求
	assert.Zero(t, task.FinishedTime)
	for _, site := range []*model.ThirdPartySite{siteA, siteB} {
		pull := payablePullOf(t, db, 20261123, site.Id)
		assert.Equal(t, task.Id, pull.TaskId)
		assert.Equal(t, model.PayablePullPending, pull.Status)
		assert.Equal(t, 1, pull.NextPage)
		assert.Equal(t, 1, pull.Pass)
		assert.Equal(t, site.Name, pull.SiteName)
		assert.Equal(t, site.BaseURL, pull.SiteBaseURL)
		assert.Zero(t, pull.NextRetryAt)
	}

	// 次日任务把前一天仍 pending 的进度改为 superseded，前一天任务变 partial_failed
	pullA := payablePullOf(t, db, 20261123, siteA.Id)
	lease, claimed, err := model.ClaimPayablePull(pullA.Id, 150) // A 的进度正在拉取
	require.NoError(t, err)
	require.True(t, claimed)
	created, err = model.CreatePayablePullTask(20261124, []*model.ThirdPartySite{siteA}, 200)
	require.NoError(t, err)
	assert.True(t, created)
	previous := payableTaskOf(t, db, 20261123)
	assert.Equal(t, model.PayableTaskPartialFailed, previous.Status)
	assert.Equal(t, int64(200), previous.FinishedTime)
	for _, site := range []*model.ThirdPartySite{siteA, siteB} {
		pull := payablePullOf(t, db, 20261123, site.Id)
		assert.Equal(t, model.PayablePullSuperseded, pull.Status)
		assert.Equal(t, int64(0), pull.NextRetryAt)
		var message common.Message
		require.NoError(t, common.UnmarshalJsonStr(pull.LastError, &message))
		assert.Equal(t, model.MsgPayablePullSuperseded, message.Key)
	}
	// 被取代后，取代前开始的拉取即使还持有租约，也不能把进度写回
	saved, err := model.SavePayablePullAttempt(pullA.Id, lease, nil, map[string]any{"status": model.PayablePullRetrying, "next_retry_at": 260}, 210)
	require.NoError(t, err)
	assert.False(t, saved)
	assert.Equal(t, model.PayablePullSuperseded, payablePullOf(t, db, 20261123, siteA.Id).Status)
	next := payableTaskOf(t, db, 20261124)
	assert.Equal(t, 1, next.SiteTotal)
	assert.Equal(t, model.PayableTaskRunning, next.Status)
	assert.Equal(t, model.PayablePullPending, payablePullOf(t, db, 20261124, siteA.Id).Status)

	// 没有站点时任务直接成功
	created, err = model.CreatePayablePullTask(20261125, nil, 300)
	require.NoError(t, err)
	assert.True(t, created)
	empty := payableTaskOf(t, db, 20261125)
	assert.Equal(t, model.PayableTaskSuccess, empty.Status)
	assert.Equal(t, int64(300), empty.StartedTime)
	assert.Equal(t, int64(300), empty.FinishedTime)
	assert.Zero(t, empty.SiteTotal)
	// 前一天任务的进度仍在跑（pending），所以被取代
	assert.Equal(t, model.PayablePullSuperseded, payablePullOf(t, db, 20261124, siteA.Id).Status)
}

func TestPayablePullSiteHooks(t *testing.T) {
	db := setupPayableBillTest(t)
	siteA := receivableSeedSite(t, db, "pb-hook-a", "https://pb-hook-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pb-hook-b", "https://pb-hook-b.example.com", 1)
	siteC := receivableSeedSite(t, db, "pb-hook-c", "https://pb-hook-c.example.com", 1)
	const now = int64(1795368600)
	const refused = `{"key":"Site request failed: {{detail}}","params":{"detail":"HTTP 502"}}`

	oldTask := &model.PayablePullTask{BillDate: 20261122, SiteTotal: 2, Status: model.PayableTaskPartialFailed, FinishedTime: 50}
	task := &model.PayablePullTask{BillDate: 20261123, SiteTotal: 3, Status: model.PayableTaskPartialFailed, FinishedTime: 90}
	require.NoError(t, db.Create(oldTask).Error)
	require.NoError(t, db.Create(task).Error)
	for _, row := range []*model.PayablePullSite{
		{TaskId: oldTask.Id, SiteId: siteA.Id, BillDate: 20261122, Status: model.PayablePullFailed, NextPage: 3, Pass: 2, Attempts: 4,
			LastError: refused, FinishedTime: 50},
		{TaskId: task.Id, SiteId: siteA.Id, BillDate: 20261123, SiteName: siteA.Name, SiteBaseURL: siteA.BaseURL,
			Status: model.PayablePullFailed, NextPage: 4, Pass: 2, ReportedTotal: 250, ChannelCount: 2, Attempts: 4, LastError: refused, FinishedTime: 90},
		{TaskId: task.Id, SiteId: siteB.Id, BillDate: 20261123, SiteName: siteB.Name, SiteBaseURL: siteB.BaseURL,
			Status: model.PayablePullFailed, NextPage: 3, Pass: 1, Attempts: 4, NextRetryAt: 999, LastError: refused, FinishedTime: 90},
		{TaskId: task.Id, SiteId: siteC.Id, BillDate: 20261123, SiteName: siteC.Name, SiteBaseURL: siteC.BaseURL,
			Status: model.PayablePullSuperseded, NextPage: 2, Pass: 1, Attempts: 1},
	} {
		require.NoError(t, db.Create(row).Error)
	}
	pullA := payablePullOf(t, db, 20261123, siteA.Id)
	for _, channelID := range []int{1, 2} {
		require.NoError(t, db.Create(&model.PayableChannelUsage{TaskId: task.Id, PullSiteId: pullA.Id, BillDate: 20261123,
			SiteId: siteA.Id, RemoteChannelId: channelID, UsedQuota: 10}).Error)
	}
	require.NoError(t, db.Create(&model.PayableChannelUsage{TaskId: oldTask.Id, PullSiteId: 9999, BillDate: 20261122,
		SiteId: siteA.Id, RemoteChannelId: 1, UsedQuota: 10}).Error)
	editSite := func(site *model.ThirdPartySite, name, baseURL string) {
		_, err := model.UpdateThirdPartySite(site.Id, model.ThirdPartySiteInput{Name: name, Type: model.ThirdPartySiteTypeNewAPI,
			BaseURL: baseURL, RemoteUserID: site.RemoteUserID, ReceivableDiscount: 0.9})
		require.NoError(t, err)
	}

	// 1) 编辑 B 只改名称：进度转 retrying，页码不动，任务回到 running
	editSite(siteB, "pb-hook-b-renamed", siteB.BaseURL)
	pullB := payablePullOf(t, db, 20261123, siteB.Id)
	assert.Equal(t, model.PayablePullRetrying, pullB.Status)
	assert.Zero(t, pullB.Attempts)
	assert.Zero(t, pullB.NextRetryAt)
	assert.Zero(t, pullB.FinishedTime) // 重开的进度不再算已结束
	assert.Equal(t, 3, pullB.NextPage)
	assert.Equal(t, siteB.Name, pullB.SiteName) // 地址未变，快照不动
	latest := payableTaskOf(t, db, 20261123)
	assert.Equal(t, model.PayableTaskRunning, latest.Status)
	assert.Zero(t, latest.FinishedTime)

	// 2) 编辑 A 改地址：进度从头重拉，明细作废，快照换成新地址
	editSite(siteA, "pb-hook-a-renamed", "https://pb-hook-a-new.example.com")
	pullA = payablePullOf(t, db, 20261123, siteA.Id)
	assert.Equal(t, model.PayablePullPending, pullA.Status)
	assert.Equal(t, 1, pullA.NextPage)
	assert.Equal(t, 1, pullA.Pass)
	assert.Zero(t, pullA.ChannelCount)
	assert.Zero(t, pullA.ReportedTotal)
	assert.Zero(t, pullA.Attempts)
	assert.Zero(t, pullA.FinishedTime)
	assert.Empty(t, pullA.LastError)
	assert.Equal(t, "pb-hook-a-renamed", pullA.SiteName)
	assert.Equal(t, "https://pb-hook-a-new.example.com", pullA.SiteBaseURL)
	var usageCount int64
	require.NoError(t, db.Model(&model.PayableChannelUsage{}).Where("pull_site_id = ?", pullA.Id).Count(&usageCount).Error)
	assert.Zero(t, usageCount)
	require.NoError(t, db.Model(&model.PayableChannelUsage{}).Where("task_id = ?", oldTask.Id).Count(&usageCount).Error)
	assert.Equal(t, int64(1), usageCount) // 其他进度的明细不受影响

	// 3) 更早任务里的 failed 行和 superseded 行都不重开
	oldPull := payablePullOf(t, db, 20261122, siteA.Id)
	assert.Equal(t, model.PayablePullFailed, oldPull.Status)
	assert.Equal(t, 3, oldPull.NextPage)
	assert.Equal(t, int64(50), oldPull.FinishedTime)
	editSite(siteC, "pb-hook-c-renamed", "https://pb-hook-c-new.example.com")
	pullC := payablePullOf(t, db, 20261123, siteC.Id)
	assert.Equal(t, model.PayablePullSuperseded, pullC.Status)
	assert.Equal(t, 2, pullC.NextPage)
	assert.Equal(t, siteC.BaseURL, pullC.SiteBaseURL)

	// 4) 删除 B：它 retrying 的进度改 failed，原因是站点已删除，任务不再有未完成进度
	require.NoError(t, model.DeleteThirdPartySite(siteB.Id))
	pullB = payablePullOf(t, db, 20261123, siteB.Id)
	assert.Equal(t, model.PayablePullFailed, pullB.Status)
	var message common.Message
	require.NoError(t, common.UnmarshalJsonStr(pullB.LastError, &message))
	assert.Equal(t, "The site has been deleted", message.Key)
	// A 仍是 pending，任务保持 running
	assert.Equal(t, model.PayablePullPending, payablePullOf(t, db, 20261123, siteA.Id).Status)
	assert.Equal(t, model.PayableTaskRunning, payableTaskOf(t, db, 20261123).Status)
}

func TestPayableChannelPageAttempt(t *testing.T) {
	const remoteFailure = "Site request failed: {{detail}}"
	const invalidList = "Site returned an invalid channel list"
	json200 := func(body string) func() (int, string, http.Header) {
		return func() (int, string, http.Header) { return http.StatusOK, body, nil }
	}
	tests := []struct {
		name      string
		reply     func() (int, string, http.Header)
		total     int
		quota     int64
		key       string
		retryable bool
		wait      time.Duration
	}{
		{name: "ok", reply: json200(`{"success":true,"data":{"items":[{"id":3,"name":"a-dcmark-1-9","used_quota":1500000}],"total":101}}`), total: 101, quota: 1500000},
		{name: "empty page", reply: json200(`{"success":true,"data":{"items":[],"total":0}}`)},
		{name: "bad gateway", reply: func() (int, string, http.Header) { return http.StatusBadGateway, "", nil }, key: remoteFailure, retryable: true},
		{name: "rate limited", reply: func() (int, string, http.Header) {
			return http.StatusTooManyRequests, "", http.Header{"Retry-After": {"120"}}
		}, key: remoteFailure, retryable: true, wait: 120 * time.Second},
		{name: "refusal", reply: json200(`{"success":false,"message":"无权进行此操作"}`), key: remoteFailure},
		{name: "missing used_quota", reply: json200(`{"success":true,"data":{"items":[{"id":3,"name":"a"}],"total":1}}`), key: invalidList},
		{name: "negative used_quota", reply: json200(`{"success":true,"data":{"items":[{"id":3,"used_quota":-1}],"total":1}}`), key: invalidList},
		{name: "zero id", reply: json200(`{"success":true,"data":{"items":[{"id":0,"used_quota":1}],"total":1}}`), key: invalidList},
		{name: "missing items", reply: json200(`{"success":true,"data":{"total":1}}`), key: invalidList},
		{name: "null items", reply: json200(`{"success":true,"data":{"items":null,"total":1}}`), key: invalidList},
		{name: "missing total", reply: json200(`{"success":true,"data":{"items":[{"id":3,"used_quota":1}]}}`), key: invalidList},
		{name: "negative total", reply: json200(`{"success":true,"data":{"items":[],"total":-1}}`), key: invalidList},
		{name: "null data", reply: json200(`{"success":true,"data":null}`), key: invalidList},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupPayableBillTest(t)
			var seen *http.Request
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = r
				status, body, header := tt.reply()
				for name, values := range header {
					for _, value := range values {
						w.Header().Add(name, value)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(server.Close)
			site := receivableSeedSite(t, db, "pb-page", server.URL, 1)

			got := service.NewChannelPushClient(site, service.PayableBillClientOptions).ReadChannelPage(t.Context(), 2)

			assert.Equal(t, tt.retryable, got.Retryable)
			assert.Equal(t, tt.wait, got.RetryAfter)
			if tt.key == "" {
				require.Nil(t, got.Failure)
				assert.Equal(t, tt.total, got.Total)
				if tt.quota > 0 {
					require.Len(t, got.Items, 1)
					assert.Equal(t, 3, got.Items[0].ID)
					assert.Equal(t, tt.quota, got.Items[0].UsedQuota)
				} else {
					assert.Empty(t, got.Items)
				}
			} else {
				require.NotNil(t, got.Failure)
				assert.Equal(t, tt.key, got.Failure.Key)
				assert.Empty(t, got.Items)
			}
			// httptest 处理函数在客户端读完响应前已返回，这里读 seen 无竞争。
			require.NotNil(t, seen)
			assert.Equal(t, http.MethodGet, seen.Method)
			assert.Equal(t, "/api/channel/", seen.URL.Path)
			assert.Equal(t, "false", seen.URL.Query().Get("tag_mode"))
			assert.Equal(t, "true", seen.URL.Query().Get("id_sort"))
			assert.Equal(t, "2", seen.URL.Query().Get("p"))
			assert.Equal(t, "100", seen.URL.Query().Get("page_size"))
			assert.Equal(t, "Bearer remote-access-token", seen.Header.Get("Authorization"))
			assert.Equal(t, "7", seen.Header.Get("New-Api-User"))
		})
	}
}

func TestPayablePullBaseURLChangeResetsUnfinishedPulls(t *testing.T) {
	db := setupPayableBillTest(t)
	const now = int64(1795363800)
	statuses := []string{model.PayablePullPending, model.PayablePullRetrying, model.PayablePullSuccess}
	sites := make(map[string]*model.ThirdPartySite, len(statuses))
	for _, status := range statuses {
		sites[status] = receivableSeedSite(t, db, "pb-url-"+status, "https://pb-url-"+status+".example.com", 1)
	}
	created, err := model.CreatePayablePullTask(20261122, []*model.ThirdPartySite{
		sites[model.PayablePullPending], sites[model.PayablePullRetrying], sites[model.PayablePullSuccess]}, now)
	require.NoError(t, err)
	require.True(t, created)
	// 三个进度都已拉完第 1 页（1 行明细），分别处于 pending、retrying、success
	pulls := make(map[string]model.PayablePullSite, len(statuses))
	for _, status := range statuses {
		pull := payablePullOf(t, db, 20261122, sites[status].Id)
		require.NoError(t, db.Model(&pull).Updates(map[string]any{"status": status, "next_page": 2, "channel_count": 1}).Error)
		require.NoError(t, db.Create(&model.PayableChannelUsage{TaskId: pull.TaskId, PullSiteId: pull.Id, BillDate: 20261122,
			SiteId: pull.SiteId, RemoteChannelId: 1, UsedQuota: 10}).Error)
		pulls[status] = pull
	}
	// pending 的进度正在拉取，持有租约
	pendingPull := pulls[model.PayablePullPending]
	lease, claimed, err := model.ClaimPayablePull(pendingPull.Id, now)
	require.NoError(t, err)
	require.True(t, claimed)

	for _, status := range statuses {
		site := sites[status]
		_, err := model.UpdateThirdPartySite(site.Id, model.ThirdPartySiteInput{Name: site.Name, Type: model.ThirdPartySiteTypeNewAPI,
			BaseURL: "https://pb-url-" + status + "-new.example.com", RemoteUserID: site.RemoteUserID, ReceivableDiscount: site.ReceivableDiscount})
		require.NoError(t, err)
	}

	// pending 与 retrying 的进度从第 1 页重拉：明细作废，租约清掉，快照换成新地址
	for _, status := range []string{model.PayablePullPending, model.PayablePullRetrying} {
		pull := payablePullOf(t, db, 20261122, sites[status].Id)
		assert.Equal(t, model.PayablePullPending, pull.Status, status)
		assert.Equal(t, 1, pull.NextPage, status)
		assert.Zero(t, pull.ChannelCount, status)
		assert.Zero(t, pull.LeaseUntil, status)
		assert.Equal(t, "https://pb-url-"+status+"-new.example.com", pull.SiteBaseURL, status)
		assert.Zero(t, payableUsageCount(t, db, pull.Id), status)
	}
	// 已成功的进度不动
	succeeded := payablePullOf(t, db, 20261122, sites[model.PayablePullSuccess].Id)
	assert.Equal(t, model.PayablePullSuccess, succeeded.Status)
	assert.Equal(t, 2, succeeded.NextPage)
	assert.Equal(t, 1, succeeded.ChannelCount)
	assert.Equal(t, sites[model.PayablePullSuccess].BaseURL, succeeded.SiteBaseURL)
	assert.Equal(t, int64(1), payableUsageCount(t, db, succeeded.Id))

	// 重置前开始的拉取带着旧租约保存它从旧地址拉到的页：被拒，不写明细
	saved, err := model.SavePayablePullAttempt(pendingPull.Id, lease, []*model.PayableChannelUsage{{TaskId: pendingPull.TaskId,
		PullSiteId: pendingPull.Id, BillDate: 20261122, SiteId: pendingPull.SiteId, RemoteChannelId: 2, UsedQuota: 20}},
		map[string]any{"status": model.PayablePullSuccess, "finished_time": now}, now)
	require.NoError(t, err)
	assert.False(t, saved)
	assert.Zero(t, payableUsageCount(t, db, pendingPull.Id))
	assert.Equal(t, model.PayablePullPending, payablePullOf(t, db, 20261122, pendingPull.SiteId).Status)
}

// fakeChannel 是假站点上的一个渠道，名称为 "ch-{ID}"。
type fakeChannel struct {
	ID        int
	UsedQuota int64
	Name      string // 为空时是 "ch-{ID}"
}

// fakeChannelSite 是假站点的渠道列表接口：把 channels 按 id 降序、每页 100 个分页返回，并记下每次请求的页码。
type fakeChannelSite struct {
	server   *httptest.Server
	mu       sync.Mutex
	channels []fakeChannel
	total    int   // 不小于 0 时代替渠道数作为 total 返回，模拟拉取期间渠道的增减
	pages    []int // 收到的页码，按请求顺序
	// intercept 在锁内、按渠道列表回答之前调用，可以改 channels 和 total；返回非 0 的 status 时用它代替渠道列表回答。
	intercept func(page int) (status int, body string)
}

func newFakeChannelSite(t *testing.T, channels ...fakeChannel) *fakeChannelSite {
	t.Helper()
	site := &fakeChannelSite{channels: channels, total: -1}
	site.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("p"))
		status, body := site.reply(page)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(site.server.Close)
	return site
}

func (s *fakeChannelSite) reply(page int) (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages = append(s.pages, page)
	if s.intercept != nil {
		if status, body := s.intercept(page); status != 0 {
			return status, body
		}
	}
	sorted := slices.SortedFunc(slices.Values(s.channels), func(a, b fakeChannel) int { return cmp.Compare(b.ID, a.ID) })
	items := []map[string]any{}
	for _, channel := range sorted[min(max(page-1, 0)*100, len(sorted)):min(max(page, 0)*100, len(sorted))] {
		name := cmp.Or(channel.Name, "ch-"+strconv.Itoa(channel.ID))
		items = append(items, map[string]any{"id": channel.ID, "name": name, "used_quota": channel.UsedQuota})
	}
	total := s.total
	if total < 0 {
		total = len(s.channels)
	}
	body, _ := common.Marshal(map[string]any{"success": true, "data": map[string]any{"items": items, "total": total}})
	return http.StatusOK, string(body)
}

// update 在锁内修改假站点，扫描之间用它改渠道、total 或注入失败。
func (s *fakeChannelSite) update(change func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change()
}

func (s *fakeChannelSite) requested() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int{}, s.pages...)
}

// payableScan 在 Unix 秒 now 扫描一次，返回 fake 在这次扫描中收到的页码。
// now 取 UTC 时间，证明结果与服务器时区无关。
func payableScan(fake *fakeChannelSite, now int64) []int {
	before := len(fake.requested())
	service.RunPayableBillScan(context.Background(), time.Unix(now, 0).UTC())
	return fake.requested()[before:]
}

func payableUsageCount(t *testing.T, db *gorm.DB, pullID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&model.PayableChannelUsage{}).Where("pull_site_id = ?", pullID).Count(&count).Error)
	return count
}

// payableDeltas 返回账单日期 billDate 的明细，键为站点渠道 ID，值为 "增量标记 增量"，增量为空时写 nil。
func payableDeltas(t *testing.T, db *gorm.DB, billDate int) map[int]string {
	t.Helper()
	var rows []model.PayableChannelUsage
	require.NoError(t, db.Where("bill_date = ?", billDate).Find(&rows).Error)
	deltas := make(map[int]string, len(rows))
	for _, row := range rows {
		delta := "nil"
		if row.DeltaQuota != nil {
			delta = strconv.FormatInt(*row.DeltaQuota, 10)
		}
		deltas[row.RemoteChannelId] = row.DeltaFlag + " " + delta
	}
	return deltas
}

func TestPayableBillScanCreatesTaskAt0010(t *testing.T) {
	db := setupPayableBillTest(t)
	fake := newFakeChannelSite(t) // 站点没有渠道：{"items":[],"total":0}
	site := receivableSeedSite(t, db, "pb-0010", fake.server.URL, 1)
	taskCount := func() int64 {
		var count int64
		require.NoError(t, db.Model(&model.PayablePullTask{}).Count(&count).Error)
		return count
	}

	// 1795363799 = 北京 2026-11-23 00:09:59：还没到 00:10，不建任务
	assert.Empty(t, payableScan(fake, 1795363799))
	assert.Zero(t, taskCount())

	// 1795363800 = 北京 2026-11-23 00:10:00：建账单日期为昨天的任务，并立即拉取
	assert.Equal(t, []int{1}, payableScan(fake, 1795363800))
	assert.Equal(t, int64(1), taskCount())
	task := payableTaskOf(t, db, 20261122)
	assert.Equal(t, 1, task.SiteTotal)
	assert.Equal(t, int64(1795363800), task.StartedTime)
	assert.Equal(t, model.PayableTaskSuccess, task.Status)
	assert.Equal(t, int64(1795363800), task.FinishedTime)
	pull := payablePullOf(t, db, 20261122, site.Id)
	assert.Equal(t, model.PayablePullSuccess, pull.Status)
	assert.Zero(t, pull.ChannelCount)
	assert.Equal(t, int64(1795363800), pull.FinishedTime)
	assert.Zero(t, pull.LeaseUntil)

	// 一分钟后再扫：任务不重复建，已成功的进度不再请求
	assert.Empty(t, payableScan(fake, 1795363860))
	assert.Equal(t, int64(1), taskCount())
}

func TestPayableBillPullResumesFromFailedPage(t *testing.T) {
	db := setupPayableBillTest(t)
	channels := make([]fakeChannel, 250)
	for i := range channels {
		channels[i] = fakeChannel{ID: i + 1, UsedQuota: int64(i+1) * 1000}
	}
	fake := newFakeChannelSite(t, channels...)
	badGatewaySent := false
	fake.update(func() {
		fake.intercept = func(page int) (int, string) {
			if page == 2 && !badGatewaySent {
				badGatewaySent = true
				return http.StatusBadGateway, ""
			}
			return 0, ""
		}
	})
	site := receivableSeedSite(t, db, "pb-resume", fake.server.URL, 1)
	const now = int64(1795363800) // 北京 2026-11-23 00:10

	// 第 2 页 502：第 1 页的 100 个渠道已落库，进度记下第 2 页，1 分钟后重试
	assert.Equal(t, []int{1, 2}, payableScan(fake, now))
	pull := payablePullOf(t, db, 20261122, site.Id)
	assert.Equal(t, model.PayablePullRetrying, pull.Status)
	assert.Equal(t, 2, pull.NextPage)
	assert.Equal(t, 1, pull.Attempts)
	assert.Equal(t, now+60, pull.NextRetryAt)
	assert.Equal(t, 100, pull.ChannelCount)
	assert.Equal(t, 250, pull.ReportedTotal)
	assert.Zero(t, pull.LeaseUntil)
	assert.Equal(t, common.Message{Key: "Site request failed: {{detail}}", Params: map[string]any{"detail": "HTTP 502"}},
		receivableLastError(t, pull.LastError))
	assert.Equal(t, model.PayableTaskRunning, payableTaskOf(t, db, 20261122).Status)

	// 还没到重试时间：不请求
	assert.Empty(t, payableScan(fake, now+59))

	// 到了重试时间：从第 2 页接续，不从第 1 页重来
	assert.Equal(t, []int{2, 3}, payableScan(fake, now+60))
	pull = payablePullOf(t, db, 20261122, site.Id)
	assert.Equal(t, model.PayablePullSuccess, pull.Status)
	assert.Equal(t, 250, pull.ChannelCount)
	assert.Zero(t, pull.Attempts)
	assert.Zero(t, pull.NextRetryAt)
	assert.Empty(t, pull.LastError)
	assert.Equal(t, now+60, pull.FinishedTime)
	assert.Equal(t, int64(250), payableUsageCount(t, db, pull.Id))
	task := payableTaskOf(t, db, 20261122)
	assert.Equal(t, model.PayableTaskSuccess, task.Status)
	assert.Equal(t, now+60, task.FinishedTime)
}

func TestPayableBillNonRetryableFailureStops(t *testing.T) {
	db := setupPayableBillTest(t)
	fake := newFakeChannelSite(t, fakeChannel{ID: 1, UsedQuota: 10})
	fake.update(func() {
		fake.intercept = func(int) (int, string) {
			return http.StatusOK, `{"success":false,"message":"无权进行此操作"}`
		}
	})
	site := receivableSeedSite(t, db, "pb-refused", fake.server.URL, 1)
	const now = int64(1795363800) // 北京 2026-11-23 00:10

	assert.Equal(t, []int{1}, payableScan(fake, now))
	pull := payablePullOf(t, db, 20261122, site.Id)
	assert.Equal(t, model.PayablePullFailed, pull.Status)
	assert.Equal(t, 1, pull.Attempts)
	assert.Zero(t, pull.NextRetryAt)
	assert.Equal(t, now, pull.FinishedTime)
	assert.Equal(t, "无权进行此操作", receivableLastError(t, pull.LastError).Params["detail"])
	task := payableTaskOf(t, db, 20261122)
	assert.Equal(t, model.PayableTaskPartialFailed, task.Status)
	assert.Equal(t, now, task.FinishedTime)

	// 失败的进度不再请求
	assert.Empty(t, payableScan(fake, now+3600))
	assert.Equal(t, model.PayablePullFailed, payablePullOf(t, db, 20261122, site.Id).Status)
}

func TestPayableBillScanRefreshesSettledTasks(t *testing.T) {
	db := setupPayableBillTest(t)
	sites := []*model.ThirdPartySite{
		receivableSeedSite(t, db, "pb-settled-a", "https://pb-settled-a.example.com", 1),
		receivableSeedSite(t, db, "pb-settled-b", "https://pb-settled-b.example.com", 1),
	}
	const now = int64(1795363500) // 北京 2026-11-23 00:05：还没到建任务的时刻
	// 三个任务都停在 running。前两个的进度已全部结束：并发刷新任务时一方读到旧快照就会留下这种状态，这里直接写库构造。
	// 第三个仍有进度在等重试，还没到重试时间，本次扫描不请求。
	for billDate, statuses := range map[int][]string{
		20261119: {model.PayablePullSuccess, model.PayablePullFailed},
		20261120: {model.PayablePullSuccess, model.PayablePullSuccess},
		20261121: {model.PayablePullSuccess, model.PayablePullRetrying},
	} {
		task := &model.PayablePullTask{BillDate: billDate, SiteTotal: len(sites), Status: model.PayableTaskRunning, StartedTime: now - 3600}
		require.NoError(t, db.Create(task).Error)
		for i, status := range statuses {
			pull := &model.PayablePullSite{TaskId: task.Id, SiteId: sites[i].Id, BillDate: billDate, Status: status, NextPage: 2, Pass: 1}
			if status == model.PayablePullRetrying {
				pull.NextRetryAt = now + 60
			}
			require.NoError(t, db.Create(pull).Error)
		}
	}

	service.RunPayableBillScan(context.Background(), time.Unix(now, 0).UTC())

	// 进度都已结束的任务按进度状态补正，完成时间记为这次扫描的时刻
	for billDate, status := range map[int]string{20261119: model.PayableTaskPartialFailed, 20261120: model.PayableTaskSuccess} {
		task := payableTaskOf(t, db, billDate)
		assert.Equal(t, status, task.Status, billDate)
		assert.Equal(t, now, task.FinishedTime, billDate)
	}
	// 还有进度在等重试的任务保持 running
	waiting := payableTaskOf(t, db, 20261121)
	assert.Equal(t, model.PayableTaskRunning, waiting.Status)
	assert.Zero(t, waiting.FinishedTime)
}

func TestPayableBillPullsOnlyLatestTask(t *testing.T) {
	db := setupPayableBillTest(t)
	fake := newFakeChannelSite(t, fakeChannel{ID: 1, UsedQuota: 100})
	fake.update(func() {
		fake.intercept = func(int) (int, string) { return http.StatusOK, `{"success":false,"message":"无权进行此操作"}` }
	})
	site := receivableSeedSite(t, db, "pb-latest", fake.server.URL, 1)
	const day1, day = int64(1795363800), int64(86400) // 北京 2026-11-23 00:10 建账单日期 20261122 的任务，之后每天 00:10 建前一天的
	// 前一天（20261122）的进度不可重试地失败，次日（20261123）的进度成功
	assert.Equal(t, []int{1}, payableScan(fake, day1))
	require.Equal(t, model.PayablePullFailed, payablePullOf(t, db, 20261122, site.Id).Status)
	fake.update(func() { fake.intercept = nil })
	assert.Equal(t, []int{1}, payableScan(fake, day1+day))
	require.Equal(t, model.PayablePullSuccess, payablePullOf(t, db, 20261123, site.Id).Status)
	// 站点编辑与次日建任务并发时，编辑按建任务之前的视图把前一天的进度改回 retrying、任务改回 running（这里直接写库构造）
	reopenStale := func() {
		require.NoError(t, db.Model(&model.PayablePullSite{}).Where("bill_date = ?", 20261122).
			Updates(map[string]any{"status": model.PayablePullRetrying, "attempts": 0, "next_retry_at": 0, "finished_time": 0}).Error)
		require.NoError(t, db.Model(&model.PayablePullTask{}).Where("bill_date = ?", 20261122).
			Updates(map[string]any{"status": model.PayableTaskRunning, "finished_time": 0}).Error)
	}

	// 下一次扫描（北京 2026-11-24 00:11）照常执行取代：旧进度改为 superseded，旧任务回到 partial_failed，不请求站点
	reopenStale()
	assert.Empty(t, payableScan(fake, day1+day+60))
	stale := payablePullOf(t, db, 20261122, site.Id)
	assert.Equal(t, model.PayablePullSuperseded, stale.Status)
	assert.Equal(t, model.PayableTaskPartialFailed, payableTaskOf(t, db, 20261122).Status)

	// 不建任务的时段（北京 2026-11-25 00:05）不执行取代；旧进度不属于最新任务，同样不拉
	reopenStale()
	assert.Empty(t, payableScan(fake, day1+2*day-300))
	assert.Equal(t, model.PayablePullRetrying, payablePullOf(t, db, 20261122, site.Id).Status)
	assert.Zero(t, payableUsageCount(t, db, stale.Id))
}

func TestPayableBillRecheckMissingChannels(t *testing.T) {
	db := setupPayableBillTest(t)
	// A：第一遍只给 2 个渠道却报 total 3（拉取期间渠道移位），第二遍给全 3 个
	fakeA := newFakeChannelSite(t, fakeChannel{ID: 3, UsedQuota: 30}, fakeChannel{ID: 2, UsedQuota: 20})
	fakeA.update(func() {
		fakeA.total = 3
		fakeA.intercept = func(int) (int, string) {
			if len(fakeA.pages) == 2 {
				fakeA.channels = append(fakeA.channels, fakeChannel{ID: 1, UsedQuota: 10})
			}
			return 0, ""
		}
	})
	// B：两遍都只给 2 个
	fakeB := newFakeChannelSite(t, fakeChannel{ID: 3, UsedQuota: 30}, fakeChannel{ID: 2, UsedQuota: 20})
	fakeB.update(func() { fakeB.total = 3 })
	siteA := receivableSeedSite(t, db, "pb-recheck-a", fakeA.server.URL, 1)
	siteB := receivableSeedSite(t, db, "pb-recheck-b", fakeB.server.URL, 1)

	// 第一遍拉完不到 total：从第 1 页补扫一遍，只写缺的渠道
	assert.Equal(t, []int{1, 1}, payableScan(fakeA, 1795363800))
	assert.Equal(t, []int{1, 1}, fakeB.requested())

	pullA := payablePullOf(t, db, 20261122, siteA.Id)
	assert.Equal(t, model.PayablePullSuccess, pullA.Status)
	assert.Equal(t, 2, pullA.Pass)
	assert.Equal(t, 3, pullA.ChannelCount)
	assert.Empty(t, pullA.Note)
	var remoteIDs []int
	require.NoError(t, db.Model(&model.PayableChannelUsage{}).Where("pull_site_id = ?", pullA.Id).
		Order("remote_channel_id").Pluck("remote_channel_id", &remoteIDs).Error)
	assert.Equal(t, []int{1, 2, 3}, remoteIDs)

	// 补扫后仍对不上：以已拉到的为准结束，记下总数不一致
	pullB := payablePullOf(t, db, 20261122, siteB.Id)
	assert.Equal(t, model.PayablePullSuccess, pullB.Status)
	assert.Equal(t, 2, pullB.ChannelCount)
	var note common.Message
	require.NoError(t, common.UnmarshalJsonStr(pullB.Note, &note))
	assert.Equal(t, common.Message{Key: "Site reported {{total}} channels but {{count}} were pulled",
		Params: map[string]any{"total": float64(3), "count": float64(2)}}, note)
	assert.Equal(t, model.PayableTaskSuccess, payableTaskOf(t, db, 20261122).Status)
}

func TestPayableBillDeltaRules(t *testing.T) {
	db := setupPayableBillTest(t)
	const a, b, c = 11, 12, 13
	fake := newFakeChannelSite(t, fakeChannel{ID: a, UsedQuota: 100}, fakeChannel{ID: b, UsedQuota: 500})
	site := receivableSeedSite(t, db, "pb-delta", fake.server.URL, 1)
	const day1, day = int64(1795363800), int64(86400) // 北京 2026-11-23 00:10 拉账单日期 20261122，之后每天 00:10
	const refusal = `{"success":false,"message":"无权进行此操作"}`

	// 日 1：站点此前没有完整拉取，没有基线，增量为空
	assert.Equal(t, []int{1}, payableScan(fake, day1))
	assert.Equal(t, map[int]string{a: "first_sync nil", b: "first_sync nil"}, payableDeltas(t, db, 20261122))
	pull := payablePullOf(t, db, 20261122, site.Id)
	var rowA model.PayableChannelUsage
	require.NoError(t, db.Where("pull_site_id = ? AND remote_channel_id = ?", pull.Id, a).First(&rowA).Error)
	assert.Equal(t, pull.TaskId, rowA.TaskId)
	assert.Equal(t, 20261122, rowA.BillDate)
	assert.Equal(t, site.Id, rowA.SiteId)
	assert.Equal(t, site.Name, rowA.SiteName)
	assert.Equal(t, site.BaseURL, rowA.SiteBaseURL)
	assert.Equal(t, "ch-11", rowA.RemoteChannelName)
	assert.Equal(t, int64(100), rowA.UsedQuota)
	assert.Equal(t, model.ReceivableBillQuotaPerUnit, rowA.QuotaPerUnit)
	assert.Positive(t, rowA.SyncedAt)
	// 渠道名不带标记、没有手动指定也没有历史：提供商未知，没有折扣
	assert.Zero(t, rowA.ProviderId)
	assert.Empty(t, rowA.ProviderName)
	assert.Equal(t, model.PayableProviderUnknown, rowA.ProviderSource)
	assert.Nil(t, rowA.PayableDiscount)

	// 日 2：基线是日 1 的完整拉取；累计值回退记 0，上次没有的渠道基线为 0
	fake.update(func() {
		fake.channels = []fakeChannel{{ID: a, UsedQuota: 160}, {ID: b, UsedQuota: 400}, {ID: c, UsedQuota: 70}}
	})
	assert.Equal(t, []int{1}, payableScan(fake, day1+day))
	assert.Equal(t, map[int]string{a: "normal 60", b: "rollback 0", c: "new_channel 70"}, payableDeltas(t, db, 20261123))

	// 日 3：站点报 101 个渠道，第 1 页成功后第 2 页不可重试失败，这一天的拉取不完整；已拉到的行照常按日 2 算增量
	fake.update(func() {
		fake.channels = []fakeChannel{{ID: a, UsedQuota: 200}, {ID: b, UsedQuota: 450}, {ID: c, UsedQuota: 70}}
		fake.total = 101
		fake.intercept = func(page int) (int, string) {
			if page == 2 {
				return http.StatusOK, refusal
			}
			return 0, ""
		}
	})
	assert.Equal(t, []int{1, 2}, payableScan(fake, day1+2*day))
	assert.Equal(t, model.PayablePullFailed, payablePullOf(t, db, 20261124, site.Id).Status)
	assert.Equal(t, map[int]string{a: "normal 40", b: "normal 50", c: "normal 0"}, payableDeltas(t, db, 20261124))

	// 日 4：不完整拉取不作基线，基线仍是日 2
	fake.update(func() {
		fake.channels = []fakeChannel{{ID: a, UsedQuota: 260}, {ID: b, UsedQuota: 450}, {ID: c, UsedQuota: 70}}
		fake.total, fake.intercept = -1, nil
	})
	assert.Equal(t, []int{1}, payableScan(fake, day1+3*day))
	assert.Equal(t, map[int]string{a: "normal 100", b: "normal 50", c: "normal 0"}, payableDeltas(t, db, 20261125))

	// 日 5：站点地址改了（同一个假站点换一种写法），可能换了实例，基线重置，所有行首次同步
	newURL := strings.Replace(fake.server.URL, "127.0.0.1", "localhost", 1)
	require.NotEqual(t, fake.server.URL, newURL)
	_, err := model.UpdateThirdPartySite(site.Id, model.ThirdPartySiteInput{Name: site.Name, Type: model.ThirdPartySiteTypeNewAPI,
		BaseURL: newURL, RemoteUserID: site.RemoteUserID, ReceivableDiscount: site.ReceivableDiscount})
	require.NoError(t, err)
	assert.Equal(t, []int{1}, payableScan(fake, day1+4*day))
	assert.Equal(t, map[int]string{a: "first_sync nil", b: "first_sync nil", c: "first_sync nil"}, payableDeltas(t, db, 20261126))
	assert.Equal(t, newURL, payablePullOf(t, db, 20261126, site.Id).SiteBaseURL)
}

func TestPayableBillDeltaAfterCountMismatch(t *testing.T) {
	db := setupPayableBillTest(t)
	const a, b, c = 11, 12, 13
	fake := newFakeChannelSite(t, fakeChannel{ID: a, UsedQuota: 100}, fakeChannel{ID: b, UsedQuota: 500})
	site := receivableSeedSite(t, db, "pb-mismatch", fake.server.URL, 1)
	const day1, day = int64(1795363800), int64(86400) // 北京 2026-11-23 00:10 拉账单日期 20261122，之后每天 00:10

	// 日 1：完整拉取
	assert.Equal(t, []int{1}, payableScan(fake, day1))
	// 日 2：站点报 3 个渠道，两遍都只给 2 个：以已拉到的为准结束（success，总数不一致）
	fake.update(func() {
		fake.channels = []fakeChannel{{ID: a, UsedQuota: 160}, {ID: b, UsedQuota: 520}}
		fake.total = 3
	})
	assert.Equal(t, []int{1, 1}, payableScan(fake, day1+day))
	mismatched := payablePullOf(t, db, 20261123, site.Id)
	require.Equal(t, model.PayablePullSuccess, mismatched.Status)
	require.Equal(t, 2, mismatched.ChannelCount)
	require.Equal(t, 3, mismatched.ReportedTotal)
	assert.Equal(t, map[int]string{a: "normal 60", b: "normal 20"}, payableDeltas(t, db, 20261123))

	// 日 3：第三个渠道出现。日 2 少拉了渠道，基线里没有它不能说明它是新建的，按首次同步（增量为空，不计入账单）；
	// 基线里有的渠道照常算增量
	fake.update(func() {
		fake.channels = []fakeChannel{{ID: a, UsedQuota: 200}, {ID: b, UsedQuota: 550}, {ID: c, UsedQuota: 70}}
		fake.total = -1
	})
	assert.Equal(t, []int{1}, payableScan(fake, day1+2*day))
	assert.Equal(t, map[int]string{a: "normal 40", b: "normal 30", c: "first_sync nil"}, payableDeltas(t, db, 20261124))
}

func TestPayableBillProviderResolution(t *testing.T) {
	db := setupPayableBillTest(t)
	s1 := channelPushSeedSupplier(t, db, "pb-s1")
	s2 := channelPushSeedSupplier(t, db, "pb-s2")
	u3 := &model.User{Username: "pb-u3", Role: common.RoleCommonUser, Status: 1, AffCode: "pb-u3"}
	require.NoError(t, db.Create(u3).Error)
	discount := 0.8
	require.NoError(t, db.Model(s1).Update("payable_discount", discount).Error)
	ch1 := channelPushSeedChannel(t, db, s1, "pb-ch1", 1, "m", 1)
	ch2 := channelPushSeedChannel(t, db, s2, "pb-ch2", 1, "m", 1)

	fake := newFakeChannelSite(t)
	site := receivableSeedSite(t, db, "pb-provider", fake.server.URL, 1)
	mark := func(siteID, channelID int) string {
		return "x-dcmark-" + strconv.Itoa(siteID) + "-" + strconv.Itoa(channelID)
	}
	fake.update(func() {
		fake.channels = []fakeChannel{
			{ID: 1, Name: mark(site.Id, ch1.Id)},
			{ID: 2, Name: mark(site.Id, ch2.Id)},
			{ID: 3, Name: mark(site.Id+100, ch1.Id)},
			{ID: 4, Name: "manual"},
			{ID: 5, Name: mark(site.Id, ch1.Id)},
			{ID: 6, Name: "renamed"},
			{ID: 7, Name: "plain"},
		}
	})
	require.NoError(t, db.Create(&[]model.PayableProviderOverride{
		{SiteId: site.Id, RemoteChannelId: 4, ProviderId: s2.Id},
		{SiteId: site.Id, RemoteChannelId: 5, ProviderId: u3.Id},
	}).Error)
	// 上一账单日的明细：r6 提供商是 s1（继承）；r7 的提供商 id 已不是供应商，不继承
	require.NoError(t, db.Create(&[]model.PayableChannelUsage{
		{TaskId: 900, PullSiteId: 900, BillDate: 20261121, SiteId: site.Id, RemoteChannelId: 6, ProviderId: s1.Id},
		{TaskId: 900, PullSiteId: 900, BillDate: 20261121, SiteId: site.Id, RemoteChannelId: 7, ProviderId: u3.Id},
		// 更早的几天：r6 前一天是 s2，以最近一天（s1）为准；r7 更早一天是 s1，但最近一天的用户已不是供应商，不往更早找
		{TaskId: 899, PullSiteId: 899, BillDate: 20261120, SiteId: site.Id, RemoteChannelId: 6, ProviderId: s2.Id},
		{TaskId: 899, PullSiteId: 899, BillDate: 20261120, SiteId: site.Id, RemoteChannelId: 7, ProviderId: s1.Id},
	}).Error)

	const day1 = int64(1795363800) // 账单日期 20261122
	assert.Equal(t, []int{1}, payableScan(fake, day1))

	type resolved struct {
		ProviderId int
		Source     string
		Discount   *float64
	}
	snapshot := func() map[int]resolved {
		var rows []model.PayableChannelUsage
		require.NoError(t, db.Where("bill_date = ?", 20261122).Find(&rows).Error)
		got := make(map[int]resolved, len(rows))
		for _, row := range rows {
			got[row.RemoteChannelId] = resolved{row.ProviderId, row.ProviderSource, row.PayableDiscount}
			if row.ProviderId > 0 {
				assert.NotEmpty(t, row.ProviderName)
			} else {
				assert.Empty(t, row.ProviderName)
			}
		}
		return got
	}
	assert.Equal(t, map[int]resolved{
		1: {s1.Id, model.PayableProviderParsed, &discount},
		2: {s2.Id, model.PayableProviderParsed, nil},
		3: {0, model.PayableProviderUnknown, nil},
		4: {s2.Id, model.PayableProviderManual, nil},
		5: {s1.Id, model.PayableProviderParsed, &discount},
		6: {s1.Id, model.PayableProviderInherited, &discount},
		7: {0, model.PayableProviderUnknown, nil},
	}, snapshot())

	// 之后改折扣，已写入的快照不变
	require.NoError(t, db.Model(s1).Update("payable_discount", 0.5).Error)
	assert.Equal(t, 0.8, *snapshot()[1].Discount)

	// 供应商被禁用仍然算供应商；改成普通用户后，手动指定、标记、历史都不再命中
	require.NoError(t, db.Model(s2).Update("status", 2).Error)
	providers, err := model.ResolvePayableProviders(site.Id, 20261123, []model.PayableRemoteChannel{{ID: 2, LocalChannelID: ch2.Id}, {ID: 4}})
	require.NoError(t, err)
	assert.Equal(t, s2.Id, providers[2].ID)
	assert.Equal(t, s2.Id, providers[4].ID)
	require.NoError(t, db.Model(s2).Update("role", common.RoleCommonUser).Error)
	providers, err = model.ResolvePayableProviders(site.Id, 20261123, []model.PayableRemoteChannel{{ID: 2, LocalChannelID: ch2.Id}, {ID: 4}})
	require.NoError(t, err)
	assert.Equal(t, model.PayableProvider{Source: model.PayableProviderUnknown}, providers[2])
	assert.Equal(t, model.PayableProvider{Source: model.PayableProviderUnknown}, providers[4])
}

func payablePtr[T any](value T) *T { return &value }

func payableGet(t *testing.T, target string, params gin.Params, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	return receivableRequest(t, http.MethodGet, target, nil, params, handler)
}

func payableSeedTask(t *testing.T, db *gorm.DB, billDate int, status string) *model.PayablePullTask {
	t.Helper()
	task := &model.PayablePullTask{BillDate: billDate, Status: status}
	require.NoError(t, db.Create(task).Error)
	return task
}

func payableSeedPull(t *testing.T, db *gorm.DB, task *model.PayablePullTask, siteID int, status string) *model.PayablePullSite {
	t.Helper()
	pull := &model.PayablePullSite{TaskId: task.Id, SiteId: siteID, BillDate: task.BillDate, Status: status}
	require.NoError(t, db.Create(pull).Error)
	return pull
}

func payableSeedUsage(t *testing.T, db *gorm.DB, pull *model.PayablePullSite, channelID int, used int64, delta *int64, providerID int, discount *float64) *model.PayableChannelUsage {
	t.Helper()
	usage := &model.PayableChannelUsage{TaskId: pull.TaskId, PullSiteId: pull.Id, BillDate: pull.BillDate, SiteId: pull.SiteId,
		RemoteChannelId: channelID, UsedQuota: used, DeltaQuota: delta, QuotaPerUnit: 500000,
		ProviderId: providerID, ProviderName: "p" + strconv.Itoa(providerID), PayableDiscount: discount}
	require.NoError(t, db.Create(usage).Error)
	return usage
}

func TestPayablePullTaskList(t *testing.T) {
	db := setupPayableBillTest(t)
	siteA := receivableSeedSite(t, db, "pq-a", "https://pq-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pq-b", "https://pq-b.example.com", 1)
	siteC := receivableSeedSite(t, db, "pq-c", "https://pq-c.example.com", 1)

	// 任务 B 先建（id 小）：已结束，A 成功、B 失败、C 被取代；只有完整拉取的行进入合计。
	taskB := payableSeedTask(t, db, 20261121, model.PayableTaskPartialFailed)
	pullBA := payableSeedPull(t, db, taskB, siteA.Id, model.PayablePullSuccess)
	pullBB := payableSeedPull(t, db, taskB, siteB.Id, model.PayablePullFailed)
	pullBC := payableSeedPull(t, db, taskB, siteC.Id, model.PayablePullSuperseded)
	payableSeedUsage(t, db, pullBA, 1, 1000000, payablePtr(int64(500000)), 0, nil)
	payableSeedUsage(t, db, pullBA, 2, 500000, payablePtr(int64(250000)), 0, nil)
	payableSeedUsage(t, db, pullBB, 1, 999, payablePtr(int64(999)), 0, nil)
	payableSeedUsage(t, db, pullBC, 1, 888, payablePtr(int64(888)), 0, nil)

	// 任务 A：进行中，A 成功含 2 行（增量 10 和 nil），B 重试中含 1 行。
	taskA := payableSeedTask(t, db, 20261122, model.PayableTaskRunning)
	pullAA := payableSeedPull(t, db, taskA, siteA.Id, model.PayablePullSuccess)
	pullAB := payableSeedPull(t, db, taskA, siteB.Id, model.PayablePullRetrying)
	require.NoError(t, db.Model(pullAB).Updates(map[string]any{"last_error": `{"key":"Site request failed: {{detail}}","params":{"detail":"HTTP 502"}}`,
		"note": `{"key":"The site reported a different channel total"}`}).Error)
	payableSeedUsage(t, db, pullAA, 1, 100, payablePtr(int64(10)), 0, nil)
	payableSeedUsage(t, db, pullAA, 2, 50, nil, 0, nil)
	payableSeedUsage(t, db, pullAB, 1, 7, payablePtr(int64(3)), 0, nil)

	response := payableGet(t, "/api/payable_bill/tasks?p=1&page_size=10", nil, GetPayablePullTasks)
	total, items := receivableListOf(t, response)
	require.Equal(t, 2, total)
	require.Len(t, items, 2)
	assert.EqualValues(t, taskA.Id, items[0]["id"])
	assert.EqualValues(t, 20261122, items[0]["bill_date"])
	assert.EqualValues(t, 1, items[0]["finished_sites"])
	assert.EqualValues(t, 1, items[0]["retrying_sites"])
	assert.EqualValues(t, 0, items[0]["failed_sites"])
	assert.EqualValues(t, 3, items[0]["channel_total"])
	assert.Nil(t, items[0]["delta_amount"])
	assert.Nil(t, items[0]["used_amount"])
	assert.EqualValues(t, taskB.Id, items[1]["id"])
	assert.EqualValues(t, 1, items[1]["finished_sites"])
	assert.EqualValues(t, 0, items[1]["retrying_sites"])
	assert.EqualValues(t, 2, items[1]["failed_sites"]) // superseded 算失败
	assert.EqualValues(t, 4, items[1]["channel_total"])
	assert.Equal(t, "1.500000", items[1]["delta_amount"])
	assert.Equal(t, "3.000000", items[1]["used_amount"])

	_, items = receivableListOf(t, payableGet(t, "/api/payable_bill/tasks?p=2&page_size=1", nil, GetPayablePullTasks))
	require.Len(t, items, 1)
	assert.EqualValues(t, taskB.Id, items[0]["id"])

	response = payableGet(t, "/api/payable_bill/tasks/x/sites", gin.Params{{Key: "id", Value: strconv.Itoa(taskA.Id)}}, GetPayablePullTaskSites)
	reply := receivableReplyOf(t, response)
	require.True(t, reply.Success, response.Body.String())
	var sites []map[string]any
	require.NoError(t, common.Unmarshal(reply.Data, &sites))
	require.Len(t, sites, 2)
	assert.EqualValues(t, siteA.Id, sites[0]["site_id"])
	assert.Nil(t, sites[0]["error"])
	assert.Nil(t, sites[0]["note"])
	assert.EqualValues(t, siteB.Id, sites[1]["site_id"])
	assert.Equal(t, "retrying", sites[1]["status"])
	assert.Equal(t, map[string]any{"key": "Site request failed: {{detail}}", "params": map[string]any{"detail": "HTTP 502"}}, sites[1]["error"])
	assert.Equal(t, map[string]any{"key": "The site reported a different channel total"}, sites[1]["note"])
	assert.NotContains(t, response.Body.String(), "last_error")
	assert.NotContains(t, response.Body.String(), "lease_until")

	reply = receivableReplyOf(t, payableGet(t, "/api/payable_bill/tasks/0/sites", gin.Params{{Key: "id", Value: "0"}}, GetPayablePullTaskSites))
	assert.False(t, reply.Success)
}

func TestPayableUsageList(t *testing.T) {
	db := setupPayableBillTest(t)
	siteA := receivableSeedSite(t, db, "pu-a", "https://pu-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pu-b", "https://pu-b.example.com", 1)
	task1121 := payableSeedTask(t, db, 20261121, model.PayableTaskSuccess)
	task1122 := payableSeedTask(t, db, 20261122, model.PayableTaskPartialFailed)
	pullA1121 := payableSeedPull(t, db, task1121, siteA.Id, model.PayablePullSuccess)
	pullA := payableSeedPull(t, db, task1122, siteA.Id, model.PayablePullSuccess)
	pullB := payableSeedPull(t, db, task1122, siteB.Id, model.PayablePullFailed)
	payableSeedUsage(t, db, pullA1121, 1, 500000, payablePtr(int64(500000)), 7, payablePtr(0.8))
	payableSeedUsage(t, db, pullA, 1, 1000000, payablePtr(int64(500000)), 7, payablePtr(0.8))
	payableSeedUsage(t, db, pullA, 2, 250000, nil, 0, nil)
	payableSeedUsage(t, db, pullB, 1, 100, payablePtr(int64(100)), 0, nil)
	require.NoError(t, db.Create(&model.PayableProviderOverride{SiteId: siteA.Id, RemoteChannelId: 2, ProviderId: 7}).Error)
	// 另一个站点的同号渠道有手动指定，不能算到 A 站点的渠道 1 头上。
	require.NoError(t, db.Create(&model.PayableProviderOverride{SiteId: siteB.Id, RemoteChannelId: 2, ProviderId: 7}).Error)

	list := func(query string) (int, []map[string]any) {
		return receivableListOf(t, payableGet(t, "/api/payable_bill/usages?"+query, nil, GetPayableUsages))
	}
	keys := func(items []map[string]any) []string {
		keys := make([]string, len(items))
		for i, item := range items {
			keys[i] = strconv.Itoa(int(item["bill_date"].(float64))) + "/" + strconv.Itoa(int(item["site_id"].(float64))) + "/" + strconv.Itoa(int(item["remote_channel_id"].(float64)))
		}
		return keys
	}
	keyOf := func(date, siteID, channelID int) string {
		return strconv.Itoa(date) + "/" + strconv.Itoa(siteID) + "/" + strconv.Itoa(channelID)
	}
	a1122c1, a1122c2, b1122c1, a1121c1 := keyOf(20261122, siteA.Id, 1), keyOf(20261122, siteA.Id, 2), keyOf(20261122, siteB.Id, 1), keyOf(20261121, siteA.Id, 1)

	response := payableGet(t, "/api/payable_bill/usages?start_date=20261121&end_date=20261123", nil, GetPayableUsages)
	total, items := receivableListOf(t, response)
	assert.Equal(t, 4, total)
	require.Equal(t, []string{a1122c1, a1122c2, b1122c1, a1121c1}, keys(items))
	assert.Equal(t, []any{true, true, false, true}, []any{items[0]["counted"], items[1]["counted"], items[2]["counted"], items[3]["counted"]})
	assert.Equal(t, []any{false, true, false, false}, []any{items[0]["manual"], items[1]["manual"], items[2]["manual"], items[3]["manual"]})
	assert.Equal(t, "1.000000", items[0]["delta_amount"])
	assert.Equal(t, "2.000000", items[0]["used_amount"])
	assert.Nil(t, items[1]["delta_amount"])
	assert.Nil(t, items[1]["delta_quota"])
	assert.Equal(t, "0.500000", items[1]["used_amount"])
	assert.EqualValues(t, 0.8, items[0]["payable_discount"])
	assert.NotContains(t, response.Body.String(), "last_error")

	_, items = list("start_date=20261122&end_date=20261122&site_ids=" + strconv.Itoa(siteB.Id))
	assert.Equal(t, []string{b1122c1}, keys(items))

	total, items = list("start_date=20261121&end_date=20261123&provider_ids=0")
	assert.Equal(t, 2, total)
	assert.Equal(t, []string{a1122c2, b1122c1}, keys(items))
	total, _ = list("start_date=20261121&end_date=20261123&provider_ids=0,7")
	assert.Equal(t, 4, total)
	total, items = list("start_date=20261121&end_date=20261121")
	assert.Equal(t, 1, total)
	assert.Equal(t, []string{a1121c1}, keys(items))

	total, items = list("start_date=20261121&end_date=20261123&p=2&page_size=3")
	assert.Equal(t, 4, total)
	assert.Equal(t, []string{a1121c1}, keys(items))

	for _, query := range []string{
		"", "start_date=20261123&end_date=20261121", "start_date=2026&end_date=20261123", "start_date=20261121&end_date=20261123&provider_ids=-1",
		"start_date=20261121&end_date=20261123&provider_ids=a", "start_date=20261121&end_date=20261123&site_ids=0",
	} {
		reply := receivableReplyOf(t, payableGet(t, "/api/payable_bill/usages?"+query, nil, GetPayableUsages))
		assert.False(t, reply.Success, query)
	}
}

func TestPayableProviderBills(t *testing.T) {
	db := setupPayableBillTest(t)
	siteA := receivableSeedSite(t, db, "pp-a", "https://pp-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pp-b", "https://pp-b.example.com", 1)
	siteC := receivableSeedSite(t, db, "pp-c", "https://pp-c.example.com", 1)
	task := payableSeedTask(t, db, 20261122, model.PayableTaskPartialFailed)
	pullA := payableSeedPull(t, db, task, siteA.Id, model.PayablePullSuccess)
	pullB := payableSeedPull(t, db, task, siteB.Id, model.PayablePullFailed)
	pullC := payableSeedPull(t, db, task, siteC.Id, model.PayablePullSuccess)
	half, d08, d09 := payablePtr(int64(500000)), payablePtr(0.8), payablePtr(0.9)

	// 提供商 7：增量 50 万和 100 万折扣 0.8，增量为 nil 的行不计。名称取组内 id 最大的参与行。
	usage := payableSeedUsage(t, db, pullA, 1, 0, half, 7, d08)
	require.NoError(t, db.Model(usage).Update("provider_name", "p7-old").Error)
	usage = payableSeedUsage(t, db, pullA, 2, 0, payablePtr(int64(1000000)), 7, d08)
	require.NoError(t, db.Model(usage).Update("provider_name", "p7-new").Error)
	usage = payableSeedUsage(t, db, pullA, 3, 0, nil, 7, nil)
	require.NoError(t, db.Model(usage).Update("provider_name", "p7-nil").Error)
	// 提供商 8：折扣 0.8 与 0.9 各一行。
	payableSeedUsage(t, db, pullA, 4, 0, half, 8, d08)
	payableSeedUsage(t, db, pullA, 5, 0, half, 8, d09)
	// 提供商 9：一行 0.8、一行缺折扣。
	payableSeedUsage(t, db, pullA, 6, 0, half, 9, d08)
	payableSeedUsage(t, db, pullA, 7, 0, half, 9, nil)
	// 提供商 10：全部缺折扣。
	payableSeedUsage(t, db, pullA, 8, 0, half, 10, nil)
	// 不完整拉取的行不计入。
	payableSeedUsage(t, db, pullB, 1, 0, payablePtr(int64(999999)), 7, d08)
	// 站点 C：未知提供商和提供商 11。
	payableSeedUsage(t, db, pullC, 1, 0, half, 0, nil)
	payableSeedUsage(t, db, pullC, 2, 0, half, 11, payablePtr(1.0))

	list := func(query string) map[string]any {
		response := payableGet(t, "/api/payable_bill/providers?"+query, nil, GetPayableProviderBills)
		reply := receivableReplyOf(t, response)
		require.True(t, reply.Success, response.Body.String())
		var page map[string]any
		require.NoError(t, common.Unmarshal(reply.Data, &page))
		return page
	}
	itemsOf := func(page map[string]any) []map[string]any {
		var items []map[string]any
		for _, item := range page["items"].([]any) {
			items = append(items, item.(map[string]any))
		}
		return items
	}

	page := list("start_date=20261122&end_date=20261122")
	items := itemsOf(page)
	require.Len(t, items, 6)
	assert.EqualValues(t, 6, page["total"])
	assert.EqualValues(t, 1, page["incomplete_site_count"])
	providerIDs := make([]any, len(items))
	for i, item := range items {
		providerIDs[i] = item["provider_id"]
	}
	assert.Equal(t, []any{0.0, 7.0, 8.0, 9.0, 10.0, 11.0}, providerIDs)

	assert.EqualValues(t, 20261122, items[1]["bill_date"])
	assert.Equal(t, "p7-new", items[1]["provider_name"])
	assert.Equal(t, "3.000000", items[1]["amount"])
	assert.EqualValues(t, 0.8, items[1]["discount"])
	assert.Equal(t, false, items[1]["discount_mixed"])
	assert.Equal(t, false, items[1]["discount_missing"])
	assert.Equal(t, "2.400000", items[1]["payable_amount"])

	assert.Equal(t, "2.000000", items[2]["amount"])
	assert.Nil(t, items[2]["discount"])
	assert.Equal(t, true, items[2]["discount_mixed"])
	assert.Equal(t, false, items[2]["discount_missing"])
	assert.Equal(t, "1.700000", items[2]["payable_amount"])

	assert.Equal(t, "2.000000", items[3]["amount"])
	assert.Nil(t, items[3]["discount"])
	assert.Equal(t, false, items[3]["discount_mixed"])
	assert.Equal(t, true, items[3]["discount_missing"])
	assert.Equal(t, "0.800000", items[3]["payable_amount"])

	assert.Equal(t, "1.000000", items[4]["amount"])
	assert.Equal(t, true, items[4]["discount_missing"])
	assert.Nil(t, items[4]["payable_amount"])
	assert.Equal(t, "p0", items[0]["provider_name"])
	assert.Nil(t, items[0]["payable_amount"])

	page = list("start_date=20261122&end_date=20261122&site_ids=" + strconv.Itoa(siteA.Id))
	assert.EqualValues(t, 4, page["total"])
	assert.EqualValues(t, 0, page["incomplete_site_count"])
	assert.Equal(t, "3.000000", itemsOf(page)[0]["amount"])

	page = list("start_date=20261122&end_date=20261122&provider_ids=0")
	items = itemsOf(page)
	require.Len(t, items, 1)
	assert.EqualValues(t, 0, items[0]["provider_id"])
	assert.Equal(t, "1.000000", items[0]["amount"])

	page = list("start_date=20261122&end_date=20261122&p=2&page_size=2")
	items = itemsOf(page)
	assert.EqualValues(t, 6, page["total"])
	assert.EqualValues(t, 2, page["page"])
	require.Len(t, items, 2)
	assert.EqualValues(t, 8, items[0]["provider_id"])
	assert.EqualValues(t, 9, items[1]["provider_id"])
	assert.Equal(t, "1.700000", items[0]["payable_amount"])

	// 范围内没有数据，也没有未完成进度。
	page = list("start_date=20261101&end_date=20261121")
	assert.EqualValues(t, 0, page["total"])
	assert.Empty(t, page["items"])
	assert.EqualValues(t, 0, page["incomplete_site_count"])

	// limit <= 0 返回全部组（导出用）。
	bills, total, err := model.ListPayableProviderBills(model.PayableUsageFilter{StartDate: 20261122, EndDate: 20261122}, 0, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 6, total)
	assert.Len(t, bills, 6)

	reply := receivableReplyOf(t, payableGet(t, "/api/payable_bill/providers?start_date=x&end_date=20261122", nil, GetPayableProviderBills))
	assert.False(t, reply.Success)
}

func TestPayableOptions(t *testing.T) {
	db := setupPayableBillTest(t)
	site := receivableSeedSite(t, db, "po-site", "https://po.example.com", 1)
	enabled := channelPushSeedSupplier(t, db, "po-enabled")
	disabled := channelPushSeedSupplier(t, db, "po-disabled")
	deleted := channelPushSeedSupplier(t, db, "po-deleted")
	require.NoError(t, db.Create(&model.User{Username: "po-common", Role: common.RoleCommonUser, Status: 1, AffCode: "po-common"}).Error)
	require.NoError(t, db.Model(enabled).Update("payable_discount", 0.8).Error)
	require.NoError(t, db.Model(disabled).Update("status", 2).Error)
	require.NoError(t, db.Delete(deleted).Error)

	response := payableGet(t, "/api/payable_bill/options", nil, GetPayableOptions)
	reply := receivableReplyOf(t, response)
	require.True(t, reply.Success, response.Body.String())
	var data struct {
		Sites     []map[string]any `json:"sites"`
		Providers []map[string]any `json:"providers"`
	}
	require.NoError(t, common.Unmarshal(reply.Data, &data))
	require.Len(t, data.Sites, 1)
	assert.EqualValues(t, site.Id, data.Sites[0]["id"])
	assert.Equal(t, []map[string]any{
		{"id": float64(enabled.Id), "username": "po-enabled", "disabled": false, "payable_discount": 0.8},
		{"id": float64(disabled.Id), "username": "po-disabled", "disabled": true, "payable_discount": nil},
	}, data.Providers)
}

// 导出走 limit <= 0 返回全部行：行数超过各库的绑定参数上限时查询也不能失败，Counted 与 Manual 仍要正确。
func TestPayableUsagesWithoutLimitReturnsAllRows(t *testing.T) {
	db := setupPayableBillTest(t)
	siteA := receivableSeedSite(t, db, "pl-a", "https://pl-a.example.com", 1)
	siteB := receivableSeedSite(t, db, "pl-b", "https://pl-b.example.com", 1)
	const days, channels = 110, 300
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var usages []model.PayableChannelUsage
	for day := range days {
		billDate, err := strconv.Atoi(first.AddDate(0, 0, day).Format("20060102"))
		require.NoError(t, err)
		task := payableSeedTask(t, db, billDate, model.PayableTaskPartialFailed)
		pullA := payableSeedPull(t, db, task, siteA.Id, model.PayablePullSuccess)
		pullB := payableSeedPull(t, db, task, siteB.Id, model.PayablePullFailed)
		for channel := 1; channel <= channels/2; channel++ {
			for _, pull := range []*model.PayablePullSite{pullA, pullB} {
				usages = append(usages, model.PayableChannelUsage{TaskId: task.Id, PullSiteId: pull.Id, BillDate: billDate, SiteId: pull.SiteId,
					RemoteChannelId: channel, QuotaPerUnit: 500000})
			}
		}
	}
	require.NoError(t, db.CreateInBatches(usages, 500).Error)
	require.NoError(t, db.Create(&model.PayableProviderOverride{SiteId: siteA.Id, RemoteChannelId: 7, ProviderId: 1}).Error)

	filter := model.PayableUsageFilter{StartDate: 20260101, EndDate: 20261231}
	rows, total, err := model.ListPayableUsages(filter, 0, 0)
	require.NoError(t, err)
	require.EqualValues(t, days*channels, total)
	require.Len(t, rows, days*channels)
	var counted, manual int
	for _, row := range rows {
		assert.Equal(t, row.SiteId == siteA.Id, row.Counted)
		assert.Equal(t, row.SiteId == siteA.Id && row.RemoteChannelId == 7, row.Manual)
		if row.Counted {
			counted++
		}
		if row.Manual {
			manual++
		}
	}
	assert.Equal(t, days*channels/2, counted)
	assert.Equal(t, days, manual)
}

func TestPayableBillSetProvider(t *testing.T) {
	db := setupPayableBillTest(t)
	s1 := channelPushSeedSupplier(t, db, "ps-s1")
	require.NoError(t, db.Model(s1).Update("payable_discount", 0.8).Error)
	s2 := channelPushSeedSupplier(t, db, "ps-s2") // 折扣为空
	u3 := &model.User{Username: "ps-u3", Role: common.RoleCommonUser, Status: 1, AffCode: "ps-u3"}
	require.NoError(t, db.Create(u3).Error)

	site := receivableSeedSite(t, db, "ps-site", "https://ps.example.com", 1)
	taskOld := payableSeedTask(t, db, 20261121, model.PayableTaskSuccess)
	taskNew := payableSeedTask(t, db, 20261122, model.PayableTaskSuccess)
	pullOld := payableSeedPull(t, db, taskOld, site.Id, model.PayablePullSuccess)
	pullNew := payableSeedPull(t, db, taskNew, site.Id, model.PayablePullSuccess)
	other := payableSeedUsage(t, db, pullOld, 1, 500000, payablePtr(int64(500000)), 0, nil)
	row := payableSeedUsage(t, db, pullNew, 1, 1000000, payablePtr(int64(500000)), 0, nil)
	sibling := payableSeedUsage(t, db, pullNew, 2, 100, payablePtr(int64(10)), 0, nil)
	require.NoError(t, db.Model(row).Updates(map[string]any{"provider_source": model.PayableProviderUnknown, "provider_name": ""}).Error)

	setProvider := func(id string, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/payable_bill/usages/"+id+"/provider", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: id}}
		c.Set("id", 1)
		c.Set("role", common.RoleRootUser)
		c.Set("username", "root")
		SetPayableUsageProvider(c)
		return response
	}
	audits := func() (actions []string, success []bool, params []map[string]any) {
		var logs []model.AuditLog
		require.NoError(t, model.LOG_DB.Where("action LIKE ?", "payable_bill.%").Order("id").Find(&logs).Error)
		for _, log := range logs {
			require.NotNil(t, log.Other.Op)
			encoded, err := common.Marshal(log.Other.Op.Params)
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, common.Unmarshal(encoded, &decoded))
			actions, success, params = append(actions, log.Action), append(success, log.Success), append(params, decoded)
		}
		return actions, success, params
	}
	rowID := strconv.Itoa(row.Id)

	// 1. 设为 s1：只改这一行，快照取 s1 此刻的折扣，指定表新增记录
	response := setProvider(rowID, `{"provider_id":`+strconv.Itoa(s1.Id)+`}`)
	reply := receivableReplyOf(t, response)
	require.True(t, reply.Success, response.Body.String())
	var view map[string]any
	require.NoError(t, common.Unmarshal(reply.Data, &view))
	assert.EqualValues(t, s1.Id, view["provider_id"])
	assert.Equal(t, "ps-s1", view["provider_name"])
	assert.Equal(t, model.PayableProviderManual, view["provider_source"])
	assert.Equal(t, 0.8, view["payable_discount"])
	assert.Equal(t, true, view["counted"])
	assert.Equal(t, true, view["manual"])
	assert.Equal(t, "1.000000", view["delta_amount"])
	assert.Equal(t, "2.000000", view["used_amount"])
	var overrides []model.PayableProviderOverride
	require.NoError(t, db.Find(&overrides).Error)
	require.Len(t, overrides, 1)
	assert.Equal(t, model.PayableProviderOverride{Id: overrides[0].Id, SiteId: site.Id, RemoteChannelId: 1, ProviderId: s1.Id,
		CreatedTime: overrides[0].CreatedTime, UpdatedTime: overrides[0].UpdatedTime}, overrides[0])
	var otherAfter, siblingAfter model.PayableChannelUsage
	require.NoError(t, db.First(&otherAfter, other.Id).Error)
	assert.Equal(t, *other, otherAfter) // 其他日期的行不变
	require.NoError(t, db.First(&siblingAfter, sibling.Id).Error)
	assert.Equal(t, *sibling, siblingAfter)
	actions, success, params := audits()
	require.Len(t, actions, 1)
	assert.Equal(t, "payable_bill.set_provider", actions[0])
	assert.True(t, success[0])
	assert.EqualValues(t, row.Id, params[0]["usage_id"])
	assert.EqualValues(t, 0, params[0]["previous_provider_id"])
	assert.EqualValues(t, s1.Id, params[0]["provider_id"])
	assert.Equal(t, "ps-s1", params[0]["provider_name"])
	assert.Equal(t, 0.8, params[0]["discount"])
	assert.EqualValues(t, 20261122, params[0]["bill_date"])

	// 之后的同步按指定表得到 s1、manual
	resolved, err := model.ResolvePayableProviders(site.Id, 20261123, []model.PayableRemoteChannel{{ID: 1}})
	require.NoError(t, err)
	assert.Equal(t, model.PayableProvider{ID: s1.Id, Name: "ps-s1", Source: model.PayableProviderManual, Discount: payablePtr(0.8)}, resolved[1])

	// 2. 改设为折扣为空的 s2：覆盖指定记录，折扣快照为空，审计带上一次的值
	response = setProvider(rowID, `{"provider_id":`+strconv.Itoa(s2.Id)+`}`)
	reply = receivableReplyOf(t, response)
	require.True(t, reply.Success, response.Body.String())
	require.NoError(t, common.Unmarshal(reply.Data, &view))
	assert.EqualValues(t, s2.Id, view["provider_id"])
	assert.Nil(t, view["payable_discount"])
	require.NoError(t, db.Find(&overrides).Error)
	require.Len(t, overrides, 1)
	assert.Equal(t, s2.Id, overrides[0].ProviderId)
	_, _, params = audits()
	require.Len(t, params, 2)
	assert.EqualValues(t, s1.Id, params[1]["previous_provider_id"])
	assert.Equal(t, 0.8, params[1]["previous_discount"])

	// 3. 设为普通用户被拒绝，数据不变，审计记失败
	response = setProvider(rowID, `{"provider_id":`+strconv.Itoa(u3.Id)+`}`)
	reply = receivableReplyOf(t, response)
	assert.False(t, reply.Success)
	assert.Equal(t, "The selected user is not a channel supplier", reply.MessageKey)
	require.NoError(t, db.Find(&overrides).Error)
	require.Len(t, overrides, 1)
	assert.Equal(t, s2.Id, overrides[0].ProviderId)
	actions, success, params = audits()
	require.Len(t, actions, 3)
	assert.False(t, success[2])
	assert.Equal(t, "invalid_provider", params[2]["failure_reason"])

	// 4. provider_id 0 清除指定记录，行不变
	var reloaded model.PayableChannelUsage
	require.NoError(t, db.First(&reloaded, row.Id).Error)
	response = setProvider(rowID, `{"provider_id":0}`)
	reply = receivableReplyOf(t, response)
	require.True(t, reply.Success, response.Body.String())
	require.NoError(t, common.Unmarshal(reply.Data, &view))
	assert.Equal(t, false, view["manual"])
	assert.EqualValues(t, s2.Id, view["provider_id"])
	var count int64
	require.NoError(t, db.Model(&model.PayableProviderOverride{}).Count(&count).Error)
	assert.Zero(t, count)
	var afterClear model.PayableChannelUsage
	require.NoError(t, db.First(&afterClear, row.Id).Error)
	assert.Equal(t, reloaded, afterClear)
	actions, success, params = audits()
	require.Len(t, actions, 4)
	assert.Equal(t, "payable_bill.clear_provider", actions[3])
	assert.True(t, success[3])
	assert.EqualValues(t, s2.Id, params[3]["previous_provider_id"])
	assert.EqualValues(t, 0, params[3]["provider_id"])

	// 5. 不存在的明细、非法参数
	reply = receivableReplyOf(t, setProvider("999999", `{"provider_id":`+strconv.Itoa(s1.Id)+`}`))
	assert.False(t, reply.Success)
	assert.Equal(t, "Channel usage does not exist", reply.MessageKey)
	actions, success, params = audits()
	require.Len(t, actions, 5)
	assert.False(t, success[4])
	assert.Equal(t, "not_found", params[4]["failure_reason"])
	assert.False(t, receivableReplyOf(t, setProvider("0", `{"provider_id":1}`)).Success)
	assert.False(t, receivableReplyOf(t, setProvider(rowID, `{"provider_id":-1}`)).Success)
	assert.False(t, receivableReplyOf(t, setProvider(rowID, `not json`)).Success)
}
