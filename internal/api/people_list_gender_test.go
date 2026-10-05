package api

import (
	"net/http"
	"testing"
)

// 性别筛选：列表页的下拉只给出男/女/未填，其余值（手改 URL、老书签）不该把列表筛成空的。

func TestAPIPeopleListGenderFilter(t *testing.T) {
	s := newTestServer(t)
	list := func(query string) []string {
		rec := s.do(http.MethodGet, "/api/v1/people/?"+query, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET ?%s => %d: %s", query, rec.Code, rec.Body.String())
		}
		got := []string{}
		for _, m := range decodeMapList(t, rec) {
			got = append(got, m["name"].(string))
		}
		return got
	}

	for _, body := range []map[string]any{
		{"name": "男甲", "gender": "男"},
		{"name": "女乙", "gender": "F"},
		{"name": "没填丙"},
		{"name": "归档丁", "gender": "F", "archived": true},
	} {
		apiCreatePersonMap(t, s, body)
	}

	for _, c := range []struct{ query, want string }{
		{"gender=M", "男甲"},
		{"gender=f", "女乙"}, // 大小写不敏感，和写入时的收口一致
		{"gender=none", "没填丙"},
	} {
		got := list(c.query)
		if len(got) != 1 || got[0] != c.want {
			t.Fatalf("?%s => %v, want 只有 %s", c.query, got, c.want)
		}
	}
	// 不认识的取值当不限：筛成空列表看起来像数据没了
	if got := list("gender=%E6%9C%AA%E7%9F%A5"); len(got) != 3 {
		t.Fatalf("非法性别值应不限，得到 %v", got)
	}
	// 已归档视图里同样按性别筛，且没归档的人不会串台
	if got := list("archived=only&gender=F"); len(got) != 1 || got[0] != "归档丁" {
		t.Fatalf("已归档视图筛女 => %v", got)
	}
	if got := list("gender=M&q=甲"); len(got) != 1 {
		t.Fatalf("性别应与关键字筛选叠加, got %v", got)
	}
}
