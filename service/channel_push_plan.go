package service

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

const channelPushMaxNameRunes = 191

var channelPushMarkerRegexp = regexp.MustCompile(`-dcmark-([1-9][0-9]*)-([1-9][0-9]*)$`)

// BuildChannelPushRemoteName returns "{localName}-dcmark-{siteID}-{localChannelID}",
// truncating localName by characters so the whole name stays within 191 characters.
func BuildChannelPushRemoteName(localName string, siteID, localChannelID int) string {
	suffix := fmt.Sprintf("-dcmark-%d-%d", siteID, localChannelID)
	maxRunes := max(channelPushMaxNameRunes-len([]rune(suffix)), 0)
	runes := []rune(localName)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes) + suffix
}

// ParseChannelPushMarker reads the trailing "-dcmark-{siteID}-{localChannelID}" marker.
func ParseChannelPushMarker(remoteName string) (siteID, localChannelID int, ok bool) {
	match := channelPushMarkerRegexp.FindStringSubmatch(remoteName)
	if match == nil {
		return 0, 0, false
	}
	siteID, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, false
	}
	localChannelID, err = strconv.Atoi(match[2])
	if err != nil {
		return 0, 0, false
	}
	return siteID, localChannelID, true
}

type ChannelPushSiteChannel struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	UsedQuota int64  `json:"used_quota"`
	Status    int    `json:"status"`
	Models    string `json:"models"`
}

type ChannelPushTargetChannel struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Type             int    `json:"type"`
	SupplierUserID   int    `json:"supplier_user_id"`
	SupplierUsername string `json:"supplier_username"`
	Status           int    `json:"status"`
	Models           string `json:"models"`
}

type ChannelPushCategory string

const (
	ChannelPushUnchanged    ChannelPushCategory = "unchanged"
	ChannelPushEnable       ChannelPushCategory = "enable"
	ChannelPushDisable      ChannelPushCategory = "disable"
	ChannelPushUpdateModels ChannelPushCategory = "update_models"
	ChannelPushCreate       ChannelPushCategory = "create"
	ChannelPushOrphan       ChannelPushCategory = "orphan"
	ChannelPushAutoDisabled ChannelPushCategory = "auto_disabled"
	ChannelPushUnsupported  ChannelPushCategory = "unsupported"
)

// Result: "pending" 待执行；"success"、"failed" 执行后；"skipped" 不调用接口
type ChannelPushAction struct {
	Category          ChannelPushCategory `json:"category"`
	LocalChannelID    int                 `json:"local_channel_id"`
	LocalChannelName  string              `json:"local_channel_name"`
	RemoteChannelID   int                 `json:"remote_channel_id"`
	RemoteChannelName string              `json:"remote_channel_name"`
	StatusFrom        int                 `json:"status_from"`
	StatusTo          int                 `json:"status_to"`
	ModelsAdded       []string            `json:"models_added"`
	ModelsRemoved     []string            `json:"models_removed"`
	Models            string              `json:"models"`        // update_models、create 要发送的模型串
	RemoteStatus      int                 `json:"remote_status"` // orphan：站点渠道当时的状态
	RemoteModels      string              `json:"remote_models"` // orphan：站点渠道当时的模型
	Reason            *common.Message     `json:"reason,omitempty"`
	Duplicate         bool                `json:"duplicate"`
	Result            string              `json:"result"`
	Error             *common.Message     `json:"error,omitempty"`
}

type ChannelPushOwnership struct {
	OtherTaskChannelIDs map[int]bool // 本站点其他任务在管的本地渠道
	ExistingChannelIDs  map[int]bool // 本站点标记里出现的本地渠道中，仍然存在的那些
}

var channelPushCategoryOrder = map[ChannelPushCategory]int{
	ChannelPushUnchanged:    0,
	ChannelPushEnable:       1,
	ChannelPushDisable:      2,
	ChannelPushUpdateModels: 3,
	ChannelPushCreate:       4,
	ChannelPushOrphan:       5,
	ChannelPushAutoDisabled: 6,
	ChannelPushUnsupported:  7,
}

// channelPushModelSet splits a comma separated model string into a set:
// trimmed, empty items dropped, duplicates removed, case sensitive.
func channelPushModelSet(models string) map[string]bool {
	set := make(map[string]bool)
	for item := range strings.SplitSeq(models, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			set[item] = true
		}
	}
	return set
}

// channelPushModelDiff returns the sorted items in a that are not in b.
func channelPushModelDiff(a, b map[string]bool) []string {
	diff := []string{}
	for item := range a {
		if !b[item] {
			diff = append(diff, item)
		}
	}
	slices.Sort(diff)
	return diff
}

