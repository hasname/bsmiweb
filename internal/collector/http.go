package collector

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"

type Client struct {
	HTTP  *http.Client
	Sleep func(context.Context, time.Duration) error
}

type dataTransport struct{ verified, openData tlsclient.HttpClient }

func (t dataTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme == "https" && r.URL.Hostname() == "data.bsmi.gov.tw" {
		return t.openData.Do(r)
	}
	return t.verified.Do(r)
}

func (t dataTransport) CloseIdleConnections() {
	t.verified.CloseIdleConnections()
	t.openData.CloseIdleConnections()
}

func newTransport(options ...tlsclient.HttpClientOption) (*dataTransport, error) {
	options = append(options,
		tlsclient.WithClientProfile(profiles.Chrome_152),
		tlsclient.WithRandomTLSExtensionOrder(),
		tlsclient.WithDisableHttp3(),
		// The outer client owns redirects and the deadline across the entire request.
		tlsclient.WithNotFollowRedirects(),
		tlsclient.WithTimeoutSeconds(0),
	)
	verified, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
	if err != nil {
		return nil, err
	}
	// The open-data endpoint uses a self-signed certificate. Keep the existing
	// exception confined to that host, including after each redirect.
	openData, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), append(options, tlsclient.WithInsecureSkipVerify())...)
	if err != nil {
		verified.CloseIdleConnections()
		return nil, err
	}
	return &dataTransport{verified: verified, openData: openData}, nil
}

func NewClient() (*Client, error) {
	transport, err := newTransport()
	if err != nil {
		return nil, err
	}
	return &Client{HTTP: &http.Client{Timeout: 2 * time.Minute, Transport: transport}, Sleep: Sleep}, nil
}

func Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) pause(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	return Sleep(ctx, d)
}

func (c *Client) request(ctx context.Context, method, endpoint, body, contentType, referer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-TW,zh;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header[http.HeaderOrderKey] = []string{"host", "content-length", "user-agent", "content-type", "accept", "origin", "referer", "accept-encoding", "accept-language"}
	if req.URL.Hostname() == "data.bsmi.gov.tw" {
		req.Header.Set("User-Agent", "bsmiweb/1.0")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if req.URL.Hostname() == "aisearch-web.shopping.friday.tw" {
		req.Header.Set("Origin", "https://ec-w.shopping.friday.tw")
		req.Header.Set("Accept", "application/json")
	}
	if req.URL.Hostname() == "ecshweb.pchome.com.tw" {
		req.Header.Set("Accept", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned HTTP %d", req.URL.Hostname(), res.StatusCode)
	}
	const limit = 256 << 20
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf("response exceeds 256 MiB")
	}
	return string(data), nil
}
