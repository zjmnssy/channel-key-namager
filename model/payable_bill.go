package model

import (
	"errors"
	"maps"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// 任务状态
	PayableTaskRunning       = "running"
	PayableTaskSuccess       = "success"
	PayableTaskPartialFailed = "partial_failed"

	// 站点进度状态，superseded 表示被次日任务取代，界面显示为失败
	PayablePullPending    = "pending"
	PayablePullRetrying   = "retrying"
	PayablePullSuccess    = "success"
	PayablePullFailed     = "failed"
	PayablePullSuperseded = "superseded"

	// 增量标记
	PayableDeltaNormal     = "normal"
	PayableDeltaFirstSync  = "first_sync"
	PayableDeltaNewChannel = "new_channel"
	PayableDeltaRollback   = "rollback"

	// 提供商来源
	PayableProviderManual    = "manual"
	PayableProviderParsed    = "parsed"
	PayableProviderInherited = "inherited"
	PayableProviderUnknown   = "unknown"

	PayablePullLeaseSeconds int64 = 300
)

var ErrPayableSiteDeleted = errors.New("The site has been deleted")

// MsgPayablePullSuperseded 是被次日任务取代时写入 last_error 的英文消息键。
const MsgPayablePullSuperseded = "Replaced by the next day's pull task"

// payablePullUnfinished 是还要继续拉取的进度状态。
var payablePullUnfinished = []string{PayablePullPending, PayablePullRetrying}

// errPayablePullLeaseLost 让 SavePayablePullAttempt 的事务回滚：这一行进度已不归本次拉取。
var errPayablePullLeaseLost = errors.New("payable pull lease lost")

// PayablePullTask 是某个北京日期的每日拉取任务。
type PayablePullTask struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	BillDate     int    `json:"bill_date" gorm:"not null;uniqueIndex:idx_payable_pull_tasks_bill_date"`
	SiteTotal    int    `json:"site_total" gorm:"not null"`
	Status       string `json:"status" gorm:"size:16;not null"`
	CreatedTime  int64  `json:"created_time"`
	StartedTime  int64  `json:"started_time"`
	FinishedTime int64  `json:"finished_time"`
	UpdatedTime  int64  `json:"updated_time"`
}

// PayablePullSite 是任务内某个站点的拉取进度。
type PayablePullSite struct {
	Id            int    `json:"id" gorm:"primaryKey"`
	TaskId        int    `json:"task_id" gorm:"not null;uniqueIndex:idx_payable_pull_sites_task_site,priority:1"`
	SiteId        int    `json:"site_id" gorm:"not null;uniqueIndex:idx_payable_pull_sites_task_site,priority:2;index:idx_payable_pull_sites_site_date,priority:1"`
	BillDate      int    `json:"bill_date" gorm:"not null;index:idx_payable_pull_sites_site_date,priority:2"`
	SiteName      string `json:"site_name" gorm:"size:128"`
	SiteBaseURL   string `json:"site_base_url" gorm:"column:site_base_url;size:1024"`
	Status        string `json:"status" gorm:"size:16;not null;index:idx_payable_pull_sites_due,priority:1"`
	NextPage      int    `json:"next_page"`          // 下一次要拉的页码，从 1 开始
	Pass          int    `json:"pass"`               // 1 = 首轮，2 = 总数核对补扫
	ReportedTotal int    `json:"reported_total"`     // 站点最近一次返回的 total
	ChannelCount  int    `json:"channel_count"`      // 本进度已写入的明细行数
	Attempts      int    `json:"attempts"`           // 连续失败次数，成功一页后归 0
	LastError     string `json:"-" gorm:"type:text"` // common.Message JSON
	Note          string `json:"-" gorm:"type:text"` // 成功但总数不一致时的说明，common.Message JSON
	NextRetryAt   int64  `json:"next_retry_at" gorm:"index:idx_payable_pull_sites_due,priority:2"`
	LeaseUntil    int64  `json:"-"`
	FinishedTime  int64  `json:"finished_time"`
	CreatedTime   int64  `json:"created_time"`
	UpdatedTime   int64  `json:"updated_time"`
}

