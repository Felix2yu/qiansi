package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/huozhi"
	"github.com/qiansi/app/internal/store"
)

// ===== 货殖账单关联 =====
//
// 关系：一个往来 ↔ N 张货殖账单。账单本体在货殖，牵丝只存「引用 + 快照」，
// 所以这里所有读路径优先用本地快照，只有搜索与刷新才真正打货殖。
// 密钥只留在服务端，前端永远拿不到——否则任何能打开页面的人都能读你的账本。

const (
	settingHuozhiBase = "huozhi_base_url"
	settingHuozhiKey  = "huozhi_api_key"
)

func (a *API) registerHuozhi(r chi.Router) {
	r.Route("/api/v1/huozhi", func(r chi.Router) {
		r.Get("/status", a.huozhiStatus)
		r.Get("/bills", a.huozhiBills)
	})
}

// huozhiClient 按当前设置构造客户端。未配置时返回 huozhi.ErrNotConfigured，
// 由调用方翻译成「去设置页填地址和密钥」的提示。
func (a *API) huozhiClient(ctx context.Context) (*huozhi.Client, error) {
	base, err := a.Store.SettingGet(ctx, settingHuozhiBase)
	if err != nil {
		return nil, err
	}
	key, err := a.Store.SettingGet(ctx, settingHuozhiKey)
	if err != nil {
		return nil, err
	}
	return huozhi.New(base, key)
}

func writeHuozhiErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, huozhi.ErrNotConfigured):
		writeErr(w, 400, "还没有配置货殖：设置 → 货殖账单，填入地址与 API Key")
	case errors.Is(err, huozhi.ErrUnauthorized):
		writeErr(w, 502, "货殖拒绝了这个 API Key（未生成或已禁用），请到货殖设置里重新生成")
	default:
		writeErr(w, 502, err.Error())
	}
}

