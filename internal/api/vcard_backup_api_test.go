package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiansi/app/internal/store"
)

// ===== helpers =====

// vcardBackupDecodeList 解码 JSON 数组响应体。
func vcardBackupDecodeList(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list %s: %v", rec.Body.String(), err)
	}
	return out
}

// vcardBackupErr 取出错误响应中的 error 文本。
func vcardBackupErr(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	s, _ := decodeMap(t, rec)["error"].(string)
	return s
}

// vcardBackupMultipart 构造 name="file" 的 multipart 上传体。
func vcardBackupMultipart(t *testing.T, filename string, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if filename == "" && data == nil {
		// 无 file 部分，只带一个普通字段
		_ = mw.WriteField("note", "no file here")
	} else {
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("mw.Close: %v", err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

func vcardBackupField(t *testing.T, s *testServer, personID, label, value string, order int) {
	t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/people/"+personID+"/fields", map[string]any{
		"label": label, "value": value, "sort_order": order,
	})
	if rec.Code != 200 {
		t.Fatalf("POST fields(%s=%s) => %d: %s", label, value, rec.Code, rec.Body.String())
	}
}

func vcardBackupMustCreatePerson(t *testing.T, s *testServer, body map[string]any) map[string]any {
	t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/people/", body)
	if rec.Code != 200 {
		t.Fatalf("POST people => %d: %s", rec.Code, rec.Body.String())
	}
	return decodeMap(t, rec)
}

// ===== vCard HTTP 往返 =====

// 建数据 → GET export/vcard → 断言 vCard 文本 → POST 到新服务 import → 字段回读一致
func TestVCardAPIFullRoundTrip(t *testing.T) {
	s := newTestServer(t)
	person := vcardBackupMustCreatePerson(t, s, map[string]any{
		"name": "张三丰", "family_name": "张", "given_name": "三丰",
		"nickname": "张真人", "gender": "M", "birthday": "1247-05-15",
		"phone": "13800138000", "wechat": "zsf_wx",
		"location": "武当山路1号 十堰市", "notes": "太极创始人\n爱喝茶",
	})
	pid, _ := person["id"].(string)
	if pid == "" {
		t.Fatal("no person id")
	}
	fields := []struct {
		label, value string
		order        int
	}{
		{"公司", "武当派;太极组", 200},
		{"职位", "掌门", 201},
		{"QQ", "123456", 210},
		{"邮箱", "zsf@wudang.com", 100},
		{"住宅", "zsf@qq.com", 101},
		{"工作", "zsf@work.com", 102},
		{"邮箱2", "zsf2@qq.com", 103},
		{"网址", "https://wd.example.com", 300},
		{"网址2", "https://2.example.com", 301},
		{"工作电话", "010-88888888", 1},
		{"电话", "0571-1111111", 2},
		{"军功章", "三枚", 999},
	}
	for _, f := range fields {
		vcardBackupField(t, s, pid, f.label, f.value, f.order)
	}

	rec := s.do(http.MethodGet, "/api/v1/people/export/vcard", nil)
	if rec.Code != 200 {
		t.Fatalf("export => %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/vcard") {
		t.Errorf("Content-Type=%q", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".vcf") {
		t.Errorf("Content-Disposition=%q", cd)
	}
	if rec.Header().Get("X-Export-Count") != "1" {
		t.Errorf("X-Export-Count=%q want 1", rec.Header().Get("X-Export-Count"))
	}
	vcf := rec.Body.String()
	must := []string{
		"BEGIN:VCARD\r\n", "VERSION:3.0\r\n",
		"FN:张三丰\r\n", "N:张;三丰;;;\r\n", "NICKNAME:张真人\r\n",
		"TEL;type=CELL;type=VOICE;type=pref:13800138000\r\n",
		`ORG:武当派\;太极组` + "\r\n",
		"TITLE:掌门\r\n", "X-QQ:123456\r\n",
		"EMAIL;type=INTERNET;type=pref:zsf@wudang.com\r\n",
		"EMAIL;type=INTERNET;type=home:zsf@qq.com\r\n",
		"EMAIL;type=INTERNET;type=work:zsf@work.com\r\n",
		"URL:https://wd.example.com\r\n",
		"X-SOCIALPROFILE;type=wechat:x-apple://com.apple.social.wechat?username=zsf_wx\r\n",
		"ADR;type=HOME;type=pref:;;武当山路1号 十堰市;;;;\r\n",
		"BDAY:1247-05-15\r\n", "X-GENDER:M\r\n",
		`NOTE:太极创始人\n爱喝茶`,
		"X-ABUID:" + strings.ToUpper(pid) + ":ABPerson\r\n",
		"END:VCARD\r\n",
	}
	for _, frag := range must {
		if !strings.Contains(vcf, frag) {
			t.Errorf("导出缺少 %q:\n%s", frag, vcf)
		}
	}
	// 自定义/分组字段：item 组 + X-ABLabel
	for _, frag := range []string{"邮箱2", "网址2", "军功章: 三枚", `TEL;type=VOICE;item`} {
		if !strings.Contains(vcf, frag) {
			t.Errorf("导出缺少分组/并入 NOTE 内容 %q:\n%s", frag, vcf)
		}
	}

	// 导入到全新服务
	s2 := newTestServer(t)
	impRec := s2.do(http.MethodPost, "/api/v1/people/import/vcard", map[string]any{"text": vcf})
	if impRec.Code != 200 {
		t.Fatalf("import => %d: %s", impRec.Code, impRec.Body.String())
	}
	res := decodeMap(t, impRec)
	if res["total"] != float64(1) || res["imported"] != float64(1) || res["skipped"] != float64(0) {
		t.Errorf("import 结果错误: %v", res)
	}
	created, _ := res["people"].([]any)
	if len(created) != 1 {
		t.Fatalf("people 应 1 条: %v", res["people"])
	}
	newID := created[0].(map[string]any)["id"].(string)

	getRec := s2.do(http.MethodGet, "/api/v1/people/"+newID, nil)
	detail := decodeMap(t, getRec)
	p := detail["person"].(map[string]any)
	for k, want := range map[string]string{
		"name": "张三丰", "family_name": "张", "given_name": "三丰",
		"nickname": "张真人", "phone": "13800138000", "wechat": "zsf_wx",
		"birthday": "1247-05-15", "gender": "M", "location": "武当山路1号 十堰市",
		"x_abuid": strings.ToUpper(pid),
	} {
		if got, _ := p[k].(string); got != want {
			t.Errorf("导入后 %s=%q want %q", k, got, want)
		}
	}
	if notes, _ := p["notes"].(string); !strings.Contains(notes, "太极创始人\n爱喝茶") || !strings.Contains(notes, "军功章: 三枚") {
		t.Errorf("NOTE 往返错误: %q", notes)
	}
	gotFields := vcardBackupDecodeList(t, vcardBackupFieldsRec(t, s2, newID))
	labelMap := map[string]string{}
	for _, f := range gotFields {
		labelMap[f["label"].(string)] = f["value"].(string)
	}
	for label, want := range map[string]string{
		"公司": "武当派;太极组", "职位": "掌门", "QQ": "123456",
		"邮箱": "zsf@wudang.com", "住宅": "zsf@qq.com", "工作": "zsf@work.com",
		"网址": "https://wd.example.com", "网址2": "https://2.example.com",
		"工作电话": "010-88888888",
		// 导出用 "TEL;...;itemN:" 参数式分组（itemN 不是组前缀），
		// 重导入时 X-ABLabel 关联不上 → 标签按位置回退，值不丢
		"邮箱4": "zsf2@qq.com", "电话3": "0571-1111111",
	} {
		if got := labelMap[label]; got != want {
			t.Errorf("导入字段 %s=%q want %q (全部: %v)", label, got, want, labelMap)
		}
	}

	// 同 UID 再导入：不重建，走 updated 路径
	impRec2 := s2.do(http.MethodPost, "/api/v1/people/import/vcard", map[string]any{"text": vcf})
	res2 := decodeMap(t, impRec2)
	if res2["imported"] != float64(0) || res2["updated"] != float64(1) {
		t.Errorf("按 UID 去重应 updated=1/imported=0: %v", res2)
	}
	if cnt := vcardBackupDecodeList(t, s2.do(http.MethodGet, "/api/v1/people/", nil)); len(cnt) != 1 {
		t.Errorf("重复导入不应新建联系人: %d", len(cnt))
	}

	// 改姓名的同 UID 卡片 → PersonUpdateNameParts 回填
	renamed := strings.Replace(vcf, "N:张;三丰;;;\r\n", "N:張;三峰;;;\r\n", 1)
	impRec3 := s2.do(http.MethodPost, "/api/v1/people/import/vcard", map[string]any{"text": renamed})
	res3 := decodeMap(t, impRec3)
	if res3["updated"] != float64(1) {
		t.Errorf("改名应 updated=1: %v", res3)
	}
	p3 := decodeMap(t, s2.do(http.MethodGet, "/api/v1/people/"+newID, nil))["person"].(map[string]any)
	if p3["name"] != "張三峰" {
		t.Errorf("姓名未按 vCard 回填: %v", p3["name"])
	}
}

func vcardBackupFieldsRec(t *testing.T, s *testServer, id string) *httptest.ResponseRecorder {
	t.Helper()
	return s.do(http.MethodGet, "/api/v1/people/"+id+"/fields", nil)
}

// 无 X-ABUID 的卡片按 (name, phone) 去重
func TestVCardAPIImportDuplicateByNamePhone(t *testing.T) {
	s := newTestServer(t)
	text := "BEGIN:VCARD\nVERSION:3.0\nFN:李四\nTEL;TYPE=CELL:13911112222\nEND:VCARD\n" +
		"BEGIN:VCARD\nVERSION:3.0\nFN:王五\nEND:VCARD"
	rec := s.do(http.MethodPost, "/api/v1/people/import/vcard", map[string]any{"text": text})
	res := decodeMap(t, rec)
	if res["total"] != float64(2) || res["imported"] != float64(2) || res["skipped"] != float64(0) {
		t.Fatalf("首次导入应 2 条: %v", res)
	}
	rec2 := s.do(http.MethodPost, "/api/v1/people/import/vcard", map[string]any{"text": text})
	res2 := decodeMap(t, rec2)
	if res2["imported"] != float64(0) || res2["skipped"] != float64(2) {
		t.Errorf("重复导入应全部跳过: %v", res2)
	}
}

// 导入带 BDAY 的卡片应自动生成生日纪念日（与手工新建联系人一致），重复导入不重复建
func TestVCardAPIImportCreatesBirthdayAnniversary(t *testing.T) {
	s := newTestServer(t)
	text := "BEGIN:VCARD\nVERSION:3.0\nFN:赵六\nBDAY:19920708\nEND:VCARD\n" +
		"BEGIN:VCARD\nVERSION:3.0\nFN:孙七\nEND:VCARD"
	rec := s.do(http.MethodPost, "/api/v1/people/import/vcard", map[string]any{"text": text})
	res := decodeMap(t, rec)
	if res["imported"] != float64(2) {
		t.Fatalf("应导入 2 人: %v", res)
	}
	ids := map[string]string{}
	for _, item := range res["people"].([]any) {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		id, _ := m["id"].(string)
		ids[name] = id
		if name == "赵六" {
			if annivID, _ := m["birthday_anniversary_id"].(string); annivID == "" {
				t.Errorf("导入响应应回写 birthday_anniversary_id: %v", m)
			}
		}
	}
	if ids["赵六"] == "" || ids["孙七"] == "" {
		t.Fatalf("未取到导入的联系人 ID: %v", ids)
	}

	var n int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM anniversaries WHERE person_id=? AND source='birthday' AND date='1992-07-08' AND is_lunar=0", ids["赵六"]).Scan(&n); err != nil {
		t.Fatalf("query anniversaries: %v", err)
	}
	if n != 1 {
		t.Fatalf("生日应生成 1 条纪念日，得到 %d", n)
	}
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM anniversaries WHERE person_id=?", ids["孙七"]).Scan(&n); err != nil {
		t.Fatalf("query anniversaries: %v", err)
	}
	if n != 0 {
		t.Fatalf("无生日不应生成纪念日，得到 %d", n)
	}

	// 重复导入走跳过分支：不新建联系人，也不重复生成纪念日
	rec2 := s.do(http.MethodPost, "/api/v1/people/import/vcard", map[string]any{"text": text})
	res2 := decodeMap(t, rec2)
	if res2["imported"] != float64(0) || res2["skipped"] != float64(2) {
		t.Fatalf("重复导入应全部跳过: %v", res2)
	}
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM anniversaries WHERE person_id=?", ids["赵六"]).Scan(&n); err != nil {
		t.Fatalf("query anniversaries: %v", err)
	}
	if n != 1 {
		t.Fatalf("重复导入后仍应只有 1 条生日纪念日，得到 %d", n)
	}
}

