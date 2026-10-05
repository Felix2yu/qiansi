<script lang="ts">
  import { onMount } from 'svelte'
  import { API, contactAgo, gradeLabel, daysSince, type DriftPerson } from '../lib/api'
  import { navigate } from '../lib/router'
  import { HeartPulse, PenLine } from '@lucide/svelte'
  import { toast } from '../lib/toast.svelte'

  let list = $state<DriftPerson[]>([])
  let onlyOverdue = $state(true)
  let loading = $state(true)
  // 连点两个筛选会并发发出两趟请求，谁后回来谁说了算——只认最新那次
  let reqId = 0

  async function load() {
    const mine = ++reqId
    loading = true
    const qs = onlyOverdue ? '?only=overdue&limit=200' : '?limit=200'
    const got = await API.get<DriftPerson[]>(`/api/v1/contacts/drift${qs}`).catch((err: any) => {
      toast.fail('加载失败', err)
      return [] as DriftPerson[]
    }) || []
    if (mine !== reqId) return
    list = got
    loading = false
  }

  onMount(load)

  // 后端给的是「距锚点几天」与「逾期几天」，还剩几天从到期日现算
  function untilDue(d: DriftPerson): number | null {
    const n = daysSince(d.due_at)
    return n === null ? null : -n
  }

  async function checkin(d: DriftPerson) {
    try {
      await API.post(`/api/v1/contacts/${d.person_id}/checkin`, {})
    } catch (err) {
      // 名单上这个人可能已经被删进回收站了，重拉一次让屏幕跟上服务端
      toast.fail('打卡失败', err)
      await load()
      return
    }
    toast.ok(`已记下今天联系过 ${d.name}`)
    await load()
  }

  function rowMeta(d: DriftPerson): string {
    const from = d.anchor_from === 'contact' ? `上次联系 ${contactAgo(d.anchor)}` : `建档于 ${contactAgo(d.anchor)}`
    return `${gradeLabel(d.grade)} · ${from} · 每 ${d.days} 天`
  }
</script>
<div class="space-y-4">
  <header class="flex items-center justify-between gap-3 flex-wrap">
    <div>
      <h1 class="text-2xl font-semibold">渐远名单</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">按亲密度设定的节奏，算出谁该联系了</p>
    </div>
    <button class="text-xs" style="color: var(--q-theme);" onclick={() => navigate('/settings')}>改联系节奏</button>
  </header>

  <div class="flex items-center gap-2">
    {#each [{ v: true, n: '已逾期' }, { v: false, n: '全部' }] as tab (tab.v)}
      <button onclick={() => { onlyOverdue = tab.v; load() }} aria-pressed={onlyOverdue === tab.v}
              class="px-3 py-1.5 rounded-lg text-sm"
              style={onlyOverdue === tab.v
                ? 'background: var(--q-theme); color: #fff;'
                : 'background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);'}>
        {tab.n}{#if tab.v && !loading}<span class="ml-1 opacity-80">{list.length}</span>{/if}
      </button>
    {/each}
  </div>

  {#if loading}
    <div class="text-center py-8 text-sm" style="color: var(--q-muted);">加载中…</div>
  {:else if list.length === 0}
    <div class="text-center py-10 text-sm" style="color: var(--q-muted);">
      {onlyOverdue ? '没有到期的人。要连同还没到期的名单一起看，切到「全部」。' : '还没有联系人。'}
    </div>
  {:else}
    <ul class="space-y-2">
      {#each list as d (d.person_id)}
        <li class="rounded-lg p-3 flex items-center gap-3 flex-wrap" style="background: var(--q-surface); border: 1px solid var(--q-border);">
          <div class="flex-1 min-w-0">
            <button class="text-sm font-medium truncate hover:underline" style="color: var(--q-text);"
                    onclick={() => navigate(`/people/${d.person_id}`)}>{d.name}</button>
            <div class="text-xs mt-0.5" style="color: var(--q-muted);">{rowMeta(d)}</div>
          </div>
          {#if d.overdue}
            <span class="text-xs px-2 py-0.5 rounded-full shrink-0 flex items-center gap-1"
                  style="background: var(--q-theme); color: #fff;"><HeartPulse size={12} /> 已逾期 {d.overdue_days} 天</span>
          {:else}
            {@const left = untilDue(d)}
            <span class="text-xs px-2 py-0.5 rounded-full shrink-0"
                  style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);">
              {left === null ? d.due_at : left === 0 ? '今天该联系' : `还有 ${left} 天`}
            </span>
          {/if}
          <button class="text-xs px-2 py-1 rounded shrink-0" style="background: var(--q-bg); border: 1px solid var(--q-border);"
                  onclick={() => checkin(d)}>联系过了</button>
          <button class="text-xs px-2 py-1 rounded shrink-0 flex items-center gap-1" style="background: var(--q-bg); border: 1px solid var(--q-border);"
                  onclick={() => navigate(`/events?new=1&with=${d.person_id}`)}><PenLine size={12} /> 记一笔</button>
        </li>
      {/each}
    </ul>
  {/if}
</div>
