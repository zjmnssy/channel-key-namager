package controller

import (
	"errors"
	"net/http"
	"strconv"
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

	// 9. 小时数据不结算
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