// 导入请求的错误分支：空文本 / 非法 JSON / 无 BEGIN / 纯姓名缺失卡片 / 超大请求体
func TestVCardAPIImportErrors(t *testing.T) {
	s := newTestServer(t)
	cases := []struct {
		name string
		body []byte
		ct   string
		want int
		msg  string
	}{
		{"空 text", []byte(`{"text":""}`), "application/json", 400, "不能为空"},
		{"空白 text", []byte(`{"text":"   \n  "}`), "application/json", 400, "不能为空"},
		{"非法 JSON", []byte(`{"text":`), "application/json", 400, "请求格式错误"},
		{"无 BEGIN", []byte(`{"text":"FN:没有边界\nTEL:123"}`), "application/json", 400, "未找到有效的 vCard"},
		{"超过 20MB", append(append([]byte(`{"text":"`), bytes.Repeat([]byte("a"), 20<<20)...), []byte(`"}`)...), "application/json", 400, "读取请求体失败"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := s.raw(http.MethodPost, "/api/v1/people/import/vcard", c.body, c.ct)
			if rec.Code != c.want {
				t.Fatalf("code=%d want %d body=%s", rec.Code, c.want, rec.Body.String())
			}
			if m := vcardBackupErr(t, rec); !strings.Contains(m, c.msg) {
				t.Errorf("error=%q want 含 %q", m, c.msg)
			}
		})
	}
	// 只有 END 没有 BEGIN：200，但 0 张→按“未找到有效 vCard”报错
	rec := s.raw(http.MethodPost, "/api/v1/people/import/vcard", []byte(`{"text":"END:VCARD\nFN:孤儿"}`), "application/json")
	if rec.Code != 400 {
		t.Errorf("仅 END 应 400, got %d: %s", rec.Code, rec.Body.String())
	}
	// 有 BEGIN/END 但无姓名 → 200 skipped=1
	rec2 := s.do(http.MethodPost, "/api/v1/people/import/vcard", map[string]any{"text": "BEGIN:VCARD\nVERSION:3.0\nNOTE:无名氏\nEND:VCARD"})
	if rec2.Code != 200 {
		t.Fatalf("无名卡片应 200 skipped, got %d: %s", rec2.Code, rec2.Body.String())
	}
	res := decodeMap(t, rec2)
	if res["total"] != float64(1) || res["imported"] != float64(0) || res["skipped"] != float64(1) {
		t.Errorf("skipped 计数错误: %v", res)
	}
}

