package model

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	ChannelPushTaskTypeOnce     = "once"
	ChannelPushTaskTypePeriodic = "periodic"
)

var ErrChannelPushTaskNotFound = errors.New("Task does not exist")

type ChannelPushTask struct {
	Id              int    `json:"id"`
	SiteId          int    `json:"site_id" gorm:"not null;index"`
	TaskType        string `json:"task_type" gorm:"size:16;not null"`
	IntervalMinutes int    `json:"interval_minutes" gorm:"not null"`
	TargetGroup     string `json:"target_group" gorm:"size:64;not null"`
	Enabled         bool   `json:"enabled" gorm:"not null"`
	NextRunAt       int64  `json:"next_run_at" gorm:"not null"`
	LastFinishedAt  int64  `json:"last_finished_at" gorm:"not null"`
	CreatedTime     int64  `json:"created_time" gorm:"not null"`
	UpdatedTime     int64  `json:"updated_time" gorm:"not null"`
}

type ChannelPushTaskSupplier struct {
	Id             int `json:"id"`
	TaskId         int `json:"task_id" gorm:"not null;index"`
	SiteId         int `json:"site_id" gorm:"not null;uniqueIndex:uk_channel_push_site_supplier,priority:1"`
	SupplierUserId int `json:"supplier_user_id" gorm:"not null;uniqueIndex:uk_channel_push_site_supplier,priority:2"`
}

type ChannelPushTaskInput struct {
	SiteId          int    `json:"site_id"`
	SupplierUserIds []int  `json:"supplier_user_ids"`
	TaskType        string `json:"task_type"`
	IntervalMinutes int    `json:"interval_minutes"`
	TargetGroup     string `json:"target_group"`
}

type ChannelPushSupplierRef struct {
	Id       int    `json:"id"`
	Username string `json:"username"`
}

type ChannelPushTaskView struct {
	ChannelPushTask
	SiteName    string                   `json:"site_name"`
	SiteDeleted bool                     `json:"site_deleted"`
	Suppliers   []ChannelPushSupplierRef `json:"suppliers"` // 按 ID 升序，用户名取当前值（含已删除用户）
}

// ChannelPushFieldError reports the request field that failed validation.
// Message is the English message key and Params fill its placeholders.
type ChannelPushFieldError struct {
	Field   string
	Message string
	Params  map[string]any
}

func (e *ChannelPushFieldError) Error() string { return e.Message }

type ChannelPushOptions struct {
	Sites     []ChannelPushSiteOption     `json:"sites"`     // 未删除的站点，按 ID 升序
	Suppliers []ChannelPushSupplierOption `json:"suppliers"` // 当前的渠道供应商（role 5、未删除），按 ID 升序
}

