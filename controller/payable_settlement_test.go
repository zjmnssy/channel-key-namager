package controller

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func setupPayableSettlementTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupPayableBillTest(t)
	originalOptions := service.ReceivableBillClientOptions
	service.ReceivableBillClientOptions = service.ChannelPushClientOptions{Pace: 0, Timeout: 2 * time.Second, MaxRetryAfter: 30 * time.Minute}
	clearTables := func() {
		for _, table := range []any{&model.PayableUsageSettlement{}, &model.PayableSettlementMethod{}, &model.ReceivableBillHour{}, &model.ReceivableBill{}} {
			require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(table).Error)
		}
	}
	clearTables()
	t.Cleanup(func() {
		clearTables()
		service.ReceivableBillClientOptions = originalOptions
		model.PayableSettlementHook = nil
	})
	return db
}

// payableSettlementSetMethod 把提供商的结算方式写成 method；空串表示删掉记录（从未设置过）。
func payableSettlementSetMethod(t *testing.T, db *gorm.DB, providerID int, method string) {
	t.Helper()
	require.NoError(t, db.Where("provider_id = ?", providerID).Delete(&model.PayableSettlementMethod{}).Error)
	if method == "" {
		return
	}
	require.NoError(t, db.Create(&model.PayableSettlementMethod{ProviderId: providerID, Method: method, CreatedTime: 1, UpdatedTime: 1}).Error)
}

// payableSettlementOf 返回站点某天的使用明细结算记录，没有记录时第二个返回值为 false。
func payableSettlementOf(t *testing.T, db *gorm.DB, siteID, billDate int) (model.PayableUsageSettlement, bool) {
	t.Helper()
	var record model.PayableUsageSettlement
	require.NoError(t, db.Where("site_id = ? AND bill_date = ?", siteID, billDate).Limit(1).Find(&record).Error)
	return record, record.Id != 0
}

func payableSettlementSuccessBill(siteID, billDate int, quota int64) *model.ReceivableBill {
	return &model.ReceivableBill{SiteId: siteID, BillDate: billDate, Quota: quota, QuotaPerUnit: 500000, Status: model.ReceivableBillStatusSuccess}
}

// payableSettlementSuccessUpdates 是把日账单写成成功时交给 FinishReceivableRow 的列。
func payableSettlementSuccessUpdates(quota int64) map[string]any {
	return map[string]any{"status": model.ReceivableBillStatusSuccess, "quota": quota, "quota_per_unit": int64(500000)}
}

// payableSettlementClaim 认领一张日账单的同步租约，返回租约。
func payableSettlementClaim(t *testing.T, billID int) int64 {
	t.Helper()
	lease, claimed, err := model.ClaimReceivableRow[model.ReceivableBill](billID, time.Now().Unix())
	require.NoError(t, err)
	require.True(t, claimed)
	return lease
}

// payableSettlementSeedWholeSite 建一个“全部渠道”规则归 owner 的站点。
func payableSettlementSeedWholeSite(t *testing.T, db *gorm.DB, name string, owner *model.User) *model.ThirdPartySite {
	t.Helper()
	site := receivableSeedSite(t, db, name, "https://"+name+".example.com", 1)
	providerMappingSeedRule(t, db, site.Id, 0, owner.Id, model.PayableRuleAll)
	return site
}

func TestPayableSettlementEligibility(t *testing.T) {
	db := setupPayableSettlementTest(t)
	owner := channelPushSeedSupplier(t, db, "ps-owner")
	require.NoError(t, db.Model(owner).Update("payable_discount", 0.7).Error)
	other := channelPushSeedSupplier(t, db, "ps-other")
	former := channelPushSeedSupplier(t, db, "ps-former")
	require.NoError(t, db.Model(former).Update("role", common.RoleCommonUser).Error)

	for i, tc := range []struct {
		name        string
		ruleType    string      // 站点级规则，空串表示没有
		ruleUser    *model.User // 站点级规则的提供商，也是被设置结算方式的人
		method      string      // 空串表示从未设置
		channelUser *model.User // 站点上一条指定渠道规则的提供商，nil 表示没有
		settled     bool
	}{
		{"全部渠道规则且使用明细结算", model.PayableRuleAll, owner, model.PayableSettlementUsage, nil, true},
		{"结算方式是渠道明细", model.PayableRuleAll, owner, model.PayableSettlementChannel, nil, false},
		{"从未设置结算方式", model.PayableRuleAll, owner, "", nil, false},
		{"不能解析的渠道规则", model.PayableRuleUnresolved, owner, model.PayableSettlementUsage, nil, false},
		{"没有站点级规则", "", owner, model.PayableSettlementUsage, nil, false},
		{"规则的提供商已不是供应商", model.PayableRuleAll, former, model.PayableSettlementUsage, nil, false},
		{"指定渠道指给另一个供应商", model.PayableRuleAll, owner, model.PayableSettlementUsage, other, false},
		{"指定渠道指给自己", model.PayableRuleAll, owner, model.PayableSettlementUsage, owner, true},
		{"指定渠道指给已不是供应商的用户", model.PayableRuleAll, owner, model.PayableSettlementUsage, former, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := receivableSeedSite(t, db, "ps-elig-"+strconv.Itoa(i), "https://ps-elig-"+strconv.Itoa(i)+".example.com", 1)
			if tc.ruleType != "" {
				providerMappingSeedRule(t, db, site.Id, 0, tc.ruleUser.Id, tc.ruleType)
			}
			payableSettlementSetMethod(t, db, tc.ruleUser.Id, tc.method)
			if tc.channelUser != nil {
				providerMappingSeedRule(t, db, site.Id, 5, tc.channelUser.Id, model.PayableRuleChannel)
			}
			require.NoError(t, model.InsertReceivableRow(payableSettlementSuccessBill(site.Id, 20261125, 103889618766)))

			record, found := payableSettlementOf(t, db, site.Id, 20261125)
			require.Equal(t, tc.settled, found)
			if tc.settled {
				assert.Equal(t, owner.Id, record.ProviderId)
				assert.Equal(t, "ps-owner", record.ProviderName)
				assert.EqualValues(t, 103889618766, record.Quota)
				assert.EqualValues(t, 500000, record.QuotaPerUnit)
				require.NotNil(t, record.PayableDiscount)
				assert.Equal(t, 0.7, *record.PayableDiscount)
			}
		})
	}
}