// 导出：空库、筛选参数
func TestVCardAPIExportEmptyAndFilter(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(http.MethodGet, "/api/v1/people/export/vcard", nil)
	if rec.Code != 200 {
		t.Fatalf("空库导出 => %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("空库应导出空文本, got %q", rec.Body.String())
	}
	if rec.Header().Get("X-Export-Count") != "0" {
		t.Errorf("X-Export-Count=%q", rec.Header().Get("X-Export-Count"))
	}

	vcardBackupMustCreatePerson(t, s, map[string]any{"name": "甲一", "phone": "10086", "x_abuid": "ABUID-JIA-1"})
	vcardBackupMustCreatePerson(t, s, map[string]any{"name": "乙二", "phone": "10087", "x_abuid": "ABUID-YI-2"})
	// 丙三 没有 x_abuid：导出时 X-ABUID 回退用 person id，回导必须认回同一个人
	vcardBackupMustCreatePerson(t, s, map[string]any{"name": "丙三", "phone": "10088"})

	if r := s.do(http.MethodGet, "/api/v1/people/export/vcard?q=甲一", nil); !strings.Contains(r.Body.String(), "FN:甲一") || strings.Contains(r.Body.String(), "FN:乙二") {
		t.Errorf("q 筛选失败:\n%s", r.Body.String())
	} else if r.Header().Get("X-Export-Count") != "1" {
		t.Errorf("筛选后计数=%q", r.Header().Get("X-Export-Count"))
	}
	// 归档只是隐藏不是删除，导出是备份手段，所以默认含归档；?archived=0 才排除
	aid := ""
	lst := vcardBackupDecodeList(t, s.do(http.MethodGet, "/api/v1/people/?q=乙二", nil))
	if len(lst) == 1 {
		aid, _ = lst[0]["id"].(string)
	}
	if r := s.do(http.MethodPost, "/api/v1/people/"+aid+"/archive", nil); r.Code != 204 {
		t.Fatalf("archive => %d", r.Code)
	}
	if r := s.do(http.MethodGet, "/api/v1/people/export/vcard", nil); !strings.Contains(r.Body.String(), "FN:乙二") {
		t.Errorf("默认导出应包含归档联系人:\n%s", r.Body.String())
	} else if !strings.Contains(r.Body.String(), "X-QIANSI-ARCHIVED:1") {
		t.Errorf("归档联系人应带 X-QIANSI-ARCHIVED 标记:\n%s", r.Body.String())
	}
	if r := s.do(http.MethodGet, "/api/v1/people/export/vcard?archived=0", nil); strings.Contains(r.Body.String(), "FN:乙二") {
		t.Errorf("archived=0 不应包含归档联系人:\n%s", r.Body.String())
	}
	// 导出→回导往返：归档状态要保得住，否则隐藏的记录会重新出现在列表里
	exported := s.do(http.MethodGet, "/api/v1/people/export/vcard", nil).Body.String()
	if r := s.do(http.MethodPost, "/api/v1/people/import/vcard", map[string]any{"text": exported}); r.Code != 200 {
		t.Fatalf("回导失败 => %d %s", r.Code, r.Body.String())
	}
	after := vcardBackupDecodeList(t, s.do(http.MethodGet, "/api/v1/people/?archived=1&q=乙二", nil))
	if len(after) != 1 || after[0]["archived"] != true {
		t.Errorf("回导后应仍为归档: %v", after)
	}
	if all := vcardBackupDecodeList(t, s.do(http.MethodGet, "/api/v1/people/?archived=1", nil)); len(all) != 3 {
		t.Errorf("回导不应产生重复记录: %v", all)
	}
	// 非法整型查询参数走默认值而不是报错
	if r := s.do(http.MethodGet, "/api/v1/people/export/vcard?category_id=abc&grade=x&tag_id=9", nil); r.Code != 200 {
		t.Errorf("非法 query 参数应回退默认值, got %d", r.Code)
	}
}

// ===== vCard 解析分支（单元级，补齐 vcard_test.go 未覆盖的字段） =====

func TestVCardParsePersonBranches(t *testing.T) {
	t.Run("FN only", func(t *testing.T) {
		p, _, ok := vCardToPerson(parseVCardText("BEGIN:VCARD\nFN: 只 名 \nEND:VCARD")[0])
		if !ok || p.Name != "只 名" || p.FamilyName != "" || p.GivenName != "" {
			t.Errorf("got %+v ok=%v", p, ok)
		}
	})
	t.Run("无姓名跳过", func(t *testing.T) {
		if _, _, ok := vCardToPerson(parseVCardText("BEGIN:VCARD\nFN:   \nN:;;;\nEND:VCARD")[0]); ok {
			t.Error("空白姓名应返回 false")
		}
	})
	t.Run("TEL 排序与标签", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:多号\n" +
			"TEL;TYPE=HOME:010-1\n" +
			"TEL;TYPE=CELL:138-2\n" +
			"TEL:  tel:33333  \n" +
			"TEL:\n" +
			"TEL;TYPE=UNKNOWN:4444\n" +
			"END:VCARD")[0]
		p, fields, ok := vCardToPerson(card)
		if !ok {
			t.Fatal("转换失败")
		}
		if p.Phone != "138-2" { // CELL rank 优先于普通 HOME
			t.Errorf("主电话应取 CELL, got %q", p.Phone)
		}
		get := func(i int) (string, string) {
			for _, f := range fields {
				if f.SortOrder == i {
					return f.Label, f.Value
				}
			}
			return "", ""
		}
		if l, v := get(1); l != "住宅电话" || v != "010-1" {
			t.Errorf("HOME TEL: %q %q", l, v)
		}
		if l, v := get(2); l != "电话3" || v != "33333" { // tel: 前缀剥离；idx>1 → 电话N（空 TEL 已跳过，序号前移）
			t.Errorf("裸 TEL: %q %q", l, v)
		}
		if l, v := get(3); l != "电话4" || v != "4444" {
			t.Errorf("第 4 个 TEL: %q %q", l, v)
		}
	})
	t.Run("PREF 优先于 CELL", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:Pref\nTEL;TYPE=CELL:111\nTEL;TYPE=HOME,VOICE,PREF:222\nEND:VCARD")[0]
		p, _, _ := vCardToPerson(card)
		if p.Phone != "222" {
			t.Errorf("PREF 应为主电话, got %q", p.Phone)
		}
	})
	t.Run("性别与昵称截断", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nN:王;二\nGENDER:男\nNICKNAME:小;王,二\nEND:VCARD")[0]
		p, _, _ := vCardToPerson(card)
		if p.Gender != "M" {
			t.Errorf("男→M got %q", p.Gender)
		}
		if p.Nickname != "小" { // IndexAny(",;") 处截断
			t.Errorf("NICKNAME 应截断, got %q", p.Nickname)
		}
		card = parseVCardText("BEGIN:VCARD\nFN:女女\nGENDER:女:v2\nEND:VCARD")[0]
		p, _, _ = vCardToPerson(card)
		if p.Gender != "F" {
			t.Errorf("女→F got %q", p.Gender)
		}
		card = parseVCardText("BEGIN:VCARD\nFN:未知\nGENDER:other\nEND:VCARD")[0]
		p, _, _ = vCardToPerson(card)
		if p.Gender != "" {
			t.Errorf("未知性别应忽略, got %q", p.Gender)
		}
	})
	t.Run("X-GENDER 回退", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:回退\nX-GENDER:f\nEND:VCARD")[0]
		p, _, _ := vCardToPerson(card)
		if p.Gender != "F" {
			t.Errorf("X-GENDER 应转大写采纳, got %q", p.Gender)
		}
		card = parseVCardText("BEGIN:VCARD\nFN:回退\nX-GENDER:other\nEND:VCARD")[0]
		p, _, _ = vCardToPerson(card)
		if p.Gender != "" {
			t.Errorf("非法 X-GENDER 应忽略, got %q", p.Gender)
		}
	})
	t.Run("社交资料与微信QQ", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:社交流\n" +
			"X-SOCIALPROFILE;type=twitter:x-apple://com.apple.social.twitter?username=bob%20x\n" +
			"X-SOCIALPROFILE;type=weibo:x-apple://com.apple.social.weibo\n" +
			"X-SOCIALPROFILE:纯文本无协议\n" +
			"X-WECHAT:wx_id_9\n" +
			"X-QQ:111\n" +
			"END:VCARD")[0]
		p, fields, _ := vCardToPerson(card)
		if p.Wechat != "wx_id_9" { // 非 wechat 社交资料的 username 为空 → 不覆盖；X-WECHAT 兜底
			t.Errorf("X-WECHAT got %q", p.Wechat)
		}
		get := func(label string) string {
			for _, f := range fields {
				if f.Label == label {
					return f.Value
				}
			}
			return ""
		}
		if v := get("Twitter"); v != "bob x" { // QueryUnescape + 首字母大写标签
			t.Errorf("twitter 字段: %q", v)
		}
		if v := get("QQ"); v != "111" {
			t.Errorf("QQ: %q", v)
		}
	})
	t.Run("X-WECHAT 覆盖社交资料", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:双微信\n" +
			"item1.X-SOCIALPROFILE;type=wechat:x-apple://com.apple.social.wechat?username=wx1\n" +
			"item1.X-ABLabel:_$!<instantmessage>!$_\n" +
			"X-WECHAT:wx2\n" +
			"END:VCARD")[0]
		p, _, _ := vCardToPerson(card)
		// X-WECHAT 是非空即赋值，会覆盖前面社交资料解析出的值（固化真实行为）
		if p.Wechat != "wx2" {
			t.Errorf("Wechat=%q want wx2", p.Wechat)
		}
	})
	t.Run("BASE64 字段忽略", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:头像\nPHOTO;ENCODING=BASE64;TYPE=JPEG:AAAA\nEND:VCARD")[0]
		p, _, ok := vCardToPerson(card)
		if !ok || p.Name != "头像" {
			t.Fatal("解析失败")
		}
		if ph := card.get("PHOTO"); ph != nil && ph.value_() != "" {
			t.Errorf("BASE64 值应返回空串, got %q", ph.value_())
		}
	})
	t.Run("ADR 边界", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:地址\nADR;TYPE=HOME:;;只有一条;;;;\nEND:VCARD")[0]
		p, _, _ := vCardToPerson(card)
		if p.Location != "只有一条" {
			t.Errorf("ADR 单段: %q", p.Location)
		}
		card = parseVCardText("BEGIN:VCARD\nFN:空地址\nADR:;;;;;;\nEND:VCARD")[0]
		p, _, _ = vCardToPerson(card)
		if p.Location != "" {
			t.Errorf("空 ADR 不应产生 Location: %q", p.Location)
		}
	})
	t.Run("多 NOTE 与空 NOTE", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:备注\nNOTE:第一条\nNOTE:\nNOTE;ENCODING=QUOTED-PRINTABLE:=E7=AC=AC=E4=BA=8C=E6=9D=A1\nEND:VCARD")[0]
		p, _, _ := vCardToPerson(card)
		if p.Notes != "第一条\n第二条" {
			t.Errorf("NOTE 合并: %q", p.Notes)
		}
	})
	t.Run("EMAIL 与 URL 序号标签", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:序号\nEMAIL:a@b.c\nEMAIL:d@e.f\nURL:https://1\nURL:https://2\nEND:VCARD")[0]
		_, fields, _ := vCardToPerson(card)
		labels := map[string]string{}
		for _, f := range fields {
			labels[f.Label] = f.Value
		}
		if labels["邮箱"] != "a@b.c" || labels["邮箱2"] != "d@e.f" {
			t.Errorf("EMAIL 标签: %v", labels)
		}
		if labels["网址"] != "https://1" || labels["网址2"] != "https://2" {
			t.Errorf("URL 标签: %v", labels)
		}
	})
	t.Run("空 ORG/TITLE/BDAY", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:空值\nORG:\nTITLE: \nBDAY:19900101T000000\nEND:VCARD")[0]
		p, fields, _ := vCardToPerson(card)
		for _, f := range fields {
			if f.Label == "公司" || f.Label == "职位" {
				t.Errorf("空 ORG/TITLE 不应生成字段: %+v", f)
			}
		}
		if p.Birthday != "1990-01-01" {
			t.Errorf("紧凑 BDAY: %q", p.Birthday)
		}
	})
	t.Run("无法解析的行被丢弃", func(t *testing.T) {
		cards := parseVCardText("BEGIN:VCARD\nFN:坏行\n没有冒号的行\n:value 无名\nEND:VCARD")
		p, _, ok := vCardToPerson(cards[0])
		if !ok || p.Name != "坏行" || len(cards[0].props) != 1 {
			t.Errorf("坏行未丢弃: %+v props=%d", p, len(cards[0].props))
		}
	})
	t.Run("卡片外内容忽略", func(t *testing.T) {
		if cards := parseVCardText("BEGIN:VCARD\r\nEND:VCARD\r\nBEGIN:VCARD\r\nFN:ok\r\nEND:VCARD"); len(cards) != 2 {
			t.Fatalf("got %d", len(cards))
		} else if p, _, ok := vCardToPerson(cards[0]); ok {
			t.Errorf("空卡应转换失败, got %+v", p)
		}
	})
	t.Run("QP 软换行与 GB 字符集", func(t *testing.T) {
		// QUOTED-PRINTABLE 行尾 = 软换行（=C4= + =E3…拼回 =C4=E3）+ GB2312 字符集
		text := "BEGIN:VCARD\r\n" +
			"FN:张三丰\r\n" +
			"NOTE;ENCODING=QUOTED-PRINTABLE;CHARSET=GB2312:=C4=\r\n =E3=BA=C3\r\nEND:VCARD"
		card := parseVCardText(text)[0]
		p, _, ok := vCardToPerson(card)
		if !ok || p.Notes != "你好" {
			t.Errorf("QP GB2312 软换行失败: ok=%v notes=%q", ok, p.Notes)
		}
		// ENCODING=QP 简写 + GBK
		card = parseVCardText("BEGIN:VCARD\nFN:裸QP\nNOTE;ENCODING=QP;CHARSET=GBK:=C4=E3=BA=C3\nEND:VCARD")[0]
		p, _, _ = vCardToPerson(card)
		if p.Notes != "你好" {
			t.Errorf("ENCODING=QP 未识别: %q", p.Notes)
		}
		// 行尾孤立的 '='（无后续 hex）按字面量保留
		if got := decodeQP("ab=", "UTF-8"); got != "ab=" {
			t.Errorf("decodeQP 截断: %q", got)
		}
		// GB18030 解码失败的字节序列原样返回
		if got := decodeQP("=FF", "GB2312"); got == "" || strings.Contains(got, "=") {
			t.Errorf("非法 GB 序列应保留原文: %q", got)
		}
	})
	t.Run("空 ABLabel 回退位置标签", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:空标签\n" +
			"TEL;TYPE=CELL:111\n" +
			"item2.TEL:222\n" +
			"item2.X-ABLabel:_$!<>!$_\n" +
			".TEL:999\n" + // 组名段为空 → group=""，仍按无名 TYPE 处理
			"END:VCARD")[0]
		p, fields, _ := vCardToPerson(card)
		if p.Phone != "111" {
			t.Errorf("phone=%q", p.Phone)
		}
		if len(fields) != 2 {
			t.Fatalf("fields=%+v", fields)
		}
		// 空 X-ABLabel 回退 telLabel 位置标签
		if fields[0].Label != "电话2" || fields[0].Value != "222" {
			t.Errorf("空 X-ABLabel 应回退 telLabel: %+v", fields[0])
		}
		if fields[1].Label != "电话3" || fields[1].Value != "999" {
			t.Errorf("第三枚 TEL 位置标签: %+v", fields[1])
		}
	})
	t.Run("BDAY 变体", func(t *testing.T) {
		card := parseVCardText("BEGIN:VCARD\nFN:生日\nBDAY:19901-2\nEND:VCARD")[0]
		p, _, _ := vCardToPerson(card)
		if p.Birthday != "" {
			t.Errorf("畸形 BDAY 应忽略: %q", p.Birthday)
		}
	})
}

