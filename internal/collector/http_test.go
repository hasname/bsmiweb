package collector

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
)

func testHTTPClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.HTTP.CloseIdleConnections)
	return c
}

func localTLSClient(t *testing.T, server *httptest.Server, trusted bool) *Client {
	t.Helper()
	roots := x509.NewCertPool()
	if trusted {
		roots.AddCert(server.Certificate())
	}
	transport, err := newTransport(
		tlsclient.WithTransportOptions(&tlsclient.TransportOptions{RootCAs: roots}),
		tlsclient.WithDialContext(func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{HTTP: &fhttp.Client{Transport: transport, Timeout: 5 * time.Second}}
	t.Cleanup(c.HTTP.CloseIdleConnections)
	return c
}

func TestHTTPPostRedirectAndCompression(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/result", http.StatusTemporaryRedirect)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || string(body) != "q=BSMI" ||
			r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" ||
			r.Header.Get("Referer") != "https://example.com/" ||
			r.UserAgent() != userAgent || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Errorf("request changed after redirect: %s %q %v", r.Method, body, r.Header)
		}
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		_, _ = writer.Write([]byte("測試資料"))
		_ = writer.Close()
	}))
	defer server.Close()
	c := testHTTPClient(t)
	body, err := c.request(context.Background(), "POST", server.URL+"/redirect", "q=BSMI", "application/x-www-form-urlencoded", "https://example.com/")
	if err != nil || body != "測試資料" {
		t.Fatalf("body=%q error=%v", body, err)
	}
}

func TestTLSFingerprintAndConcurrentHTTP2(t *testing.T) {
	var mu sync.Mutex
	var protos []string
	var ciphers []uint16
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.UserAgent() != userAgent {
			http.Error(w, "unexpected protocol or User-Agent", 500)
			return
		}
		fmt.Fprint(w, "OK")
	}))
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		protos = slices.Clone(hello.SupportedProtos)
		ciphers = slices.Clone(hello.CipherSuites)
		mu.Unlock()
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()
	c := localTLSClient(t, server, true)
	// Warm the connection, then exercise multiplexing with one shared client.
	if _, err := c.request(context.Background(), "GET", server.URL, "", "", ""); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := c.request(context.Background(), "GET", server.URL, "", "", "")
			if err != nil || body != "OK" {
				t.Errorf("body=%q error=%v", body, err)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(protos, []string{"h2", "http/1.1"}) {
		t.Fatal("unexpected ALPN protocols:", protos)
	}
	// Chrome's ClientHello includes GREASE; Go's default TLS client does not.
	if !slices.ContainsFunc(ciphers, func(v uint16) bool { return v&0x0f0f == 0x0a0a && byte(v) == byte(v>>8) }) {
		t.Fatal("browser TLS fingerprint missing GREASE cipher suite:", ciphers)
	}
}

func TestTLSExceptionIsScopedAcrossRedirects(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same-host":
			http.Redirect(w, r, "/", http.StatusFound)
		case "/other-host":
			http.Redirect(w, r, "https://untrusted.example/", http.StatusFound)
		default:
			fmt.Fprint(w, "OK")
		}
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	c := localTLSClient(t, server, false)
	for _, tc := range []struct {
		url     string
		wantErr bool
	}{
		{"https://data.bsmi.gov.tw/", false},
		{"https://data.bsmi.gov.tw/same-host", false},
		{"https://data.bsmi.gov.tw/other-host", true},
		{"https://untrusted.example/", true},
		{"https://data.bsmi.gov.tw.example/", true},
	} {
		t.Run(tc.url, func(t *testing.T) {
			body, err := c.request(context.Background(), "GET", tc.url, "", "", "")
			if tc.wantErr {
				var certError x509.UnknownAuthorityError
				var hostError x509.HostnameError
				if !errors.As(err, &certError) && !errors.As(err, &hostError) {
					t.Fatalf("expected certificate rejection, got %v", err)
				}
			} else if err != nil || body != "OK" {
				t.Fatalf("body=%q error=%v", body, err)
			}
		})
	}
}

func TestHTTPDeadlineAndCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint("deadline=", deadline), func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if deadline {
					fmt.Fprint(w, "partial body")
					w.(http.Flusher).Flush()
				}
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			c := testHTTPClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if deadline {
				c.HTTP.Timeout = 200 * time.Millisecond
			}
			done := make(chan error, 1)
			go func() {
				_, err := c.request(ctx, "GET", server.URL, "", "", "")
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not reach server")
			}
			want := context.DeadlineExceeded
			if !deadline {
				cancel()
				want = context.Canceled
			}
			select {
			case err := <-done:
				var timeout net.Error
				if deadline && errors.As(err, &timeout) && timeout.Timeout() {
					return
				}
				if !errors.Is(err, want) {
					t.Fatalf("want %v, got %v", want, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("request was not canceled")
			}
		})
	}
}
