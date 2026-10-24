package collector

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/hasname/bsmiweb/internal/db"
)

var certificateDatasets = []string{
	"https://data.bsmi.gov.tw/opendata/download/313000000G-000116-001.action",
	"https://data.bsmi.gov.tw/opendata/download/313000000G-000092-001.action",
	"https://data.bsmi.gov.tw/opendata/download/313000000G-000130-001.action",
}

const authorizationDataset = "https://data.bsmi.gov.tw/opendata/download/313000000G-000129-001.action"

func parseRows(body string) ([]map[string]string, error) {
	decoder := xml.NewDecoder(strings.NewReader(body))
	var rows []map[string]string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "row" {
			continue
		}
		// XMLName is needed for dynamic Chinese field names.
		var fields struct {
			Fields []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:",any"`
		}
		if err := decoder.DecodeElement(&fields, &start); err != nil {
			return nil, err
		}
		values := map[string]string{}
		for _, f := range fields.Fields {
			values[f.XMLName.Local] = strings.TrimSpace(f.Value)
		}
		rows = append(rows, values)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("XML dataset contains no rows")
	}
	return rows, nil
}

func toROCDate(value string) string {
	parts := strings.Split(value, "/")
	if len(parts) != 3 {
		return value
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil || year < 1911 {
		return value
	}
	return fmt.Sprintf("%03d%s%s", year-1911, parts[1], parts[2])
}

func (c *Client) ImportCertificates(ctx context.Context, s *db.Store) error {
	var certs []db.Certificate
	seen := map[string]bool{}
	for _, endpoint := range certificateDatasets {
		body, err := c.request(ctx, "GET", endpoint, "", "", "")
		if err != nil {
			return err
		}
		rows, err := parseRows(body)
		if err != nil {
			return err
		}
		valid := 0
		for _, r := range rows {
			id := r["證書號碼"]
			if id == "" {
				continue
			}
			valid++
			if seen[id] {
				continue
			}
			seen[id] = true
			certs = append(certs, db.Certificate{ID: id, ValidDate: toROCDate(r["證書期限"]), Status: r["證書狀態"], ProductName: r["商品名稱"], MainModel: r["主型式"]})
		}
		if valid == 0 {
			return fmt.Errorf("certificate dataset contains no valid IDs")
		}
	}
	return s.ImportCertificates(ctx, certs)
}

func (c *Client) ImportAuthorizations(ctx context.Context, s *db.Store) error {
	body, err := c.request(ctx, "GET", authorizationDataset, "", "", "")
	if err != nil {
		return err
	}
	rows, err := parseRows(body)
	if err != nil {
		return err
	}
	var auths []db.Authorization
	for _, r := range rows {
		if r["授權證號"] == "" {
			continue
		}
		auths = append(auths, db.Authorization{ID: r["授權證號"], CertificateID: r["證書編號"], AuthorizerName: r["授權人名稱"], MainModel: r["主型式"], AuthorizeeTaxID: r["被授權人統編"], AuthorizeeName: r["被授權人名稱"], AuthorizeeAddr: r["被授權人地址"], AuthorizeePhone: r["被授權人電話"], ValidDate: r["授權有效時間"]})
	}
	return s.ReplaceAuthorizations(ctx, auths)
}
