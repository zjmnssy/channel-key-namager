package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	// applyThenFail lets the fake handle the request normally, then answers 500.
	applyThenFail bool
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

	if fault != nil && fault.applyThenFail {
		s.mu.Lock()
		s.routeLocked(httptest.NewRecorder(), r, raw)
		s.mu.Unlock()
		s.reply(w, http.StatusInternalServerError, "")
		return
	}
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
	s.routeLocked(w, r, raw)
}

// routeLocked answers a request as a NewAPI site would. The caller holds s.mu.
func (s *fakeNewAPISite) routeLocked(w http.ResponseWriter, r *http.Request, raw []byte) {
	key := r.Method + " " + r.URL.Path
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
	slices.Sort(ids)
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
		{"long message kept to 500 characters", fakeResponse{status: 400, body: `{"success":false,"message":"` + strings.Repeat("长", 2000) + `"}`},
			1, true, strings.Repeat("长", 500), false},
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

func setupChannelPushTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupThirdPartySiteTest(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.ChannelPushTask{}, &model.ChannelPushTaskSupplier{},
		&model.ChannelPushRun{}, &model.ChannelPushRunSupplier{}))
	clearTables := func() {
		for _, table := range []any{&model.ChannelPushRunSupplier{}, &model.ChannelPushRun{}, &model.ChannelPushTaskSupplier{},
			&model.ChannelPushTask{}, &model.Channel{}, &model.User{}} {
			// A fresh session per table: GORM reuses the Statement of a stored session, so every Delete would hit the first table.
			require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(table).Error)
		}
	}
	clearTables()
	t.Cleanup(clearTables)
	return db
}

func channelPushSeedSite(t *testing.T, db *gorm.DB, name string) *model.ThirdPartySite {
	t.Helper()
	site := &model.ThirdPartySite{Name: name, Type: model.ThirdPartySiteTypeNewAPI, BaseURL: "https://" + name + ".example.com",
		RemoteUserID: 7, ApiKey: "remote-access-token", ReceivableDiscount: 0.85}
	require.NoError(t, db.Create(site).Error)
	return site
}

func channelPushSeedSupplier(t *testing.T, db *gorm.DB, username string) *model.User {
	t.Helper()
	user := &model.User{Username: username, Role: common.RoleChannelSupplierUser, Status: 1, AffCode: username}
	require.NoError(t, db.Create(user).Error)
	return user
}

func channelPushSeedChannel(t *testing.T, db *gorm.DB, supplier *model.User, name string, status int, models string, channelType int) *model.Channel {
	t.Helper()
	channel := &model.Channel{Name: name, Status: status, Models: models, Type: channelType, Key: "sk-" + name,
		CreatorUserId: supplier.Id, CreatorRole: common.RoleChannelSupplierUser}
	require.NoError(t, db.Create(channel).Error)
	return channel
}

func channelPushTaskBody(t *testing.T, siteId int, supplierIds []int, overrides map[string]any) string {
	t.Helper()
	body := map[string]any{"site_id": siteId, "supplier_user_ids": supplierIds, "task_type": "periodic",
		"interval_minutes": 30, "target_group": "vip"}
	for key, value := range overrides {
		body[key] = value
	}
	encoded, err := common.Marshal(body)
	require.NoError(t, err)
	return string(encoded)
}

func channelPushCreateTask(t *testing.T, body string) thirdPartySiteResult[model.ChannelPushTask] {
	t.Helper()
	_, result := thirdPartySiteCall[model.ChannelPushTask](t, http.MethodPost, "/api/channel_push/tasks", body, nil, CreateChannelPushTask)
	return result
}

func channelPushIdParam(id int) gin.Params { return gin.Params{{Key: "id", Value: strconv.Itoa(id)}} }

func TestChannelPushTaskCreateValidation(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	otherSite := channelPushSeedSite(t, db, "beta")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	supplier2 := channelPushSeedSupplier(t, db, "sup-b")
	normalUser := &model.User{Username: "normal", Role: common.RoleCommonUser, Status: 1, AffCode: "normal"}
	require.NoError(t, db.Create(normalUser).Error)
	deletedSupplier := channelPushSeedSupplier(t, db, "sup-deleted")
	require.NoError(t, db.Delete(deletedSupplier).Error)
	deletedSite := channelPushSeedSite(t, db, "gone")
	require.NoError(t, db.Delete(deletedSite).Error)

	type validationCase struct {
		name      string
		siteId    int
		suppliers []int
		overrides map[string]any
		key       string
		field     string
	}
	ok := []int{supplier.Id}
	cases := []validationCase{
		{name: "missing site", siteId: 9999, suppliers: ok, key: "Site does not exist", field: "site_id"},
		{name: "deleted site", siteId: deletedSite.Id, suppliers: ok, key: "Site does not exist", field: "site_id"},
		{name: "no supplier", siteId: site.Id, suppliers: []int{}, key: "Select at least one supplier", field: "supplier_user_ids"},
		{name: "duplicate supplier", siteId: site.Id, suppliers: []int{supplier.Id, supplier.Id}, key: "Each supplier can be selected only once", field: "supplier_user_ids"},
		{name: "not a supplier", siteId: site.Id, suppliers: []int{normalUser.Id}, key: "User {{id}} is not a channel supplier", field: "supplier_user_ids"},
		{name: "deleted supplier", siteId: site.Id, suppliers: []int{deletedSupplier.Id}, key: "User {{id}} is not a channel supplier", field: "supplier_user_ids"},
		{name: "bad type", siteId: site.Id, suppliers: ok, overrides: map[string]any{"task_type": "daily"}, key: "Unsupported task type", field: "task_type"},
		{name: "interval 4", siteId: site.Id, suppliers: ok, overrides: map[string]any{"interval_minutes": 4}, key: "Interval must be an integer from 5 to 1440 minutes", field: "interval_minutes"},
		{name: "interval 1441", siteId: site.Id, suppliers: ok, overrides: map[string]any{"interval_minutes": 1441}, key: "Interval must be an integer from 5 to 1440 minutes", field: "interval_minutes"},
		{name: "blank group", siteId: site.Id, suppliers: ok, overrides: map[string]any{"target_group": "   "}, key: "Target group is required", field: "target_group"},
		{name: "long group", siteId: site.Id, suppliers: ok, overrides: map[string]any{"target_group": strings.Repeat("组", 65)}, key: "Target group must not exceed 64 characters", field: "target_group"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := channelPushCreateTask(t, channelPushTaskBody(t, tc.siteId, tc.suppliers, tc.overrides))
			assert.False(t, result.Success)
			assert.Equal(t, tc.key, result.MessageKey)
			assert.Equal(t, tc.field, result.Field)
		})
	}
	var count int64
	require.NoError(t, db.Model(&model.ChannelPushTask{}).Count(&count).Error)
	require.EqualValues(t, 0, count)

	_, malformed := thirdPartySiteCall[any](t, http.MethodPost, "/api/channel_push/tasks", "{", nil, CreateChannelPushTask)
	assert.False(t, malformed.Success)
	assert.Equal(t, "Invalid parameters", malformed.MessageKey)
	assert.Empty(t, malformed.Field)

	// The interval boundaries: 5 and 1440 pass.
	for _, interval := range []int{5, 1440} {
		sup := channelPushSeedSupplier(t, db, "sup-interval-"+strconv.Itoa(interval))
		result := channelPushCreateTask(t, channelPushTaskBody(t, site.Id, []int{sup.Id}, map[string]any{"interval_minutes": interval}))
		require.True(t, result.Success, result.Message)
		assert.Equal(t, interval, result.Data.IntervalMinutes)
	}

	// A one-off task drops the interval, and the group is trimmed.
	before := common.GetTimestamp()
	created := channelPushCreateTask(t, channelPushTaskBody(t, otherSite.Id, []int{supplier.Id, supplier2.Id},
		map[string]any{"task_type": "once", "interval_minutes": 99, "target_group": " vip "}))
	require.True(t, created.Success, created.Message)
	var stored model.ChannelPushTask
	require.NoError(t, db.First(&stored, created.Data.Id).Error)
	assert.Equal(t, "once", stored.TaskType)
	assert.Equal(t, 0, stored.IntervalMinutes)
	assert.Equal(t, "vip", stored.TargetGroup)
	assert.True(t, stored.Enabled)
	assert.InDelta(t, before, stored.NextRunAt, 2)
	assert.EqualValues(t, 0, stored.LastFinishedAt)

	// The same supplier cannot join a second task on the same site; another site is fine.
	conflict := channelPushCreateTask(t, channelPushTaskBody(t, otherSite.Id, []int{supplier2.Id}, nil))
	assert.False(t, conflict.Success)
	assert.Equal(t, "Supplier {{username}} already belongs to task {{task_id}} for this site", conflict.MessageKey)
	assert.Equal(t, "supplier_user_ids", conflict.Field)
	assert.Equal(t, "Supplier sup-b already belongs to task "+strconv.Itoa(created.Data.Id)+" for this site", conflict.Message)
	elsewhere := channelPushCreateTask(t, channelPushTaskBody(t, site.Id, []int{supplier2.Id}, nil))
	assert.True(t, elsewhere.Success, elsewhere.Message)
}

