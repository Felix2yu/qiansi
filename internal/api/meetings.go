package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ===== 见面总览（N6）=====
//
// 只有一个读端点：各联系人上次见面、次数与平均间隔，最久没见的排最前。
// 见面的口径（哪些往来类型算见面）见 internal/store/meetings.go 与迁移 017。

func (a *API) registerMeetings(r chi.Router) {
	r.Get("/api/v1/meetings/overview", a.meetingOverview)
}

func (a *API) meetingOverview(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.MeetingOverview(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}
