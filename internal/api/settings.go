package api

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

// ===== settings =====

func (a *API) registerSettings(r chi.Router) {
    r.Route("/api/v1/settings", func(r chi.Router) {
        r.Get("/", a.settingsAll)
        r.Put("/{key}", a.settingsSet)
        r.Post("/bulk", a.settingsBulk)
    })
}

func (a *API) registerCategories(r chi.Router) {
	r.Route("/api/v1/categories", func(r chi.Router) {
		r.Get("/", a.categoryList)
		r.Post("/", a.categoryCreate)
		r.Put("/{id}", a.categoryUpdate)
		r.Delete("/{id}", a.categoryDelete)
	})
}

func (a *API) categoryList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.CategoryList(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) categoryCreate(w http.ResponseWriter, r *http.Request) {
	var c store.Category
	if err := decode(r, &c); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.CategoryUpsert(r.Context(), &c); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (a *API) categoryUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathIntID(r, "id")
	if err != nil {
		badRequestErr(w, err)
		return
	}
	var c store.Category
	if err := decode(r, &c); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	c.ID = id
	if err := a.Store.CategoryUpsert(r.Context(), &c); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (a *API) categoryDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathIntID(r, "id")
	if err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.CategoryDelete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) registerTags(r chi.Router) {
	r.Route("/api/v1/tags", func(r chi.Router) {
		r.Get("/", a.tagList)
		r.Post("/", a.tagCreate)
		r.Put("/{id}", a.tagUpdate)
		r.Delete("/{id}", a.tagDelete)
	})
}

func (a *API) tagList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.TagList(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) tagCreate(w http.ResponseWriter, r *http.Request) {
	var t store.Tag
	if err := decode(r, &t); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.TagUpsert(r.Context(), &t); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) tagUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathIntID(r, "id")
	if err != nil {
		badRequestErr(w, err)
		return
	}
	var t store.Tag
	if err := decode(r, &t); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	t.ID = id
	if err := a.Store.TagUpsert(r.Context(), &t); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) tagDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathIntID(r, "id")
	if err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.TagDelete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) registerEventTypes(r chi.Router) {
	r.Route("/api/v1/event-types", func(r chi.Router) {
		r.Get("/", a.eventTypeList)
		r.Post("/", a.eventTypeCreate)
		r.Put("/{id}", a.eventTypeUpdate)
		r.Delete("/{id}", a.eventTypeDelete)
	})
}

func (a *API) eventTypeList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.EventTypeList(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) eventTypeCreate(w http.ResponseWriter, r *http.Request) {
	var e store.EventType
	if err := decode(r, &e); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.EventTypeUpsert(r.Context(), &e); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, e)
}

func (a *API) eventTypeUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathIntID(r, "id")
	if err != nil {
		badRequestErr(w, err)
		return
	}
	var e store.EventType
	if err := decode(r, &e); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	e.ID = id
	if err := a.Store.EventTypeUpsert(r.Context(), &e); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, e)
}

func (a *API) eventTypeDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathIntID(r, "id")
	if err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.EventTypeDelete(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) registerTaggings(r chi.Router) {
	r.Route("/api/v1/taggings", func(r chi.Router) {
		r.Post("/add", a.tagAdd)
		r.Post("/remove", a.tagRemove)
		r.Get("/of", a.tagOf)
	})
}

func (a *API) tagAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		TagID      int    `json:"tag_id"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.TagAdd(r.Context(), body.TargetType, body.TargetID, body.TagID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) tagRemove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		TagID      int    `json:"tag_id"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.TagRemove(r.Context(), body.TargetType, body.TargetID, body.TagID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) tagOf(w http.ResponseWriter, r *http.Request) {
	t := r.URL.Query().Get("target_type")
	id := r.URL.Query().Get("target_id")
	list, err := a.Store.TagsOf(r.Context(), t, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func isNoRows(err error) bool { return err == sql.ErrNoRows }

func (a *API) settingsAll(w http.ResponseWriter, r *http.Request) {
    m, err := a.Store.SettingAll(r.Context())
    if err != nil { writeStoreErr(w, err); return }
    writeJSON(w, 200, m)
}

func (a *API) settingsSet(w http.ResponseWriter, r *http.Request) {
    key := chi.URLParam(r, "key")
    var body struct { Value string `json:"value"` }
    if err := decode(r, &body); err != nil { writeErr(w, 400, err.Error()); return }
    if err := a.Store.SettingSet(r.Context(), key, body.Value); err != nil { writeStoreErr(w, err); return }
    writeJSON(w, 200, map[string]any{"key": key, "value": body.Value})
}

func (a *API) settingsBulk(w http.ResponseWriter, r *http.Request) {
    var m map[string]string
    if err := decode(r, &m); err != nil { writeErr(w, 400, err.Error()); return }
    for k, v := range m {
        if err := a.Store.SettingSet(r.Context(), k, v); err != nil { writeStoreErr(w, err); return }
    }
    writeJSON(w, 200, map[string]string{"ok": "1"})
}