func TestChannelPushTaskUniqueIndexRace(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")

	require.NoError(t, db.Create(&model.ChannelPushTaskSupplier{TaskId: 999, SiteId: site.Id, SupplierUserId: supplier.Id}).Error)
	require.Error(t, db.Create(&model.ChannelPushTaskSupplier{TaskId: 1000, SiteId: site.Id, SupplierUserId: supplier.Id}).Error)

	// Task 999 does not exist, so the pre-check cannot see the row; the insert
	// hits the unique index and still reports the supplier conflict.
	result := channelPushCreateTask(t, channelPushTaskBody(t, site.Id, []int{supplier.Id}, nil))
	assert.False(t, result.Success)
	assert.Equal(t, "Supplier {{username}} already belongs to task {{task_id}} for this site", result.MessageKey)
	assert.Equal(t, "supplier_user_ids", result.Field)
	assert.Equal(t, "Supplier sup-a already belongs to task 999 for this site", result.Message)
	var tasks int64
	require.NoError(t, db.Model(&model.ChannelPushTask{}).Count(&tasks).Error)
	assert.EqualValues(t, 0, tasks)
}

func TestChannelPushTaskUpdate(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	otherSite := channelPushSeedSite(t, db, "beta")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	supplier2 := channelPushSeedSupplier(t, db, "sup-b")
	created := channelPushCreateTask(t, channelPushTaskBody(t, site.Id, []int{supplier.Id, supplier2.Id}, nil))
	require.True(t, created.Success, created.Message)
	id := created.Data.Id
	update := func(taskId int, body string) thirdPartySiteResult[model.ChannelPushTask] {
		_, result := thirdPartySiteCall[model.ChannelPushTask](t, http.MethodPut, "/api/channel_push/tasks/"+strconv.Itoa(taskId), body, channelPushIdParam(taskId), UpdateChannelPushTask)
		return result
	}

	// Keeping its own suppliers is not a conflict.
	require.NoError(t, db.Model(&model.ChannelPushTask{}).Where("id = ?", id).Update("next_run_at", 5).Error)
	before := common.GetTimestamp()
	result := update(id, channelPushTaskBody(t, site.Id, []int{supplier.Id, supplier2.Id}, map[string]any{"interval_minutes": 60}))
	require.True(t, result.Success, result.Message)
	var stored model.ChannelPushTask
	require.NoError(t, db.First(&stored, id).Error)
	assert.Equal(t, 60, stored.IntervalMinutes)
	assert.InDelta(t, before, stored.NextRunAt, 2)

	// Moving to another site moves the supplier rows with it.
	result = update(id, channelPushTaskBody(t, otherSite.Id, []int{supplier.Id}, nil))
	require.True(t, result.Success, result.Message)
	var links []model.ChannelPushTaskSupplier
	require.NoError(t, db.Where("task_id = ?", id).Find(&links).Error)
	require.Len(t, links, 1)
	assert.Equal(t, otherSite.Id, links[0].SiteId)
	assert.Equal(t, supplier.Id, links[0].SupplierUserId)

	missing := update(9999, channelPushTaskBody(t, site.Id, []int{supplier.Id}, nil))
	assert.False(t, missing.Success)
	assert.Equal(t, "Task does not exist", missing.MessageKey)

	// A disabled task stays disabled and unqueued.
	_, disabled := thirdPartySiteCall[model.ChannelPushTask](t, http.MethodPut, "/api/channel_push/tasks/"+strconv.Itoa(id)+"/enabled", `{"enabled":false}`, channelPushIdParam(id), SetChannelPushTaskEnabled)
	require.True(t, disabled.Success, disabled.Message)
	result = update(id, channelPushTaskBody(t, otherSite.Id, []int{supplier.Id}, map[string]any{"target_group": "svip"}))
	require.True(t, result.Success, result.Message)
	require.NoError(t, db.First(&stored, id).Error)
	assert.False(t, stored.Enabled)
	assert.EqualValues(t, 0, stored.NextRunAt)
	assert.Equal(t, "svip", stored.TargetGroup)
}

func TestChannelPushTaskToggle(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	created := channelPushCreateTask(t, channelPushTaskBody(t, site.Id, []int{supplier.Id}, nil))
	require.True(t, created.Success, created.Message)
	id := created.Data.Id
	toggle := func(taskId int, body string) thirdPartySiteResult[model.ChannelPushTask] {
		_, result := thirdPartySiteCall[model.ChannelPushTask](t, http.MethodPut, "/api/channel_push/tasks/"+strconv.Itoa(taskId)+"/enabled", body, channelPushIdParam(taskId), SetChannelPushTaskEnabled)
		return result
	}

	off := toggle(id, `{"enabled":false}`)
	require.True(t, off.Success, off.Message)
	var stored model.ChannelPushTask
	require.NoError(t, db.First(&stored, id).Error)
	assert.False(t, stored.Enabled)
	assert.EqualValues(t, 0, stored.NextRunAt)

	before := common.GetTimestamp()
	on := toggle(id, `{"enabled":true}`)
	require.True(t, on.Success, on.Message)
	require.NoError(t, db.First(&stored, id).Error)
	assert.True(t, stored.Enabled)
	assert.InDelta(t, before, stored.NextRunAt, 2)

	for _, body := range []string{"{", `{}`, `{"enabled":"yes"}`} {
		bad := toggle(id, body)
		assert.False(t, bad.Success)
		assert.Equal(t, "Invalid parameters", bad.MessageKey)
	}
	assert.Equal(t, "Task does not exist", toggle(9999, `{"enabled":true}`).MessageKey)

	require.NoError(t, db.Delete(site).Error)
	gone := toggle(id, `{"enabled":true}`)
	assert.False(t, gone.Success)
	assert.Equal(t, "Site does not exist", gone.MessageKey)
}