// ===== vCard 导出分支（personToVCard 单元级） =====

func TestVCardExportBranches(t *testing.T) {
	t.Run("最少字段", func(t *testing.T) {
		vc := personToVCard(&store.Person{ID: "abc-123", Name: "甲"}, nil)
		lines := strings.Split(vc, "\r\n")
		want := []string{"BEGIN:VCARD", "VERSION:3.0", "PRODID:-//qiansi//CN", "FN:甲", "N:;甲;;;", "X-ABUID:ABC-123:ABPerson", "END:VCARD", ""}
		if len(lines) != len(want) {
			t.Fatalf("行数 %d want %d:\n%s", len(lines), len(want), vc)
		}
		for i := range want {
			if lines[i] != want[i] {
				t.Errorf("line %d: %q want %q", i, lines[i], want[i])
			}
		}
	})
	t.Run("空值字段跳过", func(t *testing.T) {
		vc := personToVCard(&store.Person{ID: "x", Name: "甲"}, []*store.PersonField{{Label: "公司", Value: ""}})
		if strings.Contains(vc, "ORG:") {
			t.Errorf("空值字段应跳过:\n%s", vc)
		}
	})
	t.Run("电话类标签", func(t *testing.T) {
		vc := personToVCard(&store.Person{ID: "x", Name: "甲", Phone: "1"}, []*store.PersonField{
			{Label: "手机", Value: "2"},
			{Label: "住宅电话", Value: "3"},
			{Label: "工作电话", Value: "4"},
			{Label: "电话二", Value: "5"},
			{Label: "传真", Value: "6"},
			{Label: "神秘字段", Value: "7"},
		})
		for _, frag := range []string{
			"TEL;type=CELL;type=VOICE;type=pref:2",
			"TEL;type=HOME;type=VOICE:3",
			"TEL;type=WORK;type=VOICE:4",
			"TEL;type=VOICE;item1:5",
			"item1.X-ABLabel:电话二",
			"TEL;type=VOICE;item2:6",
			"item2.X-ABLabel:传真",
			"NOTE:神秘字段: 7",
		} {
			if !strings.Contains(vc, frag) {
				t.Errorf("缺少 %q:\n%s", frag, vc)
			}
		}
	})
	t.Run("邮箱网址分组标签", func(t *testing.T) {
		vc := personToVCard(&store.Person{ID: "x", Name: "甲"}, []*store.PersonField{
			{Label: "邮箱3", Value: "a@b.c"},
			{Label: "网址4", Value: "https://u"},
			{Label: "邮箱", Value: "p@q.r"},
			{Label: "住宅", Value: "h@q.r"},
			{Label: "工作", Value: "w@q.r"},
		})
		for _, frag := range []string{
			"EMAIL;type=INTERNET;item1:a@b.c",
			"item1.X-ABLabel:邮箱3",
			"URL;item2:https://u",
			"item2.X-ABLabel:网址4",
			"EMAIL;type=INTERNET;type=pref:p@q.r",
			"EMAIL;type=INTERNET;type=home:h@q.r",
			"EMAIL;type=INTERNET;type=work:w@q.r",
		} {
			if !strings.Contains(vc, frag) {
				t.Errorf("缺少 %q:\n%s", frag, vc)
			}
		}
	})
	t.Run("转义", func(t *testing.T) {
		got := escapeVCardValue("a\\b;c,d\ne\r\nf\rg")
		want := `a\\b\;c\,d\ne\nf\ng`
		if got != want {
			t.Errorf("escapeVCardValue=%q want %q", got, want)
		}
		vc := personToVCard(&store.Person{ID: "x", Name: "甲;乙", Notes: "行1\r\n行2"}, nil)
		if !strings.Contains(vc, `FN:甲\;乙`) || !strings.Contains(vc, `NOTE:行1\n行2`) {
			t.Errorf("导出转义失败:\n%s", vc)
		}
	})
	t.Run("可选字段全开", func(t *testing.T) {
		vc := personToVCard(&store.Person{
			ID: "x", Name: "甲", FamilyName: "甲", GivenName: "乙", Nickname: "小",
			Phone: "1", Wechat: "wx", Location: "某地", Birthday: "2000-01-01",
			Gender: "F", Notes: "n", XAbUID: "keep-me",
		}, nil)
		for _, frag := range []string{
			"N:甲;乙;;;", "NICKNAME:小", "X-SOCIALPROFILE;type=wechat", "ADR;type=HOME;type=pref:;;某地;;;;",
			"BDAY:2000-01-01", "X-GENDER:F", "NOTE:n", "X-ABUID:KEEP-ME:ABPerson",
		} {
			if !strings.Contains(vc, frag) {
				t.Errorf("缺少 %q:\n%s", frag, vc)
			}
		}
	})
	t.Run("性别非 M/F 不导出", func(t *testing.T) {
		vc := personToVCard(&store.Person{ID: "x", Name: "甲", Gender: "other"}, nil)
		if strings.Contains(vc, "GENDER") {
			t.Errorf("异常性别不应导出:\n%s", vc)
		}
	})
	t.Run("telExportSpec", func(t *testing.T) {
		cases := []struct {
			label, typ, custom string
			ok                 bool
		}{
			{"手机", "type=CELL;type=VOICE;type=pref", "", true},
			{"住宅电话", "type=HOME;type=VOICE", "", true},
			{"工作电话", "type=WORK;type=VOICE", "", true},
			{"电话2", "type=VOICE", "电话2", true},
			{"公司传真", "type=VOICE", "公司传真", true},
			{"邮箱", "", "", false},
		}
		for _, c := range cases {
			typ, custom, ok := telExportSpec(c.label)
			if ok != c.ok || typ != c.typ || custom != c.custom {
				t.Errorf("telExportSpec(%q)=(%q,%q,%v) want (%q,%q,%v)", c.label, typ, custom, ok, c.typ, c.custom, c.ok)
			}
		}
	})
}