func TestPayableSettlementFollowsDailyBillWrites(t *testing.T) {
	db := setupPayableSettlementTest(t)
	owner := channelPushSeedSupplier(t, db, "ps-follow")
	require.NoError(t, db.Model(owner).Update("payable_discount", 0.7).Error)
	site := payableSettlementSeedWholeSite(t, db, "ps-follow", owner)
	payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementUsage)

	// 1. 首次写入的是重试中：不结算
	bill := &model.ReceivableBill{SiteId: site.Id, BillDate: 20261125, Status: model.ReceivableBillStatusRetrying}
	require.NoError(t, model.InsertReceivableRow(bill))
	_, found := payableSettlementOf(t, db, site.Id, 20261125)
	assert.False(t, found)

	// 2. 租约不对、什么都没写：不结算
	lease := payableSettlementClaim(t, bill.Id)
	finished, err := model.FinishReceivableRow[model.ReceivableBill](bill.Id, lease-1, payableSettlementSuccessUpdates(1000000))
	require.NoError(t, err)
	assert.False(t, finished)
	_, found = payableSettlementOf(t, db, site.Id, 20261125)
	assert.False(t, found)

	// 3. 重试成功：按这次的额度和提供商此刻的折扣保存
	finished, err = model.FinishReceivableRow[model.ReceivableBill](bill.Id, lease, payableSettlementSuccessUpdates(1000000))
	require.NoError(t, err)
	assert.True(t, finished)
	saved, found := payableSettlementOf(t, db, site.Id, 20261125)
	require.True(t, found)
	assert.EqualValues(t, 1000000, saved.Quota)
	require.NotNil(t, saved.PayableDiscount)
	assert.Equal(t, 0.7, *saved.PayableDiscount)

	// 4. 首次写入撞上已有的行（唯一约束冲突、什么都没写）：不结算，记录不变
	require.NoError(t, model.InsertReceivableRow(payableSettlementSuccessBill(site.Id, 20261125, 9)))
	record, found := payableSettlementOf(t, db, site.Id, 20261125)
	require.True(t, found)
	assert.Equal(t, saved, record)

	// 5. 拉取失败（重试中）不改已保存的记录，折扣改了也不变
	require.NoError(t, db.Model(owner).Update("payable_discount", 0.8).Error)
	finished, err = model.FinishReceivableRow[model.ReceivableBill](bill.Id, payableSettlementClaim(t, bill.Id),
		map[string]any{"status": model.ReceivableBillStatusRetrying})
	require.NoError(t, err)
	assert.True(t, finished)
	record, found = payableSettlementOf(t, db, site.Id, 20261125)
	require.True(t, found)
	assert.Equal(t, saved, record)

	// 6. 再次成功：按这次的额度和此刻的折扣覆盖
	finished, err = model.FinishReceivableRow[model.ReceivableBill](bill.Id, payableSettlementClaim(t, bill.Id), payableSettlementSuccessUpdates(2000000))
	require.NoError(t, err)
	assert.True(t, finished)
	record, found = payableSettlementOf(t, db, site.Id, 20261125)
	require.True(t, found)
	assert.EqualValues(t, 2000000, record.Quota)
	require.NotNil(t, record.PayableDiscount)
	assert.Equal(t, 0.8, *record.PayableDiscount)

	// 7. 改为渠道明细结算后再次成功：删除记录，这一天按渠道明细结算
	payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementChannel)
	finished, err = model.FinishReceivableRow[model.ReceivableBill](bill.Id, payableSettlementClaim(t, bill.Id), payableSettlementSuccessUpdates(2000000))
	require.NoError(t, err)
	assert.True(t, finished)
	_, found = payableSettlementOf(t, db, site.Id, 20261125)
	assert.False(t, found)

	// 8. 提供商没有设置折扣：折扣快照为空；覆盖已有记录时同样写成空，不留下旧折扣
	payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementUsage)
	require.NoError(t, db.Model(owner).Update("payable_discount", nil).Error)
	next := payableSettlementSuccessBill(site.Id, 20261126, 1000000)
	require.NoError(t, model.InsertReceivableRow(next))
	record, found = payableSettlementOf(t, db, site.Id, 20261126)
	require.True(t, found)
	assert.Nil(t, record.PayableDiscount)
	require.NoError(t, db.Model(&record).Update("payable_discount", 0.6).Error)
	finished, err = model.FinishReceivableRow[model.ReceivableBill](next.Id, payableSettlementClaim(t, next.Id), payableSettlementSuccessUpdates(1000000))
	require.NoError(t, err)
	assert.True(t, finished)
	record, found = payableSettlementOf(t, db, site.Id, 20261126)
	require.True(t, found)
	assert.Nil(t, record.PayableDiscount)

	// 9. 重新同步前“全部渠道”规则改归另一个按使用明细结算的提供商：覆盖同一条记录，提供商 ID、名称快照、折扣都换成新提供商的，
	// 不能留着旧提供商、却换上新提供商的折扣和这次的额度（付错人）
	nextOwner := channelPushSeedSupplier(t, db, "ps-follow-next")
	require.NoError(t, db.Model(nextOwner).Update("payable_discount", 0.5).Error)
	payableSettlementSetMethod(t, db, nextOwner.Id, model.PayableSettlementUsage)
	require.NoError(t, db.Model(&model.PayableProviderOverride{}).Where("site_id = ? AND remote_channel_id = 0", site.Id).
		Update("provider_id", nextOwner.Id).Error)
	finished, err = model.FinishReceivableRow[model.ReceivableBill](next.Id, payableSettlementClaim(t, next.Id), payableSettlementSuccessUpdates(1000000))
	require.NoError(t, err)
	assert.True(t, finished)
	moved, found := payableSettlementOf(t, db, site.Id, 20261126)
	require.True(t, found)
	assert.Equal(t, record.Id, moved.Id)
	assert.Equal(t, nextOwner.Id, moved.ProviderId)
	assert.Equal(t, "ps-follow-next", moved.ProviderName)
	require.NotNil(t, moved.PayableDiscount)
	assert.Equal(t, 0.5, *moved.PayableDiscount)

	// 10. 小时数据不结算
	require.NoError(t, model.InsertReceivableRow(&model.ReceivableBillHour{SiteId: site.Id, BillHour: 1795622400, Quota: 1, QuotaPerUnit: 500000,
		Status: model.ReceivableBillStatusSuccess}))
	var records int64
	require.NoError(t, db.Model(&model.PayableUsageSettlement{}).Count(&records).Error)
	assert.EqualValues(t, 1, records)
}