func TestChannelPushTaskListShowsSuppliersAndSite(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	otherSite := channelPushSeedSite(t, db, "beta")
	s1 := channelPushSeedSupplier(t, db, "sup-a")
	s2 := channelPushSeedSupplier(t, db, "sup-b")
	s3 := channelPushSeedSupplier(t, db, "sup-c")
	ids := make([]int, 0, 3)
	for _, body := range []string{
		channelPushTaskBody(t, site.Id, []int{s2.Id, s1.Id}, nil),
		channelPushTaskBody(t, otherSite.Id, []int{s1.Id}, nil),
		channelPushTaskBody(t, otherSite.Id, []int{s3.Id}, nil),
	} {
		created := channelPushCreateTask(t, body)
		require.True(t, created.Success, created.Message)
		ids = append(ids, created.Data.Id)
	}
	require.NoError(t, db.Delete(s2).Error)
	require.NoError(t, db.Delete(otherSite).Error)

	type taskPage struct {
		Total int                         `json:"total"`
		Items []model.ChannelPushTaskView `json:"items"`
	}
	raw, result := thirdPartySiteCall[taskPage](t, http.MethodGet, "/api/channel_push/tasks?p=1&page_size=10", "", nil, GetChannelPushTasks)
	require.True(t, result.Success, result.Message)
	assert.Equal(t, 3, result.Data.Total)
	require.Len(t, result.Data.Items, 3)
	assert.Equal(t, []int{ids[2], ids[1], ids[0]}, []int{result.Data.Items[0].Id, result.Data.Items[1].Id, result.Data.Items[2].Id})
	assert.NotContains(t, raw, "remote-access-token")

	first := result.Data.Items[2]
	assert.Equal(t, "alpha", first.SiteName)
	assert.False(t, first.SiteDeleted)
	assert.Equal(t, []model.ChannelPushSupplierRef{{Id: s1.Id, Username: "sup-a"}, {Id: s2.Id, Username: "sup-b"}}, first.Suppliers)
	assert.Equal(t, "beta", result.Data.Items[0].SiteName)
	assert.True(t, result.Data.Items[0].SiteDeleted)

	_, paged := thirdPartySiteCall[taskPage](t, http.MethodGet, "/api/channel_push/tasks?p=2&page_size=2", "", nil, GetChannelPushTasks)
	assert.Equal(t, 3, paged.Data.Total)
	require.Len(t, paged.Data.Items, 1)
	assert.Equal(t, ids[0], paged.Data.Items[0].Id)
}

func TestChannelPushOptions(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	gone := channelPushSeedSite(t, db, "gone")
	require.NoError(t, db.Delete(gone).Error)
	s1 := channelPushSeedSupplier(t, db, "sup-a")
	deleted := channelPushSeedSupplier(t, db, "sup-deleted")
	require.NoError(t, db.Delete(deleted).Error)
	require.NoError(t, db.Create(&model.User{Username: "normal", Role: common.RoleCommonUser, Status: 1, AffCode: "normal"}).Error)
	require.NoError(t, db.Create(&model.User{Username: "admin2", Role: common.RoleAdminUser, Status: 1, AffCode: "admin2"}).Error)

	raw, result := thirdPartySiteCall[model.ChannelPushOptions](t, http.MethodGet, "/api/channel_push/options", "", nil, GetChannelPushOptions)
	require.True(t, result.Success, result.Message)
	assert.Equal(t, []model.ChannelPushSiteOption{{Id: site.Id, Name: "alpha"}}, result.Data.Sites)
	assert.Equal(t, []model.ChannelPushSupplierOption{{Id: s1.Id, Username: "sup-a", DisplayName: ""}}, result.Data.Suppliers)
	assert.NotContains(t, raw, "remote-access-token")
}

// channelPushRunOnce points the task's site at the fake, claims a run for the
// task as stored now, executes it, and returns the run as stored afterwards.
func channelPushRunOnce(t *testing.T, db *gorm.DB, task model.ChannelPushTask, site *fakeNewAPISite) model.ChannelPushRun {
	t.Helper()
	require.NoError(t, db.Unscoped().Model(&model.ThirdPartySite{}).Where("id = ?", task.SiteId).Update("base_url", site.server.URL).Error)
	var current model.ChannelPushTask
	require.NoError(t, db.First(&current, task.Id).Error)
	run, ok, err := model.ClaimChannelPushRun(current, common.GetTimestamp())
	require.NoError(t, err)
	require.True(t, ok)
	service.ExecuteChannelPushRun(context.Background(), run, channelPushTestOptions(&fakeSleepRecorder{}))
	var stored model.ChannelPushRun
	require.NoError(t, db.First(&stored, run.Id).Error)
	return stored
}

// channelPushNewTask creates a periodic task through the API and returns it as stored.
func channelPushNewTask(t *testing.T, db *gorm.DB, siteId int, supplierIds ...int) model.ChannelPushTask {
	t.Helper()
	created := channelPushCreateTask(t, channelPushTaskBody(t, siteId, supplierIds, nil))
	require.True(t, created.Success, created.Message)
	var task model.ChannelPushTask
	require.NoError(t, db.First(&task, created.Data.Id).Error)
	return task
}

func channelPushRunActions(t *testing.T, run model.ChannelPushRun) []service.ChannelPushAction {
	t.Helper()
	var actions []service.ChannelPushAction
	require.NoError(t, common.UnmarshalJsonStr(string(run.Actions), &actions))
	return actions
}

func channelPushRunError(t *testing.T, run model.ChannelPushRun) common.Message {
	t.Helper()
	var message common.Message
	require.NoError(t, common.UnmarshalJsonStr(run.ErrorMessage, &message))
	return message
}

func (s *fakeNewAPISite) resetRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
}

func (s *fakeNewAPISite) setRemoteStatus(id int, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channels[id].Status = status
}

func (s *fakeNewAPISite) writeRequests() []fakeRecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var writes []fakeRecordedRequest
	for _, request := range s.requests {
		if request.Method != http.MethodGet {
			writes = append(writes, request)
		}
	}
	return writes
}

func (s *fakeNewAPISite) channelsNamed(name string) []fakeRemoteChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	var matched []fakeRemoteChannel
	for _, channel := range s.channels {
		if channel.Name == name {
			matched = append(matched, *channel)
		}
	}
	slices.SortFunc(matched, func(a, b fakeRemoteChannel) int { return a.ID - b.ID })
	return matched
}

func TestChannelPushRunFirstPush(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	a := channelPushSeedChannel(t, db, supplier, "A", 1, "gpt-4o", constant.ChannelTypeOpenAI)
	channelPushSeedChannel(t, db, supplier, "B", 2, "gpt-4o", constant.ChannelTypeOpenAI)
	channelPushSeedChannel(t, db, supplier, "C", 1, "gpt-5", constant.ChannelTypeCodex)
	d := channelPushSeedChannel(t, db, supplier, "D", 1, "gpt-4o", constant.ChannelTypeOpenAI)
	require.NoError(t, db.Model(d).Updates(map[string]any{"key": "k1\nk2",
		"channel_info": model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling}}).Error)
	e := channelPushSeedChannel(t, db, supplier, "E", 1, "claude", constant.ChannelTypeOpenAI)
	require.NoError(t, db.Model(e).Update("setting", `{"proxy":"http://p","request_plugins":["x"],"task_plugin_key":"y"}`).Error)
	task := channelPushNewTask(t, db, site.Id, supplier.Id)
	fake := newFakeNewAPISite(t)

	run := channelPushRunOnce(t, db, task, fake)
	assert.Equal(t, model.ChannelPushRunSuccess, run.Status)
	assert.Equal(t, 3, run.CreateCount)
	assert.Equal(t, 1, run.UnchangedCount)
	assert.Equal(t, 1, run.UnsupportedCount)
	assert.Equal(t, 0, run.SiteChannelCount)
	assert.Equal(t, 5, run.TargetChannelCount)
	assert.Equal(t, 0, run.FailedActionCount)
	assert.Equal(t, "", run.ErrorMessage)
	assert.NotZero(t, run.FinishedAt)

	creates := fake.recorded(http.MethodPost, "/api/channel/")
	require.Len(t, creates, 3)
	bodies := map[string]map[string]any{}
	for _, request := range creates {
		var body map[string]any
		require.NoError(t, common.UnmarshalJsonStr(request.Body, &body))
		channel := body["channel"].(map[string]any)
		bodies[channel["name"].(string)] = body
		assert.Equal(t, "vip", channel["group"])
		assert.EqualValues(t, 1, channel["status"])
		for _, field := range []string{"tag", "remark", "creator_user_id", "used_quota", "balance"} {
			assert.NotContains(t, channel, field)
		}
	}
	nameA := service.BuildChannelPushRemoteName("A", site.Id, a.Id)
	nameD := service.BuildChannelPushRemoteName("D", site.Id, d.Id)
	nameE := service.BuildChannelPushRemoteName("E", site.Id, e.Id)
	require.Contains(t, bodies, nameA)
	require.Contains(t, bodies, nameD)
	require.Contains(t, bodies, nameE)
	assert.Equal(t, "single", bodies[nameA]["mode"])
	assert.Equal(t, "multi_to_single", bodies[nameD]["mode"])
	assert.Equal(t, "polling", bodies[nameD]["multi_key_mode"])
	assert.Equal(t, "k1\nk2", bodies[nameD]["channel"].(map[string]any)["key"])
	assert.Equal(t, `{"proxy":"http://p"}`, bodies[nameE]["channel"].(map[string]any)["setting"])
	assert.NotContains(t, bodies[nameA]["channel"], "setting")

	remoteA := fake.channelsNamed(nameA)
	require.Len(t, remoteA, 1)
	for _, action := range channelPushRunActions(t, run) {
		if action.LocalChannelID == a.Id {
			assert.Equal(t, service.ChannelPushCreate, action.Category)
			assert.Equal(t, "success", action.Result)
			assert.Equal(t, remoteA[0].ID, action.RemoteChannelID)
		}
	}

	var suppliers []model.ChannelPushRunSupplier
	require.NoError(t, db.Where("run_id = ?", run.Id).Find(&suppliers).Error)
	require.Len(t, suppliers, 1)
	assert.Equal(t, supplier.Id, suppliers[0].SupplierUserId)
	var refs []model.ChannelPushSupplierRef
	require.NoError(t, common.UnmarshalJsonStr(run.Suppliers, &refs))
	assert.Equal(t, []model.ChannelPushSupplierRef{{Id: supplier.Id, Username: "sup-a"}}, refs)
	assert.Equal(t, "alpha", run.SiteName)
	assert.Equal(t, "vip", run.TargetGroup)

	var after model.ChannelPushTask
	require.NoError(t, db.First(&after, task.Id).Error)
	assert.Equal(t, run.FinishedAt, after.LastFinishedAt)
	assert.Equal(t, run.FinishedAt+30*60, after.NextRunAt)
}

