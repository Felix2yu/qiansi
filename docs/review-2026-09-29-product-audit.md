# 牵丝 产品审查报告

日期：2026-09-29
范围：整体业务逻辑、11 个菜单（今日/人物/往来/对话/金钱/纪念日/待办/时间线/关系图/统计/设置）的实现与交互、新增数据的添加流程
方法：源码走查（Go 后端 `internal/{api,store,db,notify,lunar}` + Svelte 5 前端 `web/src`）+ `svelte-check`（0 error / 23 warning）+ 用 `svelte/compiler` 对可疑模板做编译复现

优先级定义：
- **P0** 功能阻断或数据计算错误，必须立即修
- **P1** 主要功能缺失或行为明显错误，影响正常使用
- **P2** 体验、一致性、可维护性问题
- **P3** 打磨项

---

## 0. 总体判断

**结论：骨架健康，接线不足，存在 4 个静默失效的 P0 缺陷。**

做得好的部分：
- 数据模型设计合理——金额统一用「分」存储（`amount_fen`）、外键已显式开启（`_pragma=foreign_keys(ON)`）、`event_participants` 多对多、vCard 往返靠 `X-ABUID` 去重。
- 事件开销与金钱账本打通（`transactions.event_id`），删事件只解绑不删账，语义正确。
- 农历换算独立成包且有 `NextOccurrence` 年循环/跨年处理，逻辑正确。

主要短板集中在两点：
1. **后端已实现、前端零入口**：标签（taggings）、关系（relationships）、还款流水（repayments）、附件、词云、归档——六个能力没有任何 UI 调用，等于不存在。
2. **静默失效**：有 4 个缺陷不会报错、不会让构建失败、不会让 CI 拦截，但功能实际是坏的（M1、M2、M3、M4）。

---

## 1. 新增数据添加流程评估（单独成节，因涉及每个模块）

| 模块 | 入口 | 必填校验 | 流程问题 | 结论 |
|---|---|---|---|---|
| 人物 | 人物页「新建」弹窗 / vCard 导入 | 姓名（前后端均有） | 表单无标签、无自定义字段、无头像上传；归档无入口 | 可用，信息维度不完整 |
| 往来 | 往来页「新建」弹窗 | 标题 + 日期 | 参与人平铺全量 checkbox（无搜索）；开销单位「元」 | 可用，人多后难用 |
| 对话/承诺 | 对话页「新建」按钮 | 内容、日期（自动补今天） | **按钮 onclick 被编译器丢弃，点击无任何反应** | **完全不可用（P0）** |
| 金钱 | 金钱页「新建」弹窗 | 联系人 + 金额 | **金额输入框单位是「分」**，与往来页的「元」冲突；无编辑、无还款 | 可用但高危 |
| 纪念日 | 纪念日页「新建」弹窗 | 标题 + 日期 | 无编辑（只能删了重建）；`remind_days` 默认 `7,3,1,0` | 可用，CRUD 不全 |
| 待办 | 待办页「新建」弹窗 | 标题 + 日期 | 日期拼接 `T09:00:00Z`（UTC），与中国时区错位 | 可用，日期可能偏一天 |
| 关系 | — | — | **无任何 UI**，只能调 API | 不可用 |
| 标签 | 设置页只能建「标签名」 | 名称 | **无法给人物/事件打标签**，列表的标签筛选永远筛不出数据 | 不可用 |

一句话：8 条新增路径中，**1 条完全断开（对话），2 条无法操作（关系、标签），1 条单位高危（金钱），3 条缺编辑**。

---

## 2. 需要优化的环节