func TestPayableSettlementIsAtomicWithDailyBill(t *testing.T) {
	db := setupPayableSettlementTest(t)
	owner := channelPushSeedSupplier(t, db, "ps-atomic")
	site := payableSettlementSeedWholeSite(t, db, "ps-atomic", owner)
	payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementUsage)
	// 保存结算记录时数据库报错：日账单不能留下成功，否则这一天悄悄按渠道明细结算且不再重试
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:ps-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "payable_usage_settlements" {
			tx.AddError(errors.New("boom"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:ps-fail") })

	// 首次写入：日账单与结算一起回滚，下一轮扫描会再补这一天
	assert.ErrorContains(t, model.InsertReceivableRow(payableSettlementSuccessBill(site.Id, 20261125, 1000000)), "boom")
	var bills int64
	require.NoError(t, db.Model(&model.ReceivableBill{}).Where("site_id = ?", site.Id).Count(&bills).Error)
	assert.Zero(t, bills)

	// 重试成功：这一行仍是重试中、租约未释放（到期后由原有的重试逻辑处理），额度没写入
	retrying := &model.ReceivableBill{SiteId: site.Id, BillDate: 20261126, Status: model.ReceivableBillStatusRetrying, Attempts: 2}
	require.NoError(t, db.Create(retrying).Error)
	lease := payableSettlementClaim(t, retrying.Id)
	finished, err := model.FinishReceivableRow[model.ReceivableBill](retrying.Id, lease, payableSettlementSuccessUpdates(1000000))
	assert.ErrorContains(t, err, "boom")
	assert.False(t, finished)
	stored := receivableBillOf(t, db, site.Id, 20261126)
	assert.Equal(t, model.ReceivableBillStatusRetrying, stored.Status)
	assert.Equal(t, lease, stored.LeaseUntil)
	assert.Zero(t, stored.Quota)
}

// 每天 1:00 多个站点的日账单同时写成成功：一个站点的结算事务未提交时，其他站点同一天的日账单和结算记录照常写入。
// MySQL 可重复读下，按 (site_id, bill_date) 直接 DELETE 不存在的键会留下间隙锁，对已存在的键 upsert 会留下临键锁，
// 两种都会让后面键落在这个间隙里的站点等锁；所以结算先普通查询，再按主键更新或删除。
func TestPayableSettlementDoesNotBlockOtherSites(t *testing.T) {
	db := setupPayableSettlementTest(t)
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		t.Skip("SQLite 同时只有一个写事务，结算提交前其他连接的写入本来就要等")
	}
	timeout := "SET SESSION innodb_lock_wait_timeout = 3"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		timeout = "SET lock_timeout = '3s'"
	}
	owner := channelPushSeedSupplier(t, db, "ps-lock")
	// writeOtherSiteOnSettle 让 A 的结算事务提交前，另一条连接写入 B 同一天的成功日账单和使用明细结算记录。
	writeOtherSiteOnSettle := func(siteA, siteB *model.ThirdPartySite) {
		model.PayableSettlementHook = func(siteID, billDate int) {
			if siteID != siteA.Id {
				return
			}
			// 结算事务占着自己的连接，Connection 从连接池另取一条；被锁住时 3 秒后报错，而不是一直等
			err := model.DB.Connection(func(conn *gorm.DB) error {
				// conn 上的调用共用一个 Statement，连着写两张表时后一次会沿用前一张表的结构；NewDB 让每次调用重新开始，仍用这条连接
				conn = conn.Session(&gorm.Session{NewDB: true})
				if err := conn.Exec(timeout).Error; err != nil {
					return err
				}
				if err := conn.Create(payableSettlementSuccessBill(siteB.Id, billDate, 1)).Error; err != nil {
					return err
				}
				return conn.Create(&model.PayableUsageSettlement{SiteId: siteB.Id, BillDate: billDate, ProviderId: owner.Id,
					ProviderName: owner.Username, Quota: 1, QuotaPerUnit: 500000}).Error
			})
			assert.NoError(t, err)
		}
	}

	t.Run("A 不按使用明细结算、也没有旧记录", func(t *testing.T) {
		// 先建 B：B 的 ID 更小，B 的键正好落在 A 的键前面的间隙里
		siteB := receivableSeedSite(t, db, "ps-lock-b", "https://ps-lock-b.example.com", 1)
		siteA := payableSettlementSeedWholeSite(t, db, "ps-lock-a", owner)
		require.Less(t, siteB.Id, siteA.Id)
		payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementChannel)
		writeOtherSiteOnSettle(siteA, siteB)

		require.NoError(t, model.InsertReceivableRow(payableSettlementSuccessBill(siteA.Id, 20261125, 1)))
		_, found := payableSettlementOf(t, db, siteA.Id, 20261125)
		assert.False(t, found)
		_, found = payableSettlementOf(t, db, siteB.Id, 20261125)
		assert.True(t, found)
	})

	t.Run("A 按使用明细结算、已有旧记录", func(t *testing.T) {
		model.PayableSettlementHook = nil
		siteB := receivableSeedSite(t, db, "ps-lock-b2", "https://ps-lock-b2.example.com", 1)
		siteA := payableSettlementSeedWholeSite(t, db, "ps-lock-a2", owner)
		require.Less(t, siteB.Id, siteA.Id)
		payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementUsage)
		bill := payableSettlementSuccessBill(siteA.Id, 20261126, 1)
		require.NoError(t, model.InsertReceivableRow(bill)) // 生成旧记录
		_, found := payableSettlementOf(t, db, siteA.Id, 20261126)
		require.True(t, found)
		writeOtherSiteOnSettle(siteA, siteB)

		finished, err := model.FinishReceivableRow[model.ReceivableBill](bill.Id, payableSettlementClaim(t, bill.Id), payableSettlementSuccessUpdates(2))
		require.NoError(t, err)
		assert.True(t, finished)
		record, found := payableSettlementOf(t, db, siteA.Id, 20261126)
		require.True(t, found)
		assert.EqualValues(t, 2, record.Quota)
		_, found = payableSettlementOf(t, db, siteB.Id, 20261126)
		assert.True(t, found)
	})
}