func TestChannelPushRunSyncsStatusAndModels(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	toEnable := channelPushSeedChannel(t, db, supplier, "enable", 1, "m1", constant.ChannelTypeOpenAI)
	toDisable := channelPushSeedChannel(t, db, supplier, "disable", 1, "m1", constant.ChannelTypeOpenAI)
	toUpdate := channelPushSeedChannel(t, db, supplier, "update", 1, "m1", constant.ChannelTypeOpenAI)
	autoDisabled := channelPushSeedChannel(t, db, supplier, "auto", 1, "m1", constant.ChannelTypeOpenAI)
	toDelete := channelPushSeedChannel(t, db, supplier, "delete", 1, "m1", constant.ChannelTypeOpenAI)
	task := channelPushNewTask(t, db, site.Id, supplier.Id)
	fake := newFakeNewAPISite(t)
	fake.addChannel(fakeRemoteChannel{ID: 900, Name: "manual", Status: 1, Models: "m1"})

	first := channelPushRunOnce(t, db, task, fake)
	require.Equal(t, model.ChannelPushRunSuccess, first.Status)
	require.Equal(t, 5, first.CreateCount)
	remoteID := func(local *model.Channel) int {
		matched := fake.channelsNamed(service.BuildChannelPushRemoteName(local.Name, site.Id, local.Id))
		require.Len(t, matched, 1)
		return matched[0].ID
	}

	fake.setRemoteStatus(remoteID(toEnable), 2)
	fake.setRemoteStatus(remoteID(autoDisabled), 3)
	require.NoError(t, db.Model(toDisable).Update("status", 2).Error)
	require.NoError(t, db.Model(toUpdate).Update("models", " m1,m2 ").Error)
	deletedRemote := remoteID(toDelete)
	require.NoError(t, db.Delete(&model.Channel{}, toDelete.Id).Error)
	fake.resetRequests()

	second := channelPushRunOnce(t, db, task, fake)
	assert.Equal(t, model.ChannelPushRunSuccess, second.Status)
	assert.Equal(t, 6, second.SiteChannelCount)
	assert.Equal(t, 4, second.TargetChannelCount)
	assert.Equal(t, 1, second.EnableCount)
	assert.Equal(t, 1, second.DisableCount)
	assert.Equal(t, 1, second.UpdateModelsCount)
	assert.Equal(t, 1, second.AutoDisabledCount)
	assert.Equal(t, 1, second.OrphanCount)
	assert.Equal(t, 0, second.CreateCount)
	assert.Equal(t, 0, second.UnchangedCount)

	enable := fake.recorded(http.MethodPost, "/api/channel/"+strconv.Itoa(remoteID(toEnable))+"/status")
	require.Len(t, enable, 1)
	assert.JSONEq(t, `{"status":1}`, enable[0].Body)
	disable := fake.recorded(http.MethodPost, "/api/channel/"+strconv.Itoa(remoteID(toDisable))+"/status")
	require.Len(t, disable, 1)
	assert.JSONEq(t, `{"status":2}`, disable[0].Body)
	update := fake.recorded(http.MethodPut, "/api/channel/")
	require.Len(t, update, 1)
	assert.JSONEq(t, `{"id":`+strconv.Itoa(remoteID(toUpdate))+`,"models":"m1,m2"}`, update[0].Body)
	assert.Len(t, fake.writeRequests(), 3)
	for _, request := range fake.writeRequests() {
		for _, untouched := range []int{remoteID(autoDisabled), deletedRemote, 900} {
			assert.NotContains(t, request.Path, "/"+strconv.Itoa(untouched)+"/")
			assert.NotContains(t, request.Body, `"id":`+strconv.Itoa(untouched)+`,`)
		}
	}
	for _, action := range channelPushRunActions(t, second) {
		assert.NotEqual(t, 900, action.RemoteChannelID)
		if action.Category == service.ChannelPushOrphan {
			assert.Equal(t, toDelete.Id, action.LocalChannelID)
			assert.Equal(t, deletedRemote, action.RemoteChannelID)
			assert.Equal(t, "The local channel has been deleted", action.Reason.Key)
		}
	}
}

func TestChannelPushRunPartialFailure(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	first := channelPushSeedChannel(t, db, supplier, "first", 1, "m1", constant.ChannelTypeOpenAI)
	second := channelPushSeedChannel(t, db, supplier, "second", 1, "m1", constant.ChannelTypeOpenAI)
	task := channelPushNewTask(t, db, site.Id, supplier.Id)
	fake := newFakeNewAPISite(t)
	fake.fail(http.MethodPost, "/api/channel/", fakeResponse{status: http.StatusBadRequest, body: `{"success":false,"message":"bad channel"}`})

	run := channelPushRunOnce(t, db, task, fake)
	assert.Equal(t, model.ChannelPushRunPartial, run.Status)
	assert.Equal(t, 1, run.FailedActionCount)
	assert.Equal(t, 2, run.CreateCount)
	assert.Empty(t, fake.channelsNamed(service.BuildChannelPushRemoteName("first", site.Id, first.Id)))
	assert.Len(t, fake.channelsNamed(service.BuildChannelPushRemoteName("second", site.Id, second.Id)), 1)
	actions := channelPushRunActions(t, run)
	require.Len(t, actions, 2)
	assert.Equal(t, "failed", actions[0].Result)
	require.NotNil(t, actions[0].Error)
	assert.Equal(t, "bad channel", actions[0].Error.Params["detail"])
	assert.EqualValues(t, 1, actions[0].Error.Params["attempts"])
	assert.Equal(t, "success", actions[1].Result)
}

func TestChannelPushRunCreateRetryDuplicates(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	local := channelPushSeedChannel(t, db, supplier, "dup", 1, "m1", constant.ChannelTypeOpenAI)
	task := channelPushNewTask(t, db, site.Id, supplier.Id)
	fake := newFakeNewAPISite(t)
	fake.fail(http.MethodPost, "/api/channel/", fakeResponse{applyThenFail: true})

	run := channelPushRunOnce(t, db, task, fake)
	assert.Equal(t, model.ChannelPushRunSuccess, run.Status)
	actions := channelPushRunActions(t, run)
	require.Len(t, actions, 1)
	assert.Equal(t, "success", actions[0].Result)
	name := service.BuildChannelPushRemoteName("dup", site.Id, local.Id)
	require.Len(t, fake.channelsNamed(name), 2)

	fake.resetRequests()
	again := channelPushRunOnce(t, db, task, fake)
	assert.Empty(t, fake.writeRequests())
	againActions := channelPushRunActions(t, again)
	require.Len(t, againActions, 2)
	for _, action := range againActions {
		assert.True(t, action.Duplicate)
		assert.Equal(t, service.ChannelPushUnchanged, action.Category)
	}
}

