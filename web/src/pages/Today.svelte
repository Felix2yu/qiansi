<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Dashboard, type Reminder, type Suggestion } from '../lib/api'
  import { navigate } from '../lib/router'
  import { CalendarDays, Users, Wallet, Bell, Sparkles, CheckCircle2, ArrowRight } from '@lucide/svelte'
  let dashboard = $state<Dashboard | null>(null)
  let upcoming = $state<Reminder[]>([])
  let suggestions = $state<Suggestion[]>([])
  let loading = $state(true)
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
    } finally { loading = false }
  }
  onMount(load)
  function done(id: string) { API.post(`/api/v1/reminders/${id}/done`, {}).then(load) }
</script>
<div class="space-y-6">
  <header>
    <h1 class="text-2xl font-semibold">今日</h1>
    <p class="text-sm mt-1" style="color: var(--q-muted);">牵丝 · 人际关系一目了然</p>
  </header>
  {#if loading}
    <div class="text-center py-16" style="color: var(--q-muted);">加载中…</div>
  {:else if dashboard}
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
            <li class="rounded-lg p-3 flex items-start gap-3 cursor-pointer transition-colors" style="background: var(--q-surface); border: 1px solid var(--q-border);"
                onclick={() => s.person_id && navigate(`/people/${s.person_id}`)}>
              <div class="text-xs px-2 py-0.5 rounded-full mt-0.5 shrink-0" style="background: var(--q-theme); color: white;">{s.type}</div>
              <div class="flex-1 text-sm min-w-0">
                <div class="font-medium truncate">{s.person_name || '系统'}</div>
                <div style="color: var(--q-muted);">{s.message}</div>
              </div>
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
        <ul class="space-y-2">
          {#each upcoming.slice(0, 10) as r}
            <li class="rounded-lg p-3 flex items-center gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
              <input type="checkbox" onchange={() => done(r.id)} />
              <div class="flex-1 min-w-0">
                <div class="text-sm truncate">{r.title}</div>
                <div class="text-xs mt-0.5" style="color: var(--q-muted);">
                  {r.person_name && `${r.person_name} · `}{new Date(r.due_at).toLocaleDateString()}
                </div>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}
</div>
