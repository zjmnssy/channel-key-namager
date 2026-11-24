package model

import (
	"errors"
	"maps"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrPayableUsageNotFound   = errors.New("Channel usage does not exist")
	ErrPayableProviderInvalid = errors.New("The selected user is not a channel supplier")
)

// PayableRemoteChannel 是站点上待确定提供商的一个渠道。
type PayableRemoteChannel struct {
	ID             int // 站点渠道 ID
	LocalChannelID int // 渠道名尾部标记里的本地渠道 ID；未解析出或标记里的站点 ID 不是本站点时为 0
}

// PayableProvider 是确定出的提供商及其此刻的应付折扣快照。
type PayableProvider struct {
	ID       int      // 0 = 未知
	Name     string   // 用户名快照
	Source   string   // PayableProvider* 常量
	Discount *float64 // 该用户此刻的 payable_discount
}

// PayableSuppliers 返回 userIDs 里仍是渠道供应商的用户（角色 5、未软删除，已禁用的也算），只取 id、username、payable_discount。
func PayableSuppliers(userIDs []int) (map[int]User, error) {
	return payableSuppliersIn(DB, userIDs)
}

func payableSuppliersIn(db *gorm.DB, userIDs []int) (map[int]User, error) {
	suppliers := map[int]User{}
	if len(userIDs) == 0 {
		return suppliers, nil
	}
	var users []User
	if err := db.Select("id", "username", "payable_discount").
		Where("id IN ? AND role = ?", userIDs, common.RoleChannelSupplierUser).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		suppliers[user.Id] = user
	}
	return suppliers, nil
}

// ResolvePayableProviders 按手动指定、渠道名标记、历史明细的顺序确定站点渠道的提供商，结果以站点渠道 ID 为键，
// 没有确定出的渠道为未知（ID 0）。每一步都批量查询。手动指定、标记、历史里的用户都必须仍是供应商。
func ResolvePayableProviders(siteID, billDate int, channels []PayableRemoteChannel) (map[int]PayableProvider, error) {
	remoteIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		remoteIDs = append(remoteIDs, channel.ID)
	}
	if len(remoteIDs) == 0 {
		return map[int]PayableProvider{}, nil
	}

	var overrides []PayableProviderOverride
	if err := DB.Select("remote_channel_id", "provider_id").
		Where("site_id = ? AND remote_channel_id IN ?", siteID, remoteIDs).Find(&overrides).Error; err != nil {
		return nil, err
	}
	manual := make(map[int]int, len(overrides))
	candidates := map[int]bool{}
	for _, override := range overrides {
		manual[override.RemoteChannelId] = override.ProviderId
		candidates[override.ProviderId] = true
	}

	localIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel.LocalChannelID > 0 {
			localIDs = append(localIDs, channel.LocalChannelID)
		}
	}
	creators := map[int]int{}
	if len(localIDs) > 0 {
		var local []Channel
		if err := DB.Select("id", "creator_user_id").Where("id IN ?", localIDs).Find(&local).Error; err != nil {
			return nil, err
		}
		for _, channel := range local {
			creators[channel.Id] = channel.CreatorUserId
			candidates[channel.CreatorUserId] = true
		}
	}

	candidateIDs := make([]int, 0, len(candidates))
	for id := range candidates {
		candidateIDs = append(candidateIDs, id)
	}
	suppliers, err := PayableSuppliers(candidateIDs)
	if err != nil {
		return nil, err
	}

	// 历史只查前两步没确定的渠道，且每个渠道只取 bill_date 更早、provider_id > 0、来源不是手动的最近一天的行
	// （同一天有多行时取 id 最大的）；该用户已不是供应商时结果为未知，不再往更早找。
	// 手动设置的行不沿用：清除手动设置后，下一次同步要按标记与历史重新确定，不能又从手动设置留下的行沿用回来。
	// 用派生表按渠道求出那一天再连回明细，不用逐行的关联子查询，数据多时 MySQL 也能走 (site_id, remote_channel_id, bill_date) 索引。
	unresolved := make([]int, 0, len(channels))
	for _, channel := range channels {
		if id, ok := manual[channel.ID]; ok && hasSupplier(suppliers, id) {
			continue
		}
		if id, ok := creators[channel.LocalChannelID]; ok && channel.LocalChannelID > 0 && hasSupplier(suppliers, id) {
			continue
		}
		unresolved = append(unresolved, channel.ID)
	}
	inherited := map[int]int{}
	if len(unresolved) > 0 {
		var history []PayableChannelUsage
		latestDays := DB.Model(&PayableChannelUsage{}).Select("remote_channel_id, MAX(bill_date) AS bill_date").
			Where("site_id = ? AND remote_channel_id IN ? AND bill_date < ? AND provider_id > 0 AND provider_source <> ?",
				siteID, unresolved, billDate, PayableProviderManual).
			Group("remote_channel_id")
		err := DB.Table("payable_channel_usages AS u").Select("u.id, u.remote_channel_id, u.provider_id").
			Joins("JOIN (?) AS latest ON latest.remote_channel_id = u.remote_channel_id AND latest.bill_date = u.bill_date", latestDays).
			Where("u.site_id = ? AND u.provider_id > 0 AND u.provider_source <> ?", siteID, PayableProviderManual).
			Order("u.id DESC").Scan(&history).Error
		if err != nil {
			return nil, err
		}
		historyIDs := []int{}
		for _, row := range history {
			if _, ok := inherited[row.RemoteChannelId]; !ok {
				inherited[row.RemoteChannelId] = row.ProviderId
				if !hasSupplier(suppliers, row.ProviderId) {
					historyIDs = append(historyIDs, row.ProviderId)
				}
			}
		}
		more, err := PayableSuppliers(historyIDs)
		if err != nil {
			return nil, err
		}
		maps.Copy(suppliers, more)
	}

	resolved := make(map[int]PayableProvider, len(channels))
	for _, channel := range channels {
		provider := PayableProvider{Source: PayableProviderUnknown}
		if id, ok := manual[channel.ID]; ok && hasSupplier(suppliers, id) {
			provider = payableProviderOf(suppliers[id], PayableProviderManual)
		} else if id, ok := creators[channel.LocalChannelID]; ok && channel.LocalChannelID > 0 && hasSupplier(suppliers, id) {
			provider = payableProviderOf(suppliers[id], PayableProviderParsed)
		} else if id, ok := inherited[channel.ID]; ok && hasSupplier(suppliers, id) {
			provider = payableProviderOf(suppliers[id], PayableProviderInherited)
		}
		resolved[channel.ID] = provider
	}
	return resolved, nil
}

