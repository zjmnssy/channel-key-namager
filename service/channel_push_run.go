package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Setting keys that bind a channel to plugins of this platform; the site has
// no such plugin instances and rejects them.
var channelPushPluginSettingKeys = []string{"task_plugin_key", "task_extend_plugin_keys", "request_plugins"}

type channelPushSiteSnapshot struct {
	FetchedAt int64                    `json:"fetched_at"`
	Channels  []ChannelPushSiteChannel `json:"channels"`
}

// ExecuteChannelPushRun performs one claimed run against its site and always
// finishes the record, whatever happens on the way.
func ExecuteChannelPushRun(ctx context.Context, run *model.ChannelPushRun, options ChannelPushClientOptions) {
	defer func() {
		if recovered := recover(); recovered != nil {
			channelPushAbortRun(run, common.NewMessage("Internal error: {{detail}}", map[string]any{"detail": fmt.Sprint(recovered)}))
		}
		if err := model.FinishChannelPushRun(run, common.GetTimestamp()); err != nil {
			common.SysError(common.LogText("channel push run could not be finished: run=%d error=%v", run.Id, err))
		}
	}()

	if abort := performChannelPushRun(ctx, run, options); abort != nil {
		channelPushAbortRun(run, abort)
	}
}

// channelPushAbortRun marks a run that stopped before its actions as failed.
func channelPushAbortRun(run *model.ChannelPushRun, reason *common.Message) {
	run.Status = model.ChannelPushRunFailed
	encoded, err := common.Marshal(reason)
	if err != nil {
		run.ErrorMessage = ""
		return
	}
	run.ErrorMessage = string(encoded)
}

// performChannelPushRun reads both sides, plans and executes the actions, and
// fills the run's details and counts. It returns the reason when the run
// stops before executing any action.
func performChannelPushRun(ctx context.Context, run *model.ChannelPushRun, options ChannelPushClientOptions) *common.Message {
	internalError := func(err error) *common.Message {
		return common.NewMessage("Internal error: {{detail}}", map[string]any{"detail": err.Error()})
	}

	site, err := model.GetChannelPushSite(run.SiteId)
	if errors.Is(err, model.ErrThirdPartySiteNotFound) {
		return common.NewMessage("Site does not exist")
	}
	if err != nil {
		return internalError(err)
	}

	client := NewChannelPushClient(site, options)
	siteChannels, err := client.ListChannels(ctx)
	if err != nil {
		return common.NewMessage("The site's channel list could not be read completely: {{detail}}", map[string]any{"detail": err.Error()})
	}
	siteSnapshot, err := common.Marshal(channelPushSiteSnapshot{FetchedAt: common.GetTimestamp(), Channels: siteChannels})
	if err != nil {
		return internalError(err)
	}

	var refs []model.ChannelPushSupplierRef
	if err := common.UnmarshalJsonStr(run.Suppliers, &refs); err != nil {
		return internalError(err)
	}
	supplierIDs := make([]int, 0, len(refs))
	for _, ref := range refs {
		supplierIDs = append(supplierIDs, ref.Id)
	}
	active, err := model.ActiveChannelSuppliers(supplierIDs)
	if err != nil {
		return internalError(err)
	}
	activeIDs := make([]int, 0, len(active))
	for _, id := range supplierIDs {
		if _, ok := active[id]; ok {
			activeIDs = append(activeIDs, id)
		}
	}
	channels, err := model.ChannelPushSupplierChannels(activeIDs)
	if err != nil {
		return internalError(err)
	}
	targets := make([]ChannelPushTargetChannel, 0, len(channels))
	inTargets := make(map[int]bool, len(channels))
	for _, channel := range channels {
		targets = append(targets, ChannelPushTargetChannel{
			ID:               channel.Id,
			Name:             channel.Name,
			Type:             channel.Type,
			SupplierUserID:   channel.CreatorUserId,
			SupplierUsername: active[channel.CreatorUserId],
			Status:           channel.Status,
			Models:           channel.Models,
		})
		inTargets[channel.Id] = true
	}
	targetSnapshot, err := common.Marshal(targets)
	if err != nil {
		return internalError(err)
	}

	otherTaskIDs, err := model.ChannelPushOtherTaskChannelIDs(run.SiteId, run.TaskId)
	if err != nil {
		return internalError(err)
	}
	markedIDs := []int{}
	for _, remote := range siteChannels {
		markerSiteID, localID, ok := ParseChannelPushMarker(remote.Name)
		if ok && markerSiteID == run.SiteId && !inTargets[localID] {
			markedIDs = append(markedIDs, localID)
		}
	}
	existingIDs, err := model.ExistingChannelIDs(markedIDs)
	if err != nil {
		return internalError(err)
	}

	actions := PlanChannelPushActions(run.SiteId, siteChannels, targets, ChannelPushOwnership{
		OtherTaskChannelIDs: otherTaskIDs,
		ExistingChannelIDs:  existingIDs,
	})
	for i := range actions {
		executeChannelPushAction(ctx, client, run, &actions[i])
	}
	actionsJSON, err := common.Marshal(actions)
	if err != nil {
		return internalError(err)
	}

	run.SiteChannelCount = len(siteChannels)
	run.TargetChannelCount = len(targets)
	run.UnchangedCount, run.EnableCount, run.DisableCount, run.UpdateModelsCount = 0, 0, 0, 0
	run.CreateCount, run.OrphanCount, run.AutoDisabledCount, run.UnsupportedCount = 0, 0, 0, 0
	run.FailedActionCount = 0
	for _, action := range actions {
		switch action.Category {
		case ChannelPushUnchanged:
			run.UnchangedCount++
		case ChannelPushEnable:
			run.EnableCount++
		case ChannelPushDisable:
			run.DisableCount++
		case ChannelPushUpdateModels:
			run.UpdateModelsCount++
		case ChannelPushCreate:
			run.CreateCount++
		case ChannelPushOrphan:
			run.OrphanCount++
		case ChannelPushAutoDisabled:
			run.AutoDisabledCount++
		case ChannelPushUnsupported:
			run.UnsupportedCount++
		}
		if action.Result == "failed" {
			run.FailedActionCount++
		}
	}
	run.SiteSnapshot = model.LongText(siteSnapshot)
	run.TargetSnapshot = model.LongText(targetSnapshot)
	run.Actions = model.LongText(actionsJSON)
	run.ErrorMessage = ""
	run.Status = model.ChannelPushRunSuccess
	if run.FailedActionCount > 0 {
		run.Status = model.ChannelPushRunPartial
	}
	return nil
}

