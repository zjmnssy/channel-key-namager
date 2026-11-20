package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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

type fakeRemoteChannel struct {
	ID        int
	Name      string
	Status    int
	Models    string
	Group     string
	UsedQuota int64
	Type      int
	Key       string
}

type fakeRecordedRequest struct {
	Method        string
	Path          string
	Query         string
	Authorization string
	User          string
	ContentType   string
	Body          string
}

// fakeResponse is a queued reply for one (method, path). hang blocks until the client gives up.
type fakeResponse struct {
	status int
	body   string
	header map[string]string
	hang   bool
	pass   bool // consume this queue entry but let the fake answer normally
}

type fakeNewAPISite struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	channels map[int]*fakeRemoteChannel
	nextID   int
	requests []fakeRecordedRequest
	faults   map[string][]fakeResponse
	omitID   bool
}

func newFakeNewAPISite(t *testing.T) *fakeNewAPISite {
	site := &fakeNewAPISite{
		t:        t,
		channels: map[int]*fakeRemoteChannel{},
		nextID:   1,
		faults:   map[string][]fakeResponse{},
	}
	site.server = httptest.NewServer(http.HandlerFunc(site.handle))
	t.Cleanup(site.server.Close)
	return site
}

func (s *fakeNewAPISite) addChannel(channel fakeRemoteChannel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channels[channel.ID] = &channel
	s.nextID = max(s.nextID, channel.ID+1)
}

func (s *fakeNewAPISite) fail(method string, path string, responses ...fakeResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := method + " " + path
	s.faults[key] = append(s.faults[key], responses...)
}

func (s *fakeNewAPISite) recorded(method string, path string) []fakeRecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var matched []fakeRecordedRequest
	for _, request := range s.requests {
		if request.Method == method && request.Path == path {
			matched = append(matched, request)
		}
	}
	return matched
}

func (s *fakeNewAPISite) reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func (s *fakeNewAPISite) handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path

	s.mu.Lock()
	s.requests = append(s.requests, fakeRecordedRequest{
		Method:        r.Method,
		Path:          r.URL.Path,
		Query:         r.URL.RawQuery,
		Authorization: r.Header.Get("Authorization"),
		User:          r.Header.Get("New-Api-User"),
		ContentType:   r.Header.Get("Content-Type"),
		Body:          string(raw),
	})
	var fault *fakeResponse
	if queue := s.faults[key]; len(queue) > 0 {
		fault = &queue[0]
		s.faults[key] = queue[1:]
	}
	s.mu.Unlock()

	if fault != nil && !fault.pass {
		if fault.hang {
			<-r.Context().Done()
			return
		}
		for name, value := range fault.header {
			w.Header().Set(name, value)
		}
		status := fault.status
		if status == 0 {
			status = http.StatusOK
		}
		s.reply(w, status, fault.body)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.URL.Path == "/api/channel" && (r.Method == http.MethodPost || r.Method == http.MethodPut):
		w.Header().Set("Location", "/api/channel/")
		w.WriteHeader(http.StatusTemporaryRedirect)
	case key == "GET /api/channel/":
		s.listLocked(w, r)
	case key == "GET /api/group/":
		s.reply(w, http.StatusOK, `{"success":true,"data":["default","vip"]}`)
	case key == "POST /api/channel/":
		var request struct {
			Channel struct {
				Name   string `json:"name"`
				Models string `json:"models"`
				Group  string `json:"group"`
				Type   int    `json:"type"`
				Key    string `json:"key"`
			} `json:"channel"`
		}
		_ = common.Unmarshal(raw, &request)
		id := s.nextID
		s.nextID++
		s.channels[id] = &fakeRemoteChannel{ID: id, Name: request.Channel.Name, Status: 1, Models: request.Channel.Models,
			Group: request.Channel.Group, Type: request.Channel.Type, Key: request.Channel.Key}
		if s.omitID {
			s.reply(w, http.StatusOK, `{"success":true,"message":"","data":null}`)
			return
		}
		s.reply(w, http.StatusOK, `{"success":true,"message":"","data":{"id":`+strconv.Itoa(id)+`}}`)
	case key == "PUT /api/channel/":
		var request struct {
			ID     int    `json:"id"`
			Models string `json:"models"`
		}
		_ = common.Unmarshal(raw, &request)
		channel := s.channels[request.ID]
		if channel == nil {
			s.reply(w, http.StatusOK, `{"success":false,"message":"channel not found"}`)
			return
		}
		channel.Models = request.Models
		s.reply(w, http.StatusOK, `{"success":true,"message":""}`)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/channel/") && strings.HasSuffix(r.URL.Path, "/status"):
		id, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/channel/"), "/status"))
		var request struct {
			Status int `json:"status"`
		}
		_ = common.Unmarshal(raw, &request)
		channel := s.channels[id]
		if channel == nil {
			s.reply(w, http.StatusNotFound, `{"success":false,"message":"channel not found"}`)
			return
		}
		channel.Status = request.Status
		s.reply(w, http.StatusOK, `{"success":true,"message":""}`)
	default:
		s.reply(w, http.StatusNotFound, `{"success":false,"message":"no route"}`)
	}
}