func TestPayableSettlementOnScanAndResync(t *testing.T) {
	db := setupPayableSettlementTest(t)
	fake := newFakeStatSite(t)
	owner := channelPushSeedSupplier(t, db, "ps-scan")
	require.NoError(t, db.Model(owner).Update("payable_discount", 0.7).Error)
	site := receivableSeedSite(t, db, "ps-scan", fake.server.URL, 1)
	providerMappingSeedRule(t, db, site.Id, 0, owner.Id, model.PayableRuleAll)
	payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementUsage)

	// 1. 定时拉取的首次写入失败（重试中）：不结算
	fake.setRespond(receivableDailyReply(http.StatusBadGateway, "", nil))
	receivableScan(fake, 1795366800) // 北京 11-23 01:00
	assert.Equal(t, model.ReceivableBillStatusRetrying, receivableBillOf(t, db, site.Id, 20261122).Status)
	_, found := payableSettlementOf(t, db, site.Id, 20261122)
	assert.False(t, found)

	// 2. 重试成功：结算
	fake.setRespond(receivableDailyReply(http.StatusOK, `{"success":true,"data":{"quota":2500000}}`, nil))
	receivableScan(fake, 1795366860)
	bill := receivableBillOf(t, db, site.Id, 20261122)
	assert.Equal(t, model.ReceivableBillStatusSuccess, bill.Status)
	record, found := payableSettlementOf(t, db, site.Id, 20261122)
	require.True(t, found)
	assert.EqualValues(t, 2500000, record.Quota)
	require.NotNil(t, record.PayableDiscount)
	assert.Equal(t, 0.7, *record.PayableDiscount)

	resync := func() {
		id := strconv.Itoa(bill.Id)
		response := receivableRequest(t, http.MethodPost, "/api/receivable_bill/daily/"+id+"/sync", nil,
			gin.Params{{Key: "id", Value: id}}, ResyncReceivableBill)
		require.True(t, receivableReplyOf(t, response).Success, response.Body.String())
	}

	// 3. 改为渠道明细结算后手动重新同步成功：删除记录
	payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementChannel)
	resync()
	_, found = payableSettlementOf(t, db, site.Id, 20261122)
	assert.False(t, found)

	// 4. 改回使用明细结算、改了折扣，再重新同步：按此刻的折扣重建
	payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementUsage)
	require.NoError(t, db.Model(owner).Update("payable_discount", 0.9).Error)
	resync()
	record, found = payableSettlementOf(t, db, site.Id, 20261122)
	require.True(t, found)
	assert.EqualValues(t, 2500000, record.Quota)
	require.NotNil(t, record.PayableDiscount)
	assert.Equal(t, 0.9, *record.PayableDiscount)
}