func TestChannelPushRunFailsBeforeActions(t *testing.T) {
	const listKey = "The site's channel list could not be read completely: {{detail}}"
	cases := []struct {
		name    string
		prepare func(t *testing.T, db *gorm.DB, site *model.ThirdPartySite, fake *fakeNewAPISite)
		key     string
		detail  string
	}{
		{name: "site deleted", key: "Site does not exist", prepare: func(t *testing.T, db *gorm.DB, site *model.ThirdPartySite, _ *fakeNewAPISite) {
			require.NoError(t, db.Delete(site).Error)
		}},
		{name: "total mismatch", key: listKey, detail: "channel list ended with 0 of 3 items", prepare: func(_ *testing.T, _ *gorm.DB, _ *model.ThirdPartySite, fake *fakeNewAPISite) {
			fake.fail(http.MethodGet, "/api/channel/", fakeResponse{body: `{"success":true,"data":{"items":[],"total":3}}`})
		}},
		{name: "unauthorized", key: listKey, detail: "invalid token", prepare: func(_ *testing.T, _ *gorm.DB, _ *model.ThirdPartySite, fake *fakeNewAPISite) {
			fake.fail(http.MethodGet, "/api/channel/", fakeResponse{status: http.StatusUnauthorized, body: `{"success":false,"message":"invalid token"}`})
		}},
		{name: "long site message", key: listKey, detail: strings.Repeat("x", 500), prepare: func(_ *testing.T, _ *gorm.DB, _ *model.ThirdPartySite, fake *fakeNewAPISite) {
			fake.fail(http.MethodGet, "/api/channel/", fakeResponse{status: http.StatusForbidden, body: `{"success":false,"message":"` + strings.Repeat("x", 2000) + `"}`})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelPushTest(t)
			site := channelPushSeedSite(t, db, "alpha")
			supplier := channelPushSeedSupplier(t, db, "sup-a")
			channelPushSeedChannel(t, db, supplier, "A", 1, "m1", constant.ChannelTypeOpenAI)
			task := channelPushNewTask(t, db, site.Id, supplier.Id)
			fake := newFakeNewAPISite(t)
			tc.prepare(t, db, site, fake)

			run := channelPushRunOnce(t, db, task, fake)
			assert.Equal(t, model.ChannelPushRunFailed, run.Status)
			message := channelPushRunError(t, run)
			assert.Equal(t, tc.key, message.Key)
			if tc.detail != "" {
				assert.Equal(t, tc.detail, message.Params["detail"])
			}
			assert.Empty(t, fake.writeRequests())
			assert.Empty(t, string(run.Actions))
			var after model.ChannelPushTask
			require.NoError(t, db.First(&after, task.Id).Error)
			assert.NotZero(t, after.LastFinishedAt)
			assert.Equal(t, run.FinishedAt, after.LastFinishedAt)
		})
	}
}

func TestChannelPushRunSkipsOtherTaskChannels(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	s1 := channelPushSeedSupplier(t, db, "sup-a")
	s2 := channelPushSeedSupplier(t, db, "sup-b")
	channelPushSeedChannel(t, db, s1, "own", 1, "m1", constant.ChannelTypeOpenAI)
	other := channelPushSeedChannel(t, db, s2, "other", 1, "m1", constant.ChannelTypeOpenAI)
	task1 := channelPushNewTask(t, db, site.Id, s1.Id)
	task2 := channelPushNewTask(t, db, site.Id, s2.Id)
	fake := newFakeNewAPISite(t)
	fake.addChannel(fakeRemoteChannel{ID: 50, Name: service.BuildChannelPushRemoteName("other", site.Id, other.Id), Status: 1, Models: "m1"})

	assertSkipped := func(run model.ChannelPushRun) {
		t.Helper()
		assert.Equal(t, model.ChannelPushRunSuccess, run.Status)
		assert.Equal(t, 0, run.OrphanCount)
		for _, action := range channelPushRunActions(t, run) {
			assert.NotEqual(t, other.Id, action.LocalChannelID)
			assert.NotEqual(t, 50, action.RemoteChannelID)
		}
	}
	first := channelPushRunOnce(t, db, task1, fake)
	assertSkipped(first)
	assert.Equal(t, 1, first.CreateCount)

	_, err := model.SetChannelPushTaskEnabled(task2.Id, false)
	require.NoError(t, err)
	second := channelPushRunOnce(t, db, task1, fake)
	assertSkipped(second)
	assert.Equal(t, 1, second.UnchangedCount)
}

func TestChannelPushRunKeepsSecretsOut(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	channelPushSeedChannel(t, db, supplier, "first", 1, "m1", constant.ChannelTypeOpenAI)
	second := channelPushSeedChannel(t, db, supplier, "second", 1, "m1", constant.ChannelTypeOpenAI)
	task := channelPushNewTask(t, db, site.Id, supplier.Id)
	fake := newFakeNewAPISite(t)
	fake.fail(http.MethodPost, "/api/channel/", fakeResponse{status: http.StatusBadRequest, body: `{"success":false,"message":"rejected"}`})

	run := channelPushRunOnce(t, db, task, fake)
	require.Equal(t, model.ChannelPushRunPartial, run.Status)
	pushed := fake.channelsNamed(service.BuildChannelPushRemoteName("second", site.Id, second.Id))
	require.Len(t, pushed, 1)
	require.Equal(t, "sk-second", pushed[0].Key)
	for _, text := range []string{string(run.SiteSnapshot), string(run.TargetSnapshot), string(run.Actions), run.ErrorMessage, run.Suppliers} {
		assert.NotContains(t, text, "remote-access-token")
		assert.NotContains(t, text, "sk-")
	}
	raw, result := thirdPartySiteCall[map[string]any](t, http.MethodGet, "/api/channel_push/tasks?p=1&page_size=10", "", nil, GetChannelPushTasks)
	require.True(t, result.Success, result.Message)
	assert.NotContains(t, raw, "remote-access-token")
	assert.NotContains(t, raw, "sk-")

	responses := map[string]string{}
	raw, result = thirdPartySiteCall[map[string]any](t, http.MethodGet, "/api/channel_push/runs?p=1&page_size=10", "", nil, GetChannelPushRuns)
	require.True(t, result.Success, result.Message)
	require.Contains(t, raw, `"id":`+strconv.Itoa(run.Id))
	responses["runs"] = raw
	for _, kind := range []string{"site", "target", "actions"} {
		params := gin.Params{{Key: "id", Value: strconv.Itoa(run.Id)}, {Key: "kind", Value: kind}}
		raw, detail := thirdPartySiteCall[any](t, http.MethodGet, "/api/channel_push/runs/"+strconv.Itoa(run.Id)+"/"+kind, "", params, GetChannelPushRunDetail)
		require.True(t, detail.Success, detail.Message)
		require.NotNil(t, detail.Data, kind)
		responses[kind] = raw
	}
	for name, raw := range responses {
		for _, secret := range []string{"remote-access-token", "sk-first", "sk-second"} {
			assert.NotContains(t, raw, secret, name)
		}
	}
}