// huozhiStatus 设置页的「测试连接」。
func (a *API) huozhiStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	base, _ := a.Store.SettingGet(ctx, settingHuozhiBase)
	key, _ := a.Store.SettingGet(ctx, settingHuozhiKey)
	if strings.TrimSpace(base) == "" || strings.TrimSpace(key) == "" {
		writeJSON(w, 200, map[string]any{"configured": false, "ok": false, "base": base})
		return
	}
	client, err := a.huozhiClient(ctx)
	if err != nil {
		writeJSON(w, 200, map[string]any{"configured": true, "ok": false, "base": base, "error": err.Error()})
		return
	}
	// 一次最小查询：能拿到结果就说明地址、网络、密钥三件事都通。
	_, err = client.List(ctx, huozhi.Query{Page: 1, PageSize: 1})
	if err != nil {
		writeJSON(w, 200, map[string]any{"configured": true, "ok": false, "base": client.Base(), "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"configured": true, "ok": true, "base": client.Base()})
}

// huozhiBillDTO 是给浏览器的账单视图。
// 不直接把货殖的响应结构透传出去：上游字段名（category/account/book）属于货殖的契约，
// 牵丝前端按自己的名字（category_name/…）消费，日后货殖改名也不会波及前端。
type huozhiBillDTO struct {
	ID           uint   `json:"id"`
	AmountFen    int    `json:"amount_fen"`
	Type         string `json:"type"`
	TxDate       string `json:"tx_date"`
	Description  string `json:"description"`
	Merchant     string `json:"merchant"`
	CategoryName string `json:"category_name"`
	AccountName  string `json:"account_name"`
	BookName     string `json:"book_name"`
}

func toHuozhiBillDTO(b huozhi.Bill) huozhiBillDTO {
	return huozhiBillDTO{
		ID:           b.ID,
		AmountFen:    b.AmountFen,
		Type:         b.Type,
		TxDate:       b.TxDate,
		Description:  b.Description,
		Merchant:     b.Merchant,
		CategoryName: b.CategoryName,
		AccountName:  b.AccountName,
		BookName:     b.BookName,
	}
}

// huozhiBills 搜索货殖账单（关联选择器用）。返回体额外带上 base 供前端拼跳转链接。
func (a *API) huozhiBills(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	client, err := a.huozhiClient(ctx)
	if err != nil {
		writeHuozhiErr(w, err)
		return
	}
	q := huozhi.Query{
		Keyword:  strings.TrimSpace(r.URL.Query().Get("q")),
		From:     r.URL.Query().Get("from"),
		To:       r.URL.Query().Get("to"),
		Type:     r.URL.Query().Get("type"),
		Page:     parseIntQuery(r, "page", 1),
		PageSize: parseIntQuery(r, "page_size", 20),
	}
	res, err := client.List(ctx, q)
	if err != nil {
		writeHuozhiErr(w, err)
		return
	}
	items := make([]huozhiBillDTO, 0, len(res.Items))
	for _, b := range res.Items {
		items = append(items, toHuozhiBillDTO(b))
	}
	writeJSON(w, 200, map[string]any{
		"items":     items,
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
		"base":      client.Base(),
	})
}

// eventBillsLink POST /api/v1/events/{id}/bills
//
// 支持一次关联多张：remote_ids 里的每一个都会先向货殖取回最新快照再落库，
// 不信任前端传来的金额/名称——那样一旦前端版本不一致，快照就会和账本对不上。
func (a *API) eventBillsLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	eventID := chi.URLParam(r, "id")
	var body struct {
		RemoteIDs []string `json:"remote_ids"`
		RemoteID  string   `json:"remote_id"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ids := body.RemoteIDs
	if body.RemoteID != "" {
		ids = append(ids, body.RemoteID)
	}
	if len(ids) == 0 {
		writeErr(w, 400, "remote_ids 不能为空")
		return
	}
	if _, err := a.Store.EventGet(ctx, eventID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, 404, "not found")
			return
		}
		writeStoreErr(w, err)
		return
	}
	client, err := a.huozhiClient(ctx)
	if err != nil {
		writeHuozhiErr(w, err)
		return
	}
	bills, err := client.ByIDs(ctx, ids)
	if err != nil {
		writeHuozhiErr(w, err)
		return
	}
	found := map[string]bool{}
	for _, b := range bills {
		remote := strconv.FormatUint(uint64(b.ID), 10)
		found[remote] = true
		if _, err := a.Store.EventBillLink(ctx, &store.EventBill{
			EventID:      eventID,
			RemoteID:     remote,
			RemoteBase:   client.Base(),
			AmountFen:    b.AmountFen,
			Type:         b.Type,
			TxDate:       b.TxDate,
			Description:  b.Description,
			CategoryName: b.CategoryName,
			AccountName:  b.AccountName,
			BookName:     b.BookName,
		}); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	// 货殖里找不到 / 无权访问的 id 要如实回报：静默当成功会让用户以为挂上了
	var missing []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !found[id] {
			missing = append(missing, id)
		}
	}
	list, err := a.Store.EventBills(ctx, eventID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"bills": list, "missing": missing})
}

// eventBillsRefresh POST /api/v1/events/{id}/bills/refresh
// 按已关联的 remote_id 批量重取快照；货殖里已删除的账单会出现在 stale 里，等用户决定去留。
func (a *API) eventBillsRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	eventID := chi.URLParam(r, "id")
	ids, err := a.Store.EventBillRemoteIDs(ctx, eventID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if len(ids) == 0 {
		writeJSON(w, 200, map[string]any{"bills": []*store.EventBill{}, "stale": []string{}})
		return
	}
	client, err := a.huozhiClient(ctx)
	if err != nil {
		writeHuozhiErr(w, err)
		return
	}
	bills, err := client.ByIDs(ctx, ids)
	if err != nil {
		writeHuozhiErr(w, err)
		return
	}
	found := map[string]bool{}
	existing, _ := a.Store.EventBills(ctx, eventID)
	byRemote := map[string]*store.EventBill{}
	for _, b := range existing {
		byRemote[b.RemoteID] = b
	}
	for _, b := range bills {
		remote := strconv.FormatUint(uint64(b.ID), 10)
		found[remote] = true
		row, ok := byRemote[remote]
		if !ok {
			continue
		}
		if err := a.Store.EventBillRefresh(ctx, eventID, row.ID, &store.EventBill{
			RemoteBase:   client.Base(),
			AmountFen:    b.AmountFen,
			Type:         b.Type,
			TxDate:       b.TxDate,
			Description:  b.Description,
			CategoryName: b.CategoryName,
			AccountName:  b.AccountName,
			BookName:     b.BookName,
		}); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	stale := []string{}
	for _, id := range ids {
		if !found[id] {
			stale = append(stale, id)
		}
	}
	list, err := a.Store.EventBills(ctx, eventID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"bills": list, "stale": stale})
}

// eventBillsUnlink DELETE /api/v1/events/{id}/bills/{billID}
func (a *API) eventBillsUnlink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	eventID := chi.URLParam(r, "id")
	billID := chi.URLParam(r, "billID")
	if err := a.Store.EventBillUnlink(ctx, eventID, billID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, 404, "not found")
			return
		}
		writeStoreErr(w, err)
		return
	}
	list, err := a.Store.EventBills(ctx, eventID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}
