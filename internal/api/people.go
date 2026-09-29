package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

func (a *API) registerPeople(r chi.Router) {
	r.Route("/api/v1/people", func(r chi.Router) {
		r.Get("/", a.peopleList)
		r.Get("/count", a.peopleCount)
		r.Post("/", a.peopleCreate)
		r.Post("/import/vcard", a.peopleImportVCard)
		r.Get("/export/vcard", a.peopleExportVCard)
		r.Get("/{id}", a.peopleGet)
		r.Put("/{id}", a.peopleUpdate)
		r.Delete("/{id}", a.peopleDelete)
		r.Delete("/", a.peopleDeleteBulk)
		r.Get("/{id}/timeline", a.peopleTimeline)
		r.Get("/{id}/intimacy", a.peopleIntimacy)
		r.Get("/{id}/wordcloud", a.peopleWordCloud)
		r.Get("/{id}/fields", a.personFieldList)
		r.Post("/{id}/fields", a.personFieldUpsert)
		r.Delete("/{id}/fields/{fid}", a.personFieldDelete)
		r.Post("/{id}/archive", a.personArchive)
		r.Delete("/{id}/archive", a.personUnarchive)
		r.Post("/{id}/merge", a.personMerge)
	})
	r.Get("/api/v1/people/duplicates", a.personDuplicates)
	r.Get("/api/v1/search", a.search)
}

// search 跨模块统一检索
func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	list, err := a.Store.Search(r.Context(), q, parseIntQuery(r, "limit", 5))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

