package model

import (
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	ChannelPushRunRunning = "running"
	ChannelPushRunSuccess = "success"
	ChannelPushRunPartial = "partial"
	ChannelPushRunFailed  = "failed"
)

type ChannelPushRun struct {
	Id                 int      `json:"id"`
	TaskId             int      `json:"task_id" gorm:"not null;index"`
	SiteId             int      `json:"site_id" gorm:"not null;index"`
	SiteName           string   `json:"site_name" gorm:"size:128;not null"`
	TargetGroup        string   `json:"target_group" gorm:"size:64;not null"`
	Suppliers          string   `json:"-" gorm:"type:text"` // []ChannelPushSupplierRef 的 JSON
	Status             string   `json:"status" gorm:"size:16;not null"`
	ErrorMessage       string   `json:"-" gorm:"type:text"` // common.Message 的 JSON，没有错误时为 ""
	StartedAt          int64    `json:"started_at" gorm:"not null;index"`
	FinishedAt         int64    `json:"finished_at" gorm:"not null"`
	SiteChannelCount   int      `json:"site_channel_count" gorm:"not null"`
	TargetChannelCount int      `json:"target_channel_count" gorm:"not null"`
	UnchangedCount     int      `json:"unchanged_count" gorm:"not null"`
	EnableCount        int      `json:"enable_count" gorm:"not null"`
	DisableCount       int      `json:"disable_count" gorm:"not null"`
	UpdateModelsCount  int      `json:"update_models_count" gorm:"not null"`
	CreateCount        int      `json:"create_count" gorm:"not null"`
	OrphanCount        int      `json:"orphan_count" gorm:"not null"`
	AutoDisabledCount  int      `json:"auto_disabled_count" gorm:"not null"`
	UnsupportedCount   int      `json:"unsupported_count" gorm:"not null"`
	FailedActionCount  int      `json:"failed_action_count" gorm:"not null"`
	SiteSnapshot       LongText `json:"-"`
	TargetSnapshot     LongText `json:"-"`
	Actions            LongText `json:"-"`
}

type ChannelPushRunSupplier struct {
	Id             int `json:"id"`
	RunId          int `json:"run_id" gorm:"not null;index"`
	SupplierUserId int `json:"supplier_user_id" gorm:"not null;index"`
}

var (
	channelPushRunResultColumns = []string{"status", "error_message", "finished_at",
		"site_channel_count", "target_channel_count", "unchanged_count", "enable_count", "disable_count",
		"update_models_count", "create_count", "orphan_count", "auto_disabled_count", "unsupported_count",
		"failed_action_count"}
	channelPushRunDetailColumns = []string{"site_snapshot", "target_snapshot", "actions"}
)

