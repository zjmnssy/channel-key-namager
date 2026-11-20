package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	channelPushMaxRetriesEnv     = "CHANNEL_PUSH_REMOTE_MAX_RETRIES"
	channelPushDefaultMaxRetries = 10
	channelPushMaxRetriesLimit   = 100
	channelPushMaxResponseBytes  = 8 << 20
	channelPushMaxListPages      = 1000
	channelPushListPageSize      = 100
	channelPushMaxBackoff        = 60 * time.Second
	channelPushDefaultRetryAfter = 30 * time.Second
	channelPushMaxRetryAfter     = 180 * time.Second
)

type ChannelPushClientOptions struct {
	MaxRetries int
	Pace       time.Duration                                    // minimum gap between two requests to the same site
	Timeout    time.Duration                                    // timeout of a single request
	Sleep      func(ctx context.Context, d time.Duration) error // used for backoff and pacing
}

var ChannelPushDefaultOptions = ChannelPushClientOptions{
	MaxRetries: channelPushDefaultMaxRetries,
	Pace:       time.Second,
	Timeout:    30 * time.Second,
	Sleep:      channelPushSleep,
}

func channelPushSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ChannelPushMaxRetriesFromEnv reads CHANNEL_PUSH_REMOTE_MAX_RETRIES: unset or
// not an integer gives 10, a negative value gives 10 and is logged, above 100 gives 100.
func ChannelPushMaxRetriesFromEnv() int {
	value := common.GetEnvOrDefault(channelPushMaxRetriesEnv, channelPushDefaultMaxRetries)
	if value < 0 {
		common.SysError(common.LogText("%s must not be negative: %d, using default value: %d",
			channelPushMaxRetriesEnv, value, channelPushDefaultMaxRetries))
		return channelPushDefaultMaxRetries
	}
	return min(value, channelPushMaxRetriesLimit)
}

type ChannelPushRemoteError struct {
	StatusCode int    // 0 when no HTTP response was received
	Detail     string // site message, "HTTP 502", or the network error text; never request body or headers
	Attempts   int
	Retryable  bool
}

func (e *ChannelPushRemoteError) Error() string {
	return e.Message().Error()
}

func (e *ChannelPushRemoteError) Message() *common.Message {
	return common.NewMessage("Site request failed after {{attempts}} attempt(s): {{detail}}", map[string]any{
		"attempts": e.Attempts,
		"detail":   e.Detail,
	})
}

// channelPushPacer spaces the requests to one site; all clients of a site share it.
type channelPushPacer struct {
	mu   sync.Mutex
	last time.Time
}

var (
	channelPushPacers   = map[int]*channelPushPacer{}
	channelPushPacersMu sync.Mutex
)

var channelPushTransport = sync.OnceValue(func() *http.Transport {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	return transport
})

type channelPushEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type ChannelPushClient struct {
	site    *model.ThirdPartySite
	options ChannelPushClientOptions
	http    *http.Client
	pacer   *channelPushPacer
}

