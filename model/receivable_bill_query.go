package model

import (
	"slices"
)

// ReceivableBillFilter 是日账单列表和导出共用的筛选条件，日期为 YYYYMMDD，
// SiteIDs 和 Statuses 为空时不筛选。
type ReceivableBillFilter struct {
	StartDate, EndDate int
	SiteIDs            []int
	Statuses           []string
}

// ListReceivableBills 返回筛选后的日账单，按账单日期倒序、站点 id 升序，同时返回总行数。limit <= 0 时返回全部。
func ListReceivableBills(filter ReceivableBillFilter, offset, limit int) ([]*ReceivableBill, int64, error) {
	query := DB.Model(&ReceivableBill{}).Where("bill_date >= ? AND bill_date <= ?", filter.StartDate, filter.EndDate)
	if len(filter.SiteIDs) > 0 {
		query = query.Where("site_id IN ?", filter.SiteIDs)
	}
	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	query = query.Order("bill_date desc, site_id asc")
	if limit > 0 {
		query = query.Offset(offset).Limit(limit)
	}
	var bills []*ReceivableBill
	if err := query.Find(&bills).Error; err != nil {
		return nil, 0, err
	}
	return bills, total, nil
}

// ReceivableBillSiteOption 是账单筛选里的一个站点，Deleted 的站点仍可能有历史账单。
type ReceivableBillSiteOption struct {
	Id      int    `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// ListReceivableBillSiteOptions 返回包括已删除在内的所有站点，在线站点在前，再按 id 升序。
func ListReceivableBillSiteOptions() ([]ReceivableBillSiteOption, error) {
	var sites []ThirdPartySite
	if err := DB.Unscoped().Select("id", "name", "deleted_at").Order("id").Find(&sites).Error; err != nil {
		return nil, err
	}
	options := make([]ReceivableBillSiteOption, len(sites))
	for i, site := range sites {
		options[i] = ReceivableBillSiteOption{Id: site.Id, Name: site.Name, Deleted: site.DeletedAt.Valid}
	}
	slices.SortStableFunc(options, func(a, b ReceivableBillSiteOption) int {
		switch {
		case a.Deleted == b.Deleted:
			return 0
		case a.Deleted:
			return 1
		default:
			return -1
		}
	})
	return options, nil
}