// ListDueChannelPushTasks returns enabled tasks whose 0 < next_run_at <= now
// and whose site is not deleted, earliest first.
func ListDueChannelPushTasks(now int64, limit int) ([]ChannelPushTask, error) {
	var tasks []ChannelPushTask
	err := DB.Where("enabled = ? AND next_run_at > ? AND next_run_at <= ? AND site_id IN (?)",
		true, 0, now, DB.Model(&ThirdPartySite{}).Select("id")).
		Order("next_run_at asc").Order("id asc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// ClaimChannelPushRun takes the task off the queue and inserts its running
// record in one transaction. The queue is cleared only while next_run_at still
// holds the value read by the scan; ok is false when someone else changed it.
func ClaimChannelPushRun(task ChannelPushTask, now int64) (run *ChannelPushRun, ok bool, err error) {
	if task.NextRunAt <= 0 {
		return nil, false, nil
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&ChannelPushTask{}).
			Where("id = ? AND enabled = ? AND next_run_at = ?", task.Id, true, task.NextRunAt).
			Update("next_run_at", 0)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		// Snapshot the task as stored now, inside the claim.
		var current ChannelPushTask
		if err := tx.First(&current, task.Id).Error; err != nil {
			return err
		}
		var sites []ThirdPartySite
		if err := tx.Unscoped().Select("id", "name").Where("id = ?", current.SiteId).Find(&sites).Error; err != nil {
			return err
		}
		siteName := ""
		if len(sites) > 0 {
			siteName = sites[0].Name
		}
		var supplierIds []int
		if err := tx.Model(&ChannelPushTaskSupplier{}).Where("task_id = ?", current.Id).
			Order("supplier_user_id asc").Pluck("supplier_user_id", &supplierIds).Error; err != nil {
			return err
		}
		usernames := map[int]string{}
		if len(supplierIds) > 0 {
			var users []User
			if err := tx.Unscoped().Select("id", "username").Where("id IN ?", supplierIds).Find(&users).Error; err != nil {
				return err
			}
			for _, user := range users {
				usernames[user.Id] = user.Username
			}
		}
		refs := make([]ChannelPushSupplierRef, 0, len(supplierIds))
		for _, id := range supplierIds {
			refs = append(refs, ChannelPushSupplierRef{Id: id, Username: usernames[id]})
		}
		encoded, err := common.Marshal(refs)
		if err != nil {
			return err
		}
		run = &ChannelPushRun{
			TaskId:      current.Id,
			SiteId:      current.SiteId,
			SiteName:    siteName,
			TargetGroup: current.TargetGroup,
			Suppliers:   string(encoded),
			Status:      ChannelPushRunRunning,
			StartedAt:   now,
		}
		if err := tx.Create(run).Error; err != nil {
			return err
		}
		if len(supplierIds) > 0 {
			rows := make([]ChannelPushRunSupplier, 0, len(supplierIds))
			for _, id := range supplierIds {
				rows = append(rows, ChannelPushRunSupplier{RunId: run.Id, SupplierUserId: id})
			}
			if err := tx.Create(&rows).Error; err != nil {
				return err
			}
		}
		ok = true
		return nil
	})
	if err != nil || !ok {
		return nil, false, err
	}
	return run, true, nil
}

