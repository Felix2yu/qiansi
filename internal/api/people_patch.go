package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

// 人物字段的局部更新：PATCH 改一个人，bulk-update 改一批，共用同一张白名单。
//
// 为什么不能拿 PUT 凑：PUT /people/{id} 是整行覆盖，只带一个键的 body 会把其余列
// 写成零值，而 preparePerson 还要求 name 非空。给 116 个人补性别这种活儿，
// 只能是「只写点名的那一列」。

// patchField 白名单里的一个字段：落成哪一列、怎么校验收口、什么算「还没填」。
type patchField struct {
	column    string
	emptyCond string
	norm      func(json.RawMessage) (any, error)
}

func patchText(raw json.RawMessage) (any, error) {
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, errors.New("要一个字符串")
	}
	return v, nil
}

func patchGender(raw json.RawMessage) (any, error) {
	v, err := patchText(raw)
	if err != nil {
		return nil, err
	}
	return normGender(v.(string))
}

func patchGrade(raw json.RawMessage) (any, error) {
	var v int
	if err := json.Unmarshal(raw, &v); err != nil || v < 0 || v > 5 {
		return nil, errors.New("亲密度等级只能是 0–5")
	}
	return v, nil
}

// personPatchFields 窄是故意的：生日、电话、微信对一群人写同一个值没有意义，
// 备注覆盖不可逆，引荐人要跑环检测，圈子已有 bulk-categories 那条路。
var personPatchFields = map[string]patchField{
	"gender":   {column: "gender", emptyCond: "gender IS NULL OR gender=''", norm: patchGender},
	"grade":    {column: "grade", emptyCond: "grade=0", norm: patchGrade},
	"location": {column: "location", emptyCond: "location IS NULL OR location=''", norm: patchText},
}

// personPatchColumns 把 set 收成待更新的列。未知键必须报错而不是跳过：
// 静默丢掉一个键的话，界面会提示「已更新 N 人」而那一列其实一个字没动。
// 按键名排序是为了让多列更新时的 SQL 和报错文案稳定，不随 map 迭代顺序变。
func personPatchColumns(set map[string]json.RawMessage) ([]store.PeoplePatchColumn, error) {
	if len(set) == 0 {
		return nil, errors.New("set 要至少一个字段")
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	cols := make([]store.PeoplePatchColumn, 0, len(names))
	for _, name := range names {
		f, ok := personPatchFields[name]
		if !ok {
			return nil, fmt.Errorf("不支持这样改的字段: %s", name)
		}
		v, err := f.norm(set[name])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		cols = append(cols, store.PeoplePatchColumn{Column: f.column, Value: v, EmptyCond: f.emptyCond})
	}
	return cols, nil
}

// peoplePatch 单人局部更新：PATCH /api/v1/people/{id} {"gender":"M"}。
// 成功返回整条人物，调用方直接拿它刷新手头那一行，省一次 GET。
func (a *API) peoplePatch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var set map[string]json.RawMessage
	if err := decode(r, &set); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	cols, err := personPatchColumns(set)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// 没勾 only_empty 时，命中的活人必然改动一行；0 行就是这个人不在（或在回收站里）。
	n, err := a.Store.PeoplePatch(r.Context(), []string{id}, cols, false)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if n == 0 {
		writeErr(w, 404, "人物不存在，可能已经在别处被删除")
		return
	}
	p, err := a.Store.PersonGet(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

// peopleBulkUpdate 批量局部更新：{"ids":[…],"set":{"gender":"M"},"only_empty":true}。
// only_empty 是「只填当前还没填的」，勾上时已填过的人原值不动；
// 返回实际改动的人数，界面好区分「选了 116 个」和「其中 30 个本来就填过」。
func (a *API) peopleBulkUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs       []string                   `json:"ids"`
		Set       map[string]json.RawMessage `json:"set"`
		OnlyEmpty bool                       `json:"only_empty"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if len(body.IDs) == 0 {
		writeErr(w, 400, "ids 不能为空")
		return
	}
	cols, err := personPatchColumns(body.Set)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	updated, err := a.Store.PeoplePatch(r.Context(), body.IDs, cols, body.OnlyEmpty)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"updated": updated})
}
