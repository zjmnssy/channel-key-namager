package model

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PayableRuleMaxApplyDays 是一次回改最多覆盖的账单天数（首尾都算），由调用方校验。
const PayableRuleMaxApplyDays = 92

var (
	ErrPayableRuleNotFound      = errors.New("Mapping rule does not exist")
	ErrPayableSiteRuleExists    = errors.New("This site already has a site-level mapping rule")
	ErrPayableChannelRuleExists = errors.New("This site channel already has a mapping rule")
	ErrPayableChannelNotPulled  = errors.New("This site channel has not been pulled yet")
)

// PayableRuleApplyHook 只供测试：stage 为 "before_commit"（回改已更新、事务未提交）或 "after_commit"（事务已提交、补扫之前）。
var PayableRuleApplyHook func(stage string)

// PayableRuleApply 是回改的账单日期范围（YYYYMMDD，含首尾），由调用方校验。
type PayableRuleApply struct {
	StartDate int `json:"start_date"`
	EndDate   int `json:"end_date"`
}

// PayableRuleInput 是新建规则的参数，也是新建接口的请求体。类型与站点渠道的搭配由调用方校验：指定渠道的站点渠道 ID 大于 0，站点级规则为 0。
type PayableRuleInput struct {
	SiteID          int               `json:"site_id"`
	RuleType        string            `json:"rule_type"`
	RemoteChannelID int               `json:"remote_channel_id"`
	ProviderID      int               `json:"provider_id"`
	Apply           *PayableRuleApply `json:"apply"` // nil = 不回改
}

// payableRuleSources 是各类规则回改后明细的提供商来源，与拉取时按规则确定的来源相同。
var payableRuleSources = map[string]string{
	PayableRuleChannel:    PayableProviderManual,
	PayableRuleAll:        PayableProviderRuleAll,
	PayableRuleUnresolved: PayableProviderRuleUnresolved,
}

// CreatePayableRule 新建一条规则；input.Apply 不为空时在同一事务里回改范围内的明细，applied 是改动的行数（含提交后的补扫）。
// 站点必须存在且未删除，提供商必须是渠道供应商，指定渠道必须在该站点拉取过。站点已有站点级规则、该渠道已有规则时
// 分别返回 ErrPayableSiteRuleExists、ErrPayableChannelRuleExists。
func CreatePayableRule(input PayableRuleInput, now int64) (rule PayableProviderOverride, applied int64, err error) {
	return savePayableRule(input.Apply, now, func(tx *gorm.DB) (saved PayableProviderOverride, supplier User, err error) {
		var sites int64
		if err = tx.Model(&ThirdPartySite{}).Where("id = ?", input.SiteID).Count(&sites).Error; err != nil {
			return saved, supplier, err
		}
		if sites == 0 {
			return saved, supplier, ErrThirdPartySiteNotFound
		}
		if supplier, err = payableRuleSupplier(tx, input.ProviderID); err != nil {
			return saved, supplier, err
		}
		conflict := ErrPayableSiteRuleExists
		if input.RuleType == PayableRuleChannel {
			conflict = ErrPayableChannelRuleExists
			var pulled int64
			err = tx.Model(&PayableChannelUsage{}).Where("site_id = ? AND remote_channel_id = ?", input.SiteID, input.RemoteChannelID).
				Count(&pulled).Error
			if err != nil {
				return saved, supplier, err
			}
			if pulled == 0 {
				return saved, supplier, ErrPayableChannelNotPulled
			}
		}
		// 先查已有的规则，冲突一般在这里返回，不靠插入时的唯一约束冲突（PostgreSQL、MySQL 冲突一次就消耗一个自增值）。
		// 两个人同时新建时双方可能都查不到，由唯一约束兜底：后插入的一方插入 0 行，得到同样的错误。
		var existing int64
		err = tx.Model(&PayableProviderOverride{}).Where("site_id = ? AND remote_channel_id = ?", input.SiteID, input.RemoteChannelID).
			Count(&existing).Error
		if err != nil {
			return saved, supplier, err
		}
		if existing > 0 {
			return saved, supplier, conflict
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&PayableProviderOverride{SiteId: input.SiteID,
			RemoteChannelId: input.RemoteChannelID, ProviderId: input.ProviderID, RuleType: input.RuleType, CreatedTime: now, UpdatedTime: now})
		if result.Error != nil {
			return saved, supplier, result.Error
		}
		if result.RowsAffected == 0 {
			return saved, supplier, conflict
		}
		// 按唯一键读回这一行，不依赖插入返回的自增 ID
		err = tx.Where("site_id = ? AND remote_channel_id = ?", input.SiteID, input.RemoteChannelID).First(&saved).Error
		return saved, supplier, err
	})
}

