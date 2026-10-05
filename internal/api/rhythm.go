package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

// ===== 联系节奏（N5）=====
//
// 三个端点：设置里那张节奏表、渐远名单、以及「联系过了」的打卡。
// 打卡与待办的「完成」是同一件事的两个入口——待办勾掉就是打卡，
// 名单上点「联系过了」也是打卡，两边共用 contact_checkins 一张表。

func (a *API) registerRhythm(r chi.Router) {
	r.Route("/api/v1/contact-rhythm", func(r chi.Router) {
		r.Get("/", a.rhythmGet)
		r.Put("/", a.rhythmPut)
		r.Delete("/", a.rhythmReset)
	})
	r.Route("/api/v1/contacts", func(r chi.Router) {
		r.Get("/drift", a.driftList)
		r.Post("/{person_id}/checkin", a.contactCheckin)
	})
}

func (a *API) rhythmGet(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ContactRhythm(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) rhythmPut(w http.ResponseWriter, r *http.Request) {
	var tiers []store.RhythmTier
	if err := decode(r, &tiers); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := validateRhythm(tiers); err != nil {
		badRequestErr(w, err)
		return
	}
	if err := a.Store.ContactRhythmSet(r.Context(), tiers); err != nil {
		writeStoreErr(w, err)
		return
	}
	// 回读一次：存进去的是归一化之后的形状，客户端以此为准
	list, err := a.Store.ContactRhythm(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

// rhythmReset 把六档全部退回默认节奏。设置页留一个「恢复默认」，
// 免得用户改乱了只能挨个数字往回想原来是多少。
func (a *API) rhythmReset(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.ContactRhythmReset(r.Context()); err != nil {
		writeStoreErr(w, err)
		return
	}
	list, err := a.Store.ContactRhythm(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) driftList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.DriftList(r.Context(), store.DriftFilter{
		OnlyOverdue: r.URL.Query().Get("only") == "overdue",
		Limit:       parseIntQuery(r, "limit", 100),
		Offset:      parseIntQuery(r, "offset", 0),
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

// contactCheckin 记一次「联系过了」：今天打过招呼、但没记成往来或对话。
func (a *API) contactCheckin(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "person_id"))
	if id == "" {
		writeErr(w, 400, "person_id required")
		return
	}
	if err := a.Store.ContactCheckin(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(204)
}