func (s *fakeNewAPISite) listLocked(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("p"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	page = max(page, 1)
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}
	ids := make([]int, 0, len(s.channels))
	for id := range s.channels {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	start := min((page-1)*pageSize, len(ids))
	end := min(start+pageSize, len(ids))
	items := make([]map[string]any, 0, end-start)
	for _, id := range ids[start:end] {
		channel := s.channels[id]
		items = append(items, map[string]any{
			"id": channel.ID, "name": channel.Name, "status": channel.Status, "models": channel.Models,
			"group": channel.Group, "used_quota": channel.UsedQuota, "type": channel.Type,
		})
	}
	body, _ := common.Marshal(map[string]any{
		"success": true,
		"data":    map[string]any{"items": items, "total": len(ids), "page": page, "page_size": pageSize},
	})
	s.reply(w, http.StatusOK, string(body))
}

type fakeSleepRecorder struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (r *fakeSleepRecorder) sleep(ctx context.Context, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waits = append(r.waits, d)
	return nil
}

func (r *fakeSleepRecorder) recorded() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.waits...)
}

func channelPushTestSite(base *fakeNewAPISite, id int) *model.ThirdPartySite {
	return &model.ThirdPartySite{Id: id, BaseURL: base.server.URL, RemoteUserID: 7, ApiKey: "remote-access-token"}
}

func channelPushTestOptions(recorder *fakeSleepRecorder) service.ChannelPushClientOptions {
	return service.ChannelPushClientOptions{MaxRetries: 10, Pace: 0, Timeout: 200 * time.Millisecond, Sleep: recorder.sleep}
}

func TestChannelPushClientHeadersAndPaths(t *testing.T) {
	fake := newFakeNewAPISite(t)
	fake.addChannel(fakeRemoteChannel{ID: 5, Name: "x", Status: 1})
	client := service.NewChannelPushClient(channelPushTestSite(fake, 1001), channelPushTestOptions(&fakeSleepRecorder{}))
	ctx := context.Background()

	_, err := client.ListChannels(ctx)
	require.NoError(t, err)
	_, err = client.CreateChannel(ctx, map[string]any{"mode": "single"})
	require.NoError(t, err)
	require.NoError(t, client.UpdateModels(ctx, 5, "a,b"))
	require.NoError(t, client.SetStatus(ctx, 5, 2))

	fake.mu.Lock()
	requests := append([]fakeRecordedRequest(nil), fake.requests...)
	fake.mu.Unlock()
	require.Len(t, requests, 4)
	for _, request := range requests {
		assert.Equal(t, "Bearer remote-access-token", request.Authorization)
		assert.Equal(t, "7", request.User)
	}
	assert.Equal(t, "GET /api/channel/", requests[0].Method+" "+requests[0].Path)
	assert.Contains(t, requests[0].Query, "p=1")
	assert.Contains(t, requests[0].Query, "page_size=100")
	assert.Contains(t, requests[0].Query, "id_sort=true")
	assert.Empty(t, requests[0].ContentType)

	assert.Equal(t, "POST /api/channel/", requests[1].Method+" "+requests[1].Path)
	assert.Equal(t, "PUT /api/channel/", requests[2].Method+" "+requests[2].Path)
	assert.Equal(t, "POST /api/channel/5/status", requests[3].Method+" "+requests[3].Path)
	for _, request := range requests[1:] {
		assert.Equal(t, "application/json", request.ContentType)
	}
	assert.JSONEq(t, `{"mode":"single"}`, requests[1].Body)
	assert.Equal(t, `{"id":5,"models":"a,b"}`, requests[2].Body)
	assert.Equal(t, `{"status":2}`, requests[3].Body)
}

