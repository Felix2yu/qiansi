<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Person, type TimelineItem } from '../lib/api'
  import { navigate } from '../lib/router'
  import { Trash2, Edit3, ArrowLeft } from '@lucide/svelte'

  let { id = '' }: { id?: string } = $props()
  let person = $state<Person | null>(null)
  let timeline = $state<TimelineItem[]>([])
  let intimacy = $state<{ current_score: number; trend: { day: string; score: number }[] } | null>(null)
  let loading = $state(true)

  async function load() {
    loading = true
    try {
      const r = await API.get(`/api/v1/people/${id}`) as any
      person = r.person
      timeline = await API.get(`/api/v1/people/${id}/timeline`) as TimelineItem[]
      intimacy = await API.get(`/api/v1/people/${id}/intimacy`) as any
    } finally { loading = false }
  }
  onMount(load)
  $effect(() => { if (id) load() })

  async function remove() {
    if (person && confirm(`删除联系人「${person.name}」及其所有关联记录？`)) {
      await API.delete(`/api/v1/people/${id}`); navigate('/people')
    }
  }
</script>

{#if loading}
  <div class="text-center py-16" style="color: var(--q-muted);">加载中…</div>
{:else if person}
  <div class="space-y-6">
    <button class="flex items-center gap-1 text-sm" style="color: var(--q-muted);" onclick={() => navigate('/people')}>
      <ArrowLeft size={14} /> 返回
    </button>
    <div class="flex items-start gap-5 p-5" style="background: var(--q-surface); border: 1px solid var(--q-border); border-radius: 16px;">
      <div class="w-16 h-16 rounded-full flex items-center justify-center text-2xl text-white font-semibold shrink-0" style="background: var(--q-theme);">{person.name.slice(0,1)}</div>
      <div class="flex-1">
        <h1 class="text-xl font-semibold">{person.name}</h1>
        <div class="mt-1 text-sm" style="color: var(--q-muted);">
          Grade {'★'.repeat(person.grade)}{person.category_name && ` · ${person.category_name}`}
          {person.gender && ` · ${person.gender}`}
          {person.birthday && ` · 生日 ${person.birthday}`}
        </div>
        {#if person.phone}<div class="text-sm mt-0.5" style="color: var(--q-muted);">📱 {person.phone}</div>{/if}
        {#if person.wechat}<div class="text-sm" style="color: var(--q-muted);">💬 {person.wechat}</div>{/if}
        {#if person.location}<div class="text-sm" style="color: var(--q-muted);">📍 {person.location}</div>{/if}
        {#if person.notes}<div class="mt-3 text-sm leading-relaxed whitespace-pre-wrap">{person.notes}</div>{/if}
      </div>
      <div class="flex flex-col gap-1">
        <button class="p-2 rounded-lg hover:bg-black/5 dark:hover:bg-white/10" title="编辑"><Edit3 size={16} /></button>
        <button class="p-2 rounded-lg hover:bg-black/5 dark:hover:bg-white/10" title="删除" onclick={remove}><Trash2 size={16} /></button>
      </div>
    </div>

    {#if intimacy}
      <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-sm font-medium">亲密度</h2>
          <div class="text-2xl font-semibold" style="color: var(--q-theme);">{intimacy.current_score}</div>
        </div>
        {#if intimacy.trend.length > 1}
          <svg width="100%" height="80" viewBox="0 0 200 80" preserveAspectRatio="none">
            {(() => {
              const pts = intimacy.trend
              const min = Math.min(...pts.map(p => p.score)), max = Math.max(...pts.map(p => p.score))
              const s = pts.map((p, i) => `${(i / (pts.length-1))*200},${70 - ((p.score-min) / Math.max(1,max-min))*60}`).join(' ')
              return `<polyline fill="none" stroke="var(--q-theme)" stroke-width="2" points="${s}" />`
            })()}
          </svg>
        {/if}
      </section>
    {/if}

    <section>
      <h2 class="text-sm font-medium mb-3">时间线</h2>
      {#if timeline.length === 0}
        <div class="rounded-lg p-6 text-center text-sm" style="color: var(--q-muted); background: var(--q-surface); border: 1px dashed var(--q-border);">暂无记录</div>
      {:else}
        <ul class="space-y-2">
          {#each timeline as t}
            <li class="rounded-lg p-3 flex items-start gap-3" style="background: var(--q-surface); border: 1px solid var(--q-border);">
              {#if t.type === 'event'}<div class="w-2 h-2 rounded-full mt-2 shrink-0" style="background: #10b981;"></div>
              {:else if t.type === 'memo'}<div class="w-2 h-2 rounded-full mt-2 shrink-0" style="background: #f59e0b;"></div>
              {:else if t.type === 'transaction'}<div class="w-2 h-2 rounded-full mt-2 shrink-0" style="background: #ef4444;"></div>
              {:else}<div class="w-2 h-2 rounded-full mt-2 shrink-0" style="background: #6366f1;"></div>{/if}
              <div class="flex-1 min-w-0">
                <div class="text-sm">{t.title}</div>
                <div class="text-xs mt-0.5" style="color: var(--q-muted);">
                  {t.type} · {new Date(t.date).toLocaleDateString()}
                </div>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  </div>
{:else}
  <div class="text-center py-16" style="color: var(--q-muted);">人物不存在</div>
{/if}