// ===== backup =====

func TestBackupExportDownload(t *testing.T) {
	s := newTestServer(t)
	// 先 snapshot 让 backups 目录存在
	snap := s.do(http.MethodPost, "/api/v1/backup/snapshot", nil)
	if snap.Code != 200 {
		t.Fatalf("snapshot => %d: %s", snap.Code, snap.Body.String())
	}
	snapPath := decodeMap(t, snap)["path"].(string)
	data, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("snapshot 文件不存在: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("SQLite format 3\x00")) {
		t.Errorf("snapshot 不是 SQLite 文件, len=%d", len(data))
	}
	if fi, _ := os.Stat(filepath.Dir(snapPath)); fi == nil {
		t.Fatal("backups dir missing")
	}

	vcardBackupMustCreatePerson(t, s, map[string]any{"name": "备份的人", "phone": "13700000000"})

	rec := s.do(http.MethodGet, "/api/v1/backup/export", nil)
	if rec.Code != 200 {
		t.Fatalf("export => %d: %s", rec.Code, rec.Body.String())
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `attachment; filename="qiansi-`) || !strings.HasSuffix(cd, `.db"`) {
		t.Errorf("Content-Disposition=%q", cd)
	}
	body := rec.Body.Bytes()
	if !bytes.HasPrefix(body, []byte("SQLite format 3\x00")) {
		t.Fatalf("导出缺少 SQLite 头, len=%d", len(body))
	}
	if !bytes.Contains(body, []byte("备份的人")) {
		t.Errorf("导出库应包含已写入的联系人数据")
	}
	// 临时导出文件用完即删
	entries, _ := os.ReadDir(s.Cfg.Backups)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "export-") {
			t.Errorf("临时导出文件未清理: %s", e.Name())
		}
	}
}