| # | 问题 | 位置 | 影响 | 建议 | 优先级 |
|---|---|---|---|---|---|
| O1 | 参与人 / 开销归属人选择器把全部联系人平铺成 checkbox 与下拉（上限 500），无搜索 | `web/src/pages/Events.svelte:196-211` | 联系人超过 ~30 人后几乎无法操作，是最先撞上的规模瓶颈 | 改为带搜索的多选（输入即过滤），已选项以 chip 展示 | P1 |
| O2 | 统计口径不一致：首页卡片「7 天内待办」= `DashboardStats`（仅 `reminders` 表），下方列表 = `ReminderUpcoming`（reminders + 纪念日衍生） | `store/events.go:767-787` vs `611-640` | 同一页面两个数字对不上，用户无法信任 | 两处统一调用 `ReminderUpcoming` 派生统计 | P1 |
| O3 | 月度趋势以 `events` 表驱动，某月无往来但有备忘/金钱时该月直接缺失 | `store/events.go:789-810` | 趋势图出现空洞月份 | 改为按月份维度表/日期序列表驱动，或三张表各自统计后外连接 | P2 |
| O4 | 亲密度折线图几乎永远空白：`PersonIntimacy` 从不返回 `trend`，快照仅由 03:00 定时任务写入 | `store/events.go:910-929`；`notify/notify.go:31-34` | 详情页把 trend 作为渲染条件，新用户永远看不到图 | `PersonIntimacy` 直接按快照表返回 trend；无快照时回退为单点并给出文案 | P2 |
| O5 | 全站列表无分页：People 200 / Events 100 / Money 200 / Memos 100 硬编码 | 各页面 `load()` | 数据增长后静默截断，用户不知道还有更多 | 统一分页或无限滚动，并在触底时提示 | P2 |
| O6 | 「近期待办」无时间下限（`due_at <= cutoff`），所有逾期未完成项都堆在列表顶部 | `store/events.go:616` | 逾期项与近期项混排，待办页失去优先级意义 | 分区展示：已逾期 / 今天 / 未来 7 天 | P2 |
| O7 | 六个后端能力前端零入口：标签 taggings、关系 relationships、还款 repayments、附件上传、词云、归档 | 全局 grep `web/src` 仅命中 relationships 的只读 GET | 功能形同不存在；People 页「全部标签」筛选永远返回空 | 见第 3 节 N2/N7/N8 | P1 |
| O8 | 主题色 / 暗色只写 localStorage，`onMount` 未回填 CSS 变量（源码里留了 `// apply on mount` 注释但无实现） | `web/src/pages/Settings.svelte:41-49` | 刷新页面后主题回到默认值 | 在 `onMount` 中读取并应用 `q_theme`/`q_dark` | P2 |
| O9 | 侧边栏当前页无高亮：CSS 写了 `a[href].active`，但模板从不添加该类 | `web/src/App.svelte:60-69, 120` | 用户不清楚自己在哪个菜单（`svelte-check` 亦报 unused selector） | 用 `$route.path` 绑定 active 类 | P3 |
| O10 | 导航图标重复：「时间线」与「统计」同为 `LineChart`；纪念日用了语义不符的 `Share2` | `web/src/App.svelte:22-26` | 识别度下降 | 换为 `History` / `ChartColumn` / `Cake` 等 | P3 |
| O11 | 金额方向图标语义含糊：金钱列表用「借 / 入」二字，且 `kind` 直接显示 `loan/gift/expense` 英文原值 | `web/src/pages/Money.svelte:48,53` | 中文界面出现英文枚举 | 统一枚举本地化映射（借还/礼物/花销/其它，支出/收入） | P2 |

---

## 3. 建议新增的功能

| # | 功能 | 说明与落地位置 | 优先级 |
|---|---|---|---|
| N1 | **编辑能力补全** | 纪念日、金钱、待办三个模块目前只有「新建 + 删除」；`PersonDetail` 的编辑按钮已渲染但无 onclick。四条编辑路径需补齐（后端 `PUT` 接口均已就绪，只差前端） | P1 |
| N2 | **还款流水 UI** | `POST/GET /api/v1/transactions/{id}/repayments` 与 `repaid_fen` 字段已实现，Money 页应提供「记一笔还款」并展示剩余待还；同时把 `markSettled` 改为走还款流水而非覆盖 `settled` | P1 |
| N3 | **人物生日 → 提醒闭环** | `people.birthday` / `birthday_is_lunar` 目前完全孤立，不进纪念日、不进提醒、不进建议。应提供「一键生成生日纪念日」，或在提醒计算中把人物生日作为虚拟纪念日纳入 | P1 |
| N4 | **数据备份与恢复** | `config.go` 已预留 `data/backups` 目录但无任何实现。需新增：手动导出（数据库 + uploads）、导入恢复、可选的定时备份与保留策略 | P1 |
| N5 | **关系维护 UI** | 在 `PersonDetail` 或关系图页增删关系（类型 + 备注），并支持反向展示。当前 `UNIQUE(from,to)` 为单向，建议改为「(A,B) 无序键 + 各自方向语义」或允许两条有向边 | P1 |
| N6 | **标签管理 UI** | 人物/事件打标签 + 按标签筛选。后端 `taggings` 三接口完备，前端零调用 | P1 |
| N7 | **可选鉴权** | 当前 CORS `*` 且无任何认证（见 M11）。建议加一个可选 `QIANSI_TOKEN`，开启后要求 Bearer / Cookie，并把 CORS 收紧为同源 | P1 |
| N8 | **全局搜索** | 跨人物/事件/备忘/金钱的统一搜索框，各表 `LIKE` 能力已具备 | P2 |
| N9 | **上下文快捷记录** | 在 `PersonDetail` 直接「记一次往来 / 记一笔 / 记一句话 / 设个提醒」，减少跨模块跳转（契合「人情往来」的核心用例） | P2 |
| N10 | **事件模板 / 快速记录** | 常见「见面、吃饭、送礼」一键带出字段组合（地点、礼物、开销） | P2 |
| N11 | **重名/重复检测与合并** | vCard 已按 `X-ABUID` 去重，但手动新建同名或同手机号人物无任何提示；应给出「可能重复」提示与合并入口 | P2 |
| N12 | **待办已完成视图与撤销** | 待办页仅列 `pending`，无法回顾历史或撤销误完成 | P2 |

