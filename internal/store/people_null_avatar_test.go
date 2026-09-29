package store

import (
	"context"
	"testing"
)

// 回归：PersonDetachAttachments 把 avatar_attachment_id 写成 NULL，
// 而 PersonGet/PersonList 早先用裸 string 扫这一列，
// 导致「带头像的人被删除（删除中途失败或附件已摘除）」后列表与详情整片 500。
func TestPeopleNullAvatarIsScannable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := &Person{Name: "带头像", AvatarAttachmentID: "att-1"}
	if err := s.PersonCreate(ctx, p); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	att := &Attachment{EntityType: "person", EntityID: p.ID, FileName: "a.png", StoredName: "a.png", Mime: "image/png"}
	if err := s.AttachmentCreate(ctx, att); err != nil {
		t.Fatalf("AttachmentCreate: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE people SET avatar_attachment_id=? WHERE id=?", att.ID, p.ID); err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	if got, err := s.PersonGet(ctx, p.ID); err != nil || got.AvatarAttachmentID != att.ID {
		t.Fatalf("删除前 PersonGet = %+v, %v", got, err)
	}

	if _, err := s.PersonDetachAttachments(ctx, p.ID); err != nil {
		t.Fatalf("PersonDetachAttachments: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM attachments"); err != nil {
		t.Fatalf("clean attachments: %v", err)
	}

	got, err := s.PersonGet(ctx, p.ID)
	if err != nil {
		t.Fatalf("头像置空后 PersonGet 失败: %v", err)
	}
	if got.AvatarAttachmentID != "" {
		t.Errorf("AvatarAttachmentID = %q, want 空", got.AvatarAttachmentID)
	}
	list, err := s.PersonList(ctx, "", 0, 0, false, 0, 10, 0)
	if err != nil {
		t.Fatalf("头像置空后 PersonList 失败: %v", err)
	}
	if len(list) != 1 || list[0].AvatarAttachmentID != "" {
		t.Errorf("PersonList = %+v", list)
	}
}