// UpdatePayableRule 把规则的提供商改为 providerID；apply 不为空时在同一事务里回改范围内的明细。提供商不变也可以保存，只做回改。
// 站点、类型、渠道不能改；站点已删除的规则也可以编辑（回改历史账单的归属）。规则存在时 before 总是有值，便于调用方写审计。
func UpdatePayableRule(id, providerID int, apply *PayableRuleApply, now int64) (before, after PayableProviderOverride, applied int64, err error) {
	after, applied, err = savePayableRule(apply, now, func(tx *gorm.DB) (saved PayableProviderOverride, supplier User, err error) {
		if err = lockForUpdate(tx).Where("id = ?", id).First(&before).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = ErrPayableRuleNotFound
			}
			return saved, supplier, err
		}
		if supplier, err = payableRuleSupplier(tx, providerID); err != nil {
			return saved, supplier, err
		}
		err = tx.Model(&PayableProviderOverride{}).Where("id = ?", id).
			Updates(map[string]any{"provider_id": providerID, "updated_time": now}).Error
		if err != nil {
			return saved, supplier, err
		}
		err = tx.Where("id = ?", id).First(&saved).Error
		return saved, supplier, err
	})
	return before, after, applied, err
}

// DeletePayableRule 删除一条规则并返回被删的行。已拉取的明细不变，下一次拉取起不再使用；站点已删除的规则也可以删。
func DeletePayableRule(id int) (PayableProviderOverride, error) {
	var rule PayableProviderOverride
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", id).First(&rule).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPayableRuleNotFound
			}
			return err
		}
		return tx.Delete(&PayableProviderOverride{}, rule.Id).Error
	})
	if err != nil {
		return PayableProviderOverride{}, err
	}
	return rule, nil
}

// CountPayableRuleApply 返回按规则回改 apply 范围时会修改的明细行数，用于保存前预览；rule.ProviderId 是目标提供商，
// 条件与回改相同。目标提供商不是渠道供应商时返回 ErrPayableProviderInvalid，与新建、编辑一致。
func CountPayableRuleApply(rule PayableProviderOverride, apply PayableRuleApply) (int64, error) {
	if _, err := payableRuleSupplier(DB, rule.ProviderId); err != nil {
		return 0, err
	}
	var count int64
	err := payableRuleUsages(DB, rule, apply).Count(&count).Error
	return count, err
}

// savePayableRule 是新建与编辑共用的保存流程：在一个事务里由 save 校验并写入规则，返回写入后的规则与目标供应商；
// apply 不为空时在同一事务里回改，规则与明细要么都改，要么都不改。回改期间该站点的拉取可能按旧规则写入，
// 所以提交后再对回改开始之后新插入的明细补扫一次。返回的行数含补扫；补扫出错只记日志，规则与回改已经提交。
func savePayableRule(apply *PayableRuleApply, now int64, save func(tx *gorm.DB) (PayableProviderOverride, User, error)) (PayableProviderOverride, int64, error) {
	sweepFrom := 0
	if apply != nil {
		if err := DB.Model(&PayableChannelUsage{}).Select("COALESCE(MAX(id), 0)").Scan(&sweepFrom).Error; err != nil {
			return PayableProviderOverride{}, 0, err
		}
	}
	var rule PayableProviderOverride
	var supplier User
	var applied int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if rule, supplier, err = save(tx); err != nil || apply == nil {
			return err
		}
		if applied, err = applyPayableRule(tx, rule, supplier, *apply, 0, now); err != nil {
			return err
		}
		if PayableRuleApplyHook != nil {
			PayableRuleApplyHook("before_commit")
		}
		return nil
	})
	if err != nil {
		return PayableProviderOverride{}, 0, err
	}
	if apply == nil {
		return rule, 0, nil
	}
	if PayableRuleApplyHook != nil {
		PayableRuleApplyHook("after_commit")
	}
	swept, err := applyPayableRule(DB, rule, supplier, *apply, sweepFrom, now)
	if err != nil {
		common.SysError(common.LogText("payable rule sweep failed: %v", err))
	}
	return rule, applied + swept, nil
}