func hasSupplier(suppliers map[int]User, id int) bool {
	_, ok := suppliers[id]
	return ok
}

func payableProviderOf(user User, source string) PayableProvider {
	return PayableProvider{ID: user.Id, Name: user.Username, Source: source, Discount: user.PayableDiscount}
}

// SetPayableUsageProvider 在一个事务里手动设置一行明细的提供商。providerID > 0 时用户必须仍是供应商：
// upsert 该站点渠道的指定记录，并只更新这一行的提供商、来源为 manual、折扣快照取该用户此刻的折扣（可为空）。
// providerID == 0 时删除该站点渠道的指定记录，行不变。其他日期的行都不改。
// 明细存在时 before 总是有值，便于调用方写审计。
func SetPayableUsageProvider(usageID, providerID int, now int64) (before PayableChannelUsage, after PayableChannelUsage, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", usageID).First(&before).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPayableUsageNotFound
			}
			return err
		}
		after = before
		if providerID == 0 {
			return tx.Where("site_id = ? AND remote_channel_id = ?", before.SiteId, before.RemoteChannelId).
				Delete(&PayableProviderOverride{}).Error
		}
		suppliers, err := payableSuppliersIn(tx, []int{providerID})
		if err != nil {
			return err
		}
		supplier, ok := suppliers[providerID]
		if !ok {
			return ErrPayableProviderInvalid
		}
		override := PayableProviderOverride{SiteId: before.SiteId, RemoteChannelId: before.RemoteChannelId,
			ProviderId: providerID, CreatedTime: now, UpdatedTime: now}
		err = tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "site_id"}, {Name: "remote_channel_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"provider_id", "updated_time"}),
		}).Create(&override).Error
		if err != nil {
			return err
		}
		err = tx.Model(&PayableChannelUsage{}).Where("id = ?", usageID).Updates(map[string]any{
			"provider_id": supplier.Id, "provider_name": supplier.Username, "provider_source": PayableProviderManual,
			"payable_discount": supplier.PayableDiscount, "updated_time": now,
		}).Error
		if err != nil {
			return err
		}
		return tx.First(&after, usageID).Error
	})
	return before, after, err
}
