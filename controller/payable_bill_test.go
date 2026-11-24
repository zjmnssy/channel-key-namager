package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPayableBillTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupThirdPartySiteTest(t)
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
		{TaskId: oldTask.Id, SiteId: siteA.Id, BillDate: 20261122, Status: model.PayablePullFailed, NextPage: 3, Pass: 2, Attempts: 4, LastError: refused},
		{TaskId: task.Id, SiteId: siteA.Id, BillDate: 20261123, SiteName: siteA.Name, SiteBaseURL: siteA.BaseURL,
			Status: model.PayablePullFailed, NextPage: 4, Pass: 2, ReportedTotal: 250, ChannelCount: 2, Attempts: 4, LastError: refused},
		{TaskId: task.Id, SiteId: siteB.Id, BillDate: 20261123, SiteName: siteB.Name, SiteBaseURL: siteB.BaseURL,
			Status: model.PayablePullFailed, NextPage: 3, Pass: 1, Attempts: 4, NextRetryAt: 999, LastError: refused},
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