type ChannelPushSiteOption struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type ChannelPushSupplierOption struct {
	Id          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// findChannelPushSupplierConflict returns a supplier row, among supplierIds,
// that already belongs to a task of the site other than excludeTaskId. With
// liveTasksOnly the row must also belong to an existing task.
func findChannelPushSupplierConflict(tx *gorm.DB, siteId int, supplierIds []int, excludeTaskId int, liveTasksOnly bool) (*ChannelPushFieldError, error) {
	query := tx.Model(&ChannelPushTaskSupplier{}).
		Where("channel_push_task_suppliers.site_id = ? AND channel_push_task_suppliers.supplier_user_id IN ? AND channel_push_task_suppliers.task_id <> ?",
			siteId, supplierIds, excludeTaskId)
	if liveTasksOnly {
		query = query.Joins("JOIN channel_push_tasks ON channel_push_tasks.id = channel_push_task_suppliers.task_id")
	}
	var rows []ChannelPushTaskSupplier
	if err := query.Order("channel_push_task_suppliers.supplier_user_id asc").Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	var users []User
	if err := tx.Unscoped().Select("id", "username").Where("id = ?", rows[0].SupplierUserId).Find(&users).Error; err != nil {
		return nil, err
	}
	username := ""
	if len(users) > 0 {
		username = users[0].Username
	}
	return &ChannelPushFieldError{
		Field:   "supplier_user_ids",
		Message: "Supplier {{username}} already belongs to task {{task_id}} for this site",
		Params:  map[string]any{"username": username, "task_id": rows[0].TaskId},
	}, nil
}

// saveChannelPushTask creates a task (id == 0) or edits task id. The task and
// its supplier rows are written in one transaction.
func saveChannelPushTask(id int, input ChannelPushTaskInput) (*ChannelPushTask, error) {
	supplierIds := slices.Clone(input.SupplierUserIds)
	if len(supplierIds) == 0 {
		return nil, &ChannelPushFieldError{Field: "supplier_user_ids", Message: "Select at least one supplier"}
	}
	slices.Sort(supplierIds)
	if len(slices.Compact(slices.Clone(supplierIds))) != len(supplierIds) {
		return nil, &ChannelPushFieldError{Field: "supplier_user_ids", Message: "Each supplier can be selected only once"}
	}
	if input.TaskType != ChannelPushTaskTypeOnce && input.TaskType != ChannelPushTaskTypePeriodic {
		return nil, &ChannelPushFieldError{Field: "task_type", Message: "Unsupported task type"}
	}
	interval := 0
	if input.TaskType == ChannelPushTaskTypePeriodic {
		interval = input.IntervalMinutes
		if interval < 5 || interval > 1440 {
			return nil, &ChannelPushFieldError{Field: "interval_minutes", Message: "Interval must be an integer from 5 to 1440 minutes"}
		}
	}
	targetGroup := strings.TrimSpace(input.TargetGroup)
	groupLen := utf8.RuneCountInString(targetGroup)
	if groupLen == 0 {
		return nil, &ChannelPushFieldError{Field: "target_group", Message: "Target group is required"}
	}
	if groupLen > 64 {
		return nil, &ChannelPushFieldError{Field: "target_group", Message: "Target group must not exceed 64 characters"}
	}

	var task ChannelPushTask
	err := DB.Transaction(func(tx *gorm.DB) error {
		if id != 0 {
			if err := tx.First(&task, id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrChannelPushTaskNotFound
				}
				return err
			}
		}
		var siteCount int64
		if err := tx.Model(&ThirdPartySite{}).Where("id = ?", input.SiteId).Count(&siteCount).Error; err != nil {
			return err
		}
		if siteCount == 0 {
			return &ChannelPushFieldError{Field: "site_id", Message: "Site does not exist"}
		}
		var liveSupplierIds []int
		if err := tx.Model(&User{}).Where("id IN ? AND role = ?", supplierIds, common.RoleChannelSupplierUser).Pluck("id", &liveSupplierIds).Error; err != nil {
			return err
		}
		for _, supplierId := range input.SupplierUserIds {
			if !slices.Contains(liveSupplierIds, supplierId) {
				return &ChannelPushFieldError{Field: "supplier_user_ids", Message: "User {{id}} is not a channel supplier", Params: map[string]any{"id": supplierId}}
			}
		}
		conflict, err := findChannelPushSupplierConflict(tx, input.SiteId, supplierIds, id, true)
		if err != nil {
			return err
		}
		if conflict != nil {
			return conflict
		}

		now := common.GetTimestamp()
		task.SiteId = input.SiteId
		task.TaskType = input.TaskType
		task.IntervalMinutes = interval
		task.TargetGroup = targetGroup
		task.UpdatedTime = now
		if id == 0 {
			task.Enabled = true
			task.CreatedTime = now
		}
		if task.Enabled {
			task.NextRunAt = now
		}
		if id == 0 {
			if err := tx.Create(&task).Error; err != nil {
				return err
			}
		} else {
			columns := []string{"site_id", "task_type", "interval_minutes", "target_group", "next_run_at", "updated_time"}
			if err := tx.Model(&task).Select(columns).Updates(&task).Error; err != nil {
				return err
			}
			if err := tx.Where("task_id = ?", task.Id).Delete(&ChannelPushTaskSupplier{}).Error; err != nil {
				return err
			}
		}
		rows := make([]ChannelPushTaskSupplier, 0, len(supplierIds))
		for _, supplierId := range supplierIds {
			rows = append(rows, ChannelPushTaskSupplier{TaskId: task.Id, SiteId: task.SiteId, SupplierUserId: supplierId})
		}
		return tx.Create(&rows).Error
	})
	if err == nil {
		return &task, nil
	}
	var fieldErr *ChannelPushFieldError
	if errors.As(err, &fieldErr) || errors.Is(err, ErrChannelPushTaskNotFound) {
		return nil, err
	}
	// A failed write may be a unique-index collision with a row the in-
	// transaction check could not see; look again outside the aborted
	// transaction, without requiring the owning task to exist.
	conflict, lookupErr := findChannelPushSupplierConflict(DB, input.SiteId, supplierIds, id, false)
	if lookupErr == nil && conflict != nil {
		return nil, conflict
	}
	return nil, err
}