func TestChannelPushRunDetailsFallback(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	channelPushSeedChannel(t, db, supplier, "A", 1, "m1", constant.ChannelTypeOpenAI)
	task := channelPushNewTask(t, db, site.Id, supplier.Id)
	fake := newFakeNewAPISite(t)

	// Fail the run update that carries the details, as a MySQL packet limit would.
	const callbackName = "test:channel_push_fail_details"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "channel_push_runs" && slices.Contains(tx.Statement.Selects, "actions") {
			_ = tx.AddError(errors.New("packet too large"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	run := channelPushRunOnce(t, db, task, fake)
	assert.Equal(t, model.ChannelPushRunSuccess, run.Status)
	assert.Equal(t, 1, run.CreateCount)
	assert.Equal(t, 1, run.TargetChannelCount)
	assert.NotZero(t, run.FinishedAt)
	assert.Empty(t, string(run.SiteSnapshot))
	assert.Empty(t, string(run.TargetSnapshot))
	assert.Empty(t, string(run.Actions))
	message := channelPushRunError(t, run)
	assert.Equal(t, "Run details could not be saved: {{detail}}", message.Key)
	assert.Contains(t, message.Params["detail"], "packet too large")
	var after model.ChannelPushTask
	require.NoError(t, db.First(&after, task.Id).Error)
	assert.Equal(t, run.FinishedAt, after.LastFinishedAt)
}

// channelPushFailRunUpdates makes the next n updates of channel_push_runs fail
// (n < 0: all of them); withDetails limits it to the updates that carry the details.
func channelPushFailRunUpdates(t *testing.T, db *gorm.DB, n int, withDetails bool) {
	t.Helper()
	callbackName := "test:channel_push_fail_run_updates:" + t.Name()
	remaining := n
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "channel_push_runs" || remaining == 0 {
			return
		}
		if withDetails && !slices.Contains(tx.Statement.Selects, "actions") {
			return
		}
		if remaining > 0 {
			remaining--
		}
		_ = tx.AddError(errors.New("database unavailable"))
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })
}

func TestChannelPushRunFinishRetries(t *testing.T) {
	second := time.Second
	cases := []struct {
		name       string
		failures   int
		wantStatus string
		wantWaits  []time.Duration
	}{
		// The update with details and its fallback both fail once.
		{"saved on a retry", 2, model.ChannelPushRunSuccess, []time.Duration{1 * second}},
		{"gives up after five retries", -1, model.ChannelPushRunRunning,
			[]time.Duration{1 * second, 2 * second, 4 * second, 8 * second, 16 * second}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelPushTest(t)
			site := channelPushSeedSite(t, db, "alpha")
			supplier := channelPushSeedSupplier(t, db, "sup-a")
			channelPushSeedChannel(t, db, supplier, "A", 1, "m1", constant.ChannelTypeOpenAI)
			task := channelPushNewTask(t, db, site.Id, supplier.Id)
			fake := newFakeNewAPISite(t)
			require.NoError(t, db.Model(&model.ThirdPartySite{}).Where("id = ?", site.Id).Update("base_url", fake.server.URL).Error)
			require.NoError(t, db.First(&task, task.Id).Error)
			run, ok, err := model.ClaimChannelPushRun(task, common.GetTimestamp())
			require.NoError(t, err)
			require.True(t, ok)
			channelPushFailRunUpdates(t, db, tc.failures, false)
			recorder := &fakeSleepRecorder{}

			service.ExecuteChannelPushRun(context.Background(), run, channelPushTestOptions(recorder))

			var stored model.ChannelPushRun
			require.NoError(t, db.First(&stored, run.Id).Error)
			assert.Equal(t, tc.wantStatus, stored.Status)
			assert.Equal(t, tc.wantWaits, recorder.recorded())
			if tc.wantStatus == model.ChannelPushRunSuccess {
				assert.Equal(t, 1, stored.CreateCount)
				assert.NotEmpty(t, string(stored.Actions), "a retry keeps the details")
				assert.Empty(t, stored.ErrorMessage)
			}
		})
	}
}

func TestChannelPushRunDetailsFallbackKeepsReason(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	task := channelPushNewTask(t, db, site.Id, supplier.Id)
	reason, err := common.Marshal(common.NewMessage("Site does not exist"))
	require.NoError(t, err)
	claimFailed := func() *model.ChannelPushRun {
		require.NoError(t, db.Model(&model.ChannelPushTask{}).Where("id = ?", task.Id).Update("next_run_at", 10).Error)
		var current model.ChannelPushTask
		require.NoError(t, db.First(&current, task.Id).Error)
		run, ok, err := model.ClaimChannelPushRun(current, 10)
		require.NoError(t, err)
		require.True(t, ok)
		run.Status, run.ErrorMessage = model.ChannelPushRunFailed, string(reason)
		return run
	}

	t.Run("an existing reason is kept", func(t *testing.T) {
		run := claimFailed()
		run.Actions = `[{"category":"create"}]`
		channelPushFailRunUpdates(t, db, 1, true)
		require.NoError(t, model.FinishChannelPushRun(run, 1000))
		var stored model.ChannelPushRun
		require.NoError(t, db.First(&stored, run.Id).Error)
		assert.Equal(t, model.ChannelPushRunFailed, stored.Status)
		assert.EqualValues(t, 1000, stored.FinishedAt)
		assert.Empty(t, string(stored.Actions))
		assert.Equal(t, "Site does not exist", channelPushRunError(t, stored).Key)
	})

	t.Run("no details means no fallback", func(t *testing.T) {
		run := claimFailed()
		channelPushFailRunUpdates(t, db, 1, false)
		require.Error(t, model.FinishChannelPushRun(run, 1000))
		var stored model.ChannelPushRun
		require.NoError(t, db.First(&stored, run.Id).Error)
		assert.Equal(t, model.ChannelPushRunRunning, stored.Status)
	})
}

