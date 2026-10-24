package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/hasname/bsmiweb/internal/db"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockClient(fn func(*http.Request) (string, int)) *Client {
	return &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body, status := fn(r)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}, Sleep: func(context.Context, time.Duration) error { return nil }}
}
func emptyFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/bsmi-not-found.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func collectorStore(t *testing.T) *db.Store {
	t.Helper()
	s, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFetchRegistration(t *testing.T) {
	empty := emptyFixture(t)
	for _, tc := range []struct {
		name, html         string
		status             int
		wantError, wantNil bool
	}{
		{"empty", empty, 200, false, true}, {"whitespace", strings.ReplaceAll(empty, "<p></p>", "<p> \n\t </p>"), 200, false, true},
		{"vendor without tax ID", strings.Replace(empty, "<p></p>", "<p>unused</p>", 1), 200, false, true},
		{"rate limited", "系統忙碌中" + empty, 200, true, false}, {"HTTP error", empty, 503, true, false},
		{"unexpected", "<html>Maintenance</html>", 200, true, false}, {"missing vendor field", strings.ReplaceAll(empty, "統編：", "Unknown:"), 200, true, false},
		{"missing certificate section", strings.ReplaceAll(empty, "證書資料", "Unknown"), 200, true, false},
		{"bad certificate row", strings.ReplaceAll(empty, `<div class="panel-body listRows"></div>`, "<div class=\"panel-body listRows\">\n<div class='row'>bad</div></div>"), 200, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mockClient(func(r *http.Request) (string, int) {
				if r.URL.String() != vendorURL || r.Method != "POST" {
					t.Fatal("wrong endpoint")
				}
				r.ParseForm()
				if r.Form.Get("q_regType") != "R" || r.Form.Get("q_regNo") != "45879" || r.Form.Get("pageSize") != "9999" {
					t.Fatal(r.Form)
				}
				return tc.html, tc.status
			})
			got, err := c.FetchRegistration(context.Background(), "r45879")
			if (err != nil) != tc.wantError {
				t.Fatal(err)
			}
			if tc.wantNil && got != nil {
				t.Fatalf("empty result retained: %+v", got)
			}
		})
	}
	withVendor := strings.Replace(empty, "申請人：</label>\n        <div class=\"form-control-static\"><p></p>", "申請人：</label>\n        <div class=\"form-control-static\"><p>測試 &amp; 公司</p>", 1)
	r, err := ParseRegistration("r45879", withVendor)
	if err != nil || r == nil || r.Applicant != "測試 & 公司" {
		t.Fatalf("vendor-only: %+v %v", r, err)
	}
	row := `<div class="panel-body listRows">
<div class='row'>
<div class="col-xs-6 col-sm-2 col-md-2 col-lg-2">CI450068790054<br>1130202<br>認可</div>
<div class="col-xs-6 col-sm-2 col-md-2 col-lg-2">充電器</div>
<div class="col-xs-6 col-sm-2 col-md-2 col-lg-2"></div>
<div class="col-xs-6 col-sm-3 col-md-2 col-lg-3"><strong>A</strong><br>B<br/>C</div>
<div class="col-xs-6 col-sm-2 col-md-2 col-lg-2">標檢局</div>
</div></div>`
	r, err = ParseRegistration("R45879", strings.ReplaceAll(empty, `<div class="panel-body listRows"></div>`, row))
	if err != nil || r == nil || len(r.Certificates) != 1 || r.Certificates[0].SeriesModels != "B\nC" {
		t.Fatalf("certificate-only: %+v %v", r, err)
	}
}