// payableSettlementFixture 是提供商账单合并用的数据：owner 折扣 0.7、按使用明细结算，A、W 是它的整站站点，B 是普通站点。
type payableSettlementFixture struct {
	owner, other        *model.User
	siteA, siteW, siteB *model.ThirdPartySite
}

// payableSettlementSeedBills 建 11-23 到 11-25 的日账单、使用明细结算记录与渠道明细。日账单和记录直接写库、不经结算逻辑，
// 数据就是这里写的样子（例如 W 在 11-23 的记录折扣为空，虽然 owner 的折扣是 0.7）。
//   - 11-25：A 的日账单成功、有记录；A 的两行明细（owner、other 各增量 50 万）都不计入，B 的一行 owner 明细（增量 6172839）计入。
//   - 11-24：A 的日账单在重试中，它的旧记录（额度 999）不用；A 的一行 owner 明细（增量 100 万）计入。
//   - 11-23：A、W 的日账单都成功，各有一条额度 50 万的记录（A 折扣 0.7、W 折扣为空）；W 当天的拉取失败。
func payableSettlementSeedBills(t *testing.T, db *gorm.DB) payableSettlementFixture {
	t.Helper()
	owner := channelPushSeedSupplier(t, db, "ps-owner")
	require.NoError(t, db.Model(owner).Update("payable_discount", 0.7).Error)
	payableSettlementSetMethod(t, db, owner.Id, model.PayableSettlementUsage)
	f := payableSettlementFixture{owner: owner, other: channelPushSeedSupplier(t, db, "ps-other"),
		siteA: payableSettlementSeedWholeSite(t, db, "ps-a", owner), siteW: payableSettlementSeedWholeSite(t, db, "ps-w", owner)}
	f.siteB = receivableSeedSite(t, db, "ps-b", "https://ps-b.example.com", 1)

	d07 := payablePtr(0.7)
	for _, bill := range []*model.ReceivableBill{
		payableSettlementSuccessBill(f.siteA.Id, 20261125, 103889618766),
		{SiteId: f.siteA.Id, BillDate: 20261124, Status: model.ReceivableBillStatusRetrying},
		payableSettlementSuccessBill(f.siteA.Id, 20261123, 500000),
		payableSettlementSuccessBill(f.siteW.Id, 20261123, 500000),
	} {
		require.NoError(t, db.Create(bill).Error)
	}
	for _, record := range []model.PayableUsageSettlement{
		{SiteId: f.siteA.Id, BillDate: 20261125, Quota: 103889618766, PayableDiscount: d07},
		{SiteId: f.siteA.Id, BillDate: 20261124, Quota: 999, PayableDiscount: d07},
		{SiteId: f.siteA.Id, BillDate: 20261123, Quota: 500000, PayableDiscount: d07},
		{SiteId: f.siteW.Id, BillDate: 20261123, Quota: 500000},
	} {
		record.ProviderId, record.ProviderName, record.QuotaPerUnit = owner.Id, "ps-owner", 500000
		require.NoError(t, db.Create(&record).Error)
	}

	task := payableSeedTask(t, db, 20261125, model.PayableTaskSuccess)
	pullA := payableSeedPull(t, db, task, f.siteA.Id, model.PayablePullSuccess)
	payableSeedUsage(t, db, pullA, 1, 0, payablePtr(int64(500000)), owner.Id, d07)
	payableSeedUsage(t, db, pullA, 2, 0, payablePtr(int64(500000)), f.other.Id, nil)
	payableSeedUsage(t, db, payableSeedPull(t, db, task, f.siteB.Id, model.PayablePullSuccess), 1, 0, payablePtr(int64(6172839)), owner.Id, d07)
	pullA = payableSeedPull(t, db, payableSeedTask(t, db, 20261124, model.PayableTaskSuccess), f.siteA.Id, model.PayablePullSuccess)
	payableSeedUsage(t, db, pullA, 1, 0, payablePtr(int64(1000000)), owner.Id, d07)
	payableSeedPull(t, db, payableSeedTask(t, db, 20261123, model.PayableTaskPartialFailed), f.siteW.Id, model.PayablePullFailed)
	return f
}

