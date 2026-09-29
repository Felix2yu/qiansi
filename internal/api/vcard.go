package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/qiansi/app/internal/store"
)

// POST /api/v1/people/import/vcard
// Body: {"text": "<vCard 文件内容>"}  上限 20MB
// 响应: {"total": n, "imported": n, "skipped": n, "people": [新创建的联系人]}

const vcardMaxBody = 20 << 20

type vProperty struct {
	name   string
	group  string              // item1.TEL -> "item1"（Apple 分组标签关联）
	params map[string][]string // 参数名(大写) -> 值列表
	value  string
}

func (p *vProperty) hasParam(key string, vals ...string) bool {
	for _, v := range p.params[key] {
		uv := strings.ToUpper(v)
		for _, want := range vals {
			if uv == strings.ToUpper(want) {
				return true
			}
		}
	}
	return false
}

func (p *vProperty) firstParam(key string) string {
	if vs := p.params[key]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func (p *vProperty) isQuotedPrintable() bool {
	if p.hasParam("ENCODING", "QUOTED-PRINTABLE", "QP") {
		return true
	}
	// vCard 2.1 裸参数形式: NOTE;ENCODING=QP 或 NOTE;QP
	return p.hasParam("ENCODING", "QP")
}

// splitLines 按行拆分并处理折叠：
//   - RFC 2426/6350 折叠：下一行以空格或 Tab 开头
//   - vCard 2.1 QUOTED-PRINTABLE 软换行：上一行以 '=' 结尾
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	raw := strings.Split(text, "\n")

	var out []string
	for i := 0; i < len(raw); i++ {
		line := raw[i]
		// 交替处理两种续行，直到无法继续：
		for {
			// QP 软换行：行尾 '=' 且该行声明了 QP 编码；
			// 若下一行是 BEGIN/END:VCARD 则不吞，避免把卡片边界并进值
			next := ""
			if i+1 < len(raw) {
				next = raw[i+1]
			}
			nextIsBoundary := strings.HasPrefix(strings.ToUpper(next), "BEGIN:VCARD") ||
				strings.HasPrefix(strings.ToUpper(next), "END:VCARD")
			if strings.HasSuffix(line, "=") && containsQPDecl(line) && i+1 < len(raw) && !nextIsBoundary {
				i++
				line = line[:len(line)-1] + strings.TrimLeft(raw[i], " \t")
				continue
			}
			// RFC 折叠：下一行以空格/Tab 开头
			if i+1 < len(raw) && len(raw[i+1]) > 0 && (raw[i+1][0] == ' ' || raw[i+1][0] == '\t') {
				i++
				line += raw[i][1:]
				continue
			}
			break
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func containsQPDecl(line string) bool {
	u := strings.ToUpper(line)
	semi := strings.IndexAny(u, ":")
	head := u
	if semi >= 0 {
		head = u[:semi]
	}
	return strings.Contains(head, "QUOTED-PRINTABLE") || strings.Contains(head, ";QP") || strings.Contains(head, "ENCODING=QP")
}

// parseProperty 解析 "GROUP.NAME;PARAM=V1,V2;P2:value"，返回 nil 表示无法解析
func parseProperty(line string) *vProperty {
	// 找到第一个未被反斜杠转义的 ':'
	colon := -1
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == ':' {
			colon = i
			break
		}
	}
	if colon < 0 {
		return nil
	}
	head, value := line[:colon], line[colon+1:]

	parts := strings.Split(head, ";")
	name := strings.ToUpper(parts[0])
	group := ""
	// 去掉 group 前缀 (item1.TEL -> TEL)
	if dot := strings.Index(name, "."); dot >= 0 {
		group = strings.ToLower(name[:dot])
		name = name[dot+1:]
	}
	if name == "" {
		return nil
	}

	params := map[string][]string{}
	for _, seg := range parts[1:] {
		if seg == "" {
			continue
		}
		if eq := strings.Index(seg, "="); eq >= 0 {
			key := strings.ToUpper(strings.TrimSpace(seg[:eq]))
			for _, v := range strings.Split(seg[eq+1:], ",") {
				params[key] = append(params[key], strings.ToUpper(strings.Trim(v, `"`)))
			}
		} else {
			// vCard 2.1 裸类型参数，视为 TYPE
			params["TYPE"] = append(params["TYPE"], strings.ToUpper(strings.Trim(seg, `"`)))
		}
	}
	return &vProperty{name: name, group: group, params: params, value: value}
}

// Apple 标准标签 token: _$!<Home>!$_ -> Home
func stripABToken(s string) string {
	if strings.HasPrefix(s, "_$!<") && strings.HasSuffix(s, ">!$_") {
		return s[4 : len(s)-4]
	}
	return s
}

var abTokenZh = map[string]string{
	"MOBILE": "手机", "CELL": "手机",
	"HOME": "住宅", "WORK": "工作", "MAIN": "主要", "OTHER": "其他",
	"HOMEPAGE": "主页", "HOME PAGE": "主页",
	"HOMEFAX": "家庭传真", "WORKFAX": "工作传真", "HOME FAX": "家庭传真", "WORK FAX": "工作传真",
	"PAGER": "传呼", "ICLOUD": "iCloud", "PREF": "",
}

// localizeABLabel 将 Apple X-ABLabel 值转为中文标签；空串表示无有效标签
func localizeABLabel(raw string) string {
	t := strings.TrimSpace(stripABToken(strings.TrimSpace(raw)))
	if t == "" {
		return ""
	}
	if zh, ok := abTokenZh[strings.ToUpper(t)]; ok {
		return zh
	}
	return t // 自定义标签原文
}

// decodeQP 解码 quoted-printable 值，charset 支持 UTF-8(默认) 与 GBK/GB2312/GB18030
func decodeQP(s, charset string) string {
	var out []byte
	for i := 0; i < len(s); {
		if s[i] == '=' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			out = append(out, byte(b))
			i += 3
			continue
		}
		out = append(out, s[i])
		i++
	}
	cs := strings.ToUpper(charset)
	if strings.Contains(cs, "GB") {
		if dec, err := simplifiedchinese.GB18030.NewDecoder().Bytes(out); err == nil {
			return string(dec)
		}
	}
	return string(out)
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'F' || c >= 'a' && c <= 'f'
}

// unescapeValue 处理 vCard 值转义: \n \N \, \; \\
func unescapeValue(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			case ',', ';', '\\':
				b.WriteByte(s[i])
			default:
				b.WriteByte('\\')
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

type vCard struct {
	props []*vProperty
}

func (c *vCard) get(name string) *vProperty {
	for _, p := range c.props {
		if p.name == name {
			return p
		}
	}
	return nil
}

func (c *vCard) getAll(name string) []*vProperty {
	var out []*vProperty
	for _, p := range c.props {
		if p.name == name {
			out = append(out, p)
		}
	}
	return out
}

// abLabelOf 取同 group 下 X-ABLabel 的本地化标签（Apple item 分组），无则空串
func (c *vCard) abLabelOf(p *vProperty) string {
	if p.group == "" {
		return ""
	}
	for _, l := range c.props {
		if l.group == p.group && l.name == "X-ABLABEL" {
			return localizeABLabel(l.value_())
		}
	}
	return ""
}

// value 返回属性值（已解码 QP 与转义）
func (p *vProperty) value_() string {
	v := p.value
	if p.isQuotedPrintable() {
		v = decodeQP(v, p.firstParam("CHARSET"))
	}
	if p.hasParam("ENCODING", "BASE64", "B") {
		return "" // 二进制字段不导入
	}
	return unescapeValue(v)
}

// nameParts 从 N 结构化姓名提取姓/名（N:Family;Given;Middle;Prefix;Suffix）
func (c *vCard) nameParts() (family, given string) {
	n := c.get("N")
	if n == nil {
		return "", ""
	}
	segs := strings.SplitN(n.value_(), ";", 5)
	family = strings.TrimSpace(segs[0])
	if len(segs) > 1 {
		given = strings.TrimSpace(segs[1])
	}
	return family, given
}

// displayName 计算显示名：N 的姓/名经 ComposeName 拼接优先——
// Apple 的 FN 按「通讯录显示顺序」偏好生成，可能是「名 姓」，不能照单全收；
// N 缺失或为空时才回退 FN。
func (c *vCard) displayName() string {
	family, given := c.nameParts()
	if name := store.ComposeName(family, given); name != "" {
		return name
	}
	if fn := c.get("FN"); fn != nil {
		if s := strings.TrimSpace(fn.value_()); s != "" {
			return s
		}
	}
	return ""
}

// normalizeBirthday 从 BDAY 提取 YYYY-MM-DD，无法识别返回空串
func normalizeBirthday(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, "Z")
	if i := strings.IndexAny(s, "T "); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "/", "-")
	if len(s) == 8 && isAllDigits(s) {
		s = s[:4] + "-" + s[4:6] + "-" + s[6:8]
	}
	if len(s) != 10 {
		return "" // --MM-DD 或缺失年份等格式不支持
	}
	if _, err := strconv.Atoi(s[:4]); err != nil || s[4] != '-' {
		return ""
	}
	return s
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// telLabel 依据 TEL 的 TYPE 参数给出中文标签
func telLabel(p *vProperty, idx int) string {
	switch {
	case p.hasParam("TYPE", "CELL", "MOBILE"):
		return "手机"
	case p.hasParam("TYPE", "HOME"):
		return "住宅电话"
	case p.hasParam("TYPE", "WORK"):
		return "工作电话"
	default:
		if idx <= 1 {
			return "电话"
		}
		return fmt.Sprintf("电话%d", idx)
	}
}

func parseVCardText(text string) []*vCard {
	var cards []*vCard
	var cur *vCard
	for _, line := range splitLines(text) {
		switch {
		case strings.HasPrefix(strings.ToUpper(line), "BEGIN:VCARD"):
			cur = &vCard{}
		case strings.HasPrefix(strings.ToUpper(line), "END:VCARD"):
			if cur != nil {
				cards = append(cards, cur)
				cur = nil
			}
		default:
			if cur != nil {
				if p := parseProperty(line); p != nil {
					cur.props = append(cur.props, p)
				}
			}
		}
	}
	return cards
}

// vCardToPerson 将一张 vCard 转换为 Person 及自定义字段；返回 false 表示缺少有效姓名
func vCardToPerson(c *vCard) (*store.Person, []*store.PersonField, bool) {
	family, given := c.nameParts()
	name := c.displayName()
	if name == "" {
		return nil, nil, false
	}
	p := &store.Person{Name: name, FamilyName: family, GivenName: given}

	if nk := c.get("NICKNAME"); nk != nil {
		v := nk.value_()
		if i := strings.IndexAny(v, ",;"); i > 0 {
			v = v[:i]
		}
		p.Nickname = strings.TrimSpace(v)
	}
	if g := c.get("GENDER"); g != nil {
		v := strings.ToUpper(strings.TrimSpace(g.value_()))
		if i := strings.IndexAny(v, ":,;"); i > 0 {
			v = v[:i]
		}
		switch v {
		case "M", "F", "男", "女":
			if v == "男" {
				v = "M"
			} else if v == "女" {
				v = "F"
			}
			p.Gender = v
		}
	} else if g := c.get("X-GENDER"); g != nil { // 导出往返保留（vCard 3.0 无标准 GENDER）
		v := strings.ToUpper(strings.TrimSpace(g.value_()))
		if v == "M" || v == "F" {
			p.Gender = v
		}
	}
	if b := c.get("BDAY"); b != nil {
		p.Birthday = normalizeBirthday(b.value_())
	}

	// TEL：pref/手机优先选为主电话，其余按标签写入自定义字段
	var fields []*store.PersonField
	tels := c.getAll("TEL")
	type telEntry struct {
		p     *vProperty
		value string
		rank  int // 0=pref, 1=cell, 2=其他
	}
	var telList []telEntry
	for _, t := range tels {
		v := strings.TrimSpace(t.value_())
		v = strings.TrimPrefix(strings.TrimPrefix(v, "tel:"), "TEL:")
		if v == "" {
			continue
		}
		rank := 2
		if t.hasParam("TYPE", "PREF") {
			rank = 0
		} else if t.hasParam("TYPE", "CELL", "MOBILE") {
			rank = 1
		}
		telList = append(telList, telEntry{t, v, rank})
	}
	// 稳定排序：pref 最优先，其次手机
	for i := 1; i < len(telList); i++ {
		for j := i; j > 0 && telList[j].rank < telList[j-1].rank; j-- {
			telList[j], telList[j-1] = telList[j-1], telList[j]
		}
	}
	for i, e := range telList {
		if i == 0 {
			p.Phone = e.value
		} else {
			label := c.abLabelOf(e.p)
			if label == "" {
				label = telLabel(e.p, i+1)
			}
			fields = append(fields, &store.PersonField{Label: label, Value: e.value, SortOrder: i})
		}
	}

	// 邮箱 / 地址 / 组织 / 网址 / 备注
	for i, e := range c.getAll("EMAIL") {
		if v := strings.TrimSpace(e.value_()); v != "" {
			label := c.abLabelOf(e)
			if label == "" {
				if e.hasParam("TYPE", "HOME") {
					label = "住宅"
				} else if e.hasParam("TYPE", "WORK") {
					label = "工作"
				} else if i == 0 {
					label = "邮箱"
				} else {
					label = fmt.Sprintf("邮箱%d", i+1)
				}
			}
			fields = append(fields, &store.PersonField{Label: label, Value: v, SortOrder: 100 + i})
		}
	}
	if adr := c.get("ADR"); adr != nil {
		segs := strings.Split(adr.value_(), ";")
		var loc []string
		// street;locality;region 拼接为位置
		for _, idx := range []int{2, 3, 4} {
			if len(segs) > idx && strings.TrimSpace(segs[idx]) != "" {
				loc = append(loc, strings.TrimSpace(segs[idx]))
			}
		}
		if len(loc) > 0 {
			p.Location = strings.Join(loc, " ")
		}
	}
	if org := c.get("ORG"); org != nil {
		if v := strings.TrimSpace(org.value_()); v != "" {
			fields = append(fields, &store.PersonField{Label: "公司", Value: v, SortOrder: 200})
		}
	}
	if title := c.get("TITLE"); title != nil {
		if v := strings.TrimSpace(title.value_()); v != "" {
			fields = append(fields, &store.PersonField{Label: "职位", Value: v, SortOrder: 201})
		}
	}
	for i, u := range c.getAll("URL") {
		if v := strings.TrimSpace(u.value_()); v != "" {
			label := c.abLabelOf(u)
			if label == "" {
				if i == 0 {
					label = "网址"
				} else {
					label = fmt.Sprintf("网址%d", i+1)
				}
			}
			fields = append(fields, &store.PersonField{Label: label, Value: v, SortOrder: 300 + i})
		}
	}

	var notes []string
	for _, nt := range c.getAll("NOTE") {
		if v := strings.TrimSpace(nt.value_()); v != "" {
			notes = append(notes, v)
		}
	}
	// Apple 分组社交资料: X-SOCIALPROFILE;type=wechat:x-apple://com.apple.social.wechat?username=xxx
	for _, sp := range c.getAll("X-SOCIALPROFILE") {
		v := sp.value_()
		if i := strings.Index(strings.ToLower(v), "x-apple://com.apple.social."); i >= 0 {
			rest := v[i+len("x-apple://com.apple.social."):]
			service := rest
			query := ""
			if j := strings.IndexAny(rest, "?"); j >= 0 {
				service = rest[:j]
				query = rest[j+1:]
			}
			username := ""
			for _, kv := range strings.Split(query, "&") {
				if strings.HasPrefix(strings.ToLower(kv), "username=") {
					username, _ = url.QueryUnescape(kv[len("username="):])
				}
			}
			service = strings.ToLower(strings.TrimSpace(service))
			if service == "wechat" && username != "" && p.Wechat == "" {
				p.Wechat = username
				continue
			}
			if username != "" {
				fields = append(fields, &store.PersonField{Label: strings.ToUpper(service[:1]) + service[1:], Value: username, SortOrder: 220})
			}
		}
	}
	// 微信/QQ 专有属性
	if wx := c.get("X-WECHAT"); wx != nil {
		if v := strings.TrimSpace(wx.value_()); v != "" {
			p.Wechat = v
		}
	}
	if qq := c.get("X-QQ"); qq != nil {
		if v := strings.TrimSpace(qq.value_()); v != "" {
			fields = append(fields, &store.PersonField{Label: "QQ", Value: v, SortOrder: 210})
		}
	}
	// Apple 通讯录唯一标识（导入回联系人.app 时匹配更新）；统一大写便于比对
	if uid := c.get("X-ABUID"); uid != nil {
		v := strings.TrimSpace(uid.value_())
		v = strings.TrimSuffix(v, ":ABPerson")
		p.XAbUID = strings.ToUpper(strings.TrimSpace(v))
	}
	// 只有本应用导出的 vcf 带这个属性，iOS 导出的没有，视为未归档
	if arch := c.get("X-QIANSI-ARCHIVED"); arch != nil {
		v := strings.ToUpper(strings.TrimSpace(arch.value_()))
		p.Archived = v == "1" || v == "TRUE"
	}
	if len(notes) > 0 {
		p.Notes = strings.Join(notes, "\n")
	}
	return p, fields, true
}

func (a *API) peopleImportVCard(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, vcardMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, "读取请求体失败: "+err.Error())
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, 400, "请求格式错误: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeErr(w, 400, "text 不能为空")
		return
	}
	if !utf8.ValidString(req.Text) {
		writeErr(w, 400, "内容不是有效的 UTF-8 文本，请先将文件另存为 UTF-8 编码")
		return
	}

	ctx := r.Context()
	cards := parseVCardText(req.Text)
	if len(cards) == 0 {
		writeErr(w, 400, "未找到有效的 vCard（BEGIN:VCARD … END:VCARD）")
		return
	}

	// 已有联系人用于去重
	existing, err := a.Store.PersonList(ctx, "", 0, 0, true, 0, 1<<30, 0)
	if err != nil {
		writeErr(w, 500, "加载已有联系人失败: "+err.Error())
		return
	}
	type key struct{ name, phone string }
	seen := make(map[key]bool, len(existing))
	uidToID := make(map[string]string, len(existing))
	for _, ep := range existing {
		seen[key{ep.Name, ep.Phone}] = true
		if ep.XAbUID != "" {
			uidToID[strings.ToUpper(ep.XAbUID)] = ep.ID
		}
		// 导出时 X-ABUID 会回退用 person id（应用内新建的联系人没有 x_abuid），
		// 所以 id 也认作同一人，否则自家导出回导会把这批人整份复制一遍
		uidToID[strings.ToUpper(ep.ID)] = ep.ID
	}

	imported := 0
	skipped := 0
	updated := 0
	created := []*store.Person{}
	for _, card := range cards {
		p, fields, ok := vCardToPerson(card)
		if !ok {
			skipped++
			continue
		}
		// X-ABUID 命中即视为同一人：不重建，只按 vCard 回填姓名三件套，
		// 其余字段保留应用内维护的值；无 UID 的卡回退到 (name, phone) 匹配
		if p.XAbUID != "" {
			if id, hit := uidToID[p.XAbUID]; hit {
				if err := a.Store.PersonUpdateNameParts(ctx, id, p.Name, p.FamilyName, p.GivenName); err != nil {
					writeErr(w, 500, fmt.Sprintf("更新「%s」姓名失败: %v", p.Name, err))
					return
				}
				// 只顺着卡上的标记归档、不反向取消：外来 vcf 没有这个属性，
				// 反向操作会把用户精心隐藏的企业联系人又放出来
				if p.Archived {
					if err := a.Store.PersonArchive(ctx, id); err != nil {
						writeErr(w, 500, fmt.Sprintf("归档「%s」失败: %v", p.Name, err))
						return
					}
				}
				updated++
				continue
			}
		} else {
			k := key{p.Name, p.Phone}
			if seen[k] {
				skipped++
				continue
			}
			seen[k] = true
		}
		if err := a.Store.PersonCreate(ctx, p); err != nil {
			writeErr(w, 500, fmt.Sprintf("导入「%s」失败: %v", p.Name, err))
			return
		}
		// BDAY 也要进入纪念日与提醒：手工新建走 peopleCreate 同步，导入路径同样补齐
		if err := a.Store.SyncBirthdayAnniversary(ctx, p); err != nil {
			writeErr(w, 500, fmt.Sprintf("同步「%s」生日纪念日失败: %v", p.Name, err))
			return
		}
		if p.XAbUID != "" {
			uidToID[strings.ToUpper(p.XAbUID)] = p.ID
		}
		uidToID[strings.ToUpper(p.ID)] = p.ID
		for _, f := range fields {
			f.PersonID = p.ID
			if err := a.Store.PersonFieldUpsert(ctx, f); err != nil {
				writeErr(w, 500, fmt.Sprintf("写入「%s」字段失败: %v", p.Name, err))
				return
			}
		}
		created = append(created, p)
		imported++
	}
	writeJSON(w, 200, map[string]any{
		"total":    len(cards),
		"imported": imported,
		"updated":  updated,
		"skipped":  skipped,
		"people":   created,
	})
}