// payableRuleSupplier 返回规则的目标提供商：此刻必须是渠道供应商（已禁用的也算），否则为 ErrPayableProviderInvalid。
func payableRuleSupplier(db *gorm.DB, providerID int) (User, error) {
	suppliers, err := payableSuppliersIn(db, []int{providerID})
	if err != nil {
		return User{}, err
	}
	supplier, ok := suppliers[providerID]
	if !ok {
		return User{}, ErrPayableProviderInvalid
	}
	return supplier, nil
}

// payableRuleSourceCondition 是规则回改一行明细的提供商与来源条件，预览、回改与分批更新共用。提供商已是目标的行一概不动
// （否则会改掉已对过账的折扣快照、抹掉名称解析的痕迹）；“全部渠道”不改手动设置的行，“不能解析”只改未知与它自己确定的行，
// 指定渠道不限来源。分批更新时重复这些条件，不覆盖期间别人刚改的行。
func payableRuleSourceCondition(tx *gorm.DB, rule PayableProviderOverride) *gorm.DB {
	query := tx.Model(&PayableChannelUsage{}).Where("provider_id <> ?", rule.ProviderId)
	switch rule.Type() {
	case PayableRuleAll:
		return query.Where("provider_source <> ?", PayableProviderManual)
	case PayableRuleUnresolved:
		return query.Where("provider_source IN ?", []string{PayableProviderUnknown, PayableProviderRuleUnresolved})
	}
	return query
}

// payableRuleUsages 是规则在 apply 范围内要回改的明细，预览与回改共用：在 payableRuleSourceCondition 之上限定站点与账单日期，
// 包括拉取未完成、不计入账单的行。指定渠道规则只看这个渠道；站点级规则跳过有有效指定渠道规则（提供商此刻是渠道供应商）的渠道，
// 与拉取时的优先级一致，指定渠道规则失效的渠道照改。
func payableRuleUsages(tx *gorm.DB, rule PayableProviderOverride, apply PayableRuleApply) *gorm.DB {
	query := payableRuleSourceCondition(tx, rule).
		Where("site_id = ? AND bill_date >= ? AND bill_date <= ?", rule.SiteId, apply.StartDate, apply.EndDate)
	if rule.Type() == PayableRuleChannel {
		return query.Where("remote_channel_id = ?", rule.RemoteChannelId)
	}
	suppliers := tx.Model(&User{}).Select("id").Where("role = ?", common.RoleChannelSupplierUser)
	channelRules := tx.Model(&PayableProviderOverride{}).Select("remote_channel_id").
		Where("site_id = ? AND remote_channel_id > 0 AND provider_id IN (?)", rule.SiteId, suppliers)
	return query.Where("remote_channel_id NOT IN (?)", channelRules)
}