// FinishChannelPushRun writes the run's outcome, then records the finish on
// the task. When the details cannot be stored, the outcome is written without
// them and error_message says why. A periodic task is queued again only if
// nothing re-queued or disabled it during the run.
func FinishChannelPushRun(run *ChannelPushRun, finishedAt int64) error {
	run.FinishedAt = finishedAt
	columns := append(slices.Clone(channelPushRunResultColumns), channelPushRunDetailColumns...)
	if err := DB.Model(&ChannelPushRun{Id: run.Id}).Select(columns).Updates(run).Error; err != nil {
		message, marshalErr := common.Marshal(common.NewMessage("Run details could not be saved: {{detail}}",
			map[string]any{"detail": err.Error()}))
		if marshalErr != nil {
			return marshalErr
		}
		run.ErrorMessage = string(message)
		run.SiteSnapshot, run.TargetSnapshot, run.Actions = "", "", ""
		if err := DB.Model(&ChannelPushRun{Id: run.Id}).Select(channelPushRunResultColumns).Updates(run).Error; err != nil {
			return err
		}
	}

	var task ChannelPushTask
	if err := DB.First(&task, run.TaskId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if err := DB.Model(&ChannelPushTask{}).Where("id = ?", task.Id).Update("last_finished_at", finishedAt).Error; err != nil {
		return err
	}
	if task.TaskType != ChannelPushTaskTypePeriodic {
		return nil
	}
	return DB.Model(&ChannelPushTask{}).
		Where("id = ? AND enabled = ? AND next_run_at = ?", task.Id, true, 0).
		Update("next_run_at", finishedAt+int64(task.IntervalMinutes)*60).Error
}

// RecoverInterruptedChannelPushRuns fails the runs a restart left in running
// and records the finish on their tasks. A periodic task is queued again only
// if next_run_at is still 0, so a save made in the meantime wins. It returns
// the number of runs recovered.
func RecoverInterruptedChannelPushRuns(now int64) (int, error) {
	message, err := common.Marshal(common.NewMessage("The run was interrupted by a service restart"))
	if err != nil {
		return 0, err
	}
	recovered := 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		var runs []ChannelPushRun
		if err := tx.Select("id", "task_id").Where("status = ?", ChannelPushRunRunning).Find(&runs).Error; err != nil {
			return err
		}
		if len(runs) == 0 {
			return nil
		}
		runIds := make([]int, 0, len(runs))
		taskIds := make([]int, 0, len(runs))
		for _, run := range runs {
			runIds = append(runIds, run.Id)
			if !slices.Contains(taskIds, run.TaskId) {
				taskIds = append(taskIds, run.TaskId)
			}
		}
		result := tx.Model(&ChannelPushRun{}).Where("id IN ? AND status = ?", runIds, ChannelPushRunRunning).
			Updates(map[string]any{"status": ChannelPushRunFailed, "error_message": string(message), "finished_at": now})
		if result.Error != nil {
			return result.Error
		}
		recovered = int(result.RowsAffected)

		var tasks []ChannelPushTask
		if err := tx.Where("id IN ?", taskIds).Find(&tasks).Error; err != nil {
			return err
		}
		for _, task := range tasks {
			if err := tx.Model(&ChannelPushTask{}).Where("id = ?", task.Id).Update("last_finished_at", now).Error; err != nil {
				return err
			}
			if task.TaskType != ChannelPushTaskTypePeriodic {
				continue
			}
			if err := tx.Model(&ChannelPushTask{}).
				Where("id = ? AND enabled = ? AND next_run_at = ?", task.Id, true, 0).
				Update("next_run_at", now+int64(task.IntervalMinutes)*60).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return recovered, nil
}

// GetChannelPushSite reads a live site with its ApiKey, outside the SQL log.
func GetChannelPushSite(id int) (*ThirdPartySite, error) {
	var site ThirdPartySite
	err := DB.Session(&gorm.Session{Logger: logger.Discard}).First(&site, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrThirdPartySiteNotFound
	}
	if err != nil {
		return nil, err
	}
	return &site, nil
}

// ActiveChannelSuppliers maps the IDs that still belong to live channel
// suppliers to their usernames.
func ActiveChannelSuppliers(userIDs []int) (map[int]string, error) {
	active := map[int]string{}
	if len(userIDs) == 0 {
		return active, nil
	}
	var users []User
	if err := DB.Select("id", "username").Where("id IN ? AND role = ?", userIDs, common.RoleChannelSupplierUser).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		active[user.Id] = user.Username
	}
	return active, nil
}

// ChannelPushSupplierChannels lists the channels each supplier created as a
// supplier, without keys, by channel ID ascending.
func ChannelPushSupplierChannels(supplierIDs []int) ([]*Channel, error) {
	channels := []*Channel{}
	for _, supplierID := range supplierIDs {
		var found []*Channel
		query := DB.Model(&Channel{}).Select("id", "name", "type", "status", "models", "creator_user_id")
		if err := CreatorChannelsScope(supplierID).Apply(query).Find(&found).Error; err != nil {
			return nil, err
		}
		channels = append(channels, found...)
	}
	slices.SortFunc(channels, func(a, b *Channel) int { return a.Id - b.Id })
	return channels, nil
}

// ChannelPushOtherTaskChannelIDs returns the channels owned by the site's
// other tasks, disabled ones included: those created by their suppliers that
// are still channel suppliers.
func ChannelPushOtherTaskChannelIDs(siteID, taskID int) (map[int]bool, error) {
	var supplierIds []int
	if err := DB.Model(&ChannelPushTaskSupplier{}).Where("site_id = ? AND task_id <> ?", siteID, taskID).
		Pluck("supplier_user_id", &supplierIds).Error; err != nil {
		return nil, err
	}
	active, err := ActiveChannelSuppliers(supplierIds)
	if err != nil {
		return nil, err
	}
	ids := map[int]bool{}
	for supplierID := range active {
		var channelIds []int
		if err := CreatorChannelsScope(supplierID).Apply(DB.Model(&Channel{})).Pluck("id", &channelIds).Error; err != nil {
			return nil, err
		}
		for _, id := range channelIds {
			ids[id] = true
		}
	}
	return ids, nil
}

// ExistingChannelIDs reports which of the IDs name an existing channel.
func ExistingChannelIDs(ids []int) (map[int]bool, error) {
	existing := map[int]bool{}
	if len(ids) == 0 {
		return existing, nil
	}
	var found []int
	if err := DB.Model(&Channel{}).Where("id IN ?", ids).Pluck("id", &found).Error; err != nil {
		return nil, err
	}
	for _, id := range found {
		existing[id] = true
	}
	return existing, nil
}

// GetChannelPushChannelForCreate reads a whole channel, key included, outside the SQL log.
func GetChannelPushChannelForCreate(id int) (*Channel, error) {
	var channel Channel
	if err := DB.Session(&gorm.Session{Logger: logger.Discard}).First(&channel, id).Error; err != nil {
		return nil, err
	}
	return &channel, nil
}

var ErrChannelPushRunNotFound = errors.New("Run does not exist")

// ChannelPushRunFilter narrows the run list; a zero field means no limit.
// StartFrom and StartTo are a closed range on started_at.
type ChannelPushRunFilter struct {
	StartFrom, StartTo int64
	SupplierUserId     int
	SiteId             int
}

// ChannelPushRunView is a run as listed: no details, suppliers and error parsed.
type ChannelPushRunView struct {
	ChannelPushRun
	Suppliers []ChannelPushSupplierRef `json:"suppliers"`
	Error     *common.Message          `json:"error"`
}

// ListChannelPushRuns pages the runs, newest first, without the three detail columns.
func ListChannelPushRuns(filter ChannelPushRunFilter, startIdx, num int) ([]ChannelPushRunView, int64, error) {
	query := DB.Model(&ChannelPushRun{})
	if filter.StartFrom > 0 {
		query = query.Where("started_at >= ?", filter.StartFrom)
	}
	if filter.StartTo > 0 {
		query = query.Where("started_at <= ?", filter.StartTo)
	}
	if filter.SiteId > 0 {
		query = query.Where("site_id = ?", filter.SiteId)
	}
	if filter.SupplierUserId > 0 {
		query = query.Where("id IN (?)", DB.Model(&ChannelPushRunSupplier{}).
			Select("run_id").Where("supplier_user_id = ?", filter.SupplierUserId))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var runs []ChannelPushRun
	if err := query.Omit(channelPushRunDetailColumns...).Order("started_at desc").Order("id desc").
		Limit(num).Offset(startIdx).Find(&runs).Error; err != nil {
		return nil, 0, err
	}
	views := make([]ChannelPushRunView, 0, len(runs))
	for _, run := range runs {
		view := ChannelPushRunView{ChannelPushRun: run, Suppliers: []ChannelPushSupplierRef{}}
		if run.Suppliers != "" {
			if err := common.UnmarshalJsonStr(run.Suppliers, &view.Suppliers); err != nil {
				return nil, 0, err
			}
		}
		if run.ErrorMessage != "" {
			var message common.Message
			if err := common.UnmarshalJsonStr(run.ErrorMessage, &message); err != nil {
				return nil, 0, err
			}
			view.Error = &message
		}
		views = append(views, view)
	}
	return views, total, nil
}

// GetChannelPushRunDetail reads one stored detail of a run; kind is site,
// target or actions.
func GetChannelPushRunDetail(id int, kind string) (LongText, error) {
	column := map[string]string{"site": "site_snapshot", "target": "target_snapshot", "actions": "actions"}[kind]
	if column == "" {
		return "", errors.New("Invalid parameters")
	}
	var runs []ChannelPushRun
	if err := DB.Select("id", column).Where("id = ?", id).Limit(1).Find(&runs).Error; err != nil {
		return "", err
	}
	if len(runs) == 0 {
		return "", ErrChannelPushRunNotFound
	}
	switch kind {
	case "site":
		return runs[0].SiteSnapshot, nil
	case "target":
		return runs[0].TargetSnapshot, nil
	}
	return runs[0].Actions, nil
}
