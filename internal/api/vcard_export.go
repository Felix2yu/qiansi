package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/qiansi/app/internal/store"
)

// GET /api/v1/people/export/vcard
// 可选筛选参数与 peopleList 相同（q/category_id/grade/tag_id/archived），
// 返回 text/vcard 下载流。每张卡带 X-ABUID（优先库中保留值，缺省用 person.id），
// 导入回 macOS 联系人.app 时可按 UID 匹配更新而非新建。

func escapeVCardValue(s string) string {
	r := strings.NewReplacer(
		"\\", "\\\\",
		";", "\\;",
		",", "\\,",
		"\r\n", "\\n",
		"\n", "\\n",
		"\r", "\\n",
	)
	return r.Replace(s)
}

type itemWriter struct {
	b     strings.Builder
	group int
}

// item 开启一个 Apple 分组（itemN.），写入载体属性行并附带 X-ABLabel
func (iw *itemWriter) write(carrier string, label string) {
	if label == "" {
		iw.b.WriteString(carrier + "\r\n")
		return
	}
	iw.group++
	g := fmt.Sprintf("item%d", iw.group)
	line := carrier
	if i := strings.Index(line, ":"); i >= 0 {
		line = line[:i] + ";" + g + line[i:]
	} else {
		line += ";" + g
	}
	iw.b.WriteString(line + "\r\n")
	iw.b.WriteString(g + ".X-ABLabel:" + escapeVCardValue(label) + "\r\n")
}

// telExportSpec 将自定义字段 label 映射到 TEL 类型；ok=false 表示不是电话字段
func telExportSpec(label string) (typ string, customLabel string, ok bool) {
	switch label {
	case "手机":
		return "type=CELL;type=VOICE;type=pref", "", true
	case "住宅电话":
		return "type=HOME;type=VOICE", "", true
	case "工作电话":
		return "type=WORK;type=VOICE", "", true
	}
	if strings.HasPrefix(label, "电话") || strings.Contains(label, "传真") {
		return "type=VOICE", label, true
	}
	return "", "", false
}

// personToVCard 生成单个联系人的 vCard 3.0 文本（CRLF 行尾，Apple 兼容）
func personToVCard(p *store.Person, fields []*store.PersonField) string {
	iw := &itemWriter{}
	iw.b.WriteString("BEGIN:VCARD\r\n")
	iw.b.WriteString("VERSION:3.0\r\n")
	iw.b.WriteString("PRODID:-//qiansi//CN\r\n")
	abuid := p.XAbUID
	if abuid == "" {
		abuid = p.ID // 稳定 UID：无保留值时复用 person id，避免每次导出漂移
	}
	iw.b.WriteString("FN:" + escapeVCardValue(p.Name) + "\r\n")
	iw.b.WriteString("N:;" + escapeVCardValue(p.Name) + ";;;\r\n")
	if p.Nickname != "" {
		iw.b.WriteString("NICKNAME:" + escapeVCardValue(p.Nickname) + "\r\n")
	}
	if p.Phone != "" {
		iw.b.WriteString("TEL;type=CELL;type=VOICE;type=pref:" + escapeVCardValue(p.Phone) + "\r\n")
	}

	noteLines := []string{}
	if p.Notes != "" {
		noteLines = append(noteLines, p.Notes)
	}

	for _, f := range fields {
		v := f.Value
		if v == "" {
			continue
		}
		switch {
		case f.Label == "公司":
			iw.b.WriteString("ORG:" + escapeVCardValue(v) + "\r\n")
		case f.Label == "职位":
			iw.b.WriteString("TITLE:" + escapeVCardValue(v) + "\r\n")
		case f.Label == "QQ":
			iw.b.WriteString("X-QQ:" + escapeVCardValue(v) + "\r\n")
		case f.Label == "邮箱":
			iw.b.WriteString("EMAIL;type=INTERNET;type=pref:" + escapeVCardValue(v) + "\r\n")
		case f.Label == "住宅" || f.Label == "工作":
			t := "home"
			if f.Label == "工作" {
				t = "work"
			}
			iw.b.WriteString("EMAIL;type=INTERNET;type=" + t + ":" + escapeVCardValue(v) + "\r\n")
		case strings.HasPrefix(f.Label, "邮箱"):
			iw.write("EMAIL;type=INTERNET:"+escapeVCardValue(v), f.Label)
		case f.Label == "网址":
			iw.b.WriteString("URL:" + escapeVCardValue(v) + "\r\n")
		case strings.HasPrefix(f.Label, "网址"):
			iw.write("URL:"+escapeVCardValue(v), f.Label)
		default:
			if typ, custom, ok := telExportSpec(f.Label); ok {
				if custom == "" {
					iw.b.WriteString("TEL;" + typ + ":" + escapeVCardValue(v) + "\r\n")
				} else {
					iw.write("TEL;"+typ+":"+escapeVCardValue(v), custom)
				}
				continue
			}
			noteLines = append(noteLines, f.Label+": "+v)
		}
	}

	if p.Wechat != "" {
		iw.b.WriteString("X-SOCIALPROFILE;type=wechat:x-apple://com.apple.social.wechat?username=" + escapeVCardValue(p.Wechat) + "\r\n")
	}
	if p.Location != "" {
		iw.b.WriteString("ADR;type=HOME;type=pref:;;" + escapeVCardValue(p.Location) + ";;;;\r\n")
	}
	if p.Birthday != "" {
		iw.b.WriteString("BDAY:" + p.Birthday + "\r\n")
	}
	if p.Gender == "M" || p.Gender == "F" {
		iw.b.WriteString("X-GENDER:" + p.Gender + "\r\n")
	}
	if len(noteLines) > 0 {
		iw.b.WriteString("NOTE:" + escapeVCardValue(strings.Join(noteLines, "\n")) + "\r\n")
	}
	iw.b.WriteString("X-ABUID:" + strings.ToUpper(abuid) + ":ABPerson\r\n")
	iw.b.WriteString("END:VCARD\r\n")
	return iw.b.String()
}

func (a *API) peopleExportVCard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := a.Store.PersonList(r.Context(),
		q.Get("q"),
		parseIntQuery(r, "category_id", 0),
		parseIntQuery(r, "grade", 0),
		q.Get("archived") == "1",
		parseIntQuery(r, "tag_id", 0),
		1<<30, 0,
	)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	var sb strings.Builder
	count := 0
	for _, p := range list {
		fields, err := a.Store.PersonFieldList(r.Context(), p.ID)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		sb.WriteString(personToVCard(p, fields))
		count++
	}

	filename := "qiansi-contacts-" + time.Now().Format("20060102-150405") + ".vcf"
	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"; filename*=UTF-8''`+filename)
	w.Header().Set("X-Export-Count", strconv.Itoa(count))
	w.WriteHeader(200)
	_, _ = w.Write([]byte(sb.String()))
}