// personDuplicates 返回可能与给定信息重复的联系人，供新建/编辑时提示
func (a *API) personDuplicates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := a.Store.PersonDuplicates(r.Context(), q.Get("name"), q.Get("phone"), q.Get("wechat"), q.Get("exclude_id"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) personArchive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.PersonArchive(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (a *API) personUnarchive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.PersonUnarchive(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

// personMerge 把 body.from 的数据合并进当前人物，然后删除 from
func (a *API) personMerge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		From string `json:"from"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if body.From == "" {
		writeErr(w, 400, "from required")
		return
	}
	if err := a.Store.PersonMerge(r.Context(), body.From, id); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "merged_into": id})
}

func (a *API) peopleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var list []*store.Person
	var err error
	if q.Get("archived") == "only" {
		// 列表页的「已归档」筛选：只看被隐藏掉的人，好把他们找回来
		list, err = a.Store.PersonArchivedOnly(r.Context(),
			q.Get("q"),
			parseIntQuery(r, "category_id", 0),
			parseIntQuery(r, "grade", 0),
			parseIntQuery(r, "tag_id", 0),
			parseIntQuery(r, "limit", 50),
			parseIntQuery(r, "offset", 0),
		)
	} else {
		list, err = a.Store.PersonList(r.Context(),
			q.Get("q"),
			parseIntQuery(r, "category_id", 0),
			parseIntQuery(r, "grade", 0),
			q.Get("archived") == "1",
			parseIntQuery(r, "tag_id", 0),
			parseIntQuery(r, "limit", 50),
			parseIntQuery(r, "offset", 0),
		)
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) peopleCount(w http.ResponseWriter, r *http.Request) {
	n, err := a.Store.PersonCount(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]int{"count": n})
}

func (a *API) peopleCreate(w http.ResponseWriter, r *http.Request) {
	var p store.Person
	if err := decode(r, &p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if p.Name == "" {
		p.Name = store.ComposeName(p.FamilyName, p.GivenName)
	}
	if p.Name == "" {
		writeErr(w, 400, "name required")
		return
	}
	if err := a.Store.PersonCreate(r.Context(), &p); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 生日要能进入提醒与建议，落库后同步生成对应纪念日
	if err := a.Store.SyncBirthdayAnniversary(r.Context(), &p); err != nil {
		writeErr(w, 500, "人物已保存，但生日纪念日生成失败: "+err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (a *API) peopleGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := a.Store.PersonGet(r.Context(), id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	fields, _ := a.Store.PersonFieldList(r.Context(), id)
	writeJSON(w, 200, map[string]any{"person": p, "fields": fields})
}

func (a *API) peopleUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var p store.Person
	if err := decode(r, &p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	p.ID = id
	if p.Name == "" {
		p.Name = store.ComposeName(p.FamilyName, p.GivenName)
	}
	if p.Name == "" {
		writeErr(w, 400, "name required")
		return
	}
	if err := a.Store.PersonUpdate(r.Context(), &p); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 生日改动要同步到纪念日；清空生日则移除此前自动生成的那条
	if err := a.Store.SyncBirthdayAnniversary(r.Context(), &p); err != nil {
		writeErr(w, 500, "人物已保存，但生日纪念日同步失败: "+err.Error())
		return
	}
	writeJSON(w, 200, p)
}

// deletePersonFull 删除联系人及其头像附件文件；关联记录（往来/对话/记账/
// 纪念日/提醒/标签/关系/自定义字段）由外键 ON DELETE CASCADE 级联清理。
func (a *API) deletePersonFull(ctx context.Context, id string) error {
	atts, err := a.Store.PersonDetachAttachments(ctx, id)
	if err != nil {
		return err
	}
	if err := a.Store.PersonDelete(ctx, id); err != nil {
		return err
	}
	for _, att := range atts {
		if att == nil {
			continue
		}
		p := filepath.Join(a.Cfg.Uploads, att.StoredName)
		if filepath.Clean(p) == p {
			_ = os.Remove(p)
		}
		_, _ = a.Store.DB.ExecContext(ctx, "DELETE FROM attachments WHERE id=?", att.ID)
	}
	return nil
}

func (a *API) peopleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.deletePersonFull(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

// peopleDeleteBulk 批量/清空删除联系人。请求体 {"ids":[...]}：
//   - 提供 id 列表：删除这些联系人（按 X-ABUID 重导入时可再建，不丢 vCard 来源）
//   - ids 为空或字段缺失：清空全部联系人（含已归档）
//
// 关联数据同样由外键级联清理，头像文件一并删除。
func (a *API) peopleDeleteBulk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ctx := r.Context()
	ids := body.IDs
	if len(ids) == 0 {
		all, err := a.Store.PersonList(ctx, "", 0, 0, true, 0, 1<<30, 0)
		if err != nil {
			writeErr(w, 500, "加载联系人失败: "+err.Error())
			return
		}
		ids = make([]string, 0, len(all))
		for _, p := range all {
			ids = append(ids, p.ID)
		}
	}
	deleted := 0
	for _, id := range ids {
		if id == "" {
			continue
		}
		if err := a.deletePersonFull(ctx, id); err != nil {
			writeErr(w, 500, fmt.Sprintf("删除联系人失败: %v", err))
			return
		}
		deleted++
	}
	writeJSON(w, 200, map[string]any{"deleted": deleted})
}

func (a *API) peopleTimeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	list, err := a.Store.PersonTimeline(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) peopleIntimacy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := a.Store.PersonIntimacy(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (a *API) peopleWordCloud(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	words, err := a.Store.PersonWordCloudText(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, words)
}

func (a *API) personFieldList(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	list, err := a.Store.PersonFieldList(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) personFieldUpsert(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var f store.PersonField
	if err := decode(r, &f); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	f.PersonID = id
	if err := a.Store.PersonFieldUpsert(r.Context(), &f); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, f)
}

func (a *API) personFieldDelete(w http.ResponseWriter, r *http.Request) {
	fid := chi.URLParam(r, "fid")
	if err := a.Store.PersonFieldDelete(r.Context(), fid); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ===== relationships =====

func (a *API) registerRelationships(r chi.Router) {
	r.Route("/api/v1/relationships", func(r chi.Router) {
		r.Get("/", a.graph)
		r.Get("/of/{id}", a.relsOf)
		r.Post("/", a.relCreate)
		r.Put("/{id}", a.relUpdate)
		r.Delete("/{id}", a.relDelete)
	})
}

func (a *API) relsOf(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	list, err := a.Store.RelationshipsOf(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

// 关系的双方与类型校验，创建与更新共用；返回空串表示通过
func validateRel(rel *store.Relationship) string {
	if rel.FromPerson == "" || rel.ToPerson == "" {
		return "需要指定关系的双方"
	}
	if rel.FromPerson == rel.ToPerson {
		return "不能与自己建立关系"
	}
	if strings.TrimSpace(rel.Type) == "" {
		return "type required"
	}
	return ""
}

func (a *API) relCreate(w http.ResponseWriter, r *http.Request) {
	var rel store.Relationship
	if err := decode(r, &rel); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if msg := validateRel(&rel); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	if err := a.Store.RelationshipCreate(r.Context(), &rel); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rel)
}

// relUpdate 以 URL 里的 id 为准，body 只提供要改的字段
func (a *API) relUpdate(w http.ResponseWriter, r *http.Request) {
	var rel store.Relationship
	if err := decode(r, &rel); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rel.ID = chi.URLParam(r, "id")
	if msg := validateRel(&rel); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	if err := a.Store.RelationshipUpdate(r.Context(), &rel); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rel)
}

func (a *API) relDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.RelationshipDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

// graph also returns nodes
func (a *API) graph(w http.ResponseWriter, r *http.Request) {
	people, rels, err := a.Store.RelationshipGraph(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"people": people, "relationships": rels})
}
