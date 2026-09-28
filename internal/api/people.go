package api

import (
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
	archived := q.Get("archived") == "1"
	list, err := a.Store.PersonList(r.Context(),
		q.Get("q"),
		parseIntQuery(r, "category_id", 0),
		parseIntQuery(r, "grade", 0),
		archived,
		parseIntQuery(r, "tag_id", 0),
		parseIntQuery(r, "limit", 50),
		parseIntQuery(r, "offset", 0),
	)
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

// peopleDelete 删除联系人。外键已开启，关联记录会级联清理；
// 附件不在级联范围内，需要显式摘掉引用并删除磁盘文件，否则留下孤儿。
func (a *API) peopleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	atts, err := a.Store.PersonDetachAttachments(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := a.Store.PersonDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for _, att := range atts {
		if att == nil {
			continue
		}
		p := filepath.Join(a.Cfg.Uploads, att.StoredName)
		if filepath.Clean(p) == p {
			_ = os.Remove(p)
		}
		_, _ = a.Store.DB.ExecContext(r.Context(), "DELETE FROM attachments WHERE id=?", att.ID)
	}
	w.WriteHeader(204)
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

func (a *API) relCreate(w http.ResponseWriter, r *http.Request) {
	var rel store.Relationship
	if err := decode(r, &rel); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if rel.FromPerson == "" || rel.ToPerson == "" {
		writeErr(w, 400, "需要指定关系的双方")
		return
	}
	if rel.FromPerson == rel.ToPerson {
		writeErr(w, 400, "不能与自己建立关系")
		return
	}
	if strings.TrimSpace(rel.Type) == "" {
		writeErr(w, 400, "type required")
		return
	}
	if err := a.Store.RelationshipCreate(r.Context(), &rel); err != nil {
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