func TestChannelPushScheduling(t *testing.T) {
	db := setupChannelPushTest(t)
	site := channelPushSeedSite(t, db, "alpha")
	supplier := channelPushSeedSupplier(t, db, "sup-a")
	const now = int64(10000)
	setQueue := func(id int, enabled bool, nextRunAt int64) {
		require.NoError(t, db.Model(&model.ChannelPushTask{}).Where("id = ?", id).
			Select("enabled", "next_run_at").Updates(&model.ChannelPushTask{Enabled: enabled, NextRunAt: nextRunAt}).Error)
	}
	load := func(id int) model.ChannelPushTask {
		var task model.ChannelPushTask
		require.NoError(t, db.First(&task, id).Error)
		return task
	}
	// newTask makes a task on its own site and supplier.
	newTask := func(name string, overrides map[string]any) model.ChannelPushTask {
		taskSite := channelPushSeedSite(t, db, name)
		taskSupplier := channelPushSeedSupplier(t, db, name)
		created := channelPushCreateTask(t, channelPushTaskBody(t, taskSite.Id, []int{taskSupplier.Id}, overrides))
		require.True(t, created.Success, created.Message)
		return load(created.Data.Id)
	}

	t.Run("due scan", func(t *testing.T) {
		due := channelPushNewTask(t, db, site.Id, supplier.Id)
		setQueue(due.Id, true, now)
		disabled := newTask("disabled", nil)
		setQueue(disabled.Id, false, now-5)
		unqueued := newTask("unqueued", nil)
		setQueue(unqueued.Id, true, 0)
		future := newTask("future", nil)
		setQueue(future.Id, true, now+1)
		gone := newTask("gone", nil)
		setQueue(gone.Id, true, now-5)
		require.NoError(t, db.Delete(&model.ThirdPartySite{}, gone.SiteId).Error)

		tasks, err := model.ListDueChannelPushTasks(now, 10)
		require.NoError(t, err)
		require.Len(t, tasks, 1)
		assert.Equal(t, due.Id, tasks[0].Id)
	})

	t.Run("claim twice", func(t *testing.T) {
		task := newTask("claim", nil)
		setQueue(task.Id, true, now)
		task = load(task.Id)
		run, ok, err := model.ClaimChannelPushRun(task, now)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, model.ChannelPushRunRunning, run.Status)
		_, ok, err = model.ClaimChannelPushRun(task, now)
		require.NoError(t, err)
		assert.False(t, ok)
		var count int64
		require.NoError(t, db.Model(&model.ChannelPushRun{}).Where("task_id = ?", task.Id).Count(&count).Error)
		assert.EqualValues(t, 1, count)
	})

	// claimAndFinish claims a queued task, runs mid, and finishes the run at 1000.
	claimAndFinish := func(task model.ChannelPushTask, mid func(task model.ChannelPushTask)) model.ChannelPushTask {
		setQueue(task.Id, true, now)
		task = load(task.Id)
		run, ok, err := model.ClaimChannelPushRun(task, now)
		require.NoError(t, err)
		require.True(t, ok)
		if mid != nil {
			mid(task)
		}
		run.Status = model.ChannelPushRunSuccess
		require.NoError(t, model.FinishChannelPushRun(run, 1000))
		return load(task.Id)
	}

	t.Run("next time after finish", func(t *testing.T) {
		periodic := claimAndFinish(newTask("periodic", nil), nil)
		assert.EqualValues(t, 1000+30*60, periodic.NextRunAt)
		assert.EqualValues(t, 1000, periodic.LastFinishedAt)

		once := claimAndFinish(newTask("once", map[string]any{"task_type": "once", "interval_minutes": 0}), nil)
		assert.EqualValues(t, 0, once.NextRunAt)
		assert.EqualValues(t, 1000, once.LastFinishedAt)
	})

	t.Run("saved during run", func(t *testing.T) {
		for _, taskType := range []string{"periodic", "once"} {
			task := newTask("saved-"+taskType, map[string]any{"task_type": taskType, "interval_minutes": 30})
			before := common.GetTimestamp()
			finished := claimAndFinish(task, func(task model.ChannelPushTask) {
				_, err := model.UpdateChannelPushTask(task.Id, model.ChannelPushTaskInput{SiteId: task.SiteId,
					SupplierUserIds: supplierIdsOf(t, db, task.Id), TaskType: taskType, IntervalMinutes: 30, TargetGroup: "svip"})
				require.NoError(t, err)
			})
			assert.InDelta(t, before, finished.NextRunAt, 2, taskType)
			assert.NotEqualValues(t, 1000+30*60, finished.NextRunAt, taskType)
		}
	})

	t.Run("disabled during run", func(t *testing.T) {
		finished := claimAndFinish(newTask("toggled", nil), func(task model.ChannelPushTask) {
			_, err := model.SetChannelPushTaskEnabled(task.Id, false)
			require.NoError(t, err)
		})
		assert.EqualValues(t, 0, finished.NextRunAt)
		assert.False(t, finished.Enabled)
	})

	t.Run("restart recovery", func(t *testing.T) {
		// Drop the run the claim subtest left running.
		require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Where("status = ?", model.ChannelPushRunRunning).Delete(&model.ChannelPushRun{}).Error)
		periodic := newTask("rec-periodic", nil)
		once := newTask("rec-once", map[string]any{"task_type": "once", "interval_minutes": 0})
		saved := newTask("rec-saved", nil)
		runs := map[int]*model.ChannelPushRun{}
		for _, task := range []model.ChannelPushTask{periodic, once, saved} {
			setQueue(task.Id, true, now)
			run, ok, err := model.ClaimChannelPushRun(load(task.Id), now)
			require.NoError(t, err)
			require.True(t, ok)
			runs[task.Id] = run
		}
		// A save made while the process was down wins over the recovery.
		setQueue(saved.Id, true, 7777)

		recovered, err := model.RecoverChannelPushRuns(5000, "The run was interrupted by a service restart", nil)
		require.NoError(t, err)
		assert.Equal(t, 3, recovered)
		for taskId, run := range runs {
			var stored model.ChannelPushRun
			require.NoError(t, db.First(&stored, run.Id).Error)
			assert.Equal(t, model.ChannelPushRunFailed, stored.Status)
			assert.EqualValues(t, 5000, stored.FinishedAt)
			message := channelPushRunError(t, stored)
			assert.Equal(t, "The run was interrupted by a service restart", message.Key)
			assert.EqualValues(t, 5000, load(taskId).LastFinishedAt)
		}
		assert.EqualValues(t, 5000+30*60, load(periodic.Id).NextRunAt)
		assert.EqualValues(t, 0, load(once.Id).NextRunAt)
		assert.EqualValues(t, 7777, load(saved.Id).NextRunAt)

		again, err := model.RecoverChannelPushRuns(6000, "The run was interrupted by a service restart", nil)
		require.NoError(t, err)
		assert.Equal(t, 0, again)
	})

	t.Run("running record with nothing executing is healed", func(t *testing.T) {
		require.NoError(t, db.Session(&gorm.Session{AllowGlobalUpdate: true}).Where("status = ?", model.ChannelPushRunRunning).Delete(&model.ChannelPushRun{}).Error)
		stranded := newTask("heal-periodic", nil)
		strandedOnce := newTask("heal-once", map[string]any{"task_type": "once", "interval_minutes": 0})
		busy := newTask("heal-busy", nil)
		runs := map[int]*model.ChannelPushRun{}
		for _, task := range []model.ChannelPushTask{stranded, strandedOnce, busy} {
			setQueue(task.Id, true, now)
			run, ok, err := model.ClaimChannelPushRun(load(task.Id), now)
			require.NoError(t, err)
			require.True(t, ok)
			runs[task.Id] = run
		}

		healed, err := model.RecoverChannelPushRuns(8000, "The run ended without saving its result", []int{busy.Id})
		require.NoError(t, err)
		assert.Equal(t, 2, healed)
		for _, task := range []model.ChannelPushTask{stranded, strandedOnce} {
			var stored model.ChannelPushRun
			require.NoError(t, db.First(&stored, runs[task.Id].Id).Error)
			assert.Equal(t, model.ChannelPushRunFailed, stored.Status)
			assert.EqualValues(t, 8000, stored.FinishedAt)
			assert.Equal(t, "The run ended without saving its result", channelPushRunError(t, stored).Key)
			assert.EqualValues(t, 8000, load(task.Id).LastFinishedAt)
		}
		assert.EqualValues(t, 8000+30*60, load(stranded.Id).NextRunAt)
		assert.EqualValues(t, 0, load(strandedOnce.Id).NextRunAt)

		var busyRun model.ChannelPushRun
		require.NoError(t, db.First(&busyRun, runs[busy.Id].Id).Error)
		assert.Equal(t, model.ChannelPushRunRunning, busyRun.Status)
		assert.EqualValues(t, 0, load(busy.Id).LastFinishedAt)
		assert.EqualValues(t, 0, load(busy.Id).NextRunAt)
	})
}

func supplierIdsOf(t *testing.T, db *gorm.DB, taskId int) []int {
	t.Helper()
	var ids []int
	require.NoError(t, db.Model(&model.ChannelPushTaskSupplier{}).Where("task_id = ?", taskId).Pluck("supplier_user_id", &ids).Error)
	return ids
}