func TestBackupList(t *testing.T) {
	s := newTestServer(t)
	// 目录不存在 → ReadDir 失败 → 空数组
	rec := s.do(http.MethodGet, "/api/v1/backup/list", nil)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("空目录 list=%d %s", rec.Code, rec.Body.String())
	}
	// 真实 snapshot
	if r := s.do(http.MethodPost, "/api/v1/backup/snapshot", nil); r.Code != 200 {
		t.Fatalf("snapshot => %s", r.Body.String())
	}

	// 手工造 25 个 .db + 干扰项（非 db、目录）
	os.MkdirAll(s.Cfg.Backups, 0o755)
	for i := 1; i <= 25; i++ {
		name := fmt.Sprintf("qiansi-20240101-0000%02d.db", i)
		if err := os.WriteFile(filepath.Join(s.Cfg.Backups, name), bytes.Repeat([]byte("x"), i), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(s.Cfg.Backups, "notes.txt"), []byte("skip"), 0o644)
	os.MkdirAll(filepath.Join(s.Cfg.Backups, "dir.db"), 0o755)

	list := vcardBackupDecodeList(t, s.do(http.MethodGet, "/api/v1/backup/list", nil))
	if len(list) != 20 {
		t.Fatalf("应截断到 20, got %d", len(list))
	}
	prev := ""
	for i, it := range list {
		name, _ := it["name"].(string)
		if strings.HasSuffix(name, ".txt") || name == "dir.db" {
			t.Errorf("过滤失败: %v", name)
		}
		if i > 0 && name >= prev {
			t.Errorf("未按名称降序: %v after %v", name, prev)
		}
		prev = name
		if sz, _ := it["size"].(float64); sz <= 0 {
			t.Errorf("size 错误: %v", it)
		}
		if tm, _ := it["time"].(string); len(tm) < 10 {
			t.Errorf("time 错误: %v", it)
		}
	}
	// snapshot 产生的真实文件时间戳最新，字典序最大，应排第一
	if first := list[0]["name"].(string); !strings.HasPrefix(first, "qiansi-"+fmt.Sprintf("%d", time.Now().Year())) {
		t.Errorf("首个应为今日 snapshot, got %s", first)
	}
}

