package api

import (
	"strings"
	"testing"

	"github.com/qiansi/app/internal/store"
)

func TestSplitLines(t *testing.T) {
	// RFC 折叠（空格开头）+ QP 软换行（行尾 =）
	in := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:张\r\n 三丰\r\nNOTE;ENCODING=QUOTED-PRINTABLE;CHARSET=UTF-8:=E4\xBD\xA0\r\n =E5\xA5\xBD=\r\n END\r\nEND:VCARD"
	lines := splitLines(in)
	var fn, note string
	for _, l := range lines {
		if p := parseProperty(l); p != nil {
			switch p.name {
			case "FN":
				fn = p.value_()
			case "NOTE":
				note = p.value_()
			}
		}
	}
	if fn != "张三丰" {
		t.Errorf("FN unfold 失败, got %q", fn)
	}
	if note != "你好END" {
		t.Errorf("QP 软换行失败, got %q", note)
	}
}

func TestParseProperty(t *testing.T) {
	cases := []struct {
		line   string
		name   string
		typ    string
		value  string
	}{
		{"TEL;TYPE=CELL:13800138000", "TEL", "CELL", "13800138000"},
		{"TEL;TYPE=HOME,VOICE:123", "TEL", "HOME", "123"},
		{"tel;CELL:123", "TEL", "CELL", "123"}, // v2.1 裸参数
		{"item1.URL:https://a.b/c?x=1", "URL", "", "https://a.b/c?x=1"},
		{"NOTE:备注\\;含分号\\n第二行", "NOTE", "", "备注;含分号\n第二行"},
		{"EMAIL;TYPE=INTERNET;TYPE=HOME:a@b.com", "EMAIL", "HOME", "a@b.com"},
	}
	for _, c := range cases {
		p := parseProperty(c.line)
		if p == nil {
			t.Fatalf("%q 解析失败", c.line)
		}
		if p.name != c.name {
			t.Errorf("%q name=%q want %q", c.line, p.name, c.name)
		}
		if c.typ != "" && !p.hasParam("TYPE", c.typ) {
			t.Errorf("%q 缺少 TYPE=%s", c.line, c.typ)
		}
		if got := p.value_(); got != c.value {
			t.Errorf("%q value=%q want %q", c.line, got, c.value)
		}
	}
}

func TestDecodeQP_GBK(t *testing.T) {
	// "你好" 的 GBK 编码: C4 E3 BA C3
	got := decodeQP("=C4=E3=BA=C3", "GB2312")
	if got != "你好" {
		t.Errorf("GBK QP 解码失败, got %q", got)
	}
	got = decodeQP("=E4=BD=A0=E5=A5=BD", "UTF-8")
	if got != "你好" {
		t.Errorf("UTF-8 QP 解码失败, got %q", got)
	}
}

func TestVCardToPerson(t *testing.T) {
	text := "BEGIN:VCARD\n" +
		"VERSION:3.0\n" +
		"N:张;三丰;;;\n" +
		"FN:张三丰\n" +
		"NICKNAME:张真人\n" +
		"TEL;TYPE=CELL:13800138000\n" +
		"TEL;TYPE=WORK:010-88888888\n" +
		"EMAIL;TYPE=HOME:zsf@example.com\n" +
		"EMAIL;TYPE=WORK:zsf2@example.com\n" +
		"ADR;TYPE=HOME:;;武当山路1号;十堰市;湖北省;;中国\n" +
		"ORG:武当派;太极组\n" +
		"TITLE:掌门\n" +
		"URL:https://wudang.example.com\n" +
		"BDAY:1247-05-15\n" +
		"GENDER:M\n" +
		"NOTE:太极创始人\n" +
		"END:VCARD"
	cards := parseVCardText(text)
	if len(cards) != 1 {
		t.Fatalf("应有 1 张 vCard, got %d", len(cards))
	}
	p, fields, ok := vCardToPerson(cards[0])
	if !ok {
		t.Fatal("转换失败")
	}
	if p.Name != "张三丰" || p.FamilyName != "张" || p.GivenName != "三丰" || p.Nickname != "张真人" || p.Phone != "13800138000" ||
		p.Gender != "M" || p.Birthday != "1247-05-15" || p.Location != "武当山路1号 十堰市 湖北省" ||
		p.Notes != "太极创始人" {
		t.Errorf("Person 字段不符: %+v", p)
	}
	get := func(label string) string {
		for _, f := range fields {
			if f.Label == label {
				return f.Value
			}
		}
		return ""
	}
	if got := get("工作电话"); got != "010-88888888" {
		t.Errorf("第二 TEL 应为自定义字段, got %q", got)
	}
	if got := get("住宅"); got != "zsf@example.com" {
		t.Errorf("EMAIL1(HOME), got %q", got)
	}
	if got := get("工作"); got != "zsf2@example.com" {
		t.Errorf("EMAIL2(WORK), got %q", got)
	}
	if got := get("公司"); got != "武当派;太极组" {
		t.Errorf("ORG, got %q", got)
	}
	if got := get("职位"); got != "掌门" {
		t.Errorf("TITLE, got %q", got)
	}
	if got := get("网址"); got != "https://wudang.example.com" {
		t.Errorf("URL, got %q", got)
	}
}