// PlanChannelPushActions compares the local channels (targets) with the site's
// channels and returns the actions in execution order: local channel ID, remote
// channel ID, then category order.
func PlanChannelPushActions(siteID int, siteChannels []ChannelPushSiteChannel, targets []ChannelPushTargetChannel, ownership ChannelPushOwnership) []ChannelPushAction {
	actions := []ChannelPushAction{}

	targetByID := make(map[int]ChannelPushTargetChannel, len(targets))
	for _, target := range targets {
		targetByID[target.ID] = target
	}

	remotesByLocalID := make(map[int][]ChannelPushSiteChannel)
	for _, remote := range siteChannels {
		markerSiteID, localID, ok := ParseChannelPushMarker(remote.Name)
		if !ok {
			continue
		}
		orphan := ChannelPushAction{
			Category:          ChannelPushOrphan,
			LocalChannelID:    localID,
			RemoteChannelID:   remote.ID,
			RemoteChannelName: remote.Name,
			RemoteStatus:      remote.Status,
			RemoteModels:      remote.Models,
			Result:            "skipped",
		}
		if markerSiteID != siteID {
			orphan.Reason = common.NewMessage("The site ID in the marker is not this site")
			actions = append(actions, orphan)
			continue
		}
		if _, inTargets := targetByID[localID]; inTargets {
			remotesByLocalID[localID] = append(remotesByLocalID[localID], remote)
			continue
		}
		if ownership.OtherTaskChannelIDs[localID] {
			continue
		}
		if ownership.ExistingChannelIDs[localID] {
			orphan.Reason = common.NewMessage("The channel's supplier is not in any task for this site, or is no longer a channel supplier")
		} else {
			orphan.Reason = common.NewMessage("The local channel has been deleted")
		}
		actions = append(actions, orphan)
	}

	for _, local := range targets {
		base := ChannelPushAction{
			LocalChannelID:   local.ID,
			LocalChannelName: local.Name,
		}

		if local.Type == constant.ChannelTypeCodex {
			base.Category = ChannelPushUnsupported
			base.Result = "skipped"
			base.Reason = common.NewMessage("Codex channels rotate their credentials automatically, so once pushed each side would invalidate the other's credentials")
			actions = append(actions, base)
			continue
		}
		if local.Type == constant.ChannelTypeTaskPlugin {
			base.Category = ChannelPushUnsupported
			base.Result = "skipped"
			base.Reason = common.NewMessage("Task plugin channels depend on plugins installed on this platform, which the site may not have")
			actions = append(actions, base)
			continue
		}

		localEnabled := local.Status == 1
		localModels := channelPushModelSet(local.Models)
		remotes := remotesByLocalID[local.ID]

		if len(remotes) == 0 {
			if localEnabled {
				base.Category = ChannelPushCreate
				base.Models = strings.TrimSpace(local.Models)
				base.Result = "pending"
			} else {
				base.Category = ChannelPushUnchanged
				base.Result = "skipped"
			}
			actions = append(actions, base)
			continue
		}

		duplicate := len(remotes) > 1
		for _, remote := range remotes {
			remoteEnabled := remote.Status == 1
			remoteAutoDisabled := remote.Status == 3
			pair := base
			pair.RemoteChannelID = remote.ID
			pair.RemoteChannelName = remote.Name
			pair.Duplicate = duplicate
			pair.Result = "pending"

			pairActions := []ChannelPushAction{}
			if localEnabled && !remoteEnabled && !remoteAutoDisabled {
				action := pair
				action.Category = ChannelPushEnable
				action.StatusFrom = remote.Status
				action.StatusTo = 1
				pairActions = append(pairActions, action)
			}
			if !localEnabled && remoteEnabled {
				action := pair
				action.Category = ChannelPushDisable
				action.StatusFrom = remote.Status
				action.StatusTo = 2
				pairActions = append(pairActions, action)
			}

			remoteModels := channelPushModelSet(remote.Models)
			added := channelPushModelDiff(localModels, remoteModels)
			removed := channelPushModelDiff(remoteModels, localModels)
			if len(added) > 0 || len(removed) > 0 {
				action := pair
				action.Category = ChannelPushUpdateModels
				action.ModelsAdded = added
				action.ModelsRemoved = removed
				action.Models = strings.TrimSpace(local.Models)
				pairActions = append(pairActions, action)
			}

			if localEnabled && remoteAutoDisabled {
				action := pair
				action.Category = ChannelPushAutoDisabled
				action.Result = "skipped"
				pairActions = append(pairActions, action)
			}

			if len(pairActions) == 0 {
				pair.Category = ChannelPushUnchanged
				pair.Result = "skipped"
				pairActions = append(pairActions, pair)
			}
			actions = append(actions, pairActions...)
		}
	}

	sort.SliceStable(actions, func(i, j int) bool {
		a, b := actions[i], actions[j]
		if a.LocalChannelID != b.LocalChannelID {
			return a.LocalChannelID < b.LocalChannelID
		}
		if a.RemoteChannelID != b.RemoteChannelID {
			return a.RemoteChannelID < b.RemoteChannelID
		}
		return channelPushCategoryOrder[a.Category] < channelPushCategoryOrder[b.Category]
	})
	return actions
}
