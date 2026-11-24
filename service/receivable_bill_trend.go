package service

import (
	"errors"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/shopspring/decimal"
)

const receivableTrendMaxDays = 366

var (
	ErrReceivableTrendInvalidQuery = errors.New("Invalid parameters")
	ErrReceivableTrendRangeTooLong = errors.New("Date range cannot exceed 366 days")
)

// ReceivableTrendQuery selects a trend: the 24 hours of Date in the "day" view, or the
// days from StartDate to EndDate (YYYYMMDD) in the "month" view.
type ReceivableTrendQuery struct {
	View      string // "day" | "month"
	Date      int
	StartDate int
	EndDate   int
	SiteIDs   []int // when empty, every live site and the deleted sites with data in range
}

// ReceivableTrendSeries is one site's points; a nil amount has no successful data.
// Missing marks the nil amounts that count towards MissingCount: the page draws them
// at 0 and says the data could not be fetched. The other nil points (not due yet,
// before the site's first data, after the site was deleted) stay blank.
type ReceivableTrendSeries struct {
	SiteID      int       `json:"site_id"`
	SiteName    string    `json:"site_name"`
	Deleted     bool      `json:"deleted"`
	Amounts     []*string `json:"amounts"`
	Provisional []bool    `json:"provisional"` // the point sums hours because the day's bill has not succeeded
	Missing     []bool    `json:"missing"`
}

type ReceivableTrend struct {
	View         string                  `json:"view"`
	Labels       []string                `json:"labels"`
	Series       []ReceivableTrendSeries `json:"series"`
	MissingCount int                     `json:"missing_count"`
}

// receivableTrendValues holds the amounts and provisional flags of the points, by site.
type receivableTrendValues struct {
	points      int
	amounts     map[int][]*decimal.Decimal
	provisional map[int][]bool
}

func (values *receivableTrendValues) set(siteID, point int, amount decimal.Decimal, provisional bool) {
	if values.amounts[siteID] == nil {
		values.amounts[siteID] = make([]*decimal.Decimal, values.points)
		values.provisional[siteID] = make([]bool, values.points)
	}
	values.amounts[siteID][point] = &amount
	values.provisional[siteID][point] = provisional
}

// BuildReceivableBillTrend builds the trend at now. A point counts as missing when
// it is due, its site is live and has data from that point on, and it shows nothing;
// such a point is marked in its series' Missing as well as counted in MissingCount.
func BuildReceivableBillTrend(query ReceivableTrendQuery, now time.Time) (*ReceivableTrend, error) {
	isDay := query.View == "day"
	var first time.Time
	var ok bool
	points := 24 // one per hour in the day view, one per day in the month view
	switch query.View {
	case "day":
		first, ok = model.ReceivableBillDayStart(query.Date)
	case "month":
		var last time.Time
		var lastOK bool
		first, ok = model.ReceivableBillDayStart(query.StartDate)
		last, lastOK = model.ReceivableBillDayStart(query.EndDate)
		ok = ok && lastOK && !last.Before(first)
		if ok {
			points = int(last.Sub(first)/receivableBillDay) + 1
		}
	}
	if !ok {
		return nil, ErrReceivableTrendInvalidQuery
	}
	if points > receivableTrendMaxDays {
		return nil, ErrReceivableTrendRangeTooLong
	}

	// starts holds each point's start; keys the value coverage is compared against (an
	// hour's Unix seconds, a day's YYYYMMDD); due whether the point has been read yet.
	trend := &ReceivableTrend{View: query.View, Labels: make([]string, points), Series: []ReceivableTrendSeries{}}
	starts := make([]time.Time, points)
	keys := make([]int64, points)
	due := make([]bool, points)
	lastHour, lastDay := receivableLatestBillHour(now), receivableLatestBillDay(now)
	for i := range points {
		if isDay {
			starts[i] = first.Add(time.Duration(i) * time.Hour)
			trend.Labels[i] = starts[i].Format("15:04")
			keys[i] = starts[i].Unix()
			due[i] = !starts[i].After(lastHour)
		} else {
			starts[i] = first.AddDate(0, 0, i)
			trend.Labels[i] = starts[i].Format("2006-01-02")
			keys[i] = int64(model.ReceivableBillDate(starts[i]))
			due[i] = !starts[i].After(lastDay)
		}
	}

	options, err := model.ListReceivableBillSiteOptions()
	if err != nil {
		return nil, err
	}
	siteOptions := make(map[int]model.ReceivableBillSiteOption, len(options))
	for _, option := range options {
		siteOptions[option.Id] = option
	}
	var requested []int // the existing sites asked for; none when the default set applies
	for _, id := range query.SiteIDs {
		if _, found := siteOptions[id]; found && !slices.Contains(requested, id) {
			requested = append(requested, id)
		}
	}
	if len(query.SiteIDs) > 0 && len(requested) == 0 {
		return trend, nil
	}

	values := &receivableTrendValues{points: points, amounts: map[int][]*decimal.Decimal{}, provisional: map[int][]bool{}}
	var coverage map[int]model.ReceivableCoverage
	if isDay {
		hours, err := model.ListSuccessfulReceivableBillHours(requested, first.Unix(), first.Add(receivableBillDay).Unix())
		if err != nil {
			return nil, err
		}
		for _, hour := range hours {
			values.set(hour.SiteId, int((hour.BillHour-first.Unix())/int64(time.Hour/time.Second)),
				model.ReceivableAmount(hour.Quota, hour.QuotaPerUnit), false)
		}
		coverage, err = model.ReceivableRowCoverage[model.ReceivableBillHour]()
		if err != nil {
			return nil, err
		}
	} else {
		if err := fillReceivableMonthValues(values, query, requested, siteOptions, starts); err != nil {
			return nil, err
		}
		coverage, err = model.ReceivableRowCoverage[model.ReceivableBill]()
		if err != nil {
			return nil, err
		}
	}

	var siteIDs []int
	if len(requested) > 0 {
		siteIDs = requested
	} else {
		for _, option := range options {
			if !option.Deleted || values.amounts[option.Id] != nil {
				siteIDs = append(siteIDs, option.Id)
			}
		}
	}
	slices.Sort(siteIDs)
	for _, siteID := range siteIDs {
		option := siteOptions[siteID]
		series := ReceivableTrendSeries{
			SiteID: siteID, SiteName: option.Name, Deleted: option.Deleted,
			Amounts: make([]*string, points), Provisional: make([]bool, points), Missing: make([]bool, points),
		}
		cover, covered := coverage[siteID]
		for i := range points {
			if amount := values.amounts[siteID]; amount != nil && amount[i] != nil {
				text := amount[i].StringFixed(6)
				series.Amounts[i] = &text
				series.Provisional[i] = values.provisional[siteID][i]
			} else if covered && !option.Deleted && due[i] && keys[i] >= cover.FirstPeriod {
				series.Missing[i] = true
				trend.MissingCount++
			}
		}
		trend.Series = append(trend.Series, series)
	}
	return trend, nil
}

