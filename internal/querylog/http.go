package querylog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// httpSource reads a small log server running next to Blocky that returns
// recent query-log entries as JSON (queryLog.type "http"). It is the way to
// reach csv or sqlite logs that live on another host.
//
// The server answers GET with an array of objects like
//
//	{"timestamp": "2026-10-30 11:13:56.2+01:00", "client_ip": "10.1.1.65",
//	 "client_name": "laptop", "reply_type": "CACHED", "domain": "example.com",
//	 "status": "NOERROR", "type": "A"}
//
// newest first. The filters and limit are also sent as query parameters
// (q, client, type, limit) for servers that can apply them; the reader
// filters again either way, so a server that ignores them still works.
type httpSource struct {
	url    string
	token  string // sent as a bearer token when set
	client *http.Client

	mu     sync.Mutex
	cached map[string]cachedResponse // keyed by request URL
}

type cachedResponse struct {
	at      time.Time
	entries []Entry
}

// httpCacheTTL coalesces the UI's refresh and search-as-you-type requests so
// the log server is not asked for the same data several times a second.
const httpCacheTTL = 5 * time.Second

func newHTTPSource(target, token string) (*httpSource, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("query log target %q: want an http(s) URL", target)
	}
	return &httpSource{
		url:    target,
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
		cached: map[string]cachedResponse{},
	}, nil
}

// remoteEntry is one entry as the log server returns it. Pointer-free
// strings decode JSON null as "".
type remoteEntry struct {
	Timestamp  string          `json:"timestamp"`
	ClientIP   string          `json:"client_ip"`
	ClientName string          `json:"client_name"`
	ReplyType  string          `json:"reply_type"`
	Domain     string          `json:"domain"`
	Status     string          `json:"status"`
	Type       string          `json:"type"`
	Reason     string          `json:"reason"`
	Answer     string          `json:"answer"`
	Duration   json.RawMessage `json:"duration_ms"`
}

func (s *httpSource) read(ctx context.Context, f filter, limit int) ([]Entry, error) {
	reqURL, err := s.requestURL(f, limit)
	if err != nil {
		return nil, err
	}
	entries, err := s.fetch(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	out := []Entry{} // encode as [] rather than null when nothing matches
	for _, e := range entries {
		if f.match(e) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Time.After(out[b].Time) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *httpSource) requestURL(f filter, limit int) (string, error) {
	u, err := url.Parse(s.url)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, v := range map[string]string{"q": f.needle, "client": f.client, "type": f.rtype} {
		if v != "" {
			q.Set(k, v)
		}
	}
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *httpSource) fetch(ctx context.Context, reqURL string) ([]Entry, error) {
	s.mu.Lock()
	if c, ok := s.cached[reqURL]; ok && time.Since(c.at) < httpCacheTTL {
		s.mu.Unlock()
		return c.entries, nil
	}
	s.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query log server: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("query log server: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if s.token == "" {
			return nil, fmt.Errorf("query log server: HTTP %d: it requires a token; set queryLog.token", resp.StatusCode)
		}
		return nil, fmt.Errorf("query log server: HTTP %d: token rejected; check queryLog.token", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("query log server: HTTP %d: %s", resp.StatusCode, e.Error)
		}
		return nil, fmt.Errorf("query log server: HTTP %d", resp.StatusCode)
	}

	var raw []*remoteEntry
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("query log server: unexpected response: %w", err)
	}
	entries := make([]Entry, 0, len(raw))
	for _, r := range raw {
		if r != nil {
			entries = append(entries, r.toEntry())
		}
	}

	s.mu.Lock()
	for k, c := range s.cached { // drop expired entries so the map stays small
		if time.Since(c.at) >= httpCacheTTL {
			delete(s.cached, k)
		}
	}
	s.cached[reqURL] = cachedResponse{at: time.Now(), entries: entries}
	s.mu.Unlock()
	return entries, nil
}

func (r remoteEntry) toEntry() Entry {
	e := Entry{
		Time:         parseTime(r.Timestamp),
		ClientIP:     r.ClientIP,
		ClientName:   r.ClientName,
		Question:     r.Domain,
		QuestionType: r.Type,
		ResponseCode: r.Status,
		Reason:       r.Reason,
		Answer:       r.Answer,
	}
	if e.ClientName == e.ClientIP {
		e.ClientName = "" // the UI falls back to the IP
	}
	// reply_type may be a bare type ("BLOCKED") or Blocky's reason
	// ("BLOCKED (ads)"): the first word is the type, the rest the reason.
	rt := strings.TrimSpace(r.ReplyType)
	e.ResponseType = strings.ToUpper(strings.SplitN(rt, " ", 2)[0])
	if e.Reason == "" && rt != e.ResponseType {
		e.Reason = rt
	}
	if len(r.Duration) > 0 {
		e.DurationMS, _ = strconv.ParseFloat(strings.Trim(string(r.Duration), `"`), 64)
	}
	return e
}
