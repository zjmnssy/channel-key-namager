package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupReceivableBillTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupThirdPartySiteTest(t)
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
