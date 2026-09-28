package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

func (a *API) registerPeople(r chi.Router) {
	r.Route("/api/v1/people", func(r chi.Router) {
		r.Get("/", a.peopleList)
		r.Get("/count", a.peopleCount)
		r.Post("/", a.peopleCreate)
		r.Get("/{id}", a.peopleGet)
		r.Put("/{id}", a.peopleUpdate)
		r.Delete("/{id}", a.peopleDelete)
		r.Get("/{id}/timeline", a.peopleTimeline)
		r.Get("/{id}/intimacy", a.peopleIntimacy)
		r.Get("/{id}/wordcloud", a.peopleWordCloud)
		r.Get("/{id}/fields", a.personFieldList)
		r.Post("/{id}/fields", a.personFieldUpsert)
		r.Delete("/{id}/fields/{fid}", a.personFieldDelete)
	})
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
	writeJSON(w, 200, p)
}

func (a *API) peopleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.PersonDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
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
