package controller

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelPushRemoteName(t *testing.T) {
	assert.Equal(t, "OpenAI 主力-dcmark-3-42", service.BuildChannelPushRemoteName("OpenAI 主力", 3, 42))

	long := service.BuildChannelPushRemoteName(strings.Repeat("渠", 200), 3, 42)
	assert.Equal(t, 191, utf8.RuneCountInString(long))
	assert.Equal(t, strings.Repeat("渠", 179)+"-dcmark-3-42", long)

	parseCases := []struct {
		name      string
		remote    string
		siteID    int
		channelID int
		ok        bool
	}{
		{"local name contains dash and dcmark", "a-dcmark-b-dcmark-3-42", 3, 42, true},
		{"leading zero in site id", "x-dcmark-03-42", 0, 0, false},
		{"leading zero in channel id", "x-dcmark-3-042", 0, 0, false},
		{"copied channel", "x-dcmark-3-42_copy", 0, 0, false},
		{"plain name", "plain name", 0, 0, false},
		{"overflow", "x-dcmark-99999999999999999999-1", 0, 0, false},
		{"empty local name", "-dcmark-7-9", 7, 9, true},
	}
	for _, tc := range parseCases {
		t.Run(tc.name, func(t *testing.T) {
			siteID, channelID, ok := service.ParseChannelPushMarker(tc.remote)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.siteID, siteID)
			assert.Equal(t, tc.channelID, channelID)
		})
	}
}

