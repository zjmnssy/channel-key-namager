package model

import (
	"errors"
	"slices"

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

// PayableRuleApply 是回改的账单日期范围。
type PayableRuleApply struct{ StartDate, EndDate int } // YYYYMMDD，含首尾；调用方已校验

// PayableRuleInput 是新建规则的参数。类型与站点渠道的搭配由调用方校验：指定渠道的站点渠道 ID 大于 0，站点级规则为 0。
type PayableRuleInput struct {
	SiteID, RemoteChannelID, ProviderID int
	RuleType                            string
	Apply                               *PayableRuleApply // nil = 不回改
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