// executeChannelPushAction calls the site for one pending action and records
// its result on the action.
func executeChannelPushAction(ctx context.Context, client *ChannelPushClient, run *model.ChannelPushRun, action *ChannelPushAction) {
	if action.Result != "pending" {
		return
	}
	var err error
	switch action.Category {
	case ChannelPushEnable, ChannelPushDisable:
		err = client.SetStatus(ctx, action.RemoteChannelID, action.StatusTo)
	case ChannelPushUpdateModels:
		err = client.UpdateModels(ctx, action.RemoteChannelID, action.Models)
	case ChannelPushCreate:
		var body map[string]any
		body, action.Error = buildChannelPushCreateBody(run, action)
		if action.Error != nil {
			action.Result = "failed"
			return
		}
		action.RemoteChannelID, err = client.CreateChannel(ctx, body)
		if err == nil {
			action.RemoteChannelName = body["channel"].(map[string]any)["name"].(string)
		}
	default:
		action.Result = "skipped"
		return
	}
	if err == nil {
		action.Result = "success"
		return
	}
	action.Result = "failed"
	var remoteErr *ChannelPushRemoteError
	if errors.As(err, &remoteErr) {
		action.Error = remoteErr.Message()
		return
	}
	action.Error = common.NewMessage("Internal error: {{detail}}", map[string]any{"detail": err.Error()})
}

// buildChannelPushCreateBody reads the local channel with its key and builds
// the site's create request. The returned message explains why it cannot be built.
func buildChannelPushCreateBody(run *model.ChannelPushRun, action *ChannelPushAction) (map[string]any, *common.Message) {
	local, err := model.GetChannelPushChannelForCreate(action.LocalChannelID)
	if err != nil {
		return nil, common.NewMessage("The local channel could not be read: {{detail}}", map[string]any{"detail": err.Error()})
	}
	if strings.TrimSpace(local.Key) == "" {
		return nil, common.NewMessage("The local channel has no key")
	}

	channel := map[string]any{
		"type":                local.Type,
		"key":                 local.Key,
		"openai_organization": local.OpenAIOrganization,
		"test_model":          local.TestModel,
		"base_url":            local.BaseURL,
		"other":               local.Other,
		"models":              action.Models,
		"model_mapping":       local.ModelMapping,
		"status_code_mapping": local.StatusCodeMapping,
		"priority":            local.Priority,
		"weight":              local.Weight,
		"auto_ban":            local.AutoBan,
		"param_override":      local.ParamOverride,
		"header_override":     local.HeaderOverride,
		"settings":            local.OtherSettings,
		"name":                BuildChannelPushRemoteName(local.Name, run.SiteId, local.Id),
		"group":               run.TargetGroup,
		"status":              1,
	}
	if local.Setting != nil && strings.TrimSpace(*local.Setting) != "" {
		// A setting that is not a JSON object is unusable here as well; leave it out.
		var setting map[string]any
		if common.UnmarshalJsonStr(*local.Setting, &setting) == nil {
			for _, key := range channelPushPluginSettingKeys {
				delete(setting, key)
			}
			if len(setting) > 0 {
				encoded, err := common.Marshal(setting)
				if err != nil {
					return nil, common.NewMessage("Internal error: {{detail}}", map[string]any{"detail": err.Error()})
				}
				channel["setting"] = string(encoded)
			}
		}
	}

	body := map[string]any{"mode": "single", "channel": channel}
	if local.ChannelInfo.IsMultiKey {
		body["mode"] = "multi_to_single"
		body["multi_key_mode"] = string(local.ChannelInfo.MultiKeyMode)
	}
	return body, nil
}