func TestVCardToPerson_NoFN_UsesN(t *testing.T) {
	// 无 FN，中文 N: 姓在前
	text := "BEGIN:VCARD\nVERSION:2.1\nN:王;小明;;;\nTEL:13900000000\nEND:VCARD"
	p, _, ok := vCardToPerson(parseVCardText(text)[0])
	if !ok || p.Name != "王小明" {
		t.Errorf("中文 N 拼接失败: ok=%v name=%q", ok, p.Name)
	}
	if p.FamilyName != "王" || p.GivenName != "小明" {
		t.Errorf("姓/名拆分失败: family=%q given=%q", p.FamilyName, p.GivenName)
	}
	// 无 FN，西文 N: given 在后
	text = "BEGIN:VCARD\nVERSION:3.0\nN:Doe;John;;;\nEND:VCARD"
	p, _, ok = vCardToPerson(parseVCardText(text)[0])
	if !ok || p.Name != "John Doe" {
		t.Errorf("西文 N 拼接失败: ok=%v name=%q", ok, p.Name)
	}
	if p.FamilyName != "Doe" || p.GivenName != "John" {
		t.Errorf("西文姓/名拆分失败: family=%q given=%q", p.FamilyName, p.GivenName)
	}
}

// Apple 通讯录「名字顺序」设为「名 姓」时，FN 会是「名 姓」；
// 导入必须以 N 的姓/名为准，不能照搬 FN 顺序
func TestVCardToPerson_NWinsOverFN(t *testing.T) {
	text := "BEGIN:VCARD\nVERSION:3.0\nN:王;小明;;;\nFN:小明 王\nEND:VCARD"
	p, _, ok := vCardToPerson(parseVCardText(text)[0])
	if !ok || p.Name != "王小明" {
		t.Errorf("N 应优先于 FN 的显示顺序: ok=%v name=%q", ok, p.Name)
	}
	if p.FamilyName != "王" || p.GivenName != "小明" {
		t.Errorf("姓/名应为 N 原始拆分: family=%q given=%q", p.FamilyName, p.GivenName)
	}
}