// applyPayableRule 按规则回改 apply 范围内的明细，返回改动的行数；minID > 0 时只看 id 更大的行（提交后的补扫）。
// 先按 id 升序取出全部 id，再按主键每批 1000 行更新：不写成一条范围 UPDATE，MySQL 上它未提交时的间隙锁会挡住其他站点拉取的插入。
// 批次条件只带 id 与 payableRuleSourceCondition，不带日期、站点和子查询，MySQL 走主键。折扣快照取目标供应商此刻的折扣（可为空）。
func applyPayableRule(tx *gorm.DB, rule PayableProviderOverride, supplier User, apply PayableRuleApply, minID int, now int64) (int64, error) {
	query := payableRuleUsages(tx, rule, apply)
	if minID > 0 {
		query = query.Where("id > ?", minID)
	}
	var ids []int
	if err := query.Order("id").Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	values := map[string]any{
		"provider_id": supplier.Id, "provider_name": supplier.Username, "provider_source": payableRuleSources[rule.Type()],
		"payable_discount": supplier.PayableDiscount, "updated_time": now,
	}
	var applied int64
	for batch := range slices.Chunk(ids, 1000) {
		result := payableRuleSourceCondition(tx, rule).Where("id IN ?", batch).Updates(values)
		if result.Error != nil {
			return applied, result.Error
		}
		applied += result.RowsAffected
	}
	return applied, nil
}

// PayableRuleFilter 是规则列表的筛选条件，字段为空表示不筛选；Types 取 PayableRuleAll / PayableRuleUnresolved / PayableRuleChannel。
type PayableRuleFilter struct {
	SiteIDs, ProviderIDs []int
	Types                []string
}

// PayableRuleRow 是规则列表的一行，附带站点、提供商与渠道名称。
type PayableRuleRow struct {
	Id                int    `json:"id"`
	SiteId            int    `json:"site_id"`
	SiteName          string `json:"site_name"`
	SiteDeleted       bool   `json:"site_deleted"`
	RuleType          string `json:"rule_type"`
	RemoteChannelId   int    `json:"remote_channel_id"` // 0 = 站点级
	RemoteChannelName string `json:"remote_channel_name"`
	ProviderId        int    `json:"provider_id"`
	ProviderName      string `json:"provider_name"` // 已硬删除的用户显示 "#<ID>"
	ProviderValid     bool   `json:"provider_valid"`
	CreatedTime       int64  `json:"created_time"`
	UpdatedTime       int64  `json:"updated_time"`
}

// ListPayableRules 按 id 倒序返回满足 filter 的规则；total 不受分页影响，limit <= 0 时返回 offset 之后的全部。
func ListPayableRules(filter PayableRuleFilter, offset, limit int) ([]PayableRuleRow, int64, error) {
	query := DB.Model(&PayableProviderOverride{})
	if len(filter.SiteIDs) > 0 {
		query = query.Where("site_id IN ?", filter.SiteIDs)
	}
	if len(filter.ProviderIDs) > 0 {
		query = query.Where("provider_id IN ?", filter.ProviderIDs)
	}
	if len(filter.Types) > 0 {
		conditions := make([]string, 0, len(filter.Types))
		args := make([]any, 0, len(filter.Types))
		for _, ruleType := range filter.Types {
			switch ruleType {
			case PayableRuleChannel:
				conditions = append(conditions, "remote_channel_id > 0")
			case PayableRuleAll, PayableRuleUnresolved:
				conditions = append(conditions, "(remote_channel_id = 0 AND rule_type = ?)")
				args = append(args, ruleType)
			}
		}
		if len(conditions) == 0 {
			conditions = append(conditions, "1 = 0") // 全是未知类型：不匹配任何规则
		}
		query = query.Where("("+strings.Join(conditions, " OR ")+")", args...)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page := query.Order("id DESC")
	if limit > 0 {
		page = page.Limit(limit)
	}
	if offset > 0 {
		page = page.Offset(offset)
	}
	var rules []PayableProviderOverride
	if err := page.Find(&rules).Error; err != nil {
		return nil, 0, err
	}
	rows, err := payableRuleRows(rules)
	return rows, total, err
}

// GetPayableRuleRow 返回一条规则的列表行，规则不存在时返回 ErrPayableRuleNotFound。
func GetPayableRuleRow(id int) (PayableRuleRow, error) {
	var rule PayableProviderOverride
	if err := DB.Where("id = ?", id).First(&rule).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PayableRuleRow{}, ErrPayableRuleNotFound
		}
		return PayableRuleRow{}, err
	}
	rows, err := payableRuleRows([]PayableProviderOverride{rule})
	if err != nil {
		return PayableRuleRow{}, err
	}
	return rows[0], nil
}

