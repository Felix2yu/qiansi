package api

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

// ===== events =====

func (a *API) registerEvents(r chi.Router) {
	r.Route("/api/v1/events", func(r chi.Router) {
		r.Get("/", a.eventList)
		r.Get("/{id}", a.eventGet)
		r.Post("/", a.eventCreate)
		r.Put("/{id}", a.eventUpdate)
		r.Delete("/{id}", a.eventDelete)
	})
}

func (a *API) eventList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.EventList(r.Context(),
		r.URL.Query().Get("person_id"),
		parseIntQuery(r, "limit", 50), parseIntQuery(r, "offset", 0))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) eventGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	e, err := a.Store.EventGet(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, e)
}

func (a *API) eventCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		store.Event
		ParticipantIDs []string `json:"participant_ids"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if body.Event.EventDate == "" {
		writeErr(w, 400, "event_date required")
		return
	}
	if err := a.Store.EventCreate(r.Context(), &body.Event, body.ParticipantIDs); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, body.Event)
}

func (a *API) eventUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		store.Event
		ParticipantIDs []string `json:"participant_ids"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	body.Event.ID = id
	if err := a.Store.EventUpdate(r.Context(), &body.Event, body.ParticipantIDs); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, body.Event)
}

func (a *API) eventDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.EventDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ===== memos =====

func (a *API) registerMemos(r chi.Router) {
	r.Route("/api/v1/memos", func(r chi.Router) {
		r.Get("/", a.memoList)
		r.Post("/", a.memoCreate)
		r.Put("/{id}", a.memoUpdate)
		r.Delete("/{id}", a.memoDelete)
	})
}

func (a *API) memoList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.MemoList(r.Context(),
		r.URL.Query().Get("person_id"),
		r.URL.Query().Get("promises_only") == "1",
		parseIntQuery(r, "limit", 50), parseIntQuery(r, "offset", 0))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) memoCreate(w http.ResponseWriter, r *http.Request) {
	var m store.Memo
	if err := decode(r, &m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if m.SaidAt == "" {
		writeErr(w, 400, "said_at required")
		return
	}
	if err := a.Store.MemoCreate(r.Context(), &m); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, m)
}

func (a *API) memoUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var m store.Memo
	if err := decode(r, &m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	m.ID = id
	if err := a.Store.MemoUpdate(r.Context(), &m); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, m)
}

func (a *API) memoDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.MemoDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ===== transactions / repayments =====

func (a *API) registerTransactions(r chi.Router) {
	r.Route("/api/v1/transactions", func(r chi.Router) {
		r.Get("/", a.txList)
		r.Get("/{id}", a.txGet)
		r.Post("/", a.txCreate)
		r.Put("/{id}", a.txUpdate)
		r.Delete("/{id}", a.txDelete)
		r.Post("/{id}/repayments", a.repaymentCreate)
		r.Get("/{id}/repayments", a.repaymentsList)
	})
}

func (a *API) txList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.TransactionList(r.Context(),
		r.URL.Query().Get("person_id"),
		parseIntQuery(r, "limit", 50), parseIntQuery(r, "offset", 0))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) txGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := a.Store.TransactionGet(r.Context(), id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) txCreate(w http.ResponseWriter, r *http.Request) {
	var t store.Transaction
	if err := decode(r, &t); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if t.OccurredAt == "" {
		writeErr(w, 400, "occurred_at required")
		return
	}
	if err := a.Store.TransactionCreate(r.Context(), &t); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) txUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var t store.Transaction
	if err := decode(r, &t); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	t.ID = id
	if err := a.Store.TransactionUpdate(r.Context(), &t); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) txDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.TransactionDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (a *API) repaymentCreate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var rp store.Repayment
	if err := decode(r, &rp); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rp.TransactionID = id
	if err := a.Store.RepaymentCreate(r.Context(), &rp); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rp)
}

func (a *API) repaymentsList(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	list, err := a.Store.RepaymentsOf(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

// ===== anniversaries =====

func (a *API) registerAnniversaries(r chi.Router) {
	r.Route("/api/v1/anniversaries", func(r chi.Router) {
		r.Get("/", a.annivList)
		r.Post("/", a.annivCreate)
		r.Put("/{id}", a.annivUpdate)
		r.Delete("/{id}", a.annivDelete)
	})
}

func (a *API) annivList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.AnniversaryList(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) annivCreate(w http.ResponseWriter, r *http.Request) {
	var a2 store.Anniversary
	if err := decode(r, &a2); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if a2.Date == "" || a2.Title == "" {
		writeErr(w, 400, "title and date required")
		return
	}
	if err := a.Store.AnniversaryCreate(r.Context(), &a2); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, a2)
}

func (a *API) annivUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var a2 store.Anniversary
	if err := decode(r, &a2); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	a2.ID = id
	if err := a.Store.AnniversaryUpdate(r.Context(), &a2); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, a2)
}

func (a *API) annivDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.AnniversaryDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ===== reminders =====

func (a *API) registerReminders(r chi.Router) {
	r.Route("/api/v1/reminders", func(r chi.Router) {
		r.Get("/", a.reminderList)
		r.Get("/upcoming", a.upcoming)
		r.Post("/", a.reminderCreate)
		r.Put("/{id}", a.reminderUpdate)
		r.Delete("/{id}", a.reminderDelete)
		r.Post("/{id}/done", a.reminderDone)
	})
}

func (a *API) reminderList(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ReminderList(r.Context(),
		r.URL.Query().Get("status"),
		parseIntQuery(r, "limit", 50), parseIntQuery(r, "offset", 0))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) upcoming(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ReminderUpcoming(r.Context(), parseIntQuery(r, "days", 30))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (a *API) reminderCreate(w http.ResponseWriter, r *http.Request) {
	var rm store.Reminder
	if err := decode(r, &rm); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := a.Store.ReminderCreate(r.Context(), &rm); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rm)
}

func (a *API) reminderUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var rm store.Reminder
	if err := decode(r, &rm); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rm.ID = id
	if err := a.Store.ReminderUpdate(r.Context(), &rm); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rm)
}

func (a *API) reminderDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.ReminderDelete(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}

func (a *API) reminderDone(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.Store.ReminderDone(r.Context(), id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}
