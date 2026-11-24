package model

import "github.com/QuantumNous/new-api/common"

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

	// 历史：每个渠道取 bill_date 最近一行 provider_id > 0 的明细，同一渠道按账单日期从新到旧，第一行即最近。
	var history []PayableChannelUsage
	if err := DB.Select("remote_channel_id", "provider_id", "bill_date").
		Where("site_id = ? AND remote_channel_id IN ? AND bill_date < ? AND provider_id > 0", siteID, remoteIDs, billDate).
		Order("bill_date DESC, id DESC").Find(&history).Error; err != nil {
		return nil, err
	}
	inherited := make(map[int]int, len(history))
	for _, row := range history {
		if _, ok := inherited[row.RemoteChannelId]; !ok {
			inherited[row.RemoteChannelId] = row.ProviderId
			candidates[row.ProviderId] = true
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