// PayableChannelUsage 是某站点某天某个远端渠道的累计用量与增量明细。
type PayableChannelUsage struct {
	Id                int      `json:"id" gorm:"primaryKey"`
	TaskId            int      `json:"task_id" gorm:"not null;index"`
	PullSiteId        int      `json:"pull_site_id" gorm:"not null;uniqueIndex:idx_payable_usages_pull_channel,priority:1"`
	BillDate          int      `json:"bill_date" gorm:"not null;index:idx_payable_usages_bill_date;index:idx_payable_usages_site_channel,priority:3"`
	SiteId            int      `json:"site_id" gorm:"not null;index:idx_payable_usages_site_channel,priority:1"`
	SiteName          string   `json:"site_name" gorm:"size:128"`
	SiteBaseURL       string   `json:"site_base_url" gorm:"column:site_base_url;size:1024"`
	RemoteChannelId   int      `json:"remote_channel_id" gorm:"not null;uniqueIndex:idx_payable_usages_pull_channel,priority:2;index:idx_payable_usages_site_channel,priority:2"`
	RemoteChannelName string   `json:"remote_channel_name" gorm:"size:255"`
	UsedQuota         int64    `json:"used_quota" gorm:"not null"`
	DeltaQuota        *int64   `json:"delta_quota"` // nil = 首次同步无基线
	DeltaFlag         string   `json:"delta_flag" gorm:"size:16"`
	QuotaPerUnit      int64    `json:"quota_per_unit"`
	ProviderId        int      `json:"provider_id" gorm:"not null;default:0;index"` // 0 = 未知
	ProviderName      string   `json:"provider_name" gorm:"size:64"`
	ProviderSource    string   `json:"provider_source" gorm:"size:16"`
	PayableDiscount   *float64 `json:"payable_discount"`
	SyncedAt          int64    `json:"synced_at"`
	CreatedTime       int64    `json:"created_time"`
	UpdatedTime       int64    `json:"updated_time"`
}

// PayableProviderOverride 是手动指定的 (站点, 远端渠道) 到提供商的对应。
type PayableProviderOverride struct {
	Id              int   `json:"id" gorm:"primaryKey"`
	SiteId          int   `json:"site_id" gorm:"not null;uniqueIndex:idx_payable_overrides_site_channel,priority:1"`
	RemoteChannelId int   `json:"remote_channel_id" gorm:"not null;uniqueIndex:idx_payable_overrides_site_channel,priority:2"`
	ProviderId      int   `json:"provider_id" gorm:"not null"`
	CreatedTime     int64 `json:"created_time"`
	UpdatedTime     int64 `json:"updated_time"`
}