---

## 4. 应当修改调整的部分

### P0（阻断 / 数据错误）

**M1. 对话/承诺页「新建」按钮完全失效**

`web/src/pages/Memos.svelte:40-41`：`<button>` 开标签在第 40 行已用 `>` 闭合，第 41 行的 `onclick={...}` 落在元素内容区，被编译器当作文本丢弃。

已用 `svelte/compiler` 复现：编译产物 `from_html` 输出为
`<button class="btn" style="background: red;"> <span>新建</span></button>` —— `onclick` 彻底消失。

- 影响：整个「对话 / 承诺」模块无法新增任何数据；页面上看不出异常（按钮照常显示）。
- 隐蔽性：`svelte-check` 报 **0 error**，CI 不会拦截。
- 修复：把 `onclick` 移回开标签内。

**M2. 事件按人筛选失效且参数语义错乱**

`internal/api/records.go:27-29` 把查询参数 `person_id` 传给了 `store.EventList` 的 **`q`（关键字）** 形参，而 `EventList`（`store/events.go:177`）也根本没有 person_id 过滤能力。

- 影响：`/api/v1/events?person_id=xxx` 会把人物 ID 当标题关键字去 `LIKE`，永远返回空；所有「按人过滤往来」的调用方都拿不到数据。
- 修复：给 `EventList` 增加 `personID` 参数并 JOIN `event_participants`，API 层按参数名分别传。

**M3. 借款/待还金额统计 SQL 错误**

`store/events.go:776-777` 与 `api/suggest.go:57-61` 使用：

```sql
SELECT COALESCE(SUM(t.amount_fen) - COALESCE(
  (SELECT SUM(r.amount_fen) FROM repayments r WHERE r.transaction_id = t.id), 0), 0)
FROM transactions t WHERE t.direction='out' AND t.settled=0
```

在聚合查询中引用裸列 `t.id`，SQLite 取值来自**未定义的某一行**（不是逐行相减）。

- 影响：多笔未结清借款时，首页「我借出 / 我待还」与建议里的「待还 ¥X」数字错误且不可预测；单笔时碰巧正确，因此极易长期不被发现。
- 修复：改为先按 `t.id` 分组求和再外层合计，或用 `LEFT JOIN repayments` 后按 `t.id` 聚合。

**M4. 「导出数据库」按钮指向不存在的接口**

`web/src/pages/Settings.svelte:61` 请求 `/api/v1/backup/export`，全仓 grep 无该路由注册（仅 `config.go` 有 `Backups` 目录定义）。

- 影响：请求被 `/*` 的 SPA 兜底命中，用户下载到的是 `index.html`，会误以为数据已备份。
- 修复：实现后端导出接口（见 N4），或在实现前移除按钮。

### P1

**M5. 纪念日提醒双轨不一致**

`AnniversaryUpcoming`（`store/events.go:644`）用 `NextOccurrence` 正确处理了每年循环与农历转换；但 `api/suggest.go:38-41` 直接比较 `date(a.date) BETWEEN date('now') AND date('now','+7 day')`。

- 影响：除建录首年外，「纪念日临近」建议永不命中——而待办列表里同一纪念日却能正常出现，两处自相矛盾。
- 修复：建议模块复用 `AnniversaryUpcoming` 的结果。

**M6. 金额单位不统一**

`Money.svelte:86` 的 placeholder 是「金额（分）」并以 `amount_fen` 直传；`Events.svelte:195` 是「开销（元，可空）」并前端 ×100。

- 影响：同一应用两套单位，用户极易把 100 元录成 1 元或 10000 元，且无任何校验提示。
- 修复：统一为「元」输入，前端转分；Money 页同样用 `yuan()` / ×100 转换。

**M7. 纪念日衍生待办无法被完成**

`ReminderUpcoming` 为纪念日生成合成 ID `anniv:<id>:<date>:<offset>`（`store/events.go:676`），前端勾选后调用 `/reminders/{id}/done`，而 `ReminderDone`（`:575`）是 `UPDATE ... WHERE id=?` 且不检查影响行数，返回 204。

- 影响：点击后看似成功并短暂消失，reload 后原样重现，用户会反复尝试。
- 修复：对 `anniv:` 前缀 ID 做专门处理（写入 `notification_logs` 或生成真实 reminder 记录），并在无匹配行时返回 404。

**M8. 时区混用**