func TestChannelPushClientRetryClassification(t *testing.T) {
	const statusPath = "/api/channel/5/status"
	cases := []struct {
		name      string
		first     fakeResponse
		attempts  int
		wantErr   bool
		detail    string
		retryable bool
	}{
		{"401", fakeResponse{status: 401, body: `{"success":false,"message":"no auth"}`}, 1, true, "no auth", false},
		{"403", fakeResponse{status: 403, body: `{"success":false,"message":"forbidden"}`}, 1, true, "forbidden", false},
		{"404", fakeResponse{status: 404, body: `{"success":false,"message":"gone"}`}, 1, true, "gone", false},
		{"400", fakeResponse{status: 400, body: `{"success":false,"message":"bad request"}`}, 1, true, "bad request", false},
		{"302", fakeResponse{status: 302, header: map[string]string{"Location": "/elsewhere"}}, 1, true, "HTTP 302", false},
		{"success false", fakeResponse{body: `{"success":false,"message":"bad"}`}, 1, true, "bad", false},
		{"408", fakeResponse{status: 408}, 2, false, "", true},
		{"429", fakeResponse{status: 429}, 2, false, "", true},
		{"500", fakeResponse{status: 500}, 2, false, "", true},
		{"502", fakeResponse{status: 502}, 2, false, "", true},
		{"empty body", fakeResponse{status: 200}, 2, false, "", true},
		{"html", fakeResponse{status: 200, body: "<html>"}, 2, false, "", true},
		{"timeout", fakeResponse{hang: true}, 2, false, "", true},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeNewAPISite(t)
			fake.addChannel(fakeRemoteChannel{ID: 5, Status: 1})
			fake.fail("POST", statusPath, tc.first)
			client := service.NewChannelPushClient(channelPushTestSite(fake, 1100+index), channelPushTestOptions(&fakeSleepRecorder{}))

			err := client.SetStatus(context.Background(), 5, 2)

			assert.Len(t, fake.recorded("POST", statusPath), tc.attempts)
			assert.Empty(t, fake.recorded("GET", "/elsewhere"))
			if !tc.wantErr {
				assert.NoError(t, err)
				return
			}
			var remoteErr *service.ChannelPushRemoteError
			require.ErrorAs(t, err, &remoteErr)
			assert.Equal(t, tc.detail, remoteErr.Detail)
			assert.Equal(t, tc.retryable, remoteErr.Retryable)
			assert.Equal(t, 1, remoteErr.Attempts)
		})
	}
}

