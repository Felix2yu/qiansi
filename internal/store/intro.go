package store

import (
	"context"
	"database/sql"
	"strings"
)

// ===== 认识路径（引荐人链）=====

// IntroHop 链上的一跳。EdgeTypes 是「上一跳 → 这一跳」之间的关系类型，
// 首跳没有来路，留空。
type IntroHop struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	EdgeTypes []string `json:"edge_types"`
}

// IntroPath 从「我」走到某个人的认识路径。详情页以前为这一条链拉全量
// people + relationships，现在只查链上那几行。
type IntroPath struct {
	Chain       []*IntroHop `json:"chain"`
	ReachesSelf bool        `json:"reaches_self"`
	// Broken：链上某一跳的引荐人已经被删掉，只能走到断点为止。
	Broken bool `json:"broken"`
	// Cyclic：历史脏数据里出现了环（写入时已拦），第二次遇到同一个人即停。
	Cyclic bool `json:"cyclic"`
	// DirectTypes：我与这个人之间现存的直达关系——经人介绍后又成了挚友，是另一条路。
	DirectTypes []string `json:"direct_types"`
}

// 引荐链最多追溯这么深。正常人脉远不到这个数，它只用来兜住脏数据里的环。
const introMaxDepth = 20

// IntroPath 沿 people.introduced_by_person_id 从 personID 往回追到 selfID。
func (s *Store) IntroPath(ctx context.Context, selfID, personID string) (*IntroPath, error) {
	// 递归从这个人出发往上跳；引荐人已被删掉时 JOIN 自然没有下一行，
	// 最后一行的 intro 还留着值，就是断链的证据。
	rows, err := s.DB.QueryContext(ctx, `WITH RECURSIVE chain(id,name,intro,depth) AS (
  SELECT id, name, NULLIF(introduced_by_person_id,''), 0 FROM people WHERE id=?
  UNION ALL
  SELECT p.id, p.name, NULLIF(p.introduced_by_person_id,''), c.depth+1
  FROM people p JOIN chain c ON p.id=c.intro WHERE c.depth < ?
) SELECT id,name,intro,depth FROM chain ORDER BY depth`, personID, introMaxDepth)
	if err != nil {
		return nil, err
	}
	type hop struct {
		id, name, intro string
		depth           int
	}
	var walked []*hop
	seen := map[string]bool{}
	cyclic := false
	for rows.Next() {
		h := &hop{}
		var intro sql.NullString
		if err := rows.Scan(&h.id, &h.name, &intro, &h.depth); err != nil {
			rows.Close()
			return nil, err
		}
		h.intro = intro.String
		if seen[h.id] {
			cyclic = true
			break
		}
		seen[h.id] = true
		walked = append(walked, h)
		// 走到本人就够了：再往上是本人的引荐人，与这条路径无关
		if h.id == selfID {
			break
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	out := &IntroPath{Chain: []*IntroHop{}, Cyclic: cyclic}
	if len(walked) == 0 {
		return out, nil
	}
	tail := walked[len(walked)-1]
	reachesSelf := tail.id == selfID
	// 没走到本人，但最后那个人还有引荐人没接上，就是断链
	out.Broken = !reachesSelf && tail.intro != ""
	out.ReachesSelf = reachesSelf

	// walked 是「此人 → … → 我」，倒过来才读得通
	ids := make([]string, len(walked))
	names := map[string]string{}
	for i, h := range walked {
		names[h.id] = h.name
		ids[len(walked)-1-i] = h.id
	}
	// 直达关系问的是「我 ↔ 此人」，链没走到本人时我并不在 ids 里
	pairIDs := ids
	if !reachesSelf {
		pairIDs = append(append([]string{}, ids...), selfID)
	}
	types, err := s.pairRelTypes(ctx, pairIDs)
	if err != nil {
		return nil, err
	}
	for i, id := range ids {
		hop := &IntroHop{ID: id, Name: names[id], EdgeTypes: []string{}}
		if i > 0 {
			if t := types[pairRelKey(ids[i-1], id)]; t != nil {
				hop.EdgeTypes = t
			}
		}
		out.Chain = append(out.Chain, hop)
	}
	out.DirectTypes = types[pairRelKey(selfID, personID)]
	if out.DirectTypes == nil {
		out.DirectTypes = []string{}
	}
	return out, nil
}

// pairRelKey 无序人对的键：同一对人有向的两条边归同一个键。
func pairRelKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

// pairRelTypes 一次取回这批人间全部关系类型（同一对人可以并存多种）。
func (s *Store) pairRelTypes(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, 2*len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT from_person_id,to_person_id,type
FROM relationships WHERE from_person_id IN (`+ph+`) OR to_person_id IN (`+ph+`)`, append(args, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var from, to, typ string
		if err := rows.Scan(&from, &to, &typ); err != nil {
			return nil, err
		}
		k := pairRelKey(from, to)
		dup := false
		for _, x := range out[k] {
			if x == typ {
				dup = true
				break
			}
		}
		if !dup {
			out[k] = append(out[k], typ)
		}
	}
	return out, rows.Err()
}