// vcardBackupSeedVersions 补齐 schema_version 记录：
// testdb 直接执行迁移文件不写版本号，而生产库由 RunMigrations 写入 1..6；
// 恢复流程末尾会跑 RunMigrations，缺版本号会重放 002+ 导致 duplicate column。
func vcardBackupSeedVersions(t *testing.T, s *testServer) {
	t.Helper()
	if _, err := s.Store.DB.Exec("INSERT INTO schema_version(version) VALUES(1),(2),(3),(4),(5),(6)"); err != nil {
		t.Fatalf("seed schema_version: %v", err)
	}
}

func TestBackupRestoreHappyPath(t *testing.T) {
	s := newTestServer(t)
	vcardBackupSeedVersions(t, s)
	vcardBackupMustCreatePerson(t, s, map[string]any{"name": "留存者", "phone": "13611111111"})
	if r := s.do(http.MethodPost, "/api/v1/backup/snapshot", nil); r.Code != 200 {
		t.Fatalf("snapshot => %s", r.Body.String())
	}
	expRec := s.do(http.MethodGet, "/api/v1/backup/export", nil)
	if expRec.Code != 200 {
		t.Fatalf("export => %d", expRec.Code)
	}
	snapshot := expRec.Body.Bytes()

	// 快照之后再写一个人，恢复后应消失
	vcardBackupMustCreatePerson(t, s, map[string]any{"name": "将被回滚的人"})
	body, ct := vcardBackupMultipart(t, "backup.db", snapshot)
	rec := s.raw(http.MethodPost, "/api/v1/backup/restore", body, ct)
	if rec.Code != 200 {
		t.Fatalf("restore => %d: %s", rec.Code, rec.Body.String())
	}
	res := decodeMap(t, rec)
	if res["ok"] != true || res["people"] != float64(1) {
		t.Fatalf("restore 响应错误: %v", res)
	}

	list := vcardBackupDecodeList(t, s.do(http.MethodGet, "/api/v1/people/", nil))
	if len(list) != 1 || list[0]["name"] != "留存者" {
		t.Fatalf("恢复后数据不符: %v", list)
	}
	if p := list[0]; p["phone"] != "13611111111" {
		t.Errorf("恢复后字段丢失: %v", p)
	}
	// 恢复后 DBPath 已落成真实文件（旧 WAL 边车在替换时被清除，
	// 新连接随后会按 WAL 模式重建自己的边车文件）
	if _, err := os.Stat(s.Cfg.DBPath); err != nil {
		t.Errorf("恢复后 DBPath 应存在: %v", err)
	}
	// 恢复前当前库先归档（此时 DBPath 存在，再恢复一次触发归档分支）
	body2, ct2 := vcardBackupMultipart(t, "backup.db", snapshot)
	rec2 := s.raw(http.MethodPost, "/api/v1/backup/restore", body2, ct2)
	if rec2.Code != 200 {
		t.Fatalf("第二次 restore => %d: %s", rec2.Code, rec2.Body.String())
	}
	entries, _ := os.ReadDir(s.Cfg.Backups)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "before-restore-") {
			found = true
		}
	}
	if !found {
		t.Errorf("第二次恢复应生成 before-restore 归档: %v", entries)
	}

	// snapshotDB 拷贝兜底路径：Backups 变成普通文件 → VACUUM INTO 失败 →
	// checkpoint+打开真实 DBPath 成功 → os.Create 失败 → 500
	second := newTestServer(t)
	vcardBackupSeedVersions(t, second)
	vcardBackupMustCreatePerson(t, second, map[string]any{"name": "兜底"})
	body3, ct3 := vcardBackupMultipart(t, "b.db", snapshot)
	if r := second.raw(http.MethodPost, "/api/v1/backup/restore", body3, ct3); r.Code != 200 {
		t.Fatalf("restore(兜底库) => %d: %s", r.Code, r.Body.String())
	}
	os.RemoveAll(second.Cfg.Backups)
	os.WriteFile(second.Cfg.Backups, []byte("i am a file"), 0o644)
	r500 := second.do(http.MethodGet, "/api/v1/backup/export", nil)
	if r500.Code != 500 || !strings.Contains(vcardBackupErr(t, r500), "生成快照失败") {
		t.Errorf("兜底失败应 500 生成快照失败, got %d: %s", r500.Code, r500.Body.String())
	}
	if r := second.do(http.MethodPost, "/api/v1/backup/snapshot", nil); r.Code != 500 {
		t.Errorf("backups 为文件时 snapshot 应 500, got %d", r.Code)
	}
	if r := second.do(http.MethodGet, "/api/v1/backup/list", nil); r.Code != 200 || strings.TrimSpace(r.Body.String()) != "[]" {
		t.Errorf("backups 为文件时 list 应空数组, got %d: %s", r.Code, r.Body.String())
	}
}

