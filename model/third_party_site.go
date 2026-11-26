package model

import (
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const ThirdPartySiteTypeNewAPI = "newapi"

var ErrThirdPartySiteNotFound = errors.New("Site does not exist")

type ThirdPartySite struct {
	Id                 int     `json:"id"`
	Name               string  `json:"name" gorm:"size:128;not null"`
	Type               string  `json:"type" gorm:"size:32;not null"`
	BaseURL            string  `json:"base_url" gorm:"column:base_url;size:1024;not null"`
	RemoteUserID       int     `json:"remote_user_id" gorm:"column:remote_user_id;not null"`
	ApiKey             string  `json:"-" gorm:"column:api_key;type:text;not null"`
	ReceivableDiscount float64 `json:"receivable_discount" gorm:"not null"`
	// BillStartDate is the Beijing date (YYYYMMDD) daily receivable bills start on.
	// Only a site saved before the column existed has 0, until
	// InitializeThirdPartySiteBillStartDates fills it in.
	BillStartDate int            `json:"bill_start_date" gorm:"not null;default:0"`
	CreatedTime   int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime   int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt     gorm.DeletedAt `json:"-" gorm:"index"`
}

type ThirdPartySiteInput struct {
	Name               string  `json:"name"`
	Type               string  `json:"type"`
	BaseURL            string  `json:"base_url"`
	RemoteUserID       int     `json:"remote_user_id"`
	ApiKey             string  `json:"api_key"`
	ReceivableDiscount float64 `json:"receivable_discount"`
	// BillStartDate is a Beijing date (YYYYMMDD); 0 means today when creating and the
	// stored date when editing.
	BillStartDate int `json:"bill_start_date"`
}

// InitializeThirdPartySiteBillStartDates gives each site saved before bill start dates
// existed the day the bill scan already starts it on, so upgrading reads no earlier
// bill: the date of its first bill, or else the later of the Beijing day it was
// created (today when that is unknown) and the latest day due. Sites that have a
// start date are left alone, so it can run on every start.
func InitializeThirdPartySiteBillStartDates(now time.Time) error {
	var sites []ThirdPartySite
	// Reading only the id and creation time keeps the ApiKey out of a debug SQL log.
	if err := DB.Unscoped().Select("id", "created_time").Where("bill_start_date = ?", 0).Find(&sites).Error; err != nil {
		return err
	}
	if len(sites) == 0 {
		return nil
	}
	daily, err := ReceivableRowCoverage[ReceivableBill]()
	if err != nil {
		return err
	}
	latestDue := ReceivableBillDate(ReceivableLatestBillDay(now))
	for _, site := range sites {
		startDate := ReceivableBillDate(now)
		if site.CreatedTime > 0 {
			startDate = ReceivableBillDate(time.Unix(site.CreatedTime, 0))
		}
		startDate = max(startDate, latestDue)
		if cover, found := daily[site.Id]; found {
			startDate = int(cover.FirstPeriod)
		}
		err := DB.Unscoped().Model(&ThirdPartySite{}).Where("id = ? AND bill_start_date = ?", site.Id, 0).
			Update("bill_start_date", startDate).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// checkThirdPartySiteBillStartDate rejects a bill start date being set that is later
// than today or earlier than the same day a year ago, both in Beijing time.
func checkThirdPartySiteBillStartDate(date int, now time.Time) error {
	beijingNow := now.In(ReceivableBillLocation)
	if date > ReceivableBillDate(beijingNow) {
		return &ThirdPartySiteFieldError{Field: "bill_start_date", Message: "Bill start date cannot be later than today"}
	}
	if date < ReceivableBillDate(beijingNow.AddDate(-1, 0, 0)) {
		return &ThirdPartySiteFieldError{Field: "bill_start_date", Message: "Bill start date cannot be earlier than one year ago"}
	}
	return nil
}

// ThirdPartySiteFieldError reports the request field that failed validation.
// Field is the request field name; Message is the English message key.
type ThirdPartySiteFieldError struct{ Field, Message string }

func (e *ThirdPartySiteFieldError) Error() string { return e.Message }

const (
	thirdPartySiteBaseURLFormatMessage = "Base URL must be an http or https address without credentials, query or fragment"
	thirdPartySiteDiscountMessage      = "Receivable discount must be greater than 0 and at most 1, with at most 4 decimal places"
)

// normalizeThirdPartySiteInput validates and normalizes a create or edit
// request. When editing, an ApiKey that is empty after trimming means "keep the
// stored one": the second result is true and the returned ApiKey is empty.
func normalizeThirdPartySiteInput(input ThirdPartySiteInput, editing bool) (ThirdPartySiteInput, bool, error) {
	name := strings.TrimSpace(input.Name)
	nameLen := utf8.RuneCountInString(name)
	if nameLen == 0 {
		return ThirdPartySiteInput{}, false, &ThirdPartySiteFieldError{Field: "name", Message: "Site name is required"}
	}
	if nameLen > 128 {
		return ThirdPartySiteInput{}, false, &ThirdPartySiteFieldError{Field: "name", Message: "Site name must not exceed 128 characters"}
	}
	if input.Type != ThirdPartySiteTypeNewAPI {
		return ThirdPartySiteInput{}, false, &ThirdPartySiteFieldError{Field: "type", Message: "Unsupported site type"}
	}
	baseURLErr := &ThirdPartySiteFieldError{Field: "base_url", Message: thirdPartySiteBaseURLFormatMessage}
	trimmedURL := strings.TrimSpace(input.BaseURL)
	// url.Parse drops an empty query or fragment, so look at the raw string.
	if strings.ContainsAny(trimmedURL, "?#") {
		return ThirdPartySiteInput{}, false, baseURLErr
	}
	parsed, parseErr := url.Parse(trimmedURL)
	if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return ThirdPartySiteInput{}, false, baseURLErr
	}
	baseURL := strings.TrimRight(trimmedURL, "/")
	if utf8.RuneCountInString(baseURL) > 1024 {
		return ThirdPartySiteInput{}, false, &ThirdPartySiteFieldError{Field: "base_url", Message: "Base URL must not exceed 1024 characters"}
	}
	if input.RemoteUserID <= 0 {
		return ThirdPartySiteInput{}, false, &ThirdPartySiteFieldError{Field: "remote_user_id", Message: "Remote user ID must be a positive integer"}
	}
	keepApiKey := false
	apiKey := strings.TrimSpace(input.ApiKey)
	if apiKey == "" {
		if !editing {
			return ThirdPartySiteInput{}, false, &ThirdPartySiteFieldError{Field: "api_key", Message: "ApiKey is required"}
		}
		keepApiKey = true
	}
	if utf8.RuneCountInString(apiKey) > 4096 {
		return ThirdPartySiteInput{}, false, &ThirdPartySiteFieldError{Field: "api_key", Message: "ApiKey must not exceed 4096 characters"}
	}
	discount, ok := NormalizeDiscount(input.ReceivableDiscount)
	if !ok {
		return ThirdPartySiteInput{}, false, &ThirdPartySiteFieldError{Field: "receivable_discount", Message: thirdPartySiteDiscountMessage}
	}
	if input.BillStartDate != 0 {
		if _, ok := ReceivableBillDayStart(input.BillStartDate); !ok {
			return ThirdPartySiteInput{}, false, &ThirdPartySiteFieldError{Field: "bill_start_date", Message: "Bill start date must be a valid date"}
		}
	}
	return ThirdPartySiteInput{
		Name:               name,
		Type:               input.Type,
		BaseURL:            baseURL,
		RemoteUserID:       input.RemoteUserID,
		ApiKey:             apiKey,
		ReceivableDiscount: discount,
		BillStartDate:      input.BillStartDate,
	}, keepApiKey, nil
}

// ensureThirdPartySiteNameFree rejects a name already used, ignoring case, by
// a live site other than excludeID (0 when creating).
func ensureThirdPartySiteNameFree(tx *gorm.DB, name string, excludeID int) error {
	var names []string
	if err := tx.Model(&ThirdPartySite{}).Where("id <> ?", excludeID).Pluck("name", &names).Error; err != nil {
		return err
	}
	for _, existing := range names {
		if strings.EqualFold(existing, name) {
			return &ThirdPartySiteFieldError{Field: "name", Message: "A site with this name already exists"}
		}
	}
	return nil
}

func GetThirdPartySites(startIdx int, num int) ([]*ThirdPartySite, int64, error) {
	var sites []*ThirdPartySite
	var total int64
	if err := DB.Model(&ThirdPartySite{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := DB.Order("id desc").Limit(num).Offset(startIdx).Find(&sites).Error; err != nil {
		return nil, 0, err
	}
	return sites, total, nil
}

func CreateThirdPartySite(input ThirdPartySiteInput) (*ThirdPartySite, error) {
	normalized, _, err := normalizeThirdPartySiteInput(input, false)
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	billStartDate := ReceivableBillDate(time.Unix(now, 0))
	if normalized.BillStartDate != 0 {
		if err := checkThirdPartySiteBillStartDate(normalized.BillStartDate, time.Unix(now, 0)); err != nil {
			return nil, err
		}
		billStartDate = normalized.BillStartDate
	}
	site := &ThirdPartySite{
		Name:               normalized.Name,
		Type:               normalized.Type,
		BaseURL:            normalized.BaseURL,
		RemoteUserID:       normalized.RemoteUserID,
		ApiKey:             normalized.ApiKey,
		ReceivableDiscount: normalized.ReceivableDiscount,
		BillStartDate:      billStartDate,
		CreatedTime:        now,
		UpdatedTime:        now,
	}
	// Keep the ApiKey out of SQL logs even with DEBUG=true; failed SQL is dropped too.
	err = DB.Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := ensureThirdPartySiteNameFree(tx, site.Name, 0); err != nil {
			return err
		}
		return tx.Create(site).Error
	})
	if err != nil {
		return nil, err
	}
	return site, nil
}

func UpdateThirdPartySite(id int, input ThirdPartySiteInput) (*ThirdPartySite, error) {
	normalized, keepApiKey, err := normalizeThirdPartySiteInput(input, true)
	if err != nil {
		return nil, err
	}
	var site ThirdPartySite
	// Keep the ApiKey out of SQL logs even with DEBUG=true; failed SQL is dropped too.
	err = DB.Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&site, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrThirdPartySiteNotFound
			}
			return err
		}
		if err := ensureThirdPartySiteNameFree(tx, normalized.Name, id); err != nil {
			return err
		}
		// A date already stored passes again unchecked, so a site whose start date is more
		// than a year old can still be saved.
		if normalized.BillStartDate != 0 && normalized.BillStartDate != site.BillStartDate {
			if err := checkThirdPartySiteBillStartDate(normalized.BillStartDate, time.Now()); err != nil {
				return err
			}
			site.BillStartDate = normalized.BillStartDate
		}
		site.Name = normalized.Name
		site.Type = normalized.Type
		oldBaseURL := site.BaseURL
		site.BaseURL = normalized.BaseURL
		site.RemoteUserID = normalized.RemoteUserID
		site.ReceivableDiscount = normalized.ReceivableDiscount
		site.UpdatedTime = common.GetTimestamp()
		columns := []string{"name", "type", "base_url", "remote_user_id", "receivable_discount", "bill_start_date", "updated_time"}
		if !keepApiKey {
			site.ApiKey = normalized.ApiKey
			columns = append(columns, "api_key")
		}
		if err := tx.Model(&site).Select(columns).Updates(&site).Error; err != nil {
			return err
		}
		if err := retryFailedReceivableRowsOfSite(tx, id, site.UpdatedTime); err != nil {
			return err
		}
		return reopenPayablePullsOnSiteEdit(tx, &site, oldBaseURL != site.BaseURL, site.UpdatedTime)
	})
	if err != nil {
		return nil, err
	}
	return &site, nil
}

// DeleteThirdPartySite soft-deletes a site and wipes its ApiKey in the same update,
// then fails the receivable bill rows of deleted sites that still wait for a retry.
func DeleteThirdPartySite(id int) error {
	// Keep the ApiKey out of SQL logs even with DEBUG=true; failed SQL is dropped too.
	return DB.Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
		var site ThirdPartySite
		if err := tx.First(&site, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrThirdPartySiteNotFound
			}
			return err
		}
		if err := tx.Model(&site).Updates(map[string]any{"api_key": "", "deleted_at": time.Now()}).Error; err != nil {
			return err
		}
		if err := FailReceivableRowsOfDeletedSites(tx); err != nil {
			return err
		}
		return FailPayablePullsOfDeletedSites(tx, common.GetTimestamp())
	})
}