// payableRuleRows 补全规则的站点、提供商与渠道名称，各用一次批量查询。站点、用户都包含已删除的：
// 软删除的仍显示名称，站点不存在或用户已被硬删除时显示 "#<ID>"。
func payableRuleRows(rules []PayableProviderOverride) ([]PayableRuleRow, error) {
	rows := make([]PayableRuleRow, 0, len(rules))
	if len(rules) == 0 {
		return rows, nil
	}
	var siteIDs, providerIDs, channelIDs []int
	for _, rule := range rules {
		siteIDs = append(siteIDs, rule.SiteId)
		providerIDs = append(providerIDs, rule.ProviderId)
		if rule.RemoteChannelId > 0 {
			channelIDs = append(channelIDs, rule.RemoteChannelId)
		}
	}
	var sites []ThirdPartySite
	if err := DB.Unscoped().Select("id", "name", "deleted_at").Where("id IN ?", siteIDs).Find(&sites).Error; err != nil {
		return nil, err
	}
	siteByID := make(map[int]ThirdPartySite, len(sites))
	for _, site := range sites {
		siteByID[site.Id] = site
	}
	var users []User
	if err := DB.Unscoped().Select("id", "username", "role", "deleted_at").Where("id IN ?", providerIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	userByID := make(map[int]User, len(users))
	for _, user := range users {
		userByID[user.Id] = user
	}
	channelNames, err := latestPayableChannelNames(siteIDs, channelIDs)
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		row := PayableRuleRow{Id: rule.Id, SiteId: rule.SiteId, SiteName: "#" + strconv.Itoa(rule.SiteId), SiteDeleted: true,
			RuleType: rule.Type(), RemoteChannelId: rule.RemoteChannelId, ProviderId: rule.ProviderId,
			ProviderName: "#" + strconv.Itoa(rule.ProviderId), CreatedTime: rule.CreatedTime, UpdatedTime: rule.UpdatedTime}
		if site, ok := siteByID[rule.SiteId]; ok {
			row.SiteName, row.SiteDeleted = site.Name, site.DeletedAt.Valid
		}
		if user, ok := userByID[rule.ProviderId]; ok {
			row.ProviderName = user.Username
			row.ProviderValid = user.Role == common.RoleChannelSupplierUser && !user.DeletedAt.Valid
		}
		row.RemoteChannelName = channelNames[[2]int{rule.SiteId, rule.RemoteChannelId}]
		rows = append(rows, row)
	}
	return rows, nil
}

// latestPayableChannelNames 返回各 (站点, 站点渠道) 在最近一天明细里的渠道名称（同一天有多行时取 id 最大的），没有明细的键不在结果里。
// 用派生表按 (站点, 渠道) 求出最近一天再连回明细，写法与 ResolvePayableProviders 的历史查询相同。
func latestPayableChannelNames(siteIDs, channelIDs []int) (map[[2]int]string, error) {
	names := map[[2]int]string{}
	if len(siteIDs) == 0 || len(channelIDs) == 0 {
		return names, nil
	}
	latestDays := DB.Model(&PayableChannelUsage{}).Select("site_id, remote_channel_id, MAX(bill_date) AS bill_date").
		Where("site_id IN ? AND remote_channel_id IN ?", siteIDs, channelIDs).Group("site_id, remote_channel_id")
	var usages []PayableChannelUsage
	err := DB.Table("payable_channel_usages AS u").Select("u.id, u.site_id, u.remote_channel_id, u.remote_channel_name").
		Joins("JOIN (?) AS latest ON latest.site_id = u.site_id AND latest.remote_channel_id = u.remote_channel_id AND latest.bill_date = u.bill_date", latestDays).
		Where("u.site_id IN ? AND u.remote_channel_id IN ?", siteIDs, channelIDs).Order("u.id DESC").Scan(&usages).Error
	if err != nil {
		return nil, err
	}
	for _, usage := range usages {
		key := [2]int{usage.SiteId, usage.RemoteChannelId}
		if _, ok := names[key]; !ok {
			names[key] = usage.RemoteChannelName
		}
	}
	return names, nil
}

