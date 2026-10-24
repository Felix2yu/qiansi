# 往来开销 ↔ 货殖账单：关联模型与实现说明

一次往来可能对应货殖里的多张账单（一顿饭 + 打车 + 门票）。本文说明这条关系存在哪、
怎么存、金额以谁为准、界面上怎么显示，以及刻意不做的部分。

## 1. 一句话

**账单的权威在货殖，关系的权威在牵丝。** 牵丝只读货殖账单，保存「关联 + 快照」，
一次往来对应 N 张账单；有账单时，往来的开销等于账单金额之和。

## 2. 为什么是这个方向

| 方案 | 结论 |
| --- | --- |
| 牵丝把开销推成货殖账单 | 否决。牵丝会变成第二个记账入口，两边口径迟早打架，还得处理幂等、改账、撤账 |
| 货殖反向认识牵丝 | 否决。账本不该依赖一个通讯录应用，删掉牵丝会在货殖里留下孤儿引用 |
| **牵丝持有引用 + 快照（本方案）** | 采纳。单向依赖、可失效、可回退；货殖完全不知道牵丝存在 |

## 3. 数据结构

```sql
-- internal/db/migrations/015_event_bills.sql
CREATE TABLE event_bills (
    id            TEXT PRIMARY KEY,
    event_id      TEXT NOT NULL,          -- 往来，1:N
    remote_id     TEXT NOT NULL,          -- 货殖 transactions.id（字符串化，跨库不做外键）
    remote_base   TEXT NOT NULL DEFAULT '',-- 关联时的货殖地址，用于拼跳转链接
    amount_fen    INTEGER NOT NULL DEFAULT 0,
    type          TEXT NOT NULL DEFAULT 'expense',
    tx_date       TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    category_name TEXT NOT NULL DEFAULT '',
    account_name  TEXT NOT NULL DEFAULT '',
    book_name     TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    synced_at     TEXT NOT NULL,          -- 快照时间，界面用来提示「可能需要刷新」
    UNIQUE (event_id, remote_id)
);
```

要点：

- **为什么要快照**：货殖没开、改域名、临时维护时，往来页仍要显示「这场花了多少、花在哪儿」。
  快照是缓存，不是数据源——`刷新` 按 `remote_id` 批量重取覆盖。
- **为什么不用外键**：`remote_id` 是另一个库的自增 id，跨系统外键无法维护。
  货殖那边删了账单，这里表现为「刷新时进 stale 列表」，由用户决定是否解除关联。
- **为什么金额也存一份**：列表页有 N 条往来，逐条去货殖取值会把一屏渲染变成 N 次跨服务调用。
- **唯一约束 `(event_id, remote_id)`**：重复关联走 `ON CONFLICT DO UPDATE`，
  保留原行 id 与 `created_at`，只刷新快照，所以前端连点两下不会产生两行。

## 4. 金额口径（唯一真源）

`internal/store/events.go` 的 `eventColumns` 里，`expense_fen` 是按「有没有账单」切换的投影：

```sql
CASE WHEN EXISTS (SELECT 1 FROM event_bills WHERE event_id = e.id)
     THEN (SELECT COALESCE(SUM(amount_fen),0) FROM event_bills WHERE event_id = e.id)
     ELSE (SELECT COALESCE(SUM(amount_fen),0) FROM live_transactions
            WHERE event_id = e.id AND direction = 'out' AND id IS NOT e.gift_transaction_id)
END
```

推论（三条都要一致）：

1. 有关联账单 → 开销 = 账单之和，**手填金额不参与**。
2. 手填的那笔历史账目**不删**。它只是不再计入投影，一旦全部解除关联就自动恢复显示。
   （这也是 `PUT /events/{id}` 在有账单时拒收 `expense_fen > 0` 的原因：那等于承认两套数字。）
3. 礼金不受影响：`gift_amount_fen` 一直是事件自带那笔账的投影，不与账单相加。

## 5. 接口

### 货殖（新增，只读，API Key 鉴权）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/integration/bills` | 搜索：`keyword` / `start_date` / `end_date` / `type` / `page` / `page_size`，复用 Web 端同一套筛选口径 |
| GET | `/api/integration/bills?ids=1,2,3` | 按 id 批量取（上限 200），刷新快照用 |
| GET | `/api/integration/bills/:id` | 单条 |

返回 `{code,message,data:{items:[...]}}`，每项含 `amount_fen`（分）、`tx_date`、
`category_name` / `account_name` / `book_name`（已解析名称）。
**单开一组而不是给 `/api/transactions` 加 API Key**：长期密钥只该有读权限。

### 牵丝

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/huozhi/status` | 设置页「保存并测试」用：`{configured, ok, base, error}` |
| GET | `/api/v1/huozhi/bills` | 代理搜索（密钥不出服务端），额外回传 `base` 供前端拼跳转 |
| POST | `/api/v1/events/{id}/bills` | `{remote_ids:[...]}`，一次关联多张；回 `{bills, missing}` |
| POST | `/api/v1/events/{id}/bills/refresh` | 按已关联 id 重取快照；回 `{bills, stale}` |
| DELETE | `/api/v1/events/{id}/bills/{billID}` | 解除一条（只删关联，不碰货殖账单） |

关联时金额与名称一律由服务端向货殖取回后落库，**不信任前端传的快照**——
否则旧版本页面能把过期金额写进去。

## 6. 展示规则

- **往来列表**：`花费 ¥114.50` + `货殖 2 笔` 徽标（`bill_count` 来自同一个投影查询的
  `COUNT` 子查询，不额外发请求）。徽标是「这笔钱从哪来」的提示，不是第二个金额。
- **往来表单**：有关联账单时，手填金额输入框和归属人选择一起让位给一行只读汇总
  「开销 ¥114.50 · 由 2 张货殖账单汇总」；下方是账单清单（日期 · 备注 · 分类 · 金额 ·
  外链 · 解除）。点外链在货殖里打开该账单，`target="_blank"`。
- **选择器**：只列货殖的**支出**流水（`type=expense`），不混收入与余额调整；
  已关联的置灰显示「已关联」。
- **失效**：刷新时货殖已不存在的账单进入 `stale`，界面提示「N 张账单在货殖里已不存在，
  请检查后解除关联」，不自动删——关联是用户的陈述，系统不该替他改。
- **新建往来**：还没保存就没有 `event_id`，账单区只显示提示，保存后再关联。

## 7. 配置与安全

1. 货殖：设置 → API 密钥 → 生成并启用（64 位十六进制）。
2. 牵丝：设置 → 货殖账单 → 填地址与密钥 → 保存并测试。
3. 密钥存 `settings.huozhi_api_key`，只在本机服务端读取，**从不下发浏览器**；
   搜索一律走后端代理。牵丝自身若开了 `QIANSI_TOKEN`，则访问这些接口同样需要令牌。

## 8. 刻意不做的

- 不在牵丝里改货殖账单（金额、分类、删除都不行）。
- 不做实时推送/回调：快照 + 手动刷新已经够用，实时同步会引入长连接与一致性问题。
- 不支持多货殖实例：`remote_base` 只用于展示与跳转校验，不按实例路由。
- 不在货殖那边记录「这张账单属于哪次往来」：单向依赖，货殖保持无感。