func TestChannelPushClientBackoff(t *testing.T) {
	const statusPath = "/api/channel/5/status"
	run := func(t *testing.T, siteID int, responses ...fakeResponse) (*fakeNewAPISite, []time.Duration, error) {
		fake := newFakeNewAPISite(t)
		fake.addChannel(fakeRemoteChannel{ID: 5, Status: 1})
		fake.fail("POST", statusPath, responses...)
		recorder := &fakeSleepRecorder{}
		client := service.NewChannelPushClient(channelPushTestSite(fake, siteID), channelPushTestOptions(recorder))
		err := client.SetStatus(context.Background(), 5, 2)
		return fake, recorder.recorded(), err
	}

	t.Run("exponential backoff capped at 60s", func(t *testing.T) {
		responses := make([]fakeResponse, 20)
		for i := range responses {
			responses[i] = fakeResponse{status: 500}
		}
		fake, waits, err := run(t, 1201, responses...)
		assert.Len(t, fake.recorded("POST", statusPath), 11)
		second := time.Second
		assert.Equal(t, []time.Duration{1 * second, 2 * second, 4 * second, 8 * second, 16 * second, 32 * second,
			60 * second, 60 * second, 60 * second, 60 * second}, waits)
		var remoteErr *service.ChannelPushRemoteError
		require.ErrorAs(t, err, &remoteErr)
		assert.Equal(t, 11, remoteErr.Attempts)
		assert.Equal(t, 500, remoteErr.StatusCode)
		assert.Equal(t, 11, remoteErr.Message().Params["attempts"])
		assert.Equal(t, "Site request failed after {{attempts}} attempt(s): {{detail}}", remoteErr.Message().Key)
	})

	retryAfterCases := []struct {
		name   string
		header map[string]string
		want   time.Duration
	}{
		{"Retry-After 7", map[string]string{"Retry-After": "7"}, 7 * time.Second},
		{"no Retry-After", nil, 30 * time.Second},
		{"Retry-After capped", map[string]string{"Retry-After": "999"}, 180 * time.Second},
		{"Retry-After not an integer", map[string]string{"Retry-After": "soon"}, 30 * time.Second},
	}
	for index, tc := range retryAfterCases {
		t.Run(tc.name, func(t *testing.T) {
			_, waits, err := run(t, 1210+index, fakeResponse{status: 429, header: tc.header})
			require.NoError(t, err)
			assert.Equal(t, []time.Duration{tc.want}, waits)
		})
	}
}

func TestChannelPushMaxRetriesFromEnv(t *testing.T) {
	cases := []struct {
		value string
		want  int
	}{
		{"", 10},
		{"0", 0},
		{"3", 3},
		{"-1", 10},
		{"abc", 10},
		{"101", 100},
	}
	for _, tc := range cases {
		t.Run("value "+tc.value, func(t *testing.T) {
			t.Setenv("CHANNEL_PUSH_REMOTE_MAX_RETRIES", tc.value)
			assert.Equal(t, tc.want, service.ChannelPushMaxRetriesFromEnv())
		})
	}

	t.Setenv("CHANNEL_PUSH_REMOTE_MAX_RETRIES", "0")
	fake := newFakeNewAPISite(t)
	fake.addChannel(fakeRemoteChannel{ID: 5, Status: 1})
	fake.fail("POST", "/api/channel/5/status", fakeResponse{status: 500}, fakeResponse{status: 500})
	options := channelPushTestOptions(&fakeSleepRecorder{})
	options.MaxRetries = service.ChannelPushMaxRetriesFromEnv()
	err := service.NewChannelPushClient(channelPushTestSite(fake, 1301), options).SetStatus(context.Background(), 5, 2)
	require.Error(t, err)
	assert.Len(t, fake.recorded("POST", "/api/channel/5/status"), 1)
}

func TestChannelPushClientListPages(t *testing.T) {
	fake := newFakeNewAPISite(t)
	for id := 250; id >= 1; id-- {
		fake.addChannel(fakeRemoteChannel{ID: id, Name: "c" + strconv.Itoa(id), Status: 1, Models: "m"})
	}
	client := service.NewChannelPushClient(channelPushTestSite(fake, 1401), channelPushTestOptions(&fakeSleepRecorder{}))

	items, err := client.ListChannels(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 250)
	for index, item := range items {
		require.Equal(t, index+1, item.ID)
	}
	assert.Len(t, fake.recorded("GET", "/api/channel/"), 3)
}

func TestChannelPushClientListRetriesFailedPage(t *testing.T) {
	fake := newFakeNewAPISite(t)
	for id := 1; id <= 250; id++ {
		fake.addChannel(fakeRemoteChannel{ID: id, Status: 1})
	}
	fake.fail("GET", "/api/channel/", fakeResponse{pass: true}, fakeResponse{status: 503})
	client := service.NewChannelPushClient(channelPushTestSite(fake, 1402), channelPushTestOptions(&fakeSleepRecorder{}))

	items, err := client.ListChannels(context.Background())

	require.NoError(t, err)
	assert.Len(t, items, 250)
	assert.Len(t, fake.recorded("GET", "/api/channel/"), 4)
}

