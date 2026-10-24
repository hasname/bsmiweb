package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var idRE = regexp.MustCompile(`(?i)[RTDQM]\d{5}`)
var contextRE = regexp.MustCompile(`(?i)(?:BSMI|檢驗|認證|標檢局|登錄字號|商檢字號)[^\n]{0,30}([RTDQM]\d{5})`)
var booksPageRE = regexp.MustCompile(`/page/(\d+)/`)
var momoChunkRE = regexp.MustCompile(`self\.__next_f\.push\(\[1,("(?:\\.|[^"\\])*")\]\)`)

func ExtractIDs(text string, withContext bool) []string {
	ids := map[string]bool{}
	if withContext {
		for _, m := range contextRE.FindAllStringSubmatch(text, -1) {
			ids[strings.ToUpper(m[1])] = true
		}
	} else {
		for _, id := range idRE.FindAllString(text, -1) {
			ids[strings.ToUpper(id)] = true
		}
	}
	return sortedIDs(ids)
}
func sortedIDs(ids map[string]bool) []string {
	values := make([]string, 0, len(ids))
	for id := range ids {
		values = append(values, id)
	}
	sort.Strings(values)
	return values
}

type product struct{ ID, Name string }
type searchPage struct {
	Products []product
	IDs      []string
	Pages    int
}

func (c *Client) Scan(ctx context.Context, source string, maxPages int) ([]string, error) {
	ids := map[string]bool{}
	pages := 1
	for page := 1; page <= min(maxPages, pages); page++ {
		var result searchPage
		var err error
		switch source {
		case "books":
			result, err = c.books(ctx, page)
		case "friday":
			result, err = c.friday(ctx, page)
		case "momo":
			result, err = c.momo(ctx, page)
		case "pchome":
			result, err = c.pchome(ctx, page)
		default:
			return nil, fmt.Errorf("unknown source %q", source)
		}
		if err != nil {
			return nil, err
		}
		pages = result.Pages
		for _, id := range result.IDs {
			ids[id] = true
		}
		for _, p := range result.Products {
			found := ExtractIDs(p.Name, false)
			if len(found) == 0 && (source == "momo" || source == "pchome") {
				if err := c.pause(ctx, time.Second); err != nil {
					return nil, err
				}
				endpoint, referer := "https://24h.pchome.com.tw/prod/"+url.PathEscape(p.ID), "https://24h.pchome.com.tw/search/?q=bsmi"
				if source == "momo" {
					endpoint = "https://www.momoshop.com.tw/goods/GoodsDetail.jsp?i_code=" + url.QueryEscape(p.ID)
					referer = "https://www.momoshop.com.tw/search/bsmi"
				}
				body, err := c.request(ctx, "GET", endpoint, "", "", referer)
				if err != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					continue
				}
				found = ExtractIDs(body, true)
			}
			for _, id := range found {
				ids[id] = true
			}
		}
		if source != "books" && len(result.Products) == 0 {
			break
		}
		if err := c.pause(ctx, 1500*time.Millisecond); err != nil {
			return nil, err
		}
	}
	return sortedIDs(ids), nil
}

func (c *Client) books(ctx context.Context, page int) (searchPage, error) {
	body, err := c.request(ctx, "GET", fmt.Sprintf("https://search.books.com.tw/search/query/cat/all/sort/5/v/1/spell/3/key/bsmi/page/%d", page), "", "", "https://www.books.com.tw/")
	if err != nil {
		return searchPage{}, err
	}
	result := searchPage{IDs: ExtractIDs(body, false), Pages: 1}
	for _, m := range booksPageRE.FindAllStringSubmatch(body, -1) {
		n, _ := strconv.Atoi(m[1])
		result.Pages = max(result.Pages, n)
	}
	return result, nil
}

func (c *Client) friday(ctx context.Context, page int) (searchPage, error) {
	body, err := c.request(ctx, "POST", "https://aisearch-web.shopping.friday.tw/aisearch", fmt.Sprintf(`{"remote":"w","sorting":"RELEVANT","page":%d,"size":40,"kws_phrase64":"YnNtaQ=="}`, page), "application/json", "https://ec-w.shopping.friday.tw/")
	if err != nil {
		return searchPage{}, err
	}
	var data []struct {
		Results []struct {
			Name string `json:"prd_name"`
		} `json:"results"`
		Total int `json:"all_cnts"`
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return searchPage{}, err
	}
	result := searchPage{Pages: 1}
	if len(data) == 0 {
		return result, nil
	}
	result.Pages = (data[0].Total + 39) / 40
	for _, p := range data[0].Results {
		result.Products = append(result.Products, product{Name: p.Name})
	}
	return result, nil
}

func (c *Client) pchome(ctx context.Context, page int) (searchPage, error) {
	body, err := c.request(ctx, "GET", fmt.Sprintf("https://ecshweb.pchome.com.tw/search/v4.3/all/results?q=bsmi&page=%d&sort=new/dc", page), "", "", "https://24h.pchome.com.tw/")
	if err != nil {
		return searchPage{}, err
	}
	var data struct {
		TotalPage int
		Prods     []struct{ Id, Name, Describe string }
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return searchPage{}, err
	}
	result := searchPage{Pages: max(1, data.TotalPage)}
	for _, p := range data.Prods {
		result.Products = append(result.Products, product{p.Id, p.Name + " " + p.Describe})
	}
	return result, nil
}

type stringOrNumber string

func (s *stringOrNumber) UnmarshalJSON(raw []byte) error {
	var v string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
	} else {
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			return err
		}
		v = n.String()
	}
	*s = stringOrNumber(v)
	return nil
}

func (c *Client) momo(ctx context.Context, page int) (searchPage, error) {
	body, err := c.request(ctx, "GET", fmt.Sprintf("https://www.momoshop.com.tw/search/bsmi?_isFuzzy=0&searchType=5&curPage=%d", page), "", "", "https://www.momoshop.com.tw/")
	if err != nil {
		return searchPage{}, err
	}
	chunks := momoChunkRE.FindAllStringSubmatch(body, -1)
	for _, chunk := range chunks {
		var text string
		if err := json.Unmarshal([]byte(chunk[1]), &text); err != nil {
			return searchPage{}, err
		}
		if !strings.Contains(text, "rtnSearchData") {
			continue
		}
		start := strings.Index(text, `{"success"`)
		if start < 0 {
			continue
		}
		var data struct {
			Search struct {
				MaxPage int `json:"maxPage"`
				Goods   []struct {
					Code    stringOrNumber `json:"goodsCode"`
					Name    string         `json:"goodsName"`
					SubName string         `json:"goodsSubName"`
				} `json:"goodsInfoList"`
			} `json:"rtnSearchData"`
		}
		if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&data); err != nil {
			return searchPage{}, err
		}
		result := searchPage{Pages: max(1, data.Search.MaxPage)}
		for _, p := range data.Search.Goods {
			result.Products = append(result.Products, product{string(p.Code), p.Name + " " + p.SubName})
		}
		return result, nil
	}
	return searchPage{}, fmt.Errorf("Momo search data not found")
}