// PayableChannelOption 是新建规则时可选的一个站点渠道。
type PayableChannelOption struct {
	RemoteChannelId   int    `json:"remote_channel_id"`
	RemoteChannelName string `json:"remote_channel_name"`
	HasRule           bool   `json:"has_rule"`
}

// payableChannelLikeEscaper 把关键字里的 LIKE 通配符转义，配合 ESCAPE '!'。
var payableChannelLikeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

// SearchPayableChannels 在站点最近拉取过的渠道里按名称（不区分大小写的子串）搜索，按站点渠道 ID 升序最多返回 limit 个（<= 0 不限）。
// “最近”指从该站点最近一次成功拉取的账单日起的全部进度（没有成功的拉取时从最近一次失败的起）；站点从未拉取过时返回空。
// 关键字是正整数时，该渠道在站点的全部历史明细里按 ID 精确命中，排在最前（已被站点删除的渠道仍可建规则）。
func SearchPayableChannels(siteID int, keyword string, limit int) ([]PayableChannelOption, error) {
	options := []PayableChannelOption{}
	keyword = strings.TrimSpace(keyword)
	var since int
	err := DB.Model(&PayablePullSite{}).Select("COALESCE(MAX(bill_date), 0)").
		Where("site_id = ? AND status = ?", siteID, PayablePullSuccess).Scan(&since).Error
	if err == nil && since == 0 {
		err = DB.Model(&PayablePullSite{}).Select("COALESCE(MAX(bill_date), 0)").Where("site_id = ?", siteID).Scan(&since).Error
	}
	if err != nil {
		return nil, err
	}
	if since == 0 {
		return options, nil
	}

	var channelIDs []int
	if exactID, parseErr := strconv.Atoi(keyword); parseErr == nil && exactID > 0 {
		var pulled int64
		if err := DB.Model(&PayableChannelUsage{}).Where("site_id = ? AND remote_channel_id = ?", siteID, exactID).Count(&pulled).Error; err != nil {
			return nil, err
		}
		if pulled > 0 {
			channelIDs = append(channelIDs, exactID)
		}
	}
	pulls := DB.Model(&PayablePullSite{}).Select("id").Where("site_id = ? AND bill_date >= ?", siteID, since)
	query := DB.Model(&PayableChannelUsage{}).Where("pull_site_id IN (?)", pulls)
	if keyword != "" {
		// 两边都转小写：MySQL 默认不区分大小写，PostgreSQL 的 LIKE 区分大小写
		pattern := "%" + strings.ToLower(payableChannelLikeEscaper.Replace(keyword)) + "%"
		query = query.Where("LOWER(remote_channel_name) LIKE ? ESCAPE '!'", pattern)
	}
	query = query.Group("remote_channel_id").Order("remote_channel_id")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var matched []int
	if err := query.Pluck("remote_channel_id", &matched).Error; err != nil {
		return nil, err
	}
	for _, id := range matched {
		if !slices.Contains(channelIDs, id) {
			channelIDs = append(channelIDs, id)
		}
	}
	if limit > 0 && len(channelIDs) > limit {
		channelIDs = channelIDs[:limit]
	}
	if len(channelIDs) == 0 {
		return options, nil
	}

	names, err := latestPayableChannelNames([]int{siteID}, channelIDs)
	if err != nil {
		return nil, err
	}
	var ruled []int
	err = DB.Model(&PayableProviderOverride{}).Where("site_id = ? AND remote_channel_id IN ?", siteID, channelIDs).
		Pluck("remote_channel_id", &ruled).Error
	if err != nil {
		return nil, err
	}
	for _, id := range channelIDs {
		options = append(options, PayableChannelOption{RemoteChannelId: id, RemoteChannelName: names[[2]int{siteID, id}],
			HasRule: slices.Contains(ruled, id)})
	}
	return options, nil
}
