package fetch

import (
	"context"
	"errors"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

type Options struct {
	Timeout   time.Duration
	UserAgent string
	Delay     time.Duration
}

type Client struct {
	http *http.Client
	ua   string
	lim  *rate.Limiter
}

func NewClient(opt Options) *Client {
	delay := opt.Delay
	if delay <= 0 { delay = 500 * time.Millisecond }
	return &Client{
		http: &http.Client{Timeout: opt.Timeout},
		ua:   opt.UserAgent,
		lim:  rate.NewLimiter(rate.Every(delay), 1),
	}
}

func (c *Client) Get(ctx context.Context, url string) (*http.Response, error) {
	if err := c.lim.Wait(ctx); err != nil { return nil, err }
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if c.ua != "" { req.Header.Set("User-Agent", c.ua) }
	resp, err := c.http.Do(req)
	if err != nil { return nil, err }
	if resp.StatusCode >= 400 { resp.Body.Close(); return nil, errors.New(resp.Status) }
	return resp, nil
}
