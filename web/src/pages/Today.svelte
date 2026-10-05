<script lang="ts">
  import { onMount } from 'svelte'
  import { API, todayLocal, REF_TYPE_LABEL, type Dashboard, type Reminder, type Suggestion } from '../lib/api'
  import { navigate } from '../lib/router'
  import { toast } from '../lib/toast.svelte'
  import { completeReminder } from '../lib/reminderActions'
  import { self, loadSelf } from '../lib/self.svelte'
  import { CalendarDays, Users, Wallet, Bell, Sparkles, CheckCircle2, ArrowRight, Check, X } from '@lucide/svelte'

  const today = todayLocal()
  let dashboard = $state<Dashboard | null>(null)
  let upcoming = $state<Reminder[]>([])
  let suggestions = $state<Suggestion[]>([])
  let loading = $state(true)
  // 首启引导：三步全靠已有数据判定，不做「已读」标记，走完自然就不出现了。
  // 唯一例外是用户明确说不用教——那就在本机收起，别让它赖在首屏上。
  let onboardHidden = $state(localStorage.getItem('q_onboard_off') === '1')
  const onboardSteps = $derived([
    { label: '先把第一个人记进来', hint: '通讯录是这一切的起点',
      done: (dashboard?.total_people || 0) > 0, to: '/people/new', cta: '去新建' },
    { label: '说出哪一条记录是你自己', hint: '关系图拼色、认识路径都以此为原点',
      done: !!self.id, to: '/settings', cta: '去设置' },
    { label: '记一次往来', hint: '吃过饭、随过礼都算一场',
      done: (dashboard?.total_events || 0) > 0, to: '/events?new=1', cta: '去记一笔' },
  ])
  const showOnboard = $derived(!onboardHidden && onboardSteps.some((s) => !s.done))
  function hideOnboard() {
    onboardHidden = true
    localStorage.setItem('q_onboard_off', '1')
  }
  // 首页只给「要紧的」：逾期与今天，其余留在下方或待办页
  const imminent = $derived((upcoming || []).filter(r => (r.due_at || '').slice(0, 10) <= today))
  const later = $derived((upcoming || []).filter(r => (r.due_at || '').slice(0, 10) > today))
  async function load() {
    loading = true
    try {
      const [d, r, s] = await Promise.all([
        API.get('/api/v1/dashboard/stats'),
        API.get('/api/v1/reminders/upcoming?days=30'),
        API.get('/api/v1/dashboard/suggestions'),
      ])
      dashboard = d as Dashboard
      upcoming = r as Reminder[]
      suggestions = s as Suggestion[]
    } catch (err) {
      // 拉不到要留在屏幕上：首页一片空白和真的没有数据看起来一样
      toast.fail('加载失败', err)
    } finally { loading = false }
  }
  onMount(() => { loadSelf(); load() })
  function done(r: Reminder) { void completeReminder(r, load) }