`Reminders.svelte:11` 拼接 `T09:00:00Z`（UTC）存储；`ReminderUpcoming`/`AnniversaryUpcoming` 用本地日期 `NowLocal()`；`DashboardStats` 用 SQLite `date('now')`（UTC）；`notify.go:32,40` 用 `time.Now().Hour()`（本地）判断推送小时。

- 影响：中国时区下 09:00Z = 17:00 本地，跨零点场景待办日期会整体偏移一天；推送时间与用户设置的「小时」在不同时区语义不同。
- 修复：全链路统一为本地时区（存 `+08:00` 偏移或统一用本地时间字符串），Dashboard 的日期比较改为传入本地日期参数。

**M9. `PersonDetail` 编辑按钮无响应**

`PersonDetail.svelte:54` 渲染了编辑按钮但无 `onclick`；该页同时不展示自定义字段（`person_fields`）、标签、关系。

- 影响：详情页无法完成任何编辑，只能删除。
- 修复：接入编辑（复用 People 页表单或抽成 `PersonForm.svelte`），并补齐字段/标签/关系展示。

**M10. 金钱「结清」实现粗糙**

`Money.svelte:23-27` 先 GET 全量对象再 PUT 覆盖，且只翻转 `settled`，不产生还款流水。

- 影响：并发/多端场景下会覆盖其他字段；`repaid_fen` 永远为 0，部分还款无法表达。
- 修复：改为语义化动作（记还款 / 全部结清），后端按金额生成 `repayments` 记录并自动判定结清。

**M11. 安全：无鉴权 + CORS 全开**

`api/api.go:24-28` `AllowedOrigins: ["*"]`，全站无任何认证中间件。

- 影响：用户浏览任意网页时，该页面可跨站读写本机 8080 端口上的全部人际数据（姓名、电话、微信、金钱）。README 已提示「自行加反代/鉴权」，但默认配置即为最宽松。
- 修复：见 N7。另 `misc.go:33-36` 仅校验 `Content-Type` 头判断图片类型，该头可伪造，应改为嗅探文件魔数。

### P2 / P3

| # | 问题 | 位置 | 修复建议 | 优先级 |
|---|---|---|---|---|
| M12 | 状态与类型枚举直接显示英文：`open/fulfilled/broken`、`custom/anniversary` | `Memos.svelte:61`、`Reminders.svelte:30` | 建立统一枚举映射并在前端展示中文 | P2 |
| M13 | `truncate` 按字节截断中文，`s[:30]` 会切坏 UTF-8 字符产生乱码 | `api/suggest.go:97-102` | 改用 `[]rune` 截断 | P2 |
| M14 | 「测试推送」名不副实：仅保存配置，实际未推送 | `Settings.svelte:36-39` | 增加真正触发一次推送的后端接口 | P2 |
| M15 | 创建接口校验缺失：`ReminderCreate` 不校验 `title`/`due_at`；`relCreate` 不校验自环与重复 | `records.go:455`、`people.go:194` | 补入参校验并返回 400 而非 500 | P2 |
| M16 | `EventGet` 未命中返回 `(nil, nil)`，API 层 `err == sql.ErrNoRows` 分支永不触发 | `store/events.go:832`；`records.go:41` | 未命中返回 `sql.ErrNoRows`，统一 404 | P2 |
| M17 | 删除联系人后 `attachments` 中头像记录与磁盘文件残留；删除圈子/标签/事件类型时未处理引用 | `store/people.go:95-98`；`settings.go` 各 Delete | 删除时级联清理或置空引用 | P3 |
| M18 | `relationships` 唯一键 `(from,to)` 单向，无法表达非对称关系（A 视 B 为同事、B 视 A 为领导） | `001_schema.sql:77` | 允许两条有向边，或增加方向字段 | P3 |

---

## 5. 建议的修复顺序

1. **第一批（P0，当天）**：M1 对话新建按钮、M2 事件按人筛选、M3 金额统计 SQL、M4 备份导出接口（修或撤按钮）。
2. **第二批（P1）**：M6 金额单位统一、M5 纪念日建议、M7 纪念日待办完成、M8 时区统一、N1 编辑能力、N2 还款 UI、N6 标签 UI、N5 关系 UI、N7 鉴权。
3. **第三批（P2）**：O2/O3/O4 统计口径与图表、O1 参与人选择器、O5 分页、O8 主题持久化、N3 生日闭环、N8 全局搜索。

验证门槛建议：在 CI 中除 `svelte-check` 外，增加针对 M1 类问题的防护——对交互元素做「事件处理器必须落在开标签内」的静态检查不现实，更实际的是补一组关键路径的端到端冒烟（新建对话、按人筛选事件、借款金额统计、备份导出），这 4 个 P0 全部可被一条冒烟用例捕获。
