package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/hasname/bsmiweb/internal/collector"
	"github.com/hasname/bsmiweb/internal/db"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	htmlmin "github.com/tdewolff/minify/v2/html"
)

//go:embed templates/*.html static/*
var assets embed.FS

type FetchRegistration func(context.Context, string) (*db.Registration, error)
type Server struct {
	Store      *db.Store
	fetch      FetchRegistration
	router     *gin.Engine
	templates  *template.Template
	minifier   *minify.M
	sentryDSN  string
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	refreshing map[string]bool
	closed     bool
	wg         sync.WaitGroup
}
type page struct {
	Query, CanonicalURL, SentryDSN, TaxID string
	Counts                                db.Counts
	Recent, Registrations                 []db.Registration
	Registration                          *db.Registration
	Certificates                          []db.Certificate
	Authorizations                        []db.Authorization
	Organization                          map[string]any
}

func New(store *db.Store, fetch FetchRegistration, sentryDSN string) (*Server, error) {
	t, err := template.New("").Funcs(template.FuncMap{"series": func(s string) string {
		var parts []string
		for _, p := range strings.Split(s, "\n") {
			if p != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, ", ")
	}}).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{Store: store, fetch: fetch, templates: t, sentryDSN: sentryDSN, ctx: ctx, cancel: cancel, refreshing: map[string]bool{}, minifier: minify.New()}
	// CSS attribute selectors depend on explicit defaults such as input[type="text"].
	s.minifier.Add("text/html", &htmlmin.Minifier{KeepDefaultAttrVals: true, KeepDocumentTags: true, KeepEndTags: true, KeepQuotes: true})
	s.minifier.Add("text/css", &css.Minifier{})
	r := gin.New()
	s.router = r
	if err := r.SetTrustedProxies([]string{"127.0.0.0/8", "::1/128"}); err != nil {
		cancel()
		return nil, err
	}
	r.Use(gin.Recovery(), sentrygin.New(sentrygin.Options{Repanic: true}), func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'self'; base-uri 'self'; font-src 'self' https: data:; form-action 'self'; frame-ancestors 'self'; img-src 'self' data:; object-src 'none'; script-src 'self' 'unsafe-inline' https://p.hasname.com; script-src-attr 'none'; style-src 'self' https: 'unsafe-inline'; connect-src 'self' https://*.ingest.sentry.io https://p.hasname.com; upgrade-insecure-requests")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Header("Cross-Origin-Opener-Policy", "same-origin")
		c.Header("Cross-Origin-Resource-Policy", "same-origin")
	})
	for path, handler := range map[string]gin.HandlerFunc{"/": s.index, "/bsmi/:id": s.item, "/ban/:id": s.ban, "/atom.xml": s.atom, "/sitemap.xml": s.sitemap, "/healthz": s.health, "/robots.txt": s.static, "/sentry.min.js": s.static, "/sentry-LICENSE.txt": s.static} {
		r.GET(path, handler)
		r.HEAD(path, handler)
	}
	r.NoRoute(func(c *gin.Context) { c.String(http.StatusNotFound, "Not Found") })
	return s, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Server) fail(c *gin.Context, err error) {
	log.Printf("request failed: %v", err)
	if hub := sentrygin.GetHubFromContext(c); hub != nil {
		hub.CaptureException(err)
	}
	c.String(500, "Internal Server Error")
}
func (s *Server) render(c *gin.Context, name string, p page) {
	p.SentryDSN = s.sentryDSN
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name+".html", p); err != nil {
		s.fail(c, err)
		return
	}
	body := buf.Bytes()
	if minified, err := s.minifier.Bytes("text/html", body); err == nil {
		body = minified
	}
	s.send(c, "text/html; charset=utf-8", body)
}
func (s *Server) send(c *gin.Context, contentType string, body []byte) {
	c.Header("Vary", "Accept-Encoding")
	for _, part := range strings.Split(c.GetHeader("Accept-Encoding"), ",") {
		encoding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if encoding != "gzip" || len(body) < 1024 {
			continue
		}
		quality := 1.0
		for _, parameter := range strings.Split(params, ";") {
			key, value, _ := strings.Cut(strings.TrimSpace(parameter), "=")
			if key == "q" {
				quality, _ = strconv.ParseFloat(value, 64)
			}
		}
		if quality <= 0 || quality > 1 {
			continue
		}
		var buf bytes.Buffer
		writer := gzip.NewWriter(&buf)
		_, err := writer.Write(body)
		if err == nil {
			err = writer.Close()
		}
		if err == nil {
			body = buf.Bytes()
			c.Header("Content-Encoding", "gzip")
		}
		break
	}
	c.Header("Content-Length", fmt.Sprint(len(body)))
	if c.Request.Method == "HEAD" {
		c.Header("Content-Type", contentType)
		c.Status(200)
		return
	}
	c.Data(200, contentType, body)
}
func baseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err == nil && net.ParseIP(host).IsLoopback() {
		proto := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0])
		if proto == "https" || proto == "http" {
			scheme = proto
		}
	}
	return scheme + "://" + c.Request.Host
}
func cache(c *gin.Context) { c.Header("Cache-Control", "public, max-age=3600") }

