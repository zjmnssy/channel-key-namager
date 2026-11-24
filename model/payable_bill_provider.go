package model

import (
	"maps"

	"github.com/QuantumNous/new-api/common"
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
	suppliers := map[int]User{}
	if len(userIDs) == 0 {
		return suppliers, nil
	}
	var users []User
	if err := DB.Select("id", "username", "payable_discount").
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

	// 历史只查前两步没确定的渠道，且每个渠道只取 bill_date 更早、provider_id > 0 的最近一天的行
	// （同一天有多行时取 id 最大的）；该用户已不是供应商时结果为未知，不再往更早找。
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
		latestEarlier := DB.Model(&PayableChannelUsage{}).Table("payable_channel_usages AS earlier").Select("MAX(earlier.bill_date)").
			Where("earlier.site_id = ? AND earlier.remote_channel_id = payable_channel_usages.remote_channel_id AND earlier.bill_date < ? AND earlier.provider_id > 0", siteID, billDate)
		if err := DB.Select("id", "remote_channel_id", "provider_id").
			Where("site_id = ? AND remote_channel_id IN ? AND provider_id > 0", siteID, unresolved).
			Where("bill_date = (?)", latestEarlier).
			Order("id DESC").Find(&history).Error; err != nil {
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
