// Package huozhi 是「货殖」只读对接客户端。
//
// 只做一件事：按 API Key 读账单，供往来关联开销用。
// 刻意不实现写入——账单的增删改是账本里的事，牵丝一旦能改账，
// 两边口径就再也说不清了。货殖那边也只开了 /api/public/bills 这一组读接口，
// 同一组接口幕间也在用（按 id 取单张），所以这里不自己造第二套契约。
package huozhi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNotConfigured 未配置货殖地址或密钥。
var ErrNotConfigured = errors.New("未配置货殖地址或 API Key")

// ErrUnauthorized 货殖拒绝了密钥（未生成、被禁用、已重置）。
var ErrUnauthorized = errors.New("货殖拒绝了该 API Key")

// ErrNotFound 货殖里没有这张账单（已删除或不属于该密钥的账号）。
var ErrNotFound = errors.New("货殖没有这张账单")

// Bill 货殖账单。货殖对外的字段口径：
//   - amount：元（浮点，历史字段，幕间在用）
//   - amount_fen：分（整数，牵丝一律用这个）
//   - tx_date："2006-01-02 15:04:05"，牵丝只取日期部分
//
// 老版本货殖没有 amount_fen / category / account / book，取值时自行兜底。
type Bill struct {
	ID           uint    `json:"id"`
	AmountFen    int     `json:"amount_fen"`
	Amount       float64 `json:"amount"`
	Type         string  `json:"type"`
	TxDate       string  `json:"tx_date"`
	Description  string  `json:"description"`
	Merchant     string  `json:"merchant"`
	Remark       string  `json:"remark"`
	CategoryName string  `json:"category"`
	AccountName  string  `json:"account"`
	BookName     string  `json:"book"`
}

// normalize 把不同版本货殖的字段差异抹平：
// 没有 amount_fen 时按「元」换算成分（四舍五入，避免 88.1*100=8809.999…），
// tx_date 统一截成日期，分类/账户/账本缺失时留空串。
func (b *Bill) normalize() {
	if b.AmountFen == 0 && b.Amount != 0 {
		b.AmountFen = int(math.Round(b.Amount * 100))
	}
	if len(b.TxDate) >= 10 {
		b.TxDate = b.TxDate[:10]
	}
}

// Query 搜索条件。日期是本地日期字符串（YYYY-MM-DD），由货殖按自己的时区裁决。
type Query struct {
	Keyword  string
	From     string
	To       string
	Type     string
	Page     int
	PageSize int
}

// ListResult 搜索结果。
type ListResult struct {
	Items    []Bill
	Total    int
	Page     int
	PageSize int
}

// Client 固定的 base/key 组合，调用方每次请求现构造即可（无连接以外的状态）。
type Client struct {
	base string
	key  string
	http *http.Client
}

// New 规范化地址：允许用户填 https://host、带尾斜杠、或直接粘 /api 结尾。
func New(base, apiKey string) (*Client, error) {
	base = strings.TrimSpace(base)
	apiKey = strings.TrimSpace(apiKey)
	if base == "" || apiKey == "" {
		return nil, ErrNotConfigured
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(base, "/api")
	if u, err := url.Parse(base); err != nil || u.Host == "" {
		return nil, fmt.Errorf("货殖地址不合法: %s", base)
	}
	return &Client{base: base, key: apiKey, http: &http.Client{Timeout: 8 * time.Second}}, nil
}

// Base 返回规范化后的地址，用于展示与拼跳转链接。
func (c *Client) Base() string { return c.base }

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type listPayload struct {
	List       []Bill `json:"list"`
	Pagination struct {
		Page     int   `json:"page"`
		PageSize int   `json:"page_size"`
		Total    int64 `json:"total"`
	} `json:"pagination"`
}

// List 按条件搜索账单。
func (c *Client) List(ctx context.Context, q Query) (*ListResult, error) {
	vals := url.Values{}
	if q.Keyword != "" {
		vals.Set("keyword", q.Keyword)
	}
	if q.From != "" {
		vals.Set("start_date", q.From)
	}
	if q.To != "" {
		vals.Set("end_date", q.To)
	}
	if q.Type != "" {
		vals.Set("type", q.Type)
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.PageSize
	if size < 1 || size > 50 {
		size = 20
	}
	vals.Set("page", strconv.Itoa(page))
	vals.Set("page_size", strconv.Itoa(size))

	var out listPayload
	if err := c.get(ctx, "/api/public/bills?"+vals.Encode(), &out); err != nil {
		return nil, err
	}
	for i := range out.List {
		out.List[i].normalize()
	}
	pg := out.Pagination
	if pg.Page < 1 {
		pg.Page = page
	}
	return &ListResult{Items: out.List, Total: int(pg.Total), Page: pg.Page, PageSize: pg.PageSize}, nil
}

// ByIDs 按 id 批量取（刷新快照用）。货殖侧会忽略不存在或无权访问的 id。
func (c *Client) ByIDs(ctx context.Context, ids []string) ([]Bill, error) {
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		return nil, nil
	}
	var out listPayload
	if err := c.get(ctx, "/api/public/bills?ids="+url.QueryEscape(strings.Join(clean, ",")), &out); err != nil {
		return nil, err
	}
	for i := range out.List {
		out.List[i].normalize()
	}
	return out.List, nil
}

// One 取单条。不存在时返回 ErrNotFound。
func (c *Client) One(ctx context.Context, id string) (*Bill, error) {
	var out Bill
	if err := c.get(ctx, "/api/public/bills/"+url.PathEscape(strings.TrimSpace(id)), &out); err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	out.normalize()
	return &out, nil
}

var errUpstreamNotFound = errors.New("not found")

func (c *Client) get(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	// 货殖的对接端点只认 X-API-Key；Authorization: Bearer 在那边是 JWT 通道，
	// 拿 API Key 走 Bearer 会被当成过期会话，别混用。
	req.Header.Set("X-API-Key", c.key)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("连不上货殖：%w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return errUpstreamNotFound
	case resp.StatusCode >= 400:
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("货殖返回 %d：%s", resp.StatusCode, msg)
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("货殖响应不是合法 JSON：%w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("货殖返回错误 %d：%s", env.Code, env.Message)
	}
	if err := json.Unmarshal(env.Data, dst); err != nil {
		return fmt.Errorf("货殖响应结构不符：%w", err)
	}
	return nil
}