// fillReceivableMonthValues sets the month view's values: a day's successful bill, or,
// where the bill has not succeeded, the sum of its successful hours. Hours are read
// site by site, only for the runs of days on which the site has no successful bill:
// the days not yet due, the days the scan has not inserted, and the days that
// failed or are retrying.
func fillReceivableMonthValues(values *receivableTrendValues, query ReceivableTrendQuery, requested []int,
	siteOptions map[int]model.ReceivableBillSiteOption, starts []time.Time) error {
	bills, _, err := model.ListReceivableBills(model.ReceivableBillFilter{
		StartDate: query.StartDate, EndDate: query.EndDate, SiteIDs: requested,
	}, 0, 0)
	if err != nil {
		return err
	}
	dayIndex := make(map[int]int, len(starts))
	for i, start := range starts {
		dayIndex[model.ReceivableBillDate(start)] = i
	}
	succeeded := make(map[int][]bool)
	for _, bill := range bills {
		if bill.Status != model.ReceivableBillStatusSuccess {
			continue
		}
		if succeeded[bill.SiteId] == nil {
			succeeded[bill.SiteId] = make([]bool, len(starts))
		}
		i := dayIndex[bill.BillDate]
		values.set(bill.SiteId, i, model.ReceivableAmount(bill.Quota, bill.QuotaPerUnit), false)
		succeeded[bill.SiteId][i] = true
	}

	// Without a choice of sites, the live ones can show hours, and so can a deleted one
	// whose hours start by the end of the range: a day it was deleted on never gets a bill.
	candidates := requested
	if len(requested) == 0 {
		hourly, err := model.ReceivableRowCoverage[model.ReceivableBillHour]()
		if err != nil {
			return err
		}
		rangeEnd := starts[len(starts)-1].AddDate(0, 0, 1).Unix()
		for siteID, option := range siteOptions {
			if cover, hasHours := hourly[siteID]; !option.Deleted || hasHours && cover.FirstPeriod < rangeEnd {
				candidates = append(candidates, siteID)
			}
		}
	}
	slices.Sort(candidates)

	for _, siteID := range candidates {
		for first := 0; first < len(starts); first++ {
			if succeeded[siteID] != nil && succeeded[siteID][first] {
				continue
			}
			last := first
			for last+1 < len(starts) && (succeeded[siteID] == nil || !succeeded[siteID][last+1]) {
				last++
			}
			hours, err := model.ListSuccessfulReceivableBillHours([]int{siteID}, starts[first].Unix(), starts[last].AddDate(0, 0, 1).Unix())
			if err != nil {
				return err
			}
			sums := make(map[int]decimal.Decimal) // day -> the day's hours so far
			for _, hour := range hours {
				i := dayIndex[model.ReceivableBillDate(time.Unix(hour.BillHour, 0))]
				sums[i] = sums[i].Add(model.ReceivableAmount(hour.Quota, hour.QuotaPerUnit))
			}
			for i, sum := range sums {
				values.set(siteID, i, sum, true)
			}
			first = last
		}
	}
	return nil
}
