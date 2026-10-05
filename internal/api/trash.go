package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/store"
)

// ===== trash =====
//
// 六类主记录的 DELETE 现在只置 deleted_at，这一层负责把它们请回来或真正删掉。
// 路径里的 kind 由 store 的白名单表翻译，不认识的一律 400。

func (a *API) registerTrash(r chi.Router) {
	r.Route("/api/v1/trash", func(r chi.Router) {
		r.Get("/", a.trashList)
		r.Delete("/", a.trashEmpty)
		r.Post("/{kind}/{id}/restore", a.trashRestore)
		r.Delete("/{kind}/{id}", a.trashPurge)
	})
}

func (a *API) trashList(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.TrashList(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, items)
}

func (a *API) trashRestore(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.TrashRestore(r.Context(), chi.URLParam(r, "kind"), chi.URLParam(r, "id")); err != nil {
		writeTrashErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) trashPurge(w http.ResponseWriter, r *http.Request) {
	kind, id := chi.URLParam(r, "kind"), chi.URLParam(r, "id")
	ctx := r.Context()
	if kind == "person" {
		// 彻底删除才动磁盘：头像文件与 attachments 行一起走，回收站里的头像原样留着。
		// 先确认这一行真在回收站里，否则会把还在使用的联系人的头像指针清空才撞上 404。
		inTrash, err := a.Store.TrashHas(ctx, kind, id)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if !inTrash {
			writeErr(w, http.StatusNotFound, "回收站里没有这条记录")
			return
		}
		atts, err := a.Store.PersonDetachAttachments(ctx, id)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if err := a.Store.TrashPurge(ctx, kind, id); err != nil {
			writeTrashErr(w, err)
			return
		}
		a.removeAttachmentFiles(ctx, atts)
		w.WriteHeader(204)
		return
	}
	if err := a.Store.TrashPurge(ctx, kind, id); err != nil {
		writeTrashErr(w, err)
		return
	}
	w.WriteHeader(204)
}

// trashEmpty 清空回收站。这一步不可逆，前端必须先给确认框；
// 联系人的头像文件在删行之前读出来，删完一并从磁盘上清掉。
func (a *API) trashEmpty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	res, err := a.Store.TrashEmpty(ctx)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.removeAttachmentFiles(ctx, res.Attachments)
	writeJSON(w, 200, res)
}

// removeAttachmentFiles 删掉头像/附件文件与对应记录。
// 磁盘删不掉（权限、文件已被手工清理）不该让整件事停在半路，记录照删。
func (a *API) removeAttachmentFiles(ctx context.Context, atts []*store.Attachment) {
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
}

// writeTrashErr 回收站专用：不认识的类型是用法错误（400），
// 找不到行才是 404，别让「拼错 kind」伪装成服务端故障。
func writeTrashErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrUnknownTrashKind) {
		writeErr(w, http.StatusBadRequest, "回收站里没有这一类记录")
		return
	}
	writeStoreErr(w, err)
}