func CreateChannelPushTask(input ChannelPushTaskInput) (*ChannelPushTask, error) {
	return saveChannelPushTask(0, input)
}

func UpdateChannelPushTask(id int, input ChannelPushTaskInput) (*ChannelPushTask, error) {
	return saveChannelPushTask(id, input)
}

// SetChannelPushTaskEnabled enables or disables a task. Enabling queues it to
// run now; disabling clears the queue.
func SetChannelPushTaskEnabled(id int, enabled bool) (*ChannelPushTask, error) {
	var task ChannelPushTask
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&task, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrChannelPushTaskNotFound
			}
			return err
		}
		now := common.GetTimestamp()
		task.Enabled = enabled
		task.NextRunAt = 0
		if enabled {
			var siteCount int64
			if err := tx.Model(&ThirdPartySite{}).Where("id = ?", task.SiteId).Count(&siteCount).Error; err != nil {
				return err
			}
			if siteCount == 0 {
				return ErrThirdPartySiteNotFound
			}
			task.NextRunAt = now
		}
		task.UpdatedTime = now
		return tx.Model(&task).Select("enabled", "next_run_at", "updated_time").Updates(&task).Error
	})
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func ListChannelPushTasks(startIdx, num int) ([]ChannelPushTaskView, int64, error) {
	var total int64
	if err := DB.Model(&ChannelPushTask{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var tasks []ChannelPushTask
	if err := DB.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}
	views := make([]ChannelPushTaskView, 0, len(tasks))
	if len(tasks) == 0 {
		return views, total, nil
	}
	taskIds := make([]int, 0, len(tasks))
	siteIds := make([]int, 0, len(tasks))
	for _, task := range tasks {
		taskIds = append(taskIds, task.Id)
		siteIds = append(siteIds, task.SiteId)
	}
	var links []ChannelPushTaskSupplier
	if err := DB.Where("task_id IN ?", taskIds).Order("supplier_user_id asc").Find(&links).Error; err != nil {
		return nil, 0, err
	}
	userIds := make([]int, 0, len(links))
	for _, link := range links {
		userIds = append(userIds, link.SupplierUserId)
	}
	var users []User
	if len(userIds) > 0 {
		if err := DB.Unscoped().Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
			return nil, 0, err
		}
	}
	usernames := make(map[int]string, len(users))
	for _, user := range users {
		usernames[user.Id] = user.Username
	}
	// The ApiKey column is not read here.
	var sites []ThirdPartySite
	if err := DB.Unscoped().Select("id", "name", "deleted_at").Where("id IN ?", siteIds).Find(&sites).Error; err != nil {
		return nil, 0, err
	}
	siteByID := make(map[int]ThirdPartySite, len(sites))
	for _, site := range sites {
		siteByID[site.Id] = site
	}
	suppliersByTask := make(map[int][]ChannelPushSupplierRef, len(tasks))
	for _, link := range links {
		suppliersByTask[link.TaskId] = append(suppliersByTask[link.TaskId], ChannelPushSupplierRef{Id: link.SupplierUserId, Username: usernames[link.SupplierUserId]})
	}
	for _, task := range tasks {
		site := siteByID[task.SiteId]
		suppliers := suppliersByTask[task.Id]
		if suppliers == nil {
			suppliers = []ChannelPushSupplierRef{}
		}
		views = append(views, ChannelPushTaskView{
			ChannelPushTask: task,
			SiteName:        site.Name,
			SiteDeleted:     site.DeletedAt.Valid,
			Suppliers:       suppliers,
		})
	}
	return views, total, nil
}

func GetChannelPushOptions() (*ChannelPushOptions, error) {
	options := &ChannelPushOptions{Sites: []ChannelPushSiteOption{}, Suppliers: []ChannelPushSupplierOption{}}
	if err := DB.Model(&ThirdPartySite{}).Select("id", "name").Order("id asc").Find(&options.Sites).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&User{}).Select("id", "username", "display_name").Where("role = ?", common.RoleChannelSupplierUser).Order("id asc").Find(&options.Suppliers).Error; err != nil {
		return nil, err
	}
	return options, nil
}