// payableSettlementExportRows 以中文调用导出处理函数，返回工作表各行的单元格原值。
func payableSettlementExportRows(t *testing.T, target string, handler gin.HandlerFunc) [][]string {
	t.Helper()
	header := http.Header{}
	header.Set("Accept-Language", "zh-CN")
	response := receivableRequest(t, http.MethodGet, target, header, nil, handler)
	require.Equal(t, http.StatusOK, response.Code)
	file, err := excelize.OpenReader(bytes.NewReader(response.Body.Bytes()))
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	rows, err := file.GetRows(file.GetSheetName(0), excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	return rows
}

// 提供商账单 = 使用明细结算部分 + 渠道明细结算部分（设计 5.4）：按使用明细结算的站点天（有记录、且日账单当前成功）的明细不论提供商一律不计入；
// 日账单当前不是成功的记录不用，这一天回到渠道明细。两部分合并后统一排序、分页，导出与页面同序、列不变。
func TestPayableProviderBillsWithUsageSettlement(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := setupPayableSettlementTest(t)
	f := payableSettlementSeedBills(t, db)
	list := func(query string) (map[string]any, []map[string]any) {
		t.Helper()
		response := payableGet(t, "/api/payable_bill/providers?start_date=20261123&end_date=20261125"+query, nil, GetPayableProviderBills)
		reply := receivableReplyOf(t, response)
		require.True(t, reply.Success, response.Body.String())
		var page map[string]any
		require.NoError(t, common.Unmarshal(reply.Data, &page))
		raw, _ := page["items"].([]any)
		items := make([]map[string]any, len(raw))
		for i, item := range raw {
			items[i] = item.(map[string]any)
		}
		return page, items
	}
	type row struct {
		date                 float64
		method, name, amount string
		discount             any
		missing              bool
		payable              any
	}
	rowsOf := func(items []map[string]any) []row {
		rows := make([]row, len(items))
		for i, item := range items {
			rows[i].date, _ = item["bill_date"].(float64)
			rows[i].method, _ = item["settlement_method"].(string)
			rows[i].name, _ = item["provider_name"].(string)
			rows[i].amount, _ = item["amount"].(string)
			rows[i].discount, rows[i].payable = item["discount"], item["payable_amount"]
			rows[i].missing, _ = item["discount_missing"].(bool)
		}
		return rows
	}
	ownerName := "p" + strconv.Itoa(f.owner.Id)
	// 顺序：日期倒序、提供商升序，同一天同一提供商使用明细结算在前；other 在 A 的 11-25 的明细被排除，没有行
	want := []row{
		{20261125, "usage", "ps-owner", "207779.237532", 0.7, false, "145445.466272"},
		{20261125, "channel", ownerName, "12.345678", 0.7, false, "8.641975"},
		{20261124, "channel", ownerName, "2.000000", 0.7, false, "1.400000"},
		{20261123, "usage", "ps-owner", "2.000000", nil, true, "0.700000"},
	}

	page, items := list("")
	assert.EqualValues(t, 4, page["total"])
	assert.EqualValues(t, 1, page["unsynced_usage_site_count"]) // A 的 11-24 在重试中
	assert.EqualValues(t, 1, page["incomplete_site_count"])     // W 的 11-23 拉取失败照常计入（方案 A）
	assert.EqualValues(t, 0, page["mismatch_site_count"])
	assert.Equal(t, want, rowsOf(items))

	// 分页拆开同一天同一提供商的两行
	page, items = list("&p=2&page_size=1")
	assert.EqualValues(t, 4, page["total"])
	assert.Equal(t, want[1:2], rowsOf(items))

	// 站点筛选：只有 B
	page, items = list("&site_ids=" + strconv.Itoa(f.siteB.Id))
	assert.EqualValues(t, 1, page["total"])
	assert.Equal(t, want[1:2], rowsOf(items))
	assert.EqualValues(t, 0, page["unsynced_usage_site_count"])

	// 提供商筛选 0：使用明细结算部分不会有提供商 0 的行
	page, items = list("&provider_ids=0")
	assert.EqualValues(t, 0, page["total"])
	assert.Empty(t, items)

	// 已保存的记录不随设置变化：owner 改回渠道明细结算、删掉 A 的“全部渠道”规则后，4 行完全不变
	payableSettlementSetMethod(t, db, f.owner.Id, model.PayableSettlementChannel)
	require.NoError(t, db.Where("site_id = ? AND remote_channel_id = 0", f.siteA.Id).Delete(&model.PayableProviderOverride{}).Error)
	_, items = list("")
	assert.Equal(t, want, rowsOf(items))

	// 快照：改掉 owner 的用户名、角色后，使用明细结算行的名称仍是 ps-owner
	require.NoError(t, db.Model(f.owner).Updates(map[string]any{"username": "ps-renamed", "role": common.RoleCommonUser}).Error)
	_, items = list("")
	assert.Equal(t, want, rowsOf(items))

	// 导出：列不变（不加结算方式），行与页面同序，同一天同一提供商仍是两行
	rows := payableSettlementExportRows(t, "/api/payable_bill/providers/export?start_date=20261123&end_date=20261125", ExportPayableProviderBills)
	require.NotEmpty(t, rows)
	assert.Equal(t, []string{"账单日期", "提供商", "金额(折前)($)", "折扣(应付)", "应付金额($)", "备注"}, rows[0])
	exported := make([][]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		require.GreaterOrEqual(t, len(row), 2)
		exported = append(exported, row[:2])
	}
	assert.Equal(t, [][]string{{"2026-11-25", "ps-owner"}, {"2026-11-25", ownerName}, {"2026-11-24", ownerName}, {"2026-11-23", "ps-owner"}}, exported)
}

// 渠道消费明细每行带 usage_settled（设计 5.6），与提供商账单排除的站点天一致；导出的“计入账单”在拉取完整且不按使用明细结算时才为“是”。
func TestPayableUsagesMarkUsageSettledDays(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := setupPayableSettlementTest(t)
	f := payableSettlementSeedBills(t, db)

	_, items := receivableListOf(t, payableGet(t, "/api/payable_bill/usages?start_date=20261124&end_date=20261125", nil, GetPayableUsages))
	type mark struct {
		date, siteID, channelID int
		settled, counted        any
	}
	got := make([]mark, len(items))
	for i, item := range items {
		got[i] = mark{int(item["bill_date"].(float64)), int(item["site_id"].(float64)), int(item["remote_channel_id"].(float64)),
			item["usage_settled"], item["counted"]}
	}
	// A 的 11-24 日账单在重试中、B 不是整站站点：都不按使用明细结算
	assert.Equal(t, []mark{
		{20261125, f.siteA.Id, 1, true, true},
		{20261125, f.siteA.Id, 2, true, true},
		{20261125, f.siteB.Id, 1, false, true},
		{20261124, f.siteA.Id, 1, false, true},
	}, got)

	rows := payableSettlementExportRows(t, "/api/payable_bill/usages/export?start_date=20261124&end_date=20261125", ExportPayableUsages)
	require.Len(t, rows, 5)
	assert.Equal(t, "计入账单", rows[0][17])
	counted := make([][]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		require.Len(t, row, 18)
		counted = append(counted, []string{row[1], row[2], row[6], row[17]})
	}
	siteA, siteB := strconv.Itoa(f.siteA.Id), strconv.Itoa(f.siteB.Id)
	assert.Equal(t, [][]string{
		{"2026-11-25", siteA, "1", "否"},
		{"2026-11-25", siteA, "2", "否"},
		{"2026-11-25", siteB, "1", "是"},
		{"2026-11-24", siteA, "1", "是"},
	}, counted)

	// 设置提供商返回的行同样带 usage_settled
	var usage model.PayableChannelUsage
	require.NoError(t, db.Where("site_id = ? AND bill_date = ? AND provider_id = ?", f.siteA.Id, 20261125, f.other.Id).First(&usage).Error)
	id := strconv.Itoa(usage.Id)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/payable_bill/usages/"+id+"/provider", strings.NewReader(`{"provider_id":`+strconv.Itoa(f.owner.Id)+`}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser)
	c.Set("username", "root")
	SetPayableUsageProvider(c)
	reply := receivableReplyOf(t, response)
	require.True(t, reply.Success, response.Body.String())
	var view map[string]any
	require.NoError(t, common.Unmarshal(reply.Data, &view))
	assert.EqualValues(t, f.owner.Id, view["provider_id"])
	assert.Equal(t, true, view["usage_settled"])
}

// 结算方式的列表与设置接口（设计 5.1、5.8、5.10）。列表只列渠道供应商（含已禁用），从未设置的按渠道明细结算、更新时间为 0；
// 整站站点只算未删除站点上的“全部渠道”规则，并标出冲突。设置只能给渠道供应商，与当前相同也保存；处理函数记成功与失败的审计，
// ID 与请求体不合法的在它之前返回，留给兜底审计。
func TestPayableSettlementMethodAPI(t *testing.T) {
	db := setupPayableSettlementTest(t)
	owner := channelPushSeedSupplier(t, db, "psm-owner")
	require.NoError(t, db.Model(owner).Update("payable_discount", 0.7).Error)
	disabled := channelPushSeedSupplier(t, db, "psm-disabled")
	require.NoError(t, db.Model(disabled).Update("status", common.UserStatusDisabled).Error)
	// 不该出现在列表里的：普通管理员、普通用户、已软删除的供应商
	admin := &model.User{Username: "psm-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffCode: "psm-admin"}
	require.NoError(t, db.Create(admin).Error)
	require.NoError(t, db.Create(&model.User{Username: "psm-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "psm-user"}).Error)
	deleted := channelPushSeedSupplier(t, db, "psm-deleted")
	require.NoError(t, db.Delete(deleted).Error)

	siteA := payableSettlementSeedWholeSite(t, db, "psm-a", owner)
	siteC := payableSettlementSeedWholeSite(t, db, "psm-c", owner)
	providerMappingSeedRule(t, db, siteC.Id, 5, disabled.Id, model.PayableRuleChannel) // 渠道指给另一个供应商（已禁用也是）：冲突
	siteD := payableSettlementSeedWholeSite(t, db, "psm-d", owner)
	require.NoError(t, db.Delete(siteD).Error) // 站点已删除：不是整站站点
	siteU := receivableSeedSite(t, db, "psm-u", "https://psm-u.example.com", 1)
	providerMappingSeedRule(t, db, siteU.Id, 0, owner.Id, model.PayableRuleUnresolved) // “不能解析的渠道”规则：不是整站站点

	list := func(query string) (int, []map[string]any) {
		t.Helper()
		return receivableListOf(t, payableGet(t, "/api/payable_bill/settlement_methods"+query, nil, GetPayableSettlementMethods))
	}
	root := &model.User{Id: 1, Username: "root", Role: common.RoleRootUser}
	setMethod := func(id, body string) receivableReply {
		t.Helper()
		return receivableReplyOf(t, channelSupplierRequest(t, http.MethodPut, "/api/payable_bill/settlement_methods/"+id, body, root,
			gin.Params{{Key: "provider_id", Value: id}}, SetPayableSettlementMethod))
	}
	type audit struct {
		Success bool
		Content string
		Params  map[string]any
	}
	audits := func() []audit {
		t.Helper()
		var logs []model.AuditLog
		require.NoError(t, model.LOG_DB.Where("action = ?", "payable_bill.set_settlement_method").Order("id").Find(&logs).Error)
		result := make([]audit, len(logs))
		for i, log := range logs {
			require.NotNil(t, log.Other.Op)
			result[i] = audit{Success: log.Success, Content: log.Content, Params: providerMappingJSON(t, log.Other.Op.Params)}
		}
		return result
	}

	// 1. 按提供商 ID 升序；整站站点按站点 ID 升序，没有时是 [] 而不是 null
	ownerRow := map[string]any{"provider_id": float64(owner.Id), "username": "psm-owner", "disabled": false, "payable_discount": 0.7,
		"method": "channel", "updated_time": float64(0), "whole_sites": []any{
			map[string]any{"site_id": float64(siteA.Id), "site_name": "psm-a", "conflict": false},
			map[string]any{"site_id": float64(siteC.Id), "site_name": "psm-c", "conflict": true},
		}}
	disabledRow := map[string]any{"provider_id": float64(disabled.Id), "username": "psm-disabled", "disabled": true, "payable_discount": nil,
		"method": "channel", "updated_time": float64(0), "whole_sites": []any{}}
	total, items := list("")
	assert.Equal(t, 2, total)
	assert.Equal(t, []map[string]any{ownerRow, disabledRow}, items)

	// 2. 设为使用明细结算：新建记录，原结算方式是渠道明细结算
	ownerID := strconv.Itoa(owner.Id)
	reply := setMethod(ownerID, `{"method":"usage"}`)
	require.True(t, reply.Success, reply.MessageKey)
	var saved map[string]any
	require.NoError(t, common.Unmarshal(reply.Data, &saved))
	updated, _ := saved["updated_time"].(float64)
	assert.Positive(t, updated)
	assert.Equal(t, map[string]any{"provider_id": float64(owner.Id), "method": "usage", "updated_time": updated}, saved)
	logs := audits()
	require.Len(t, logs, 1)
	assert.Equal(t, audit{Success: true, Content: "Set the settlement method of provider #" + ownerID, Params: map[string]any{
		"provider_id": float64(owner.Id), "settlement_method": "usage", "provider_name": "psm-owner", "previous_settlement_method": "channel"}}, logs[0])

	// 3. 筛选与分页：从未设置过的 disabled 算渠道明细结算
	ownerRow["method"], ownerRow["updated_time"] = "usage", updated
	for _, tc := range []struct {
		query string
		total int
		rows  []map[string]any
	}{
		{"?methods=usage", 1, []map[string]any{ownerRow}},
		{"?methods=channel", 1, []map[string]any{disabledRow}},
		{"?methods=channel,usage", 2, []map[string]any{ownerRow, disabledRow}},
		{"?provider_ids=" + strconv.Itoa(disabled.Id), 1, []map[string]any{disabledRow}},
		{"?p=2&page_size=1", 2, []map[string]any{disabledRow}},
	} {
		total, items = list(tc.query)
		assert.Equal(t, tc.total, total, tc.query)
		assert.Equal(t, tc.rows, items, tc.query)
	}

	// 4. 与当前相同也保存：更新这一条记录，原结算方式是 usage
	reply = setMethod(ownerID, `{"method":"usage"}`)
	require.True(t, reply.Success, reply.MessageKey)
	logs = audits()
	require.Len(t, logs, 2)
	assert.True(t, logs[1].Success)
	assert.Equal(t, "usage", logs[1].Params["previous_settlement_method"])

	// 5. ID、请求体不合法：在处理函数记审计之前返回，留给兜底审计
	for _, id := range []string{"abc", "0"} {
		reply = setMethod(id, `{"method":"usage"}`)
		assert.False(t, reply.Success, id)
		assert.Equal(t, "Invalid ID", reply.MessageKey, id)
	}
	for _, body := range []string{`{}`, `{"method":null}`} {
		reply = setMethod(ownerID, body)
		assert.False(t, reply.Success, body)
		assert.Equal(t, "Invalid parameters", reply.MessageKey, body)
	}
	assert.Len(t, audits(), 2)

	// 结算方式不是两个取值之一，或用户不是渠道供应商（普通管理员、已软删除的供应商、不存在的用户）：处理函数记失败，数据不变
	reply = setMethod(ownerID, `{"method":"weekly"}`)
	assert.False(t, reply.Success)
	assert.Equal(t, "Invalid parameters", reply.MessageKey)
	failed := []audit{{Content: "Failed to set the settlement method",
		Params: map[string]any{"provider_id": float64(owner.Id), "settlement_method": "weekly", "failure_reason": "invalid_method"}}}
	for _, id := range []int{admin.Id, deleted.Id, 999999} {
		reply = setMethod(strconv.Itoa(id), `{"method":"usage"}`)
		assert.False(t, reply.Success, id)
		assert.Equal(t, "The selected user is not a channel supplier", reply.MessageKey, id)
		failed = append(failed, audit{Content: "Failed to set the settlement method",
			Params: map[string]any{"provider_id": float64(id), "settlement_method": "usage", "failure_reason": "invalid_provider"}})
	}
	logs = audits()
	require.Len(t, logs, 6)
	assert.Equal(t, failed, logs[2:])
	var methods []model.PayableSettlementMethod
	require.NoError(t, db.Find(&methods).Error)
	require.Len(t, methods, 1)
	assert.Equal(t, owner.Id, methods[0].ProviderId)
	assert.Equal(t, model.PayableSettlementUsage, methods[0].Method)

	// 列表的筛选参数不合法
	for _, query := range []string{"?methods=weekly", "?provider_ids=0"} {
		reply = receivableReplyOf(t, payableGet(t, "/api/payable_bill/settlement_methods"+query, nil, GetPayableSettlementMethods))
		assert.False(t, reply.Success, query)
		assert.Equal(t, "Invalid parameters", reply.MessageKey, query)
	}
}