</script>
<div class="space-y-6">
  <header>
    <h1 class="text-2xl font-semibold">今日</h1>
    <p class="text-sm mt-1" style="color: var(--q-muted);">牵丝 · 人际关系一目了然</p>
  </header>
  {#if loading}
    <div class="text-center py-16" style="color: var(--q-muted);">加载中…</div>
  {:else if dashboard}
    {#if showOnboard}
      <section class="rounded-xl p-4" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-start justify-between gap-2 mb-2">
          <h2 class="text-sm font-medium flex items-center gap-2"><Sparkles size={14} /> 三步开始用牵丝</h2>
          <button class="p-1 rounded hover:bg-black/5 dark:hover:bg-white/10" style="color: var(--q-muted);"
                  title="不用了" onclick={hideOnboard}><X size={14} /></button>
        </div>
        <ol class="space-y-1.5">
          {#each onboardSteps as s, i}
            <li class="flex items-center gap-3 rounded-lg px-2 py-1.5" style="background: var(--q-bg);">
              <span class="w-5 h-5 shrink-0 rounded-full flex items-center justify-center text-xs"
                    style={s.done ? 'background: var(--q-theme); color: #fff;' : 'border: 1px solid var(--q-border); color: var(--q-muted);'}>
                {#if s.done}<Check size={12} />{:else}{i + 1}{/if}
              </span>
              <div class="flex-1 min-w-0">
                <div class="text-sm" style={s.done ? 'color: var(--q-muted); text-decoration: line-through;' : ''}>{s.label}</div>
                <div class="text-xs" style="color: var(--q-muted);">{s.hint}</div>
              </div>
              {#if !s.done}
                <button class="shrink-0 inline-flex items-center gap-1 text-xs px-2.5 py-1 rounded-lg text-white" style="background: var(--q-theme);"
                        onclick={() => navigate(s.to)}>{s.cta} <ArrowRight size={11} /></button>
              {/if}
            </li>
          {/each}
        </ol>
      </section>
    {/if}
    <section class="grid grid-cols-2 md:grid-cols-4 gap-3">
      <div class="rounded-xl p-4" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-center gap-2 text-sm" style="color: var(--q-muted);"><Users size={16} /> 联系人</div>
        <div class="text-2xl font-semibold mt-1">{dashboard.total_people}</div>
      </div>
      <div class="rounded-xl p-4" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-center gap-2 text-sm" style="color: var(--q-muted);"><CalendarDays size={16} /> 往来事件</div>
        <div class="text-2xl font-semibold mt-1">{dashboard.total_events}</div>
      </div>
      <div class="rounded-xl p-4" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-center gap-2 text-sm" style="color: var(--q-muted);"><Bell size={16} /> 7 天内待办</div>
        <div class="text-2xl font-semibold mt-1">{dashboard.upcoming_days7}</div>
      </div>
      <div class="rounded-xl p-4" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-center gap-2 text-sm" style="color: var(--q-muted);"><CheckCircle2 size={16} /> 未兑现承诺</div>
        <div class="text-2xl font-semibold mt-1">{dashboard.pending_promises}</div>
      </div>
    </section>
    <section class="grid grid-cols-2 gap-3">
      <div class="rounded-xl p-4" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-center gap-2 text-sm" style="color: var(--q-muted);"><Wallet size={14} /> 我借出（未还）</div>
        <div class="text-2xl font-semibold mt-1">¥ {(dashboard.lend_fen / 100).toFixed(2)}</div>
      </div>
      <div class="rounded-xl p-4" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-center gap-2 text-sm" style="color: var(--q-muted);"><Wallet size={14} /> 我待还</div>
        <div class="text-2xl font-semibold mt-1">¥ {(dashboard.borrow_fen / 100).toFixed(2)}</div>
      </div>
    </section>
    {#if suggestions.length > 0}
      <section>
        <h2 class="text-sm font-medium mb-3 flex items-center gap-2"><Sparkles size={14} /> 智能建议</h2>
        <ul class="space-y-2">
          {#each suggestions.slice(0, 6) as s}
            <li>
              <button class="w-full text-left rounded-lg p-3 flex items-start gap-3 transition-colors disabled:cursor-default"
                      style="background: var(--q-surface); border: 1px solid var(--q-border);"
                      disabled={!s.person_id}
                      onclick={() => s.person_id && navigate(`/people/${s.person_id}`)}>
                <div class="text-xs px-2 py-0.5 rounded-full mt-0.5 shrink-0" style="background: var(--q-theme); color: white;">{s.type}</div>
                <div class="flex-1 text-sm min-w-0">
                  <div class="font-medium truncate">{s.person_name || '系统'}</div>
                  <div style="color: var(--q-muted);">{s.message}</div>
                </div>
              </button>
            </li>
          {/each}
        </ul>
      </section>
    {/if}
    <section>
      <div class="flex items-center justify-between mb-3">
        <h2 class="text-sm font-medium flex items-center gap-2"><Bell size={14} /> 近期待办</h2>
        <button class="text-xs flex items-center gap-1 hover:opacity-80" style="color: var(--q-muted);" onclick={() => navigate('/reminders')}>
          查看全部 <ArrowRight size={12} />
        </button>
      </div>
      {#if upcoming.length === 0}
        <div class="rounded-lg p-6 text-center text-sm" style="color: var(--q-muted); background: var(--q-surface); border: 1px dashed var(--q-border);">近期没有待办 🎉</div>
      {:else}
        {#if imminent.length > 0}
          <div class="text-xs mb-1 font-medium" style="color: #ef4444;">逾期 / 今天 · {imminent.length}</div>
          <ul class="space-y-2 mb-3">
            {#each imminent as r}
              <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
                <input type="checkbox" onchange={() => done(r)} />
                <div class="flex-1 min-w-0">
                  <div class="text-sm truncate">{r.title}</div>
                  <div class="text-xs mt-0.5" style="color: var(--q-muted);">
                    {REF_TYPE_LABEL[r.ref_type] || r.ref_type}{r.person_name && ` · ${r.person_name}`} · {new Date(r.due_at).toLocaleDateString()}
                  </div>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
        {#if later.length > 0}
          <div class="text-xs mb-1 font-medium" style="color: var(--q-muted);">之后 · {later.length}</div>
          <ul class="space-y-2">
            {#each later.slice(0, 6) as r}
              <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
                <input type="checkbox" onchange={() => done(r)} />
                <div class="flex-1 min-w-0">
                  <div class="text-sm truncate">{r.title}</div>
                  <div class="text-xs mt-0.5" style="color: var(--q-muted);">
                    {REF_TYPE_LABEL[r.ref_type] || r.ref_type}{r.person_name && ` · ${r.person_name}`} · {new Date(r.due_at).toLocaleDateString()}
                  </div>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      {/if}
    </section>
  {/if}
</div>