func TestNormalizeBirthday(t *testing.T) {
	cases := map[string]string{
		"1990-01-02":           "1990-01-02",
		"19900102":             "1990-01-02",
		"1990-01-02T08:00:00":  "1990-01-02",
		"1990/01/02":           "1990-01-02",
		"1990-01-02T08:00:00Z": "1990-01-02",
		"--01-02":              "", // 无年份不支持
		"1990":                 "",
		"":                     "",
	}
	for in, want := range cases {
		if got := normalizeBirthday(in); got != want {
			t.Errorf("normalizeBirthday(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseVCardText_Multiple(t *testing.T) {
	text := "BEGIN:VCARD\nFN:A\nEND:VCARD\n垃圾内容\nBEGIN:VCARD\nFN:B\nEND:VCARD"
	cards := parseVCardText(text)
	if len(cards) != 2 {
		t.Fatalf("应有 2 张, got %d", len(cards))
	}
	if cards[0].displayName() != "A" || cards[1].displayName() != "B" {
		t.Errorf("姓名解析错误")
	}
}

// macOS 联系人.app 导出的典型卡片结构
func TestVCardToPerson_AppleContact(t *testing.T) {
	text := "BEGIN:VCARD\r\n" +
		"VERSION:3.0\r\n" +
		"PRODID:-//Apple Inc.//macOS 15.0//EN\r\n" +
		"N:王;小明;;;\r\n" +
		"FN:王小明\r\n" +
		"NICKNAME:小王\r\n" +
		"X-ABUID:8A5D5E1D-1234-4C5D-9E6F-AABBCCDDEEFF:ABPerson\r\n" +
		"TEL;type=CELL;type=VOICE;type=pref:13800138000\r\n" +
		"TEL;type=HOME;type=VOICE:010-66666666\r\n" +
		"item1.EMAIL;type=INTERNET;type=pref:xm@icloud.com\r\n" +
		"item1.X-ABLabel:_$!<Work>!$_\r\n" +
		"item2.EMAIL;type=INTERNET:xm@qq.com\r\n" +
		"item2.X-ABLabel:邮箱小号\r\n" +
		"item3.URL:https://blog.example.com\r\n" +
		"item3.X-ABLabel:_$!<HomePage>!$_\r\n" +
		"item4.X-SOCIALPROFILE;type=wechat:x-apple://com.apple.social.wechat?username=xiaoming\r\n" +
		"item4.X-ABLabel:_$!<instantmessage>!$_\r\n" +
		"ORG:某科技公司;研发部\r\n" +
		"TITLE:工程师\r\n" +
		"X-QQ:87654321\r\n" +
		"BDAY:1990-01-02\r\n" +
		"ADR;type=HOME;type=pref:;;中关村大街1号;北京市;北京市;100000;中国\r\n" +
		"X-GENDER:M\r\n" +
		"NOTE:大学同学\\n登山爱好者\r\n" +
		"END:VCARD"
	cards := parseVCardText(text)
	if len(cards) != 1 {
		t.Fatalf("应有 1 张, got %d", len(cards))
	}
	p, fields, ok := vCardToPerson(cards[0])
	if !ok {
		t.Fatal("转换失败")
	}
	if p.Name != "王小明" || p.Nickname != "小王" {
		t.Errorf("姓名/昵称错误: %+v", p)
	}
	if p.XAbUID != "8A5D5E1D-1234-4C5D-9E6F-AABBCCDDEEFF" {
		t.Errorf("X-ABUID 未剥离 ABPerson 后缀: %q", p.XAbUID)
	}
	if p.Phone != "13800138000" {
		t.Errorf("pref TEL 应为主电话, got %q", p.Phone)
	}
	if p.Wechat != "xiaoming" {
		t.Errorf("X-SOCIALPROFILE wechat 未解析: %q", p.Wechat)
	}
	if p.Birthday != "1990-01-02" || p.Gender != "M" ||
		p.Location != "中关村大街1号 北京市 北京市" || p.Notes != "大学同学\n登山爱好者" {
		t.Errorf("基础字段错误: %+v", p)
	}
	get := func(label string) string {
		for _, f := range fields {
			if f.Label == label {
				return f.Value
			}
		}
		return ""
	}
	if got := get("住宅电话"); got != "010-66666666" {
		t.Errorf("HOME TEL, got %q", got)
	}
	if got := get("工作"); got != "xm@icloud.com" {
		t.Errorf("item EMAIL X-ABLabel(_$!<Work>!$_) 未映射, got %q", got)
	}
	if got := get("邮箱小号"); got != "xm@qq.com" {
		t.Errorf("自定义 EMAIL 标签丢失, got %q", got)
	}
	if got := get("主页"); got != "https://blog.example.com" {
		t.Errorf("item URL X-ABLabel(_$!<HomePage>!$_) 未映射, got %q", got)
	}
	if got := get("QQ"); got != "87654321" {
		t.Errorf("X-QQ, got %q", got)
	}
	if got := get("公司"); got != "某科技公司;研发部" {
		t.Errorf("ORG, got %q", got)
	}
}

// 导出 → 重新解析：核心字段往返一致
func TestRoundTrip_ExportImport(t *testing.T) {
	src := &store.Person{
		ID: "11111111-2222-3333-4444-555555555555",
		Name: "张三丰", FamilyName: "张", GivenName: "三丰",
		Nickname: "张真人", Phone: "13800138000", Wechat: "zsf",
		Location: "武当山路1号 十堰市", Birthday: "1247-05-15", Gender: "M",
		Notes: "太极创始人\n爱好喝茶", XAbUID: "AAAABBBB-CCCC-DDDD-EEEE-FFFF00001111",
	}
	fields := []*store.PersonField{
		{Label: "工作电话", Value: "010-88888888", SortOrder: 1},
		{Label: "邮箱", Value: "zsf@wudang.com", SortOrder: 100},
		{Label: "住宅", Value: "zsf@qq.com", SortOrder: 101},
		{Label: "QQ", Value: "123456", SortOrder: 210},
		{Label: "军功章", Value: "三枚", SortOrder: 999},
	}
	vc := personToVCard(src, fields)
	if !strings.Contains(vc, "X-ABUID:AAAABBBB-CCCC-DDDD-EEEE-FFFF00001111:ABPerson\r\n") {
		t.Errorf("导出缺少 X-ABUID:\n%s", vc)
	}
	if !strings.Contains(vc, "EMAIL;type=INTERNET;type=home:zsf@qq.com\r\n") {
		t.Errorf("住宅邮箱应导出为 type=home:\n%s", vc)
	}
	if !strings.Contains(vc, "\\n") {
		t.Errorf("NOTE 换行未转义:\n%s", vc)
	}

	// 重新解析
	card := parseVCardText(vc)[0]
	p2, f2, ok := vCardToPerson(card)
	if !ok {
		t.Fatal("再解析失败")
	}
	if p2.Name != src.Name || p2.FamilyName != src.FamilyName || p2.GivenName != src.GivenName ||
		p2.Nickname != src.Nickname || p2.Phone != src.Phone ||
		p2.Wechat != src.Wechat || p2.Location != src.Location || p2.Birthday != src.Birthday ||
		p2.Gender != src.Gender || p2.XAbUID != src.XAbUID {
		t.Errorf("往返字段不一致:\n src=%+v\n dst=%+v", src, p2)
	}
	if !strings.HasPrefix(p2.Notes, src.Notes) || !strings.Contains(p2.Notes, "军功章: 三枚") {
		t.Errorf("备注往返不一致(自定义字段应并入 NOTE): %q", p2.Notes)
	}
	get := func(label string) string {
		for _, f := range f2 {
			if f.Label == label {
				return f.Value
			}
		}
		return ""
	}
	if got := get("工作电话"); got != "010-88888888" {
		t.Errorf("往返 TEL 丢失, got %q", got)
	}
	if got := get("邮箱"); got != "zsf@wudang.com" {
		t.Errorf("往返 EMAIL 丢失, got %q", got)
	}
	if got := get("住宅"); got != "zsf@qq.com" {
		t.Errorf("往返住宅邮箱丢失, got %q", got)
	}
	if got := get("QQ"); got != "123456" {
		t.Errorf("往返 QQ 丢失, got %q", got)
	}
	// 非标准字段并入 NOTE
	if !strings.Contains(p2.Notes, "军功章: 三枚") {
		t.Errorf("自定义字段应并入 NOTE, notes=%q", p2.Notes)
	}
}

// 无 X-ABUID 的联系人导出时用 person.id 作为稳定 UID
func TestRoundTrip_StableUID(t *testing.T) {
	src := &store.Person{ID: "99998888-7777-6666-5555-444433332222", Name: "李四", Phone: "139"}
	vc := personToVCard(src, nil)
	card := parseVCardText(vc)[0]
	p2, _, ok := vCardToPerson(card)
	if !ok || p2.XAbUID != "99998888-7777-6666-5555-444433332222" {
		t.Errorf("person.id 应作为稳定 UID, ok=%v uid=%q", ok, p2.XAbUID)
	}
}
