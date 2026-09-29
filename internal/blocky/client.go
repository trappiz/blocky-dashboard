package blocky

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/trappiz/blocky-dashboard/internal/config"
)

type Client struct {
	baseURL string
	token   string
	headers map[string]string
	http    *http.Client
}

func New(cfg config.BlockyConfig) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.URL, "/"),
		token:   cfg.Token,
		headers: cfg.Headers,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, int, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		r = bytes.NewReader(b)
	}

	u := c.baseURL + path
	if len(query) != 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		return data, resp.StatusCode, fmt.Errorf("blocky returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, resp.StatusCode, nil
}
