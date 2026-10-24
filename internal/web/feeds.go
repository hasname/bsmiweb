package web

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

func escape(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

func (s *Server) sitemap(c *gin.Context) {
	regs, err := s.Store.All(c.Request.Context(), false)
	if err != nil {
		s.fail(c, err)
		return
	}
	base := baseURL(c)
	var buf bytes.Buffer
	buf.WriteString(xml.Header + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	fmt.Fprintf(&buf, "<url><loc>%s/</loc></url>", escape(base))
	for _, r := range regs {
		fmt.Fprintf(&buf, "<url><loc>%s/bsmi/%s</loc><lastmod>%s</lastmod></url>", escape(base), escape(r.ID), r.UpdatedAt.Format("2006-01-02"))
	}
	buf.WriteString("</urlset>")
	cache(c)
	s.send(c, "application/xml; charset=utf-8", buf.Bytes())
}

func (s *Server) atom(c *gin.Context) {
	regs, err := s.Store.Recent(c.Request.Context())
	if err != nil {
		s.fail(c, err)
		return
	}
	base := baseURL(c)
	updated := time.Now().UTC()
	if len(regs) > 0 {
		updated = regs[0].UpdatedAt.Time
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header + `<feed xmlns="http://www.w3.org/2005/Atom">`)
	fmt.Fprintf(&buf, `<title>BSMI 檢驗標識更新</title><id>%s/atom.xml</id><updated>%s</updated><link rel="self" href="%s/atom.xml"/><link rel="alternate" href="%s"/>`, escape(base), updated.Format(time.RFC3339Nano), escape(base), escape(base))
	for _, r := range regs {
		link := escape(base + "/bsmi/" + r.ID)
		fmt.Fprintf(&buf, `<entry><title>%s</title><id>%s</id><updated>%s</updated><link href="%s"/><summary>%s</summary></entry>`, escape(r.ID+" — "+r.Applicant), link, r.UpdatedAt.Format(time.RFC3339Nano), link, escape(fmt.Sprintf("統編 %s，共 %d 張證書", r.TaxID, r.CertificateCount)))
	}
	buf.WriteString("</feed>")
	cache(c)
	s.send(c, "application/atom+xml; charset=utf-8", buf.Bytes())
}