func TestBackupRestoreErrors(t *testing.T) {
	t.Run("非 multipart 请求体", func(t *testing.T) {
		s := newTestServer(t)
		rec := s.raw(http.MethodPost, "/api/v1/backup/restore", []byte(`{"a":1}`), "application/json")
		if rec.Code != 400 || !strings.Contains(vcardBackupErr(t, rec), "上传解析失败") {
			t.Errorf("got %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("缺少 file 字段", func(t *testing.T) {
		s := newTestServer(t)
		body, ct := vcardBackupMultipart(t, "", nil)
		rec := s.raw(http.MethodPost, "/api/v1/backup/restore", body, ct)
		if rec.Code != 400 || decodeMap(t, rec)["error"] != "file required" {
			t.Errorf("got %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("非 SQLite 内容", func(t *testing.T) {
		s := newTestServer(t)
		body, ct := vcardBackupMultipart(t, "x.db", []byte(strings.Repeat("这不是数据库", 20)))
		rec := s.raw(http.MethodPost, "/api/v1/backup/restore", body, ct)
		if rec.Code != 400 || !strings.Contains(vcardBackupErr(t, rec), "不是有效的 SQLite") {
			t.Errorf("got %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("头部不足 16 字节", func(t *testing.T) {
		s := newTestServer(t)
		body, ct := vcardBackupMultipart(t, "x.db", []byte("SQLite format 3"))
		rec := s.raw(http.MethodPost, "/api/v1/backup/restore", body, ct)
		if rec.Code != 400 {
			t.Errorf("短文件应 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("有头无体损坏库", func(t *testing.T) {
		s := newTestServer(t)
		junk := append([]byte("SQLite format 3\x00"), bytes.Repeat([]byte{0xDE, 0xAD}, 2048)...)
		body, ct := vcardBackupMultipart(t, "x.db", junk)
		rec := s.raw(http.MethodPost, "/api/v1/backup/restore", body, ct)
		if rec.Code != 400 {
			t.Fatalf("损坏库应 400, got %d: %s", rec.Code, rec.Body.String())
		}
		if m := vcardBackupErr(t, rec); !strings.Contains(m, "数据库无法打开") && !strings.Contains(m, "数据库结构不符合预期") {
			t.Errorf("error=%q", m)
		}
	})
}

// 每个场景用独立服务：快照文件名秒级时间戳，同秒重复 snapshot 会撞名
func TestBackupDailyPrune(t *testing.T) {
	qiansiNames := func(dir string) []string {
		entries, _ := os.ReadDir(dir)
		names := []string{}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "qiansi-") && strings.HasSuffix(e.Name(), ".db") {
				names = append(names, e.Name())
			}
		}
		return names
	}

	t.Run("keep=2 删除最旧", func(t *testing.T) {
		s := newTestServer(t)
		os.MkdirAll(s.Cfg.Backups, 0o755)
		for i := 1; i <= 5; i++ {
			if err := os.WriteFile(filepath.Join(s.Cfg.Backups, fmt.Sprintf("qiansi-20230101-00000%d.db", i)), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(s.Cfg.Backups, "not-qiansi-20230101-000001.db"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		s.API.dailyBackupIfNeeded(context.Background(), 2)
		names := qiansiNames(s.Cfg.Backups)
		// 新建 1 个今日快照，总数裁剪到 2：今日快照 + 最旧的 1 个应已被删，只留最新 2 个
		if len(names) != 2 {
			t.Fatalf("keep=2 应剩 2 个 qiansi 快照: %v", names)
		}
		for _, n := range names {
			if strings.Contains(n, "20230101-000001") || strings.Contains(n, "20230101-000002") || strings.Contains(n, "20230101-000003") || strings.Contains(n, "20230101-000004") {
				t.Errorf("最旧快照未删除: %v", names)
			}
		}
		if _, err := os.Stat(filepath.Join(s.Cfg.Backups, "not-qiansi-20230101-000001.db")); err != nil {
			t.Errorf("非 qiansi 前缀文件不应被动: %v", err)
		}
		// 今日快照是真实 SQLite 文件
		for _, n := range names {
			if strings.HasPrefix(n, "qiansi-2023") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(s.Cfg.Backups, n))
			if err != nil || !bytes.HasPrefix(data, []byte("SQLite format 3\x00")) {
				t.Errorf("快照 %s 不是有效 SQLite: err=%v", n, err)
			}
		}
	})

	t.Run("keep<=0 默认 7", func(t *testing.T) {
		s := newTestServer(t)
		os.MkdirAll(s.Cfg.Backups, 0o755)
		for i := 1; i <= 8; i++ {
			if err := os.WriteFile(filepath.Join(s.Cfg.Backups, fmt.Sprintf("qiansi-20230301-00000%d.db", i)), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		s.API.dailyBackupIfNeeded(context.Background(), 0) // keep<=0 → 7
		names := qiansiNames(s.Cfg.Backups)
		if len(names) != 7 {
			t.Fatalf("默认 keep=7 应剩 7 个: %v", names)
		}
		if strings.Contains(names[len(names)-1], "000001") || strings.Contains(names[len(names)-1], "000002") {
			t.Errorf("降序尾应为最新: %v", names)
		}
	})

	t.Run("不超过 keep 不清理", func(t *testing.T) {
		s := newTestServer(t)
		os.MkdirAll(s.Cfg.Backups, 0o755)
		for i := 1; i <= 6; i++ {
			if err := os.WriteFile(filepath.Join(s.Cfg.Backups, fmt.Sprintf("qiansi-20230401-00000%d.db", i)), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		s.API.DailyBackupTick(context.Background()) // keep=7，6+1=7 个不裁剪
		if names := qiansiNames(s.Cfg.Backups); len(names) != 7 {
			t.Errorf("Tick 后应恰好 7 个: %v", names)
		}
	})

	// snapshot 失败分支：Backups 是普通文件 → 只记日志不 panic
	t.Run("快照失败仅记日志", func(t *testing.T) {
		bad := newTestServer(t)
		if err := os.WriteFile(bad.Cfg.Backups, []byte("file"), 0o644); err != nil {
			t.Fatal(err)
		}
		bad.API.DailyBackupTick(context.Background())
		data, err := os.ReadFile(bad.Cfg.Backups)
		if err != nil || string(data) != "file" {
			t.Errorf("失败路径不应改动 Backups: %v %q", err, data)
		}
	})
}