func TestChannelPushClientListRejectsIncomplete(t *testing.T) {
	page := func(total int, items ...map[string]any) fakeResponse {
		body, _ := common.Marshal(map[string]any{"success": true, "data": map[string]any{"items": items, "total": total}})
		return fakeResponse{body: string(body)}
	}
	item := func(id int) map[string]any { return map[string]any{"id": id, "name": "c", "status": 1} }
	cases := []struct {
		name       string
		maxRetries int
		responses  []fakeResponse
	}{
		{"total larger than actual", 10, []fakeResponse{page(3, item(1)), page(3)}},
		{"duplicate id across pages", 10, []fakeResponse{page(2, item(1)), page(2, item(1))}},
		{"total changed", 10, []fakeResponse{page(2, item(1)), page(3, item(2))}},
		{"items missing", 10, []fakeResponse{{body: `{"success":true,"data":{"total":1}}`}}},
		{"id is zero", 10, []fakeResponse{page(1, item(0))}},
		{"success false", 10, []fakeResponse{{body: `{"success":false,"message":"denied"}`}}},
		{"unauthorized", 10, []fakeResponse{{status: 401, body: `{"success":false,"message":"no auth"}`}}},
		{"timeouts exhaust retries", 2, []fakeResponse{{hang: true}, {hang: true}, {hang: true}}},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeNewAPISite(t)
			fake.fail("GET", "/api/channel/", tc.responses...)
			options := channelPushTestOptions(&fakeSleepRecorder{})
			options.MaxRetries = tc.maxRetries
			client := service.NewChannelPushClient(channelPushTestSite(fake, 1500+index), options)

			items, err := client.ListChannels(context.Background())

			require.Error(t, err)
			assert.Nil(t, items)
		})
	}
}

func TestChannelPushClientListToleratesNullFields(t *testing.T) {
	fake := newFakeNewAPISite(t)
	fake.fail("GET", "/api/channel/", fakeResponse{body: `{"success":true,"data":{"items":[{"id":5,"name":"x","status":1,"models":null}],"total":1}}`})
	client := service.NewChannelPushClient(channelPushTestSite(fake, 1601), channelPushTestOptions(&fakeSleepRecorder{}))

	items, err := client.ListChannels(context.Background())

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, service.ChannelPushSiteChannel{ID: 5, Name: "x", Status: 1}, items[0])
}

func TestChannelPushClientCreateReturnsID(t *testing.T) {
	fake := newFakeNewAPISite(t)
	client := service.NewChannelPushClient(channelPushTestSite(fake, 1701), channelPushTestOptions(&fakeSleepRecorder{}))
	body := map[string]any{"mode": "single", "channel": map[string]any{"name": "n", "models": "a"}}

	id, err := client.CreateChannel(context.Background(), body)
	require.NoError(t, err)
	assert.Equal(t, 1, id)

	fake.mu.Lock()
	fake.omitID = true
	fake.mu.Unlock()
	id, err = client.CreateChannel(context.Background(), body)
	require.NoError(t, err)
	assert.Equal(t, 0, id)
}

func TestChannelPushClientPacesPerSite(t *testing.T) {
	fake := newFakeNewAPISite(t)
	fake.addChannel(fakeRemoteChannel{ID: 5, Status: 1})
	recorder := &fakeSleepRecorder{}
	options := channelPushTestOptions(recorder)
	options.Pace = time.Second
	ctx := context.Background()

	client := service.NewChannelPushClient(channelPushTestSite(fake, 1801), options)
	for range 3 {
		require.NoError(t, client.SetStatus(ctx, 5, 1))
	}
	waits := recorder.recorded()
	require.Len(t, waits, 2)
	for _, wait := range waits {
		assert.Greater(t, wait, time.Duration(0))
		assert.LessOrEqual(t, wait, time.Second)
	}

	otherRecorder := &fakeSleepRecorder{}
	options.Sleep = otherRecorder.sleep
	other := service.NewChannelPushClient(channelPushTestSite(fake, 1802), options)
	require.NoError(t, other.SetStatus(ctx, 5, 1))
	assert.Empty(t, otherRecorder.recorded())
}
