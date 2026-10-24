package collector

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hasname/bsmiweb/internal/db"
)

const vendorURL = "https://civil.bsmi.gov.tw/bsmi_pqn/pqn/uqi5102f.do"
const certURL = "https://civil.bsmi.gov.tw/bsmi_pqn/pqn/uqi2102f.do"
const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var ErrRateLimited = errors.New("BSMI rate limited")
var ValidID = regexp.MustCompile(`(?i)^[RTDQM][A-Z0-9]{5}$`)
var tags = regexp.MustCompile(`<[^>]*>`)
var breaks = regexp.MustCompile(`(?i)<br\s*/?>`)

type Client struct {
	HTTP  *http.Client
	Sleep func(context.Context, time.Duration) error
}

type dataTransport struct{ verified, openData *http.Transport }

func (t dataTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme == "https" && r.URL.Hostname() == "data.bsmi.gov.tw" {
		return t.openData.RoundTrip(r)
	}
	return t.verified.RoundTrip(r)
}

func NewClient() *Client {
	verified := http.DefaultTransport.(*http.Transport).Clone()
	openData := verified.Clone()
	// The open-data endpoint uses a self-signed certificate. Keep the existing
	// exception confined to that host; all other hosts still verify TLS.
	openData.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402
	return &Client{HTTP: &http.Client{Timeout: 2 * time.Minute, Transport: dataTransport{verified, openData}}, Sleep: Sleep}
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

func clean(s string) string {
	return strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(s, "")))
}

func ParseRegistration(id, body string) (*db.Registration, error) {
	if strings.Contains(body, "系統忙碌中") {
		return nil, ErrRateLimited
	}
	if !strings.Contains(body, "廠商資料") {
		return nil, fmt.Errorf("BSMI vendor section not found")
	}
	r := &db.Registration{ID: strings.ToUpper(id)}
	fields := []struct {
		label string
		dest  *string
	}{{"統編", &r.TaxID}, {"申請人", &r.Applicant}, {"聯絡地址", &r.ContactAddr}, {`公司地址\s*`, &r.CompanyAddr}, {"電話", &r.Phone}, {"廠商資料備註", &r.Note}}
	for _, field := range fields {
		match := regexp.MustCompile(`(?s)` + field.label + `[：:]\s*</(?:p|label)>\s*(?:</div>\s*)?<div[^>]*>\s*<p>(.*?)</p>`).FindStringSubmatch(body)
		if match == nil {
			return nil, fmt.Errorf("BSMI vendor field could not be parsed: %s", field.label)
		}
		*field.dest = clean(match[1])
	}
	start := strings.Index(body, `panel-heading ">證書資料`)
	if start < 0 {
		return nil, fmt.Errorf("BSMI certificate section not found")
	}
	rows := regexp.MustCompile(`\n\s*<div\s+class='row'>`).Split(body[start:], -1)
	colsRE := regexp.MustCompile(`(?s)<div class="col-xs-6 col-sm-\d+ col-md-\d+ col-lg-\d+">\s*(.*?)\s*</div>`)
	strongRE := regexp.MustCompile(`(?s)<strong>\s*(.*?)\s*</strong>`)
	for _, row := range rows[1:] {
		cols := colsRE.FindAllStringSubmatch(row, -1)
		if len(cols) < 4 {
			return nil, fmt.Errorf("BSMI certificate row could not be parsed")
		}
		parts := breaks.Split(cols[0][1], -1)
		cert := db.Certificate{ID: clean(parts[0]), RegistrationID: r.ID, ProductName: clean(cols[1][1]), SoldAs: clean(cols[2][1])}
		if cert.ID == "" {
			return nil, fmt.Errorf("BSMI certificate ID not found")
		}
		if len(parts) > 1 {
			cert.ValidDate = clean(parts[1])
		}
		if len(parts) > 2 {
			cert.Status = clean(parts[2])
		}
		if match := strongRE.FindStringSubmatch(cols[3][1]); match != nil {
			cert.MainModel = clean(match[1])
		}
		var series []string
		for _, part := range breaks.Split(strongRE.ReplaceAllString(cols[3][1], ""), -1) {
			if value := clean(part); value != "" {
				series = append(series, value)
			}
		}
		cert.SeriesModels = strings.Join(series, "\n")
		if len(cols) > 4 {
			cert.Issuer = clean(cols[4][1])
		}
		r.Certificates = append(r.Certificates, cert)
	}
	if r.TaxID == "" && r.Applicant == "" && r.ContactAddr == "" && r.CompanyAddr == "" && r.Phone == "" && r.Note == "" && len(r.Certificates) == 0 {
		return nil, nil
	}
	return r, nil
}

func (c *Client) FetchRegistration(ctx context.Context, id string) (*db.Registration, error) {
	if !ValidID.MatchString(id) {
		return nil, fmt.Errorf("invalid registration ID")
	}
	id = strings.ToUpper(id)
	form := url.Values{"state": {"queryAll"}, "q_regType": {id[:1]}, "q_regNo": {id[1:]}, "pageSize": {"9999"}, "currentPage": {"1"}}
	body, err := c.request(ctx, "POST", vendorURL, form.Encode(), "application/x-www-form-urlencoded", "")
	if err != nil {
		return nil, err
	}
	return ParseRegistration(id, body)
}

func rocDate(t time.Time) string {
	return fmt.Sprintf("%d/%02d/%02d", t.Year()-1911, t.Month(), t.Day())
}

func (c *Client) HasRecentChanges(ctx context.Context, id string, from, to time.Time) (bool, error) {
	form := url.Values{"state": {"queryAll"}, "q_certType": {"1"}, "q_regNo": {id[1:]}, "pageSize": {"100"}, "currentPage": {"1"}, "q_modDateS": {rocDate(from)}, "q_modDateE": {rocDate(to)}}
	body, err := c.request(ctx, "POST", certURL, form.Encode(), "application/x-www-form-urlencoded", "")
	if err != nil {
		return false, err
	}
	if strings.Contains(body, "系統忙碌中") {
		return false, ErrRateLimited
	}
	// Only existence matters: the full registration is fetched when any result
	// changes, so subsequent certificate-list pages need not be downloaded.
	if !strings.Contains(body, `<div id="listContainer"`) {
		return false, fmt.Errorf("BSMI certificate list not found")
	}
	match := regexp.MustCompile(`共(\d+)筆`).FindStringSubmatch(body)
	if match != nil {
		count, _ := strconv.Atoi(match[1])
		return count > 0, nil
	}
	rows := regexp.MustCompile(`<a href="javascript:queryOne\('\d+'\)">([^<]*)</a>`).FindAllStringSubmatch(body, -1)
	return len(rows) >= 7, nil
}
