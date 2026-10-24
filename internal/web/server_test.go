package web

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hasname/bsmiweb/internal/db"
)

func sampleRegistration() *db.Registration {
	return &db.Registration{ID: "R45879", TaxID: "82781974", Applicant: "樂澤國際有限公司", ContactAddr: "桃園市", CompanyAddr: "桃園市", Phone: "03-3674356", Certificates: []db.Certificate{{ID: "CI450068790054", ProductName: "充電器", MainModel: "CH-866", SeriesModels: "A\nB"}}}
}
func testServer(t *testing.T, fetch FetchRegistration) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fetch == nil {
		fetch = func(context.Context, string) (*db.Registration, error) { return nil, nil }
	}
	app, err := New(s, fetch, "https://key@example.ingest.sentry.io/1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return app
}
func request(app http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestRoutesAndSearch(t *testing.T) {
	app := testServer(t, nil)
	ctx := context.Background()
	if err := app.Store.SaveRegistration(ctx, sampleRegistration()); err != nil {
		t.Fatal(err)
	}
	if err := app.Store.ReplaceAuthorizations(ctx, []db.Authorization{{ID: "AUTH", CertificateID: "CI450068790054", AuthorizeeTaxID: "82781974", AuthorizeeName: "授權公司"}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		status int
		text   string
	}{
		{"/", 200, "目前站上共有 1 筆檢驗標識、1 張證書、1 筆授權資料。"},
		{"/?q=樂澤", 200, "R45879"}, {"/?q=CH-866", 200, "CI450068790054"}, {"/?q=授權公司", 200, "AUTH"}, {"/?q=unmatched", 200, "無搜尋結果"},
		{"/bsmi/r45879", 200, "A, B"}, {"/bsmi/INVALID", 404, "Not Found"}, {"/bsmi/R00000", 404, "Not Found"},
		{"/ban/82781974", 200, "授權公司"}, {"/ban/12345678", 404, "Not Found"}, {"/ban/no", 404, "Not Found"},
		{"/atom.xml", 200, "R45879 — 樂澤國際有限公司"}, {"/sitemap.xml", 200, "/bsmi/R45879"}, {"/robots.txt", 200, "Sitemap: /sitemap.xml"}, {"/healthz", 200, "OK"}, {"/missing", 404, "Not Found"},
		{"/sentry.min.js", 200, "Sentry"}, {"/sentry-LICENSE.txt", 200, "MIT License"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := request(app, tc.path)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.text) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	w := request(app, "/?q=r45879")
	if w.Code != 302 || w.Header().Get("Location") != "/bsmi/R45879" {
		t.Fatal(w)
	}
	w = request(app, "/")
	// Minification must preserve the attributes used by form CSS selectors.
	for _, text := range []string{"p.hasname.com/js/pa-43fhI07VwO-gqJCOrcpFU.js", "/sentry.min.js", "最近更新", `<input type="text"`, `<button type="submit"`} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatal("missing " + text)
		}
	}
}

func TestLookupCachingAndEmptyResults(t *testing.T) {
	var calls atomic.Int32
	app := testServer(t, func(_ context.Context, id string) (*db.Registration, error) {
		calls.Add(1)
		if id == "R00000" {
			return nil, nil
		}
		return sampleRegistration(), nil
	})
	for range 2 {
		w := request(app, "/bsmi/r45879")
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatal("cached item fetched again")
	}
	for range 2 {
		w := request(app, "/bsmi/R00000")
		if w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("empty result cached")
	}
	counts, _ := app.Store.Counts(context.Background())
	if counts.Registrations != 1 || counts.Certificates != 1 {
		t.Fatal(counts)
	}
}

func TestStaleRefreshPreservesData(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "not found", true: "failed"}[failed], func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			app := testServer(t, func(context.Context, string) (*db.Registration, error) {
				close(started)
				<-release
				if failed {
					return nil, errors.New("unexpected upstream page")
				}
				return nil, nil
			})
			ctx := context.Background()
			if err := app.Store.SaveRegistration(ctx, sampleRegistration()); err != nil {
				t.Fatal(err)
			}
			if _, err := app.Store.DB.Exec("UPDATE registration SET updated_at='2020-01-01 00:00:00'"); err != nil {
				t.Fatal(err)
			}
			before, _ := app.Store.Registration(ctx, "R45879")
			w := request(app, "/bsmi/R45879")
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			<-started
			// Concurrent stale requests share one refresh while serving saved data.
			w = request(app, "/bsmi/R45879")
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			close(release)
			app.wg.Wait()
			after, _ := app.Store.Registration(ctx, "R45879")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed refresh changed cached data")
			}
		})
	}
}

func TestEscapingProxyHeadersAndCompression(t *testing.T) {
	app := testServer(t, nil)
	r := sampleRegistration()
	r.Applicant = `A & </script><script>alert("x")</script>`
	if err := app.Store.SaveRegistration(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	w := request(app, "/bsmi/R45879")
	match := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("structured data missing")
	}
	var organization map[string]any
	if err := json.Unmarshal([]byte(match[1]), &organization); err != nil || organization["name"] != r.Applicant || strings.Contains(match[1], "<") {
		t.Fatal("unsafe structured data", match[1], err)
	}
	for _, path := range []string{"/atom.xml", "/sitemap.xml"} {
		w := request(app, path)
		var doc any
		if err := xml.Unmarshal(w.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(w.Body.String(), "<script>") {
			t.Fatal("XML not escaped")
		}
	}
	for _, remote := range []string{"127.0.0.1:1234", "[::1]:1234", "192.0.2.1:1234"} {
		req := httptest.NewRequest("GET", "http://example.com/", nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
		scheme := "https"
		if strings.HasPrefix(remote, "192.") {
			scheme = "http"
		}
		if !strings.Contains(w.Body.String(), `href="`+scheme+`://example.com/"`) {
			t.Fatal("incorrect proxy trust", w.Body.String())
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("compression missing")
	}
	reader, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, _ := io.ReadAll(reader)
	if !strings.Contains(string(body), "搜尋") {
		t.Fatal("invalid compressed body")
	}
	req = httptest.NewRequest("HEAD", "/", nil)
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("HEAD returned body")
	}
}