// CreatePayablePullTask 在一个事务里创建 billDate 的拉取任务和各站点进度，
// 并把更早日期仍未完成的进度改为 superseded。billDate 已有任务（之前的扫描或另一节点已建）时返回 created=false。
func CreatePayablePullTask(billDate int, sites []*ThirdPartySite, now int64) (created bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		// 取代在任务已存在时也执行：站点编辑与建任务并发时，编辑可能按建任务之前的视图
		// 把更早任务的进度改回未完成，下一次扫描在这里把它重新取代。
		var staleTaskIDs []int
		err := tx.Model(&PayablePullSite{}).Where("bill_date < ? AND status IN ?", billDate, payablePullUnfinished).
			Distinct().Pluck("task_id", &staleTaskIDs).Error
		if err != nil {
			return err
		}
		if len(staleTaskIDs) > 0 {
			lastError, err := common.Marshal(common.NewMessage(MsgPayablePullSuperseded))
			if err != nil {
				return err
			}
			err = tx.Model(&PayablePullSite{}).Where("bill_date < ? AND status IN ?", billDate, payablePullUnfinished).
				Updates(map[string]any{
					"status":        PayablePullSuperseded,
					"last_error":    string(lastError),
					"next_retry_at": 0,
					"updated_time":  now,
				}).Error
			if err != nil {
				return err
			}
			for _, taskID := range staleTaskIDs {
				if err := RefreshPayableTask(tx, taskID, now); err != nil {
					return err
				}
			}
		}

		// 扫描每分钟都会调用：任务已存在就直接返回，不靠插入时的唯一约束冲突，
		// 否则 PostgreSQL、MySQL 每次冲突都会消耗一个自增值，页面上的任务 ID 会跳号。
		var existing int64
		if err := tx.Model(&PayablePullTask{}).Where("bill_date = ?", billDate).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		task := &PayablePullTask{
			BillDate: billDate, SiteTotal: len(sites), Status: PayableTaskRunning,
			CreatedTime: now, UpdatedTime: now,
		}
		// 开始时间是第一次请求的时刻，由 StartPayableTask 记下；没有站点时任务建好即结束。
		if len(sites) == 0 {
			task.Status, task.StartedTime, task.FinishedTime = PayableTaskSuccess, now, now
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		for _, site := range sites {
			err := tx.Create(&PayablePullSite{
				TaskId: task.Id, SiteId: site.Id, BillDate: billDate, SiteName: site.Name, SiteBaseURL: site.BaseURL,
				Status: PayablePullPending, NextPage: 1, Pass: 1, CreatedTime: now, UpdatedTime: now,
			}).Error
			if err != nil {
				return err
			}
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
}

// RefreshPayableTask 按进度行状态重算任务状态与完成时间。
func RefreshPayableTask(tx *gorm.DB, taskID int, now int64) error {
	// 先锁任务行，让同一任务的刷新依次进行：后来的等前一个提交后才读进度状态，
	// 两个站点同时拉完时，任务不会因为双方都没看到对方的结果而一直停在 running。
	var task PayablePullTask
	if err := lockForUpdate(tx).Select("id").Where("id = ?", taskID).Find(&task).Error; err != nil {
		return err
	}
	var statuses []string
	if err := tx.Model(&PayablePullSite{}).Where("task_id = ?", taskID).Distinct().Pluck("status", &statuses).Error; err != nil {
		return err
	}
	status := PayableTaskSuccess
	for _, s := range statuses {
		switch s {
		case PayablePullPending, PayablePullRetrying:
			status = PayableTaskRunning
		case PayablePullFailed, PayablePullSuperseded:
			if status != PayableTaskRunning {
				status = PayableTaskPartialFailed
			}
		}
	}
	if status == PayableTaskRunning {
		return tx.Model(&PayablePullTask{}).Where("id = ?", taskID).
			Updates(map[string]any{"status": status, "finished_time": 0, "updated_time": now}).Error
	}
	if err := tx.Model(&PayablePullTask{}).Where("id = ?", taskID).
		Updates(map[string]any{"status": status, "updated_time": now}).Error; err != nil {
		return err
	}
	return tx.Model(&PayablePullTask{}).Where("id = ? AND finished_time = 0", taskID).Update("finished_time", now).Error
}

// FailPayablePullsOfDeletedSites 把已删除站点未完成的进度改为失败，原因是站点已删除，并刷新受影响的任务。
func FailPayablePullsOfDeletedSites(tx *gorm.DB, now int64) error {
	lastError, err := common.Marshal(common.NewMessage(ErrPayableSiteDeleted.Error()))
	if err != nil {
		return err
	}
	// 用 IN 已删除的站点，不用 NOT IN 在线的站点：没有在线站点时 GORM 会生成 NOT IN (NULL)，一行都不匹配。
	deletedSites := func() *gorm.DB {
		return tx.Unscoped().Model(&ThirdPartySite{}).Select("id").Where("deleted_at IS NOT NULL")
	}
	var taskIDs []int
	err = tx.Model(&PayablePullSite{}).Where("status IN ? AND site_id IN (?)", payablePullUnfinished, deletedSites()).
		Distinct().Pluck("task_id", &taskIDs).Error
	if err != nil {
		return err
	}
	if len(taskIDs) == 0 {
		return nil
	}
	err = tx.Model(&PayablePullSite{}).Where("status IN ? AND site_id IN (?)", payablePullUnfinished, deletedSites()).
		Updates(map[string]any{
			"status":        PayablePullFailed,
			"last_error":    string(lastError),
			"next_retry_at": 0,
			"updated_time":  now,
		}).Error
	if err != nil {
		return err
	}
	for _, taskID := range taskIDs {
		if err := RefreshPayableTask(tx, taskID, now); err != nil {
			return err
		}
	}
	return nil
}

// RefreshSettledPayableTasks 重算仍为 running、但已没有 pending/retrying 进度的任务，每次扫描调用。
// 正常情况下没有这样的任务。MySQL 可重复读下，删除站点的事务刷新任务时用的是事务开头建立的快照，
// 恰好另一站点的进度同时结束时，双方都会算出 running，之后再没有刷新，任务就停在那里，这里在下一次扫描补正。
// 每个任务单独一个事务：RefreshPayableTask 先锁任务行再读进度状态，读到的是加锁之后的数据。
func RefreshSettledPayableTasks(now int64) error {
	unfinishedTaskIDs := DB.Model(&PayablePullSite{}).Select("task_id").Where("status IN ?", payablePullUnfinished)
	var taskIDs []int
	err := DB.Model(&PayablePullTask{}).Where("status = ? AND id NOT IN (?)", PayableTaskRunning, unfinishedTaskIDs).
		Pluck("id", &taskIDs).Error
	if err != nil {
		return err
	}
	for _, taskID := range taskIDs {
		if err := DB.Transaction(func(tx *gorm.DB) error { return RefreshPayableTask(tx, taskID, now) }); err != nil {
			return err
		}
	}
	return nil
}

// reopenPayablePullsOnSiteEdit 编辑站点后重开它在最新任务里的进度行；superseded 行和更早任务的行不动。
// 地址变化时旧地址拉到的明细作废，pending/retrying/failed 的行从第 1 页重拉；
// 地址未变时只把 failed 的行改为重试，页码不动。
func reopenPayablePullsOnSiteEdit(tx *gorm.DB, site *ThirdPartySite, baseURLChanged bool, now int64) error {
	var latest PayablePullTask
	if err := tx.Order("bill_date DESC").Limit(1).Find(&latest).Error; err != nil {
		return err
	}
	if latest.Id == 0 {
		return nil
	}
	var pull PayablePullSite
	if err := tx.Where("task_id = ? AND site_id = ?", latest.Id, site.Id).Limit(1).Find(&pull).Error; err != nil {
		return err
	}
	if pull.Id == 0 {
		return nil
	}
	var err error
	switch {
	case baseURLChanged && pull.Status != PayablePullSuccess && pull.Status != PayablePullSuperseded:
		// 先改进度行（并清掉租约）再删明细：正在保存的拉取要么先提交，它写的明细随后一起删掉；
		// 要么等这里提交后因租约不符被拒。旧地址拉到的页不会在重置后写回。
		err = tx.Model(&PayablePullSite{}).Where("id = ?", pull.Id).Updates(map[string]any{
			"status":         PayablePullPending,
			"next_page":      1,
			"pass":           1,
			"channel_count":  0,
			"reported_total": 0,
			"site_name":      site.Name,
			"site_base_url":  site.BaseURL,
			"attempts":       0,
			"next_retry_at":  0,
			"last_error":     "",
			"lease_until":    0,
			"finished_time":  0,
			"updated_time":   now,
		}).Error
		if err == nil {
			err = tx.Where("pull_site_id = ?", pull.Id).Delete(&PayableChannelUsage{}).Error
		}
	case !baseURLChanged && pull.Status == PayablePullFailed:
		err = tx.Model(&PayablePullSite{}).Where("id = ?", pull.Id).Updates(map[string]any{
			"status":        PayablePullRetrying,
			"attempts":      0,
			"next_retry_at": 0,
			"finished_time": 0,
			"updated_time":  now,
		}).Error
	default:
		return nil
	}
	if err != nil {
		return err
	}
	return RefreshPayableTask(tx, latest.Id, now)
}

// payablePullsDue 选出到了拉取时间、没有人在拉、属于最新任务的未完成进度。
// 更早任务的进度即使被改回未完成（站点编辑与建任务并发时可能发生）也不拉：它和最新任务的进度用同一个基线，
// 拉成功会让同一段消费同时计入两天的账单；之后的扫描执行取代时会把它改为 superseded。
func payablePullsDue(now int64) *gorm.DB {
	latestBillDate := DB.Model(&PayablePullTask{}).Select("MAX(bill_date)")
	return DB.Model(&PayablePullSite{}).Where("status IN ? AND next_retry_at <= ? AND lease_until < ? AND bill_date = (?)",
		payablePullUnfinished, now, now, latestBillDate)
}

// DuePayablePulls 返回到期可拉的进度（见 payablePullsDue），按下次重试时间、id 升序。
func DuePayablePulls(now int64) ([]PayablePullSite, error) {
	var pulls []PayablePullSite
	err := payablePullsDue(now).Order("next_retry_at, id").Find(&pulls).Error
	return pulls, err
}

// ClaimPayablePull 认领一行进度的拉取租约：只有它仍到期可拉（见 payablePullsDue）且本次抢到时 ok 才为 true。
func ClaimPayablePull(id int, now int64) (lease int64, ok bool, err error) {
	lease = now + PayablePullLeaseSeconds
	result := payablePullsDue(now).Where("id = ?", id).Update("lease_until", lease)
	if result.Error != nil {
		return 0, false, result.Error
	}
	return lease, result.RowsAffected == 1, nil
}

// StartPayableTask 把任务的开始时间记为第一次请求的时刻，已记过的不再改。
func StartPayableTask(taskID int, now int64) error {
	return DB.Model(&PayablePullTask{}).Where("id = ? AND started_time = 0", taskID).
		Updates(map[string]any{"started_time": now, "updated_time": now}).Error
}

// PayableBaseline 找站点在账单日期 billDate 之前、地址为 baseURL 的最近一次完整拉取（进度 success），
// 返回其中 remoteIDs 各渠道的累计值。没有这样的拉取时 found 为 false：站点还没有完整拉取过，或地址变了、基线已重置。
// 失败、被取代的进度留下的是不完整拉取，永不作基线。
// complete 表示那次拉取写入的渠道数不少于站点报的总数；少于时（补扫后仍对不上、以已拉到的为准结束），
// 基线里没有的渠道可能只是那次漏拉了，不能当作新建的渠道。
func PayableBaseline(siteID, billDate int, baseURL string, remoteIDs []int) (found, complete bool, quotas map[int]int64, err error) {
	var pull PayablePullSite
	err = DB.Select("id", "channel_count", "reported_total").
		Where("site_id = ? AND bill_date < ? AND status = ? AND site_base_url = ?", siteID, billDate, PayablePullSuccess, baseURL).
		Order("bill_date DESC").Limit(1).Find(&pull).Error
	if err != nil || pull.Id == 0 {
		return false, false, nil, err
	}
	var usages []PayableChannelUsage
	err = DB.Select("remote_channel_id", "used_quota").
		Where("pull_site_id = ? AND remote_channel_id IN ?", pull.Id, remoteIDs).Find(&usages).Error
	if err != nil {
		return false, false, nil, err
	}
	quotas = make(map[int]int64, len(usages))
	for _, usage := range usages {
		quotas[usage.RemoteChannelId] = usage.UsedQuota
	}
	return true, pull.ChannelCount >= pull.ReportedTotal, quotas, nil
}

// ExistingPayableChannels 返回 remoteIDs 中在该进度下已有明细行的远端渠道。
func ExistingPayableChannels(pullSiteID int, remoteIDs []int) (map[int]bool, error) {
	var ids []int
	err := DB.Model(&PayableChannelUsage{}).Where("pull_site_id = ? AND remote_channel_id IN ?", pullSiteID, remoteIDs).
		Pluck("remote_channel_id", &ids).Error
	if err != nil {
		return nil, err
	}
	existing := make(map[int]bool, len(ids))
	for _, id := range ids {
		existing[id] = true
	}
	return existing, nil
}

// SavePayablePullAttempt 在一个事务里保存一次拉取尝试：仍持有租约时写入 updates 并释放租约，插入新明细行（已有的跳过），
// 重新数出本进度的明细行数写入 channel_count；进度结束（success/failed）时同一事务刷新任务。
// 租约已丢失，或进度在此期间被改成了别的状态（被次日任务取代、站点已删除）时什么都不写，返回 false。
func SavePayablePullAttempt(pullSiteID int, lease int64, rows []*PayableChannelUsage, updates map[string]any, now int64) (saved bool, err error) {
	values := make(map[string]any, len(updates)+2)
	maps.Copy(values, updates)
	values["lease_until"] = int64(0)
	values["updated_time"] = now
	err = DB.Transaction(func(tx *gorm.DB) error {
		// 先带租约条件改进度行，再插明细：这一行的行锁让站点编辑的重置要么等本次提交后再删明细，
		// 要么先生效并清掉租约，使这里更新 0 行。事务由写开始，SQLite 上也不会在读锁升级为写锁时直接报忙。
		result := tx.Model(&PayablePullSite{}).
			Where("id = ? AND lease_until = ? AND status IN ?", pullSiteID, lease, payablePullUnfinished).Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errPayablePullLeaseLost
		}
		if len(rows) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(rows).Error; err != nil {
				return err
			}
		}
		channelCount := tx.Model(&PayableChannelUsage{}).Select("COUNT(*)").Where("pull_site_id = ?", pullSiteID)
		if err := tx.Model(&PayablePullSite{}).Where("id = ?", pullSiteID).Update("channel_count", channelCount).Error; err != nil {
			return err
		}
		if status := updates["status"]; status != PayablePullSuccess && status != PayablePullFailed {
			return nil
		}
		// 加锁读：MySQL 可重复读下，普通读会在等任务行锁之前建立快照，刷新任务时就看不到别的进度刚提交的结果。
		var pull PayablePullSite
		if err := lockForUpdate(tx).Select("task_id").First(&pull, pullSiteID).Error; err != nil {
			return err
		}
		return RefreshPayableTask(tx, pull.TaskId, now)
	})
	if errors.Is(err, errPayablePullLeaseLost) {
		return false, nil
	}
	return err == nil, err
}