func NewChannelPushClient(site *model.ThirdPartySite, options ChannelPushClientOptions) *ChannelPushClient {
	if options.Sleep == nil {
		options.Sleep = channelPushSleep
	}
	if options.Timeout <= 0 {
		options.Timeout = ChannelPushDefaultOptions.Timeout
	}
	options.MaxRetries = max(options.MaxRetries, 0)

	channelPushPacersMu.Lock()
	pacer := channelPushPacers[site.Id]
	if pacer == nil {
		pacer = &channelPushPacer{}
		channelPushPacers[site.Id] = pacer
	}
	channelPushPacersMu.Unlock()

	return &ChannelPushClient{
		site:    site,
		options: options,
		http: &http.Client{
			Transport: channelPushTransport(),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		pacer: pacer,
	}
}

// pace waits until the minimum gap since the previous request to this site has passed.
// The lock is held while waiting so concurrent callers queue up one by one.
func (c *ChannelPushClient) pace(ctx context.Context) error {
	if c.options.Pace <= 0 {
		return ctx.Err()
	}
	c.pacer.mu.Lock()
	defer c.pacer.mu.Unlock()
	if !c.pacer.last.IsZero() {
		if wait := c.options.Pace - time.Since(c.pacer.last); wait > 0 {
			if err := c.options.Sleep(ctx, wait); err != nil {
				return err
			}
		}
	}
	c.pacer.last = time.Now()
	return nil
}

// attempt sends one request. On failure it returns the error with Retryable set,
// and the delay the site asked for through Retry-After (0 when none).
func (c *ChannelPushClient) attempt(ctx context.Context, method string, path string, query url.Values, payload []byte) (*channelPushEnvelope, time.Duration, *ChannelPushRemoteError) {
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()

	target := c.site.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, 0, &ChannelPushRemoteError{Detail: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.site.ApiKey)
	req.Header.Set("New-Api-User", strconv.Itoa(c.site.RemoteUserID))
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error text carries the URL only, never headers or body.
		return nil, 0, &ChannelPushRemoteError{Detail: err.Error(), Retryable: true}
	}
	defer resp.Body.Close()

	status := resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, channelPushMaxResponseBytes+1))
	if err != nil {
		return nil, 0, &ChannelPushRemoteError{StatusCode: status, Detail: err.Error(), Retryable: true}
	}
	if len(body) > channelPushMaxResponseBytes {
		return nil, 0, &ChannelPushRemoteError{StatusCode: status, Detail: "response body too large", Retryable: true}
	}

	var envelope channelPushEnvelope
	parseErr := common.Unmarshal(body, &envelope)
	httpDetail := "HTTP " + strconv.Itoa(status)

	switch {
	case status >= 200 && status < 300:
		if parseErr != nil {
			return nil, 0, &ChannelPushRemoteError{StatusCode: status, Detail: "invalid response body", Retryable: true}
		}
		if !envelope.Success {
			detail := envelope.Message
			if detail == "" {
				detail = httpDetail
			}
			return nil, 0, &ChannelPushRemoteError{StatusCode: status, Detail: detail}
		}
		return &envelope, 0, nil
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		retryAfter := time.Duration(0)
		if status == http.StatusTooManyRequests {
			retryAfter = channelPushDefaultRetryAfter
			if seconds, convErr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); convErr == nil && seconds >= 0 {
				retryAfter = min(time.Duration(seconds)*time.Second, channelPushMaxRetryAfter)
			}
		}
		return nil, retryAfter, &ChannelPushRemoteError{StatusCode: status, Detail: httpDetail, Retryable: true}
	default:
		detail := httpDetail
		if status >= 400 && parseErr == nil && envelope.Message != "" {
			detail = envelope.Message
		}
		return nil, 0, &ChannelPushRemoteError{StatusCode: status, Detail: detail}
	}
}

// do sends a request, retrying as the design requires, and returns the successful envelope.
func (c *ChannelPushClient) do(ctx context.Context, method string, path string, query url.Values, body any) (*channelPushEnvelope, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = common.Marshal(body)
		if err != nil {
			return nil, &ChannelPushRemoteError{Detail: err.Error()}
		}
	}

	for attempts := 1; ; attempts++ {
		var envelope *channelPushEnvelope
		var retryAfter time.Duration
		failure := c.pace(ctx)
		if failure == nil {
			var remoteErr *ChannelPushRemoteError
			envelope, retryAfter, remoteErr = c.attempt(ctx, method, path, query, payload)
			if remoteErr == nil {
				return envelope, nil
			}
			remoteErr.Attempts = attempts
			if ctx.Err() != nil {
				remoteErr.Retryable = false
			}
			if !remoteErr.Retryable || attempts > c.options.MaxRetries {
				common.SysError(common.LogText("channel push site request failed: site=%d path=%s status=%d attempts=%d",
					c.site.Id, path, remoteErr.StatusCode, attempts))
				return nil, remoteErr
			}

			delay := retryAfter
			if delay == 0 {
				delay = min(time.Second<<min(attempts-1, 10), channelPushMaxBackoff)
			}
			failure = c.options.Sleep(ctx, delay)
			if failure == nil {
				continue
			}
		}
		// The caller's context ended while waiting.
		return nil, &ChannelPushRemoteError{Detail: failure.Error(), Attempts: attempts}
	}
}