func TestChannelPushPlan(t *testing.T) {
	const siteID = 3
	type want struct {
		Category  service.ChannelPushCategory
		LocalID   int
		RemoteID  int
		Duplicate bool
		Result    string
	}
	type remote struct {
		id     int
		name   string
		status int
		models string
	}
	type local struct {
		typ    int
		status int
		models string
	}
	const (
		unchanged = service.ChannelPushUnchanged
		enable    = service.ChannelPushEnable
		disable   = service.ChannelPushDisable
		update    = service.ChannelPushUpdateModels
		create    = service.ChannelPushCreate
		orphan    = service.ChannelPushOrphan
		autoDis   = service.ChannelPushAutoDisabled
		unsupp    = service.ChannelPushUnsupported
	)
	const (
		msgDeleted   = "The local channel has been deleted"
		msgUnmanaged = "The channel's supplier is not in any task for this site, or is no longer a channel supplier"
		msgOtherSite = "The site ID in the marker is not this site"
		msgCodex     = "Codex channels rotate their credentials automatically, so once pushed each side would invalidate the other's credentials"
	)
	mark := "n-dcmark-3-10"

	cases := []struct {
		name      string
		local     *local
		remotes   []remote
		other     map[int]bool
		existing  map[int]bool
		want      []want
		reasonKey string
	}{
		{name: "enabled without remote creates", local: &local{status: 1, models: " a,b "}, want: []want{{create, 10, 0, false, "pending"}}},
		{name: "disabled without remote unchanged", local: &local{status: 2}, want: []want{{unchanged, 10, 0, false, "skipped"}}},
		{name: "both enabled same models", local: &local{status: 1, models: "a,b"}, remotes: []remote{{1, mark, 1, "b,a"}}, want: []want{{unchanged, 10, 1, false, "skipped"}}},
		{name: "models equal as sets", local: &local{status: 1, models: " gpt-4o, gpt-4o,,claude "}, remotes: []remote{{1, mark, 1, "claude,gpt-4o"}}, want: []want{{unchanged, 10, 1, false, "skipped"}}},
		{name: "models case sensitive", local: &local{status: 1, models: "GPT-4o"}, remotes: []remote{{1, mark, 1, "gpt-4o"}}, want: []want{{update, 10, 1, false, "pending"}}},
		{name: "remote manually disabled", local: &local{status: 1}, remotes: []remote{{1, mark, 2, ""}}, want: []want{{enable, 10, 1, false, "pending"}}},
		{name: "remote odd status treated as manual", local: &local{status: 1}, remotes: []remote{{1, mark, 0, ""}}, want: []want{{enable, 10, 1, false, "pending"}}},
		{name: "remote manually disabled and models differ", local: &local{status: 1, models: "a"}, remotes: []remote{{1, mark, 2, "b"}}, want: []want{{enable, 10, 1, false, "pending"}, {update, 10, 1, false, "pending"}}},
		{name: "remote auto disabled", local: &local{status: 1}, remotes: []remote{{1, mark, 3, ""}}, want: []want{{autoDis, 10, 1, false, "skipped"}}},
		{name: "remote auto disabled and models differ", local: &local{status: 1, models: "a"}, remotes: []remote{{1, mark, 3, "b"}}, want: []want{{update, 10, 1, false, "pending"}, {autoDis, 10, 1, false, "skipped"}}},
		{name: "local disabled remote enabled", local: &local{status: 2}, remotes: []remote{{1, mark, 1, ""}}, want: []want{{disable, 10, 1, false, "pending"}}},
		{name: "local auto disabled remote enabled", local: &local{status: 3}, remotes: []remote{{1, mark, 1, ""}}, want: []want{{disable, 10, 1, false, "pending"}}},
		{name: "local disabled remote disabled models differ", local: &local{status: 2, models: "a"}, remotes: []remote{{1, mark, 3, "b"}}, want: []want{{update, 10, 1, false, "pending"}}},
		{name: "duplicate markers", local: &local{status: 1}, remotes: []remote{{5, mark, 2, ""}, {6, mark, 1, ""}}, want: []want{{enable, 10, 5, true, "pending"}, {unchanged, 10, 6, true, "skipped"}}},
		{name: "codex unsupported", local: &local{typ: constant.ChannelTypeCodex, status: 1}, want: []want{{unsupp, 10, 0, false, "skipped"}}, reasonKey: msgCodex},
		{name: "task plugin unsupported even with remote", local: &local{typ: constant.ChannelTypeTaskPlugin, status: 1}, remotes: []remote{{1, mark, 1, "x"}}, want: []want{{unsupp, 10, 0, false, "skipped"}}},
		{name: "unmarked remote ignored", remotes: []remote{{1, "hand made", 1, ""}}, want: []want{}},
		{name: "other task channel skipped", remotes: []remote{{1, "n-dcmark-3-11", 1, ""}}, other: map[int]bool{11: true}, want: []want{}},
		{name: "orphan deleted", remotes: []remote{{1, "n-dcmark-3-12", 1, "m"}}, existing: map[int]bool{}, want: []want{{orphan, 12, 1, false, "skipped"}}, reasonKey: msgDeleted},
		{name: "orphan unmanaged", remotes: []remote{{1, "n-dcmark-3-13", 1, "m"}}, existing: map[int]bool{13: true}, want: []want{{orphan, 13, 1, false, "skipped"}}, reasonKey: msgUnmanaged},
		{name: "orphan other site marker", local: &local{status: 1}, remotes: []remote{{1, "n-dcmark-4-10", 1, "m"}}, want: []want{{create, 10, 0, false, "pending"}, {orphan, 10, 1, false, "skipped"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var targets []service.ChannelPushTargetChannel
			if tc.local != nil {
				targets = append(targets, service.ChannelPushTargetChannel{ID: 10, Name: "n", Type: tc.local.typ, Status: tc.local.status, Models: tc.local.models})
			}
			var siteChannels []service.ChannelPushSiteChannel
			for _, r := range tc.remotes {
				siteChannels = append(siteChannels, service.ChannelPushSiteChannel{ID: r.id, Name: r.name, Status: r.status, Models: r.models})
			}
			actions := service.PlanChannelPushActions(siteID, siteChannels, targets, service.ChannelPushOwnership{
				OtherTaskChannelIDs: tc.other,
				ExistingChannelIDs:  tc.existing,
			})

			got := make([]want, 0, len(actions))
			for _, a := range actions {
				got = append(got, want{a.Category, a.LocalChannelID, a.RemoteChannelID, a.Duplicate, a.Result})
			}
			require.Equal(t, tc.want, got)

			if tc.reasonKey != "" {
				require.NotNil(t, actions[0].Reason)
				assert.Equal(t, tc.reasonKey, actions[0].Reason.Key)
			}
		})
	}

	t.Run("orphan other site marker reason", func(t *testing.T) {
		actions := service.PlanChannelPushActions(siteID,
			[]service.ChannelPushSiteChannel{{ID: 1, Name: "n-dcmark-4-10", Status: 1, Models: "m"}},
			[]service.ChannelPushTargetChannel{{ID: 10, Status: 1}},
			service.ChannelPushOwnership{})
		require.Len(t, actions, 2)
		assert.Equal(t, service.ChannelPushOrphan, actions[1].Category)
		require.NotNil(t, actions[1].Reason)
		assert.Equal(t, msgOtherSite, actions[1].Reason.Key)
	})

	t.Run("create and update details", func(t *testing.T) {
		actions := service.PlanChannelPushActions(siteID, nil,
			[]service.ChannelPushTargetChannel{{ID: 10, Status: 1, Models: " a,b "}}, service.ChannelPushOwnership{})
		require.Len(t, actions, 1)
		assert.Equal(t, "a,b", actions[0].Models)

		actions = service.PlanChannelPushActions(siteID,
			[]service.ChannelPushSiteChannel{{ID: 1, Name: mark, Status: 1, Models: "gpt-4o"}},
			[]service.ChannelPushTargetChannel{{ID: 10, Status: 1, Models: "GPT-4o"}}, service.ChannelPushOwnership{})
		require.Len(t, actions, 1)
		assert.Equal(t, []string{"GPT-4o"}, actions[0].ModelsAdded)
		assert.Equal(t, []string{"gpt-4o"}, actions[0].ModelsRemoved)

		actions = service.PlanChannelPushActions(siteID,
			[]service.ChannelPushSiteChannel{{ID: 1, Name: mark, Status: 2}},
			[]service.ChannelPushTargetChannel{{ID: 10, Status: 1}}, service.ChannelPushOwnership{})
		require.Len(t, actions, 1)
		assert.Equal(t, 2, actions[0].StatusFrom)
		assert.Equal(t, 1, actions[0].StatusTo)
	})

	t.Run("orphan carries remote values", func(t *testing.T) {
		actions := service.PlanChannelPushActions(siteID,
			[]service.ChannelPushSiteChannel{{ID: 1, Name: "n-dcmark-3-12", Status: 2, Models: "m1"}},
			nil, service.ChannelPushOwnership{})
		require.Len(t, actions, 1)
		assert.Equal(t, 2, actions[0].RemoteStatus)
		assert.Equal(t, "m1", actions[0].RemoteModels)
	})
}