func (s *Server) index(c *gin.Context) {
	ctx := c.Request.Context()
	q := strings.TrimSpace(c.Query("q"))
	if collector.ValidID.MatchString(q) {
		c.Redirect(302, "/bsmi/"+strings.ToUpper(q))
		return
	}
	p := page{Query: q, CanonicalURL: baseURL(c) + "/"}
	var err error
	if q == "" {
		p.Recent, err = s.Store.Recent(ctx)
	} else {
		p.Registrations, p.Certificates, p.Authorizations, err = s.Store.Search(ctx, q)
	}
	if err != nil {
		s.fail(c, err)
		return
	}
	p.Counts, err = s.Store.Counts(ctx)
	if err != nil {
		s.fail(c, err)
		return
	}
	s.render(c, "index", p)
}

func (s *Server) item(c *gin.Context) {
	ctx := c.Request.Context()
	id := strings.ToUpper(c.Param("id"))
	if !collector.ValidID.MatchString(id) {
		c.String(404, "Not Found")
		return
	}
	r, err := s.Store.Registration(ctx, id)
	if err != nil {
		s.fail(c, err)
		return
	}
	if r == nil {
		r, err = s.fetch(ctx, id)
		if err != nil {
			s.fail(c, err)
			return
		}
		if r == nil {
			c.String(404, "Not Found")
			return
		}
		if err = s.Store.SaveRegistration(ctx, r); err != nil {
			s.fail(c, err)
			return
		}
	} else if time.Since(r.UpdatedAt.Time) > 24*time.Hour {
		s.refresh(id)
	}
	auths, err := s.Store.AuthorizationsByRegistration(ctx, id)
	if err != nil {
		s.fail(c, err)
		return
	}
	canonical := baseURL(c) + "/bsmi/" + r.ID
	p := page{Registration: r, Authorizations: auths, CanonicalURL: canonical, Organization: map[string]any{"@context": "https://schema.org", "@type": "Organization", "name": r.Applicant, "taxID": r.TaxID, "telephone": r.Phone, "address": map[string]string{"@type": "PostalAddress", "streetAddress": r.CompanyAddr}, "url": canonical}}
	cache(c)
	s.render(c, "item", p)
}

func (s *Server) refresh(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.refreshing[id] {
		return
	}
	s.refreshing[id] = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { s.mu.Lock(); delete(s.refreshing, id); s.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
		defer cancel()
		r, err := s.fetch(ctx, id)
		if err == nil && r != nil {
			err = s.Store.SaveRegistration(ctx, r)
		}
		if err != nil {
			log.Printf("background refresh %s: %v", id, err)
			sentry.CaptureException(err)
		}
	}()
}

var taxIDRE = regexp.MustCompile(`^\d{8}$`)

func (s *Server) ban(c *gin.Context) {
	id := c.Param("id")
	if !taxIDRE.MatchString(id) {
		c.String(404, "Not Found")
		return
	}
	regs, err := s.Store.ByTaxID(c.Request.Context(), id)
	if err != nil {
		s.fail(c, err)
		return
	}
	if len(regs) == 0 {
		c.String(404, "Not Found")
		return
	}
	auths, err := s.Store.AuthorizationsByTaxID(c.Request.Context(), id)
	if err != nil {
		s.fail(c, err)
		return
	}
	cache(c)
	s.render(c, "ban", page{TaxID: id, Registrations: regs, Authorizations: auths, CanonicalURL: baseURL(c) + "/ban/" + id})
}
func (s *Server) health(c *gin.Context) {
	if err := s.Store.DB.PingContext(c.Request.Context()); err != nil {
		c.String(503, "Unavailable")
		return
	}
	c.String(200, "OK")
}
func (s *Server) static(c *gin.Context) {
	name := strings.TrimPrefix(c.Request.URL.Path, "/")
	body, err := assets.ReadFile("static/" + name)
	if err != nil {
		c.String(404, "Not Found")
		return
	}
	typ := "text/plain; charset=utf-8"
	if strings.HasSuffix(name, ".js") {
		typ = "application/javascript; charset=utf-8"
		cache(c)
	}
	s.send(c, typ, body)
}
