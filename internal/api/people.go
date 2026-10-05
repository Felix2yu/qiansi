package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

func (a *API) registerPeople(r chi.Router) {
	r.Route("/api/v1/people", func(r chi.Router) {
		r.Get("/", a.peopleList)
		r.Get("/count", a.peopleCount)
		r.Post("/", a.peopleCreate)
		r.Post("/bulk-categories", a.peopleBulkCategories)
		r.Post("/bulk-update", a.peopleBulkUpdate)
		r.Post("/import/vcard", a.peopleImportVCard)
		r.Get("/export/vcard", a.peopleExportVCard)
		r.Get("/{id}", a.peopleGet)
		r.Put("/{id}", a.peopleUpdate)
		r.Patch("/{id}", a.peoplePatch)
		r.Delete("/{id}", a.peopleDelete)
		r.Delete("/", a.peopleDeleteBulk)
		r.Get("/{id}/timeline", a.peopleTimeline)
		r.Get("/{id}/intimacy", a.peopleIntimacy)
		r.Get("/{id}/intro-path", a.personIntroPath)
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
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

// personDuplicates 返回可能与给定信息重复的联系人，供新建/编辑时提示
func (a *API) personDuplicates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := a.Store.PersonDuplicates(r.Context(), q.Get("name"), q.Get("phone"), q.Get("wechat"), q.Get("exclude_id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) personArchive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.PersonArchive(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) personUnarchive(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.PersonUnarchive(r.Context(), id); err != nil {
		writeStoreErr(w, err)
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
	filter := store.PeopleFilter{
		Q:          q.Get("q"),
		CategoryID: parseIntQuery(r, "category_id", 0),
		Grade:      parseIntQuery(r, "grade", 0),
		TagID:      parseIntQuery(r, "tag_id", 0),
		Gender:     genderQuery(q.Get("gender")),
	}
	limit := parseIntQuery(r, "limit", 50)
	offset := parseIntQuery(r, "offset", 0)
	var list []*store.Person
	var err error
	if q.Get("archived") == "only" {
		// 列表页的「已归档」筛选：只看被隐藏掉的人，好把他们找回来
		list, err = a.Store.PersonListBy(r.Context(), filter, false, true, limit, offset)
	} else {
		list, err = a.Store.PersonListBy(r.Context(), filter, q.Get("archived") == "1", false, limit, offset)
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

// genderQuery 收口性别筛选参数：只认男/女/未填三种，其余（包括手写的乱码）当不限处理，
// 免得一个非法值把列表筛成空的、看起来像数据没了。
func genderQuery(v string) string {
	switch g := strings.ToUpper(strings.TrimSpace(v)); g {
	case "M", "F":
		return g
	case "NONE":
		return "none"
	default:
		return ""
	}
}

func (a *API) peopleCount(w http.ResponseWriter, r *http.Request) {
	n, err := a.Store.PersonCount(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]int{"count": n})
}

// preparePerson 落库前的入参收口：表单可以只填姓和名，姓名在这里拼出来；
// 生日要归一成 YYYY-MM-DD——它会生成纪念日并参与「下一次」的字符串比较，
// 少写补零的值（1990-1-1）会让提醒窗口算偏。
func preparePerson(p *store.Person) error {
	if p.Name == "" {
		p.Name = store.ComposeName(p.FamilyName, p.GivenName)
	}
	if p.Name == "" {
		return errors.New("name required")
	}
	return validatePerson(p)
}

func (a *API) peopleCreate(w http.ResponseWriter, r *http.Request) {
	var p store.Person
	if err := decode(r, &p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := preparePerson(&p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.PersonCreate(r.Context(), &p); err != nil {
		if errors.Is(err, store.ErrIntroMissing) || errors.Is(err, store.ErrIntroCycle) {
			writeErr(w, 400, err.Error())
			return
		}
		writeStoreErr(w, err)
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
	if err := preparePerson(&p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.PersonUpdate(r.Context(), &p); err != nil {
		if errors.Is(err, store.ErrIntroMissing) || errors.Is(err, store.ErrIntroCycle) {
			writeErr(w, 400, err.Error())
			return
		}
		writeStoreErr(w, err)
		return
	}
	// 生日改动要同步到纪念日；清空生日则移除此前自动生成的那条
	if err := a.Store.SyncBirthdayAnniversary(r.Context(), &p); err != nil {
		writeErr(w, 500, "人物已保存，但生日纪念日同步失败: "+err.Error())
		return
	}
	writeJSON(w, 200, p)
}

// purgePerson 真删除一个人：头像文件与关联记录一并清掉，靠外键级联收尾。
// 只有回收站的「彻底删除」走这里；普通的删除是软删除，什么都不能碰。
func (a *API) purgePerson(ctx context.Context, id string) error {
	atts, err := a.Store.PersonDetachAttachments(ctx, id)
	if err != nil {
		return err
	}
	if err := a.Store.PersonPurge(ctx, id); err != nil {
		return err
	}
	a.removeAttachmentFiles(ctx, atts)
	return nil
}

// peopleDelete 把联系人收进回收站：只置 deleted_at。
// 他名下的往来/对话/账目随 live_* 视图一起消失，头像留在原地，
// 恢复时人和照片都在，不必重新上传。
func (a *API) peopleDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.PersonDelete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// peopleDeleteBulk 批量删除联系人，ids 为空表示删除全部可见联系人。
//
// 这里取的是「还没进回收站」的人（PersonList 走 live_people），所以连着点两次
// 也不会把回收站里的人一起清掉——清空回收站是另一个动作，得由用户显式发起。
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
		if err := a.Store.PersonDelete(ctx, id); err != nil {
			if isNoRows(err) {
				continue // 这一条已经在别处删掉了，不算失败，也不该中断整批
			}
			writeErr(w, 500, fmt.Sprintf("删除联系人失败: %v", err))
			return
		}
		deleted++
	}
	writeJSON(w, 200, map[string]any{"deleted": deleted})
}

// peopleBulkCategories 批量把选中的人加进若干圈子：{"ids":[…],"category_ids":[…]}。
// 只增不减——移出圈子仍回到各自的编辑弹窗，批量误删的代价太高。
func (a *API) peopleBulkCategories(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs         []string `json:"ids"`
		CategoryIDs []int    `json:"category_ids"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if len(body.IDs) == 0 || len(body.CategoryIDs) == 0 {
		writeErr(w, 400, "ids 与 category_ids 均不能为空")
		return
	}
	added, err := a.Store.PeopleAddCategories(r.Context(), body.IDs, body.CategoryIDs)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"added": added})
}

func (a *API) peopleTimeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	list, err := a.Store.PersonTimeline(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) peopleIntimacy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := a.Store.PersonIntimacy(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

// personIntroPath 认识路径（引荐人链）。详情页以前为了画这一条链，
// 把全量 people 和全量 relationships 都拉回浏览器，现在换成按需单接口。
func (a *API) personIntroPath(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	selfID, err := a.Store.SettingGet(r.Context(), "self_person_id")
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if selfID == "" {
		writeErr(w, 400, "请先在设置里指定「我是谁」")
		return
	}
	path, err := a.Store.IntroPath(r.Context(), selfID, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, path)
}

func (a *API) peopleWordCloud(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	words, err := a.Store.PersonWordCloudText(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, words)
}

func (a *API) personFieldList(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	list, err := a.Store.PersonFieldList(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
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
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, f)
}

func (a *API) personFieldDelete(w http.ResponseWriter, r *http.Request) {
	fid := chi.URLParam(r, "fid")
	if err := a.Store.PersonFieldDelete(r.Context(), fid); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// ===== relationships =====

func (a *API) registerRelationships(r chi.Router) {
	r.Route("/api/v1/relationships", func(r chi.Router) {
		r.Get("/", a.graph)
		r.Get("/types", a.relTypes)
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
		writeStoreErr(w, err)
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
		if errors.Is(err, store.ErrRelationshipExists) {
			writeErr(w, 409, err.Error())
			return
		}
		writeStoreErr(w, err)
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
		if errors.Is(err, store.ErrRelationshipExists) {
			writeErr(w, 409, err.Error())
			return
		}
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, rel)
}

func (a *API) relDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.RelationshipDelete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// relTypes 返回关系边已使用的类型，供前端自定义类型输入框补历史候选
func (a *API) relTypes(w http.ResponseWriter, r *http.Request) {
	types, err := a.Store.RelationshipTypes(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, types)
}

// graph also returns nodes and their tags
func (a *API) graph(w http.ResponseWriter, r *http.Request) {
	data, err := a.Store.RelationshipGraph(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, data)
}