func TestCertificateImportDatasetsAndFailure(t *testing.T) {
	s := collectorStore(t)
	ctx := context.Background()
	seen := map[string]bool{}
	c := mockClient(func(r *http.Request) (string, int) {
		seen[r.URL.String()] = true
		if r.Header.Get("User-Agent") != "bsmiweb/1.0" {
			t.Fatal("wrong user agent")
		}
		id := ""
		for i, u := range certificateDatasets {
			if r.URL.String() == u {
				id = fmt.Sprint(i)
			}
		}
		if id == "" {
			t.Fatal(r.URL)
		}
		return fmt.Sprintf(`<bsmi><row><證書號碼>C%s</證書號碼><商品名稱>O'Reilly &amp; %s</商品名稱><證書期限>2028/04/06</證書期限><主型式>M%s</主型式></row><row><證書號碼>OVERLAP</證書號碼><商品名稱>%s</商品名稱></row><row><商品名稱>missing ID</商品名稱></row></bsmi>`, id, id, id, id), 200
	})
	if err := c.ImportCertificates(ctx, s); err != nil {
		t.Fatal(err)
	}
	counts, _ := s.Counts(ctx)
	if counts.Certificates != 4 || len(seen) != 3 {
		t.Fatal(counts, seen)
	}
	_, certs, _, err := s.Search(ctx, "C2")
	if err != nil || len(certs) != 1 || certs[0].ValidDate != "1170406" || certs[0].ProductName != "O'Reilly & 2" || certs[0].RegistrationID != "" {
		t.Fatalf("new-issue data: %+v %v", certs, err)
	}
	_, certs, _, _ = s.Search(ctx, "OVERLAP")
	if certs[0].ProductName != "0" {
		t.Fatal("dataset precedence changed")
	}
	c = mockClient(func(r *http.Request) (string, int) {
		if r.URL.String() == certificateDatasets[2] {
			return "unavailable", 503
		}
		return `<bsmi><row><證書號碼>SHOULD_NOT_INSERT</證書號碼></row></bsmi>`, 200
	})
	if err := c.ImportCertificates(ctx, s); err == nil {
		t.Fatal("expected fetch error")
	}
	counts, _ = s.Counts(ctx)
	if counts.Certificates != 4 {
		t.Fatal("partial import committed")
	}
}

func TestAuthorizationImportRejectsBadFeed(t *testing.T) {
	s := collectorStore(t)
	ctx := context.Background()
	c := mockClient(func(*http.Request) (string, int) {
		return `<bsmi><row><授權證號>A</授權證號><證書編號>C</證書編號><被授權人名稱>公司 &amp; Co</被授權人名稱><被授權人統編>12345678</被授權人統編></row></bsmi>`, 200
	})
	if err := c.ImportAuthorizations(ctx, s); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`<html>maintenance</html>`, `<bsmi><row>`, `<bsmi><row><Unknown>A</Unknown></row></bsmi>`} {
		c = mockClient(func(*http.Request) (string, int) { return body, 200 })
		if err := c.ImportAuthorizations(ctx, s); err == nil {
			t.Fatal("accepted invalid feed")
		}
	}
	auths, err := s.AuthorizationsByTaxID(ctx, "12345678")
	if err != nil || len(auths) != 1 || auths[0].AuthorizeeName != "公司 & Co" {
		t.Fatal(auths, err)
	}
}

func TestECScans(t *testing.T) {
	for _, source := range []string{"books", "friday", "momo", "pchome"} {
		t.Run(source, func(t *testing.T) {
			calls := 0
			c := mockClient(func(r *http.Request) (string, int) {
				calls++
				switch source {
				case "books":
					return `R12345 /page/2/`, 200
				case "friday":
					var req map[string]any
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatal(err)
					}
					if req["kws_phrase64"] != "YnNtaQ==" || r.Header.Get("Origin") != "https://ec-w.shopping.friday.tw" {
						t.Fatal(req, r.Header)
					}
					return `[{"results":[{"prd_name":"認證 r12345"}],"all_cnts":41}]`, 200
				case "pchome":
					if strings.Contains(r.URL.Host, "ecshweb") {
						return `{"TotalPage":2,"Prods":[{"Id":"P1","Name":"商品"}]}`, 200
					}
					return `model T99999; BSMI R12345`, 200
				default:
					if strings.Contains(r.URL.Path, "GoodsDetail") {
						return `model T99999; BSMI R12345`, 200
					}
					chunk, _ := json.Marshal(`0:{"success":true,"rtnSearchData":{"maxPage":2,"goodsInfoList":[{"goodsCode":123,"goodsName":"商品"}]}}\ntrailing`)
					return "self.__next_f.push([1," + string(chunk) + "])", 200
				}
			})
			ids, err := c.Scan(context.Background(), source, 2)
			if err != nil || len(ids) != 1 || ids[0] != "R12345" || calls < 2 {
				t.Fatalf("scan %s: %v %v calls=%d", source, ids, err, calls)
			}
		})
	}
}

func TestRecentChangesRequest(t *testing.T) {
	c := mockClient(func(r *http.Request) (string, int) {
		r.ParseForm()
		if r.URL.String() != certURL || r.Form.Get("q_regNo") != "45879" || r.Form.Get("q_modDateS") != "115/10/17" {
			t.Fatal(r.URL, r.Form)
		}
		return `<div id="listContainer">共101筆</div>`, 200
	})
	changed, err := c.HasRecentChanges(context.Background(), "R45879", time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
}