func TestChannelPushRunList(t *testing.T) {
	db := setupChannelPushTest(t)
	alpha := channelPushSeedSite(t, db, "alpha")
	beta := channelPushSeedSite(t, db, "beta")
	sup1 := channelPushSeedSupplier(t, db, "sup-a")
	sup2 := channelPushSeedSupplier(t, db, "sup-b")
	errorMessage, err := common.Marshal(common.NewMessage("Run details could not be saved: {{detail}}", map[string]any{"detail": "disk full"}))
	require.NoError(t, err)
	seed := func(site *model.ThirdPartySite, supplier *model.User, startedAt int64, errorText string) model.ChannelPushRun {
		refs, err := common.Marshal([]model.ChannelPushSupplierRef{{Id: supplier.Id, Username: supplier.Username}})
		require.NoError(t, err)
		run := model.ChannelPushRun{TaskId: 1, SiteId: site.Id, SiteName: site.Name, TargetGroup: "vip", Suppliers: string(refs),
			Status: model.ChannelPushRunFailed, ErrorMessage: errorText, StartedAt: startedAt, FinishedAt: startedAt + 5,
			SiteSnapshot: `[{"name":"site_snapshot_marker"}]`, TargetSnapshot: `[{"name":"target_snapshot_marker"}]`,
			Actions: `[{"category":"actions_marker"}]`}
		require.NoError(t, db.Create(&run).Error)
		require.NoError(t, db.Create(&model.ChannelPushRunSupplier{RunId: run.Id, SupplierUserId: supplier.Id}).Error)
		return run
	}
	r100 := seed(alpha, sup1, 100, "")
	r200 := seed(beta, sup2, 200, string(errorMessage))
	r300 := seed(alpha, sup2, 300, "")

	type runPage struct {
		Total int                        `json:"total"`
		Items []model.ChannelPushRunView `json:"items"`
	}
	list := func(query string) (string, []model.ChannelPushRunView, int) {
		raw, result := thirdPartySiteCall[runPage](t, http.MethodGet, "/api/channel_push/runs?"+query, "", nil, GetChannelPushRuns)
		require.True(t, result.Success, result.Message)
		return raw, result.Data.Items, result.Data.Total
	}
	ids := func(items []model.ChannelPushRunView) []int {
		out := make([]int, 0, len(items))
		for _, item := range items {
			out = append(out, item.Id)
		}
		return out
	}

	raw, items, total := list("p=1&page_size=10")
	assert.Equal(t, 3, total)
	assert.Equal(t, []int{r300.Id, r200.Id, r100.Id}, ids(items))
	for _, text := range []string{"site_snapshot", "target_snapshot", "actions", "remote-access-token", "sk-"} {
		assert.NotContains(t, raw, text)
	}
	assert.Nil(t, items[0].Error)
	require.NotNil(t, items[1].Error)
	assert.Equal(t, "Run details could not be saved: {{detail}}", items[1].Error.Key)
	assert.Equal(t, "disk full", items[1].Error.Params["detail"])
	assert.Equal(t, []model.ChannelPushSupplierRef{{Id: sup2.Id, Username: "sup-b"}}, items[1].Suppliers)

	_, items, _ = list("start_from=150&start_to=250")
	assert.Equal(t, []int{r200.Id}, ids(items))
	_, items, _ = list("start_from=200&start_to=300")
	assert.Equal(t, []int{r300.Id, r200.Id}, ids(items))
	_, items, _ = list("supplier_user_id=" + strconv.Itoa(sup2.Id))
	assert.Equal(t, []int{r300.Id, r200.Id}, ids(items))
	_, items, _ = list("site_id=" + strconv.Itoa(alpha.Id))
	assert.Equal(t, []int{r300.Id, r100.Id}, ids(items))
	_, items, total = list("site_id=" + strconv.Itoa(alpha.Id) + "&supplier_user_id=" + strconv.Itoa(sup1.Id))
	assert.Equal(t, 1, total)
	assert.Equal(t, []int{r100.Id}, ids(items))
	_, items, total = list("p=2&page_size=2")
	assert.Equal(t, 3, total)
	assert.Equal(t, []int{r100.Id}, ids(items))

	_, bad := thirdPartySiteCall[any](t, http.MethodGet, "/api/channel_push/runs?start_from=abc", "", nil, GetChannelPushRuns)
	assert.False(t, bad.Success)
	assert.Equal(t, "Invalid parameters", bad.MessageKey)
}

func TestChannelPushRunDetail(t *testing.T) {
	db := setupChannelPushTest(t)
	full := model.ChannelPushRun{TaskId: 1, SiteId: 1, SiteName: "alpha", TargetGroup: "vip", Status: model.ChannelPushRunSuccess, StartedAt: 1,
		SiteSnapshot: `[{"id":1}]`, TargetSnapshot: `[{"id":2}]`, Actions: `[{"category":"create"}]`}
	require.NoError(t, db.Create(&full).Error)
	empty := model.ChannelPushRun{TaskId: 1, SiteId: 1, SiteName: "alpha", TargetGroup: "vip", Status: model.ChannelPushRunRunning, StartedAt: 2}
	require.NoError(t, db.Create(&empty).Error)

	detail := func(id int, kind string) (string, thirdPartySiteResult[any]) {
		params := gin.Params{{Key: "id", Value: strconv.Itoa(id)}, {Key: "kind", Value: kind}}
		return thirdPartySiteCall[any](t, http.MethodGet, "/api/channel_push/runs/"+strconv.Itoa(id)+"/"+kind, "", params, GetChannelPushRunDetail)
	}
	for kind, want := range map[string]string{"site": `"data":[{"id":1}]`, "target": `"data":[{"id":2}]`, "actions": `"data":[{"category":"create"}]`} {
		raw, result := detail(full.Id, kind)
		require.True(t, result.Success, result.Message)
		assert.Contains(t, raw, want, kind)
	}
	for _, kind := range []string{"site", "target", "actions"} {
		raw, result := detail(empty.Id, kind)
		require.True(t, result.Success, result.Message)
		assert.Nil(t, result.Data)
		assert.Contains(t, raw, `"data":null`)
	}
	_, result := detail(full.Id, "x")
	assert.False(t, result.Success)
	assert.Equal(t, "Invalid parameters", result.MessageKey)
	_, result = detail(full.Id+1000, "site")
	assert.False(t, result.Success)
	assert.Equal(t, "Run does not exist", result.MessageKey)
}

func TestChannelPushSiteGroups(t *testing.T) {
	db := setupChannelPushTest(t)
	previous := service.ChannelPushDefaultOptions
	service.ChannelPushDefaultOptions.Pace = 0
	service.ChannelPushDefaultOptions.Timeout = 200 * time.Millisecond
	waits := &fakeSleepRecorder{}
	service.ChannelPushDefaultOptions.Sleep = waits.sleep
	t.Cleanup(func() { service.ChannelPushDefaultOptions = previous })

	site := channelPushSeedSite(t, db, "alpha")
	groups := func(siteId int) (string, thirdPartySiteResult[[]string]) {
		return thirdPartySiteCall[[]string](t, http.MethodGet, "/api/channel_push/sites/"+strconv.Itoa(siteId)+"/groups", "", channelPushIdParam(siteId), GetChannelPushSiteGroups)
	}
	pointAt := func(fake *fakeNewAPISite) {
		require.NoError(t, db.Model(&model.ThirdPartySite{}).Where("id = ?", site.Id).Update("base_url", fake.server.URL).Error)
	}

	healthy := newFakeNewAPISite(t)
	pointAt(healthy)
	raw, result := groups(site.Id)
	require.True(t, result.Success, result.Message)
	assert.Equal(t, []string{"default", "vip"}, result.Data)
	assert.NotContains(t, raw, "remote-access-token")

	html := newFakeNewAPISite(t)
	html.fail(http.MethodGet, "/api/group/", fakeResponse{body: "<html>"}, fakeResponse{body: "<html>"}, fakeResponse{body: "<html>"}, fakeResponse{body: "<html>"})
	pointAt(html)
	raw, result = groups(site.Id)
	assert.False(t, result.Success)
	assert.Equal(t, "Failed to read the site's groups: {{detail}}", result.MessageKey)
	assert.Len(t, html.recorded(http.MethodGet, "/api/group/"), 3)
	assert.NotContains(t, raw, "remote-access-token")

	// A rate limit is waited out for at most 5 seconds while the user waits.
	limited := newFakeNewAPISite(t)
	limited.fail(http.MethodGet, "/api/group/", fakeResponse{status: http.StatusTooManyRequests, header: map[string]string{"Retry-After": "120"}},
		fakeResponse{status: http.StatusTooManyRequests})
	pointAt(limited)
	before := len(waits.recorded())
	_, result = groups(site.Id)
	require.True(t, result.Success, result.Message)
	assert.Equal(t, []time.Duration{5 * time.Second, 5 * time.Second}, waits.recorded()[before:])

	unauthorized := newFakeNewAPISite(t)
	unauthorized.fail(http.MethodGet, "/api/group/", fakeResponse{status: 401, body: `{"success":false,"message":"no auth"}`})
	pointAt(unauthorized)
	raw, result = groups(site.Id)
	assert.False(t, result.Success)
	assert.Equal(t, "Failed to read the site's groups: {{detail}}", result.MessageKey)
	assert.Contains(t, raw, "no auth")
	assert.Len(t, unauthorized.recorded(http.MethodGet, "/api/group/"), 1)

	gone := channelPushSeedSite(t, db, "gone")
	require.NoError(t, db.Delete(gone).Error)
	for _, id := range []int{gone.Id, gone.Id + 1000} {
		_, result = groups(id)
		assert.False(t, result.Success)
		assert.Equal(t, "Site does not exist", result.MessageKey)
	}
}
