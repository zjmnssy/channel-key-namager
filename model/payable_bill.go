package model

import (
	"errors"

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
	BillDate          int      `json:"bill_date" gorm:"not null;index:idx_payable_usages_bill_date"`
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
// 并把更早日期仍未完成的进度改为 superseded。billDate 已有任务（另一节点已建）时返回 created=false。
func CreatePayablePullTask(billDate int, sites []*ThirdPartySite, now int64) (created bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		unfinished := []string{PayablePullPending, PayablePullRetrying}
		var staleTaskIDs []int
		err := tx.Model(&PayablePullSite{}).Where("bill_date < ? AND status IN ?", billDate, unfinished).
			Distinct().Pluck("task_id", &staleTaskIDs).Error
		if err != nil {
			return err
		}
		if len(staleTaskIDs) > 0 {
			lastError, err := common.Marshal(common.NewMessage(MsgPayablePullSuperseded))
			if err != nil {
				return err
			}
			err = tx.Model(&PayablePullSite{}).Where("bill_date < ? AND status IN ?", billDate, unfinished).
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

		task := &PayablePullTask{
			BillDate: billDate, SiteTotal: len(sites), Status: PayableTaskRunning,
			CreatedTime: now, StartedTime: now, UpdatedTime: now,
		}
		if len(sites) == 0 {
			task.Status, task.FinishedTime = PayableTaskSuccess, now
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
	unfinished := []string{PayablePullPending, PayablePullRetrying}
	var taskIDs []int
	err = tx.Model(&PayablePullSite{}).Where("status IN ? AND site_id IN (?)", unfinished, deletedSites()).
		Distinct().Pluck("task_id", &taskIDs).Error
	if err != nil {
		return err
	}
	if len(taskIDs) == 0 {
		return nil
	}
	err = tx.Model(&PayablePullSite{}).Where("status IN ? AND site_id IN (?)", unfinished, deletedSites()).
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
		if err := tx.Where("pull_site_id = ?", pull.Id).Delete(&PayableChannelUsage{}).Error; err != nil {
			return err
		}
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
			"updated_time":   now,
		}).Error
	case !baseURLChanged && pull.Status == PayablePullFailed:
		err = tx.Model(&PayablePullSite{}).Where("id = ?", pull.Id).Updates(map[string]any{
			"status":        PayablePullRetrying,
			"attempts":      0,
			"next_retry_at": 0,
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