type channelPushListData struct {
	Items []ChannelPushSiteChannel `json:"items"`
	Total int                      `json:"total"`
}

// ListChannels reads every page of the site's channel list. A list that cannot be
// proven complete is an error.
func (c *ChannelPushClient) ListChannels(ctx context.Context) ([]ChannelPushSiteChannel, error) {
	all := []ChannelPushSiteChannel{}
	seen := map[int]struct{}{}
	total := 0
	for page := 1; page <= channelPushMaxListPages; page++ {
		query := url.Values{}
		query.Set("p", strconv.Itoa(page))
		query.Set("page_size", strconv.Itoa(channelPushListPageSize))
		query.Set("id_sort", "true")
		envelope, err := c.do(ctx, http.MethodGet, "/api/channel/", query, nil)
		if err != nil {
			return nil, err
		}

		var data channelPushListData
		if len(envelope.Data) == 0 || common.Unmarshal(envelope.Data, &data) != nil || data.Items == nil {
			return nil, errors.New("channel list response has no items")
		}
		if page == 1 {
			total = data.Total
		} else if data.Total != total {
			return nil, fmt.Errorf("channel list total changed from %d to %d while reading", total, data.Total)
		}
		for _, item := range data.Items {
			if item.ID <= 0 {
				return nil, errors.New("channel list contains an item without a positive id")
			}
			if _, duplicated := seen[item.ID]; duplicated {
				return nil, fmt.Errorf("channel list contains duplicate id %d", item.ID)
			}
			seen[item.ID] = struct{}{}
			all = append(all, item)
		}
		if len(all) > total {
			return nil, fmt.Errorf("channel list returned more items than total %d", total)
		}
		if len(all) == total {
			return all, nil
		}
		if len(data.Items) == 0 {
			return nil, fmt.Errorf("channel list ended with %d of %d items", len(all), total)
		}
	}
	return nil, fmt.Errorf("channel list exceeds %d pages", channelPushMaxListPages)
}

func (c *ChannelPushClient) ListGroups(ctx context.Context) ([]string, error) {
	envelope, err := c.do(ctx, http.MethodGet, "/api/group/", nil, nil)
	if err != nil {
		return nil, err
	}
	var groups []string
	if len(envelope.Data) == 0 || common.Unmarshal(envelope.Data, &groups) != nil || groups == nil {
		return nil, errors.New("group list response has no data")
	}
	return groups, nil
}

// CreateChannel returns the new remote channel id, or 0 when the site does not report one.
func (c *ChannelPushClient) CreateChannel(ctx context.Context, body map[string]any) (remoteID int, err error) {
	envelope, err := c.do(ctx, http.MethodPost, "/api/channel/", nil, body)
	if err != nil {
		return 0, err
	}
	var data struct {
		ID int `json:"id"`
	}
	if len(envelope.Data) > 0 && common.Unmarshal(envelope.Data, &data) == nil {
		remoteID = data.ID
	}
	return remoteID, nil
}

func (c *ChannelPushClient) UpdateModels(ctx context.Context, remoteID int, models string) error {
	body := struct {
		ID     int    `json:"id"`
		Models string `json:"models"`
	}{remoteID, models}
	_, err := c.do(ctx, http.MethodPut, "/api/channel/", nil, body)
	return err
}

func (c *ChannelPushClient) SetStatus(ctx context.Context, remoteID int, status int) error {
	body := struct {
		Status int `json:"status"`
	}{status}
	_, err := c.do(ctx, http.MethodPost, "/api/channel/"+strconv.Itoa(remoteID)+"/status", nil, body)
	return err
}
