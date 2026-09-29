<script lang="ts">
  import { onMount } from 'svelte'
  import { API, TIMELINE_LABEL, todayLocal, toFen,
           type Person, type TimelineItem, type Category, type Tag,
           type PersonField, type TrendPoint } from '../lib/api'
  import PersonForm from '../lib/PersonForm.svelte'
  import PersonRelEditor from '../lib/PersonRelEditor.svelte'
  import EventForm from '../lib/EventForm.svelte'
  import { navigate } from '../lib/router'
  import { self, setSelf, loadSelf } from '../lib/self.svelte'
  import { Trash2, Edit3, ArrowLeft, X, Plus, Archive, Upload, UserCheck, ImageOff, Pencil, Check } from '@lucide/svelte'

  let { id = '' }: { id?: string } = $props()
  let person = $state<Person | null>(null)
  // 没设亲密度就不排五个空心号，元信息按实际有的字段拼
  const metaLine = $derived.by(() => {
    const p = person
    if (!p) return ''
    return [
      p.grade > 0 ? '♥'.repeat(p.grade) : '',
      (p.categories ?? []).map(c => c.name).join('·'),
      p.gender || '',
      p.birthday ? `生日 ${p.birthday}${p.birthday_is_lunar ? '（农历）' : ''}` : '',
    ].filter(Boolean).join(' · ')
  })
  let timeline = $state<TimelineItem[]>([])
  let intimacy = $state<{ current_score: number; trend: TrendPoint[] } | null>(null)
  let categories = $state<Category[]>([])
  let tags = $state<Tag[]>([])
  let ownedTags = $state<Tag[]>([])
  let fields = $state<PersonField[]>([])
  let loading = $state(true)
  let showEdit = $state(false)
  let newField = $state({ label: '', value: '' })
  let editingFieldId = $state('')
  let fieldForm = $state({ label: '', value: '' })
  let busyField = $state(false)
  let words = $state<{ word: string; count: number }[]>([])
  let avatarInput: HTMLInputElement | undefined = $state()
  let uploadingAvatar = $state(false)
  // 快捷记录：不用离开详情页就能补一笔钱 / 一句话；往来走完整表单（可改日期、地点、礼物）
  let quick = $state<'' | 'money' | 'memo'>('')
  let quickText = $state('')
  let quickAmount = $state('')
  // 往来表单：复用往来页的新建/编辑框
  let showEventForm = $state(false)
  let eventEditId = $state('')
  // 预填对象引用保持稳定，表单组件只在打开时读取，变动会重置已填内容
  let eventPreset = $state<{ event_date?: string; participant_ids?: string[] }>({})

  function openEventForm(editId = '') {
    eventPreset = { event_date: todayLocal(), participant_ids: [id] }
    eventEditId = editId
    showEventForm = true
  }
  function closeEventForm() {
    showEventForm = false
    eventEditId = ''
  }
  async function afterEventForm() {
    closeEventForm()
    await load()
    await loadWords()
  }
  async function removeEvent(eventId: string, title: string, expenseFen?: number) {
    const tail = expenseFen ? '该往来关联的开销账目会保留，仅解除关联。' : ''
    if (!confirm(`确定删除「${title}」吗？${tail}`)) return
    try {
      await API.delete(`/api/v1/events/${eventId}`)
    } catch (err: any) {
      alert('删除失败：' + (err?.message || err))
      return
    }
    await afterEventForm()
  }

  async function loadWords() {
    try {
      words = await API.get(`/api/v1/people/${id}/wordcloud`) as { word: string; count: number }[]
    } catch { words = [] }
  }

  async function uploadAvatar(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file || !person) return
    uploadingAvatar = true
    try {
      const fd = new FormData()
      fd.append('file', file)
      fd.append('entity_type', 'person')
      fd.append('entity_id', id)
      const res = await fetch('/api/v1/attachments', {
        method: 'POST',
        headers: (localStorage.getItem('q_token') || '') ? { Authorization: 'Bearer ' + localStorage.getItem('q_token') } : undefined,
        body: fd,
      })
      if (!res.ok) {
        let msg = res.statusText
        try { msg = (await res.json()).error || msg } catch {}
        throw new Error(msg)
      }
      const data = await res.json()
      // 存可直接渲染的 URL（/uploads/xxx）——avatar_attachment_id 是自由文本字段
      await API.put(`/api/v1/people/${id}`, { ...person, avatar_attachment_id: data.url })
      await load()
    } catch (err: any) {
      alert('头像上传失败：' + (err?.message || err))
    } finally {
      input.value = ''
      uploadingAvatar = false
    }
  }

  // 之前只能换头像不能摘，传错了也没法回到首字占位
  async function removeAvatar() {
    if (!person) return
    try {
      await API.put(`/api/v1/people/${id}`, { ...person, avatar_attachment_id: '' })
      await load()
    } catch (err: any) {
      alert('移除头像失败：' + (err?.message || err))
    }
  }

  async function toggleArchive() {
    if (!person) return
    if (person.archived) await API.delete(`/api/v1/people/${id}/archive`)
    else await API.post(`/api/v1/people/${id}/archive`, {})
    await load()
  }

  // 本人只能有一个，再设别人等于把指针挪过去
  async function toggleSelf() {
    const next = self.id === id ? '' : id
    try {
      await API.post('/api/v1/settings/bulk', { self_person_id: next })
      setSelf(next)
    } catch (err: any) {
      alert('设置失败：' + (err?.message || err))
    }
  }

  async function submitQuick() {
    if (!quick) return
    const text = quickText.trim()
    try {
      if (quick === 'money') {
        const fen = toFen(quickAmount)
        if (fen <= 0) { alert('请填写金额（元）'); return }
        await API.post('/api/v1/transactions', {
          person_id: id, kind: 'loan', direction: 'out', amount_fen: fen,
          title: text, occurred_at: todayLocal(),
        })
      } else {
        if (!text) { alert('请填写内容'); return }
        await API.post('/api/v1/memos', { person_id: id, content: text, said_at: todayLocal(), speaker: 'other' })
      }
      quick = ''
      quickText = ''
      quickAmount = ''
      await load()
      await loadWords()
    } catch (err: any) {
      alert('保存失败：' + (err?.message || err))
    }
  }

  async function load() {
    loading = true
    try {
      const r = await API.get(`/api/v1/people/${id}`) as any
      person = r.person
      fields = (r.fields || []) as PersonField[]
      const [tl, inti, cats, tg, owned] = await Promise.all([
        API.get(`/api/v1/people/${id}/timeline`) as Promise<TimelineItem[]>,
        API.get(`/api/v1/people/${id}/intimacy`) as Promise<any>,
        API.get('/api/v1/categories') as Promise<Category[]>,
        API.get('/api/v1/tags') as Promise<Tag[]>,
        API.get(`/api/v1/taggings/of?target_type=person&target_id=${id}`) as Promise<Tag[]>,
        loadSelf(true),
      ])
      timeline = tl
      intimacy = inti
      categories = cats
      tags = tg
      ownedTags = owned || []
      loadWords()
    } finally { loading = false }
  }
  onMount(load)
  $effect(() => { if (id) load() })

  async function remove() {
    if (person && confirm(`删除联系人「${person.name}」及其所有关联记录？`)) {
      await API.delete(`/api/v1/people/${id}`); navigate('/people')
    }
  }

  async function addField() {
    if (!newField.label.trim()) return
    try {
      const saved = await API.post(`/api/v1/people/${id}/fields`, { label: newField.label.trim(), value: newField.value.trim() }) as PersonField
      fields = [...fields, saved]
      newField = { label: '', value: '' }
    } catch (err: any) {
      alert('添加失败：' + (err?.message || err))
    }
  }
  // 带 id 提交就是更新（后端 ON CONFLICT(id) DO UPDATE），否则改一个字只能删了重建
  function startEditField(f: PersonField) {
    editingFieldId = f.id
    fieldForm = { label: f.label, value: f.value || '' }
  }
  async function saveField(f: PersonField) {
    if (!fieldForm.label.trim()) { alert('字段名不能为空'); return }
    busyField = true
    try {
      const saved = await API.post(`/api/v1/people/${id}/fields`,
        { id: f.id, label: fieldForm.label.trim(), value: fieldForm.value.trim(), sort_order: f.sort_order ?? 0 }) as PersonField
      fields = fields.map(x => x.id === saved.id ? saved : x)
      editingFieldId = ''
    } catch (err: any) {
      alert('保存失败：' + (err?.message || err))
    } finally { busyField = false }
  }
  async function removeField(fid: string) {
    if (!confirm('删除这个自定义字段？')) return
    try {
      await API.delete(`/api/v1/people/${id}/fields/${fid}`)
      if (editingFieldId === fid) editingFieldId = ''
      fields = fields.filter(f => f.id !== fid)
    } catch (err: any) {
      alert('删除失败：' + (err?.message || err))
    }
  }

  // 兼容两种存法：完整 URL（/uploads/x）或裸的 stored_name
  function avatarSrc(v: string) {
    return v.startsWith('/') || v.startsWith('http') ? v : '/uploads/' + v
  }

  function trendPoints(): TrendPoint[] {
    return (intimacy?.trend || []).filter(p => typeof p?.score === 'number')
  }
  // 折线坐标点串；点数不足时返回空串，由模板决定是否渲染
  const trendPath = $derived.by(() => {
    const pts = trendPoints()
    if (pts.length < 2) return ''
    const min = Math.min(...pts.map(p => p.score))
    const max = Math.max(...pts.map(p => p.score))
    return pts.map((p, i) => `${(i / (pts.length - 1)) * 200},${70 - ((p.score - min) / Math.max(1, max - min)) * 60}`).join(' ')
  })
</script>

{#if loading}
  <div class="text-center py-16" style="color: var(--q-muted);">加载中…</div>
{:else if person}
  <div class="space-y-6">
    <button class="flex items-center gap-1 text-sm" style="color: var(--q-muted);" onclick={() => navigate('/people')}>
      <ArrowLeft size={14} /> 返回
    </button>
    <div class="flex items-start gap-5 p-5" style="background: var(--q-surface); border: 1px solid var(--q-border); border-radius: 16px;">
      <div class="shrink-0">
        <input type="file" accept="image/*" class="hidden" bind:this={avatarInput} onchange={uploadAvatar} />
        <button class="w-16 h-16 rounded-full overflow-hidden flex items-center justify-center text-2xl text-white font-semibold disabled:opacity-60"
                style="background: var(--q-theme);" disabled={uploadingAvatar} onclick={() => avatarInput?.click()} title="点击更换头像">
          {#if person.avatar_attachment_id}
            <img src={avatarSrc(person.avatar_attachment_id)} alt="" class="w-full h-full object-cover" />
          {:else}
            {person.name.slice(0,1)}
          {/if}
        </button>
      </div>
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-2">
          <h1 class="text-xl font-semibold">{person.name}</h1>
          {#if self.id === person.id}<span class="text-xs px-2 py-0.5 rounded-full text-white" style="background: var(--q-theme);">本人</span>{/if}
          {#if person.archived}<span class="text-xs px-2 py-0.5 rounded-full" style="background: var(--q-bg); color: var(--q-muted);">已归档</span>{/if}
        </div>
        {#if metaLine}
          <div class="mt-1 text-sm" style="color: var(--q-muted);">{metaLine}</div>
        {/if}
        {#if person.phone}<div class="text-sm mt-0.5" style="color: var(--q-muted);">📱 {person.phone}</div>{/if}
        {#if person.wechat}<div class="text-sm" style="color: var(--q-muted);">💬 {person.wechat}</div>{/if}
        {#if person.location}<div class="text-sm" style="color: var(--q-muted);">📍 {person.location}</div>{/if}
        {#if ownedTags.length > 0}
          <div class="flex flex-wrap gap-1 mt-2">
            {#each ownedTags as t}
              <span class="text-xs px-2 py-0.5 rounded-full" style="background: color-mix(in srgb, {t.color} 18%, transparent); color: {t.color};">{t.name}</span>
            {/each}
          </div>
        {/if}
        {#if person.notes}<div class="mt-3 text-sm leading-relaxed whitespace-pre-wrap">{person.notes}</div>{/if}
      </div>
      <div class="flex flex-col gap-1">
        <button class="p-2 rounded-lg hover:bg-black/5 dark:hover:bg-white/10" title="编辑" onclick={() => (showEdit = true)}><Edit3 size={16} /></button>
        <button class="p-2 rounded-lg hover:bg-black/5 dark:hover:bg-white/10" title={person.archived ? '取消归档' : '归档'} onclick={toggleArchive}><Archive size={16} /></button>
        {#if person.avatar_attachment_id}
          <button class="p-2 rounded-lg hover:bg-black/5 dark:hover:bg-white/10" title="移除头像" onclick={removeAvatar}><ImageOff size={16} /></button>
        {/if}
        <button class="p-2 rounded-lg hover:bg-black/5 dark:hover:bg-white/10" style={self.id === id ? 'color: var(--q-theme);' : ''}
                title={self.id === id ? '取消本人' : '设为本人（关系图以此为中心）'} onclick={toggleSelf}><UserCheck size={16} /></button>
        <button class="p-2 rounded-lg hover:bg-black/5 dark:hover:bg-white/10" title="删除" onclick={remove}><Trash2 size={16} /></button>
      </div>
    </div>

    <!-- 快捷记录：不离开详情页就能补一条记录 -->
    <section class="rounded-xl p-4" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-xs" style="color: var(--q-muted);">快捷记录</span>
        <button class="text-xs px-2 py-1 rounded-md" style="background: var(--q-theme); color: white; border: 1px solid var(--q-theme);" onclick={() => openEventForm()}>记一次往来（完整）</button>
        <button class="text-xs px-2 py-1 rounded-md" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => (quick = quick === 'money' ? '' : 'money')}>记一笔钱</button>
        <button class="text-xs px-2 py-1 rounded-md" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={() => (quick = quick === 'memo' ? '' : 'memo')}>记一句话</button>
      </div>
      {#if quick}
        <div class="flex gap-2 mt-3">
          <input bind:value={quickText} class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
                 placeholder={quick === 'memo' ? 'TA 说了什么' : '用途，如 借款'} />
          {#if quick === 'money'}
            <input type="number" step="0.01" min="0" bind:value={quickAmount} class="w-28 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" placeholder="金额（元）" />
          {/if}
          <button class="px-3 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submitQuick}>保存</button>
        </div>
      {/if}
    </section>

    {#if intimacy}
      <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-sm font-medium">亲密度</h2>
          <div class="text-2xl font-semibold" style="color: var(--q-theme);">{intimacy.current_score}</div>
        </div>
        {#if trendPath}
          <svg width="100%" height="80" viewBox="0 0 200 80" preserveAspectRatio="none">
            <polyline fill="none" stroke="var(--q-theme)" stroke-width="2" points={trendPath} />
          </svg>
        {:else}
          <div class="text-xs" style="color: var(--q-muted);">暂无历史趋势，系统每日会自动记录一次快照</div>
        {/if}
      </section>
    {/if}

    <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <h2 class="text-sm font-medium mb-3">自定义字段</h2>
      <ul class="space-y-1 mb-3">
        {#each fields as f (f.id)}
          <li class="text-sm px-2 py-1 rounded-md" style="background: var(--q-bg);">
            {#if editingFieldId === f.id}
              <div class="flex flex-wrap items-center gap-2">
                <input bind:value={fieldForm.label} placeholder="字段名" class="w-24 px-2 py-1 rounded-lg text-xs outline-none"
                       style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);" />
                <input bind:value={fieldForm.value} placeholder="值" class="flex-1 min-w-24 px-2 py-1 rounded-lg text-xs outline-none"
                       style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);" />
                <button class="p-1 rounded disabled:opacity-60" style="color: var(--q-theme);" title="保存" disabled={busyField} onclick={() => saveField(f)}><Check size={14} /></button>
                <button class="p-1 rounded" style="color: var(--q-muted);" title="取消" onclick={() => (editingFieldId = '')}><X size={14} /></button>
              </div>
            {:else}
              <div class="flex items-center gap-2">
                <span style="color: var(--q-muted);">{f.label}</span>
                <span class="truncate">{f.value || '—'}</span>
                <span class="ml-auto flex items-center gap-1 shrink-0">
                  <button class="p-0.5" style="color: var(--q-muted);" title="编辑" onclick={() => startEditField(f)}><Pencil size={14} /></button>
                  <button class="p-0.5" style="color: var(--q-muted);" title="删除" onclick={() => removeField(f.id)}><Trash2 size={14} /></button>
                </span>
              </div>
            {/if}
          </li>
        {:else}
          <li class="text-sm" style="color: var(--q-muted);">还没有自定义字段</li>
        {/each}
      </ul>
      <div class="flex gap-2">
        <input bind:value={newField.label} placeholder="字段名（如 口味）" class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" onkeydown={(e) => e.key === 'Enter' && addField()} />
        <input bind:value={newField.value} placeholder="值" class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" onkeydown={(e) => e.key === 'Enter' && addField()} />
        <button class="flex items-center gap-1 px-3 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={addField}><Plus size={14} /> 添加</button>
      </div>
    </section>

    <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <h2 class="text-sm font-medium mb-3">关系</h2>
      <PersonRelEditor personId={id} />
    </section>

    {#if words.length > 0}
      <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <h2 class="text-sm font-medium mb-3">对话词云</h2>
        <div class="flex flex-wrap gap-2 items-baseline">
          {#each words.slice(0, 20) as w}
            <span style="color: var(--q-theme); font-size: {12 + Math.min(w.count, 6) * 2}px;">{w.word}</span>
          {/each}
        </div>
      </section>
    {/if}

    <section>
      <div class="flex items-center justify-between mb-3">
        <h2 class="text-sm font-medium">时间线</h2>
        <button class="flex items-center gap-1 text-xs px-2 py-1 rounded-md" style="background: var(--q-theme); color: white;"
                onclick={() => openEventForm()}><Plus size={13} /> 新建往来</button>
      </div>
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
                <div class="flex items-start justify-between gap-2">
                  <div class="text-sm">{t.title}</div>
                  {#if t.type === 'event'}
                    <button class="text-xs px-2 py-0.5 rounded shrink-0" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-muted);"
                            onclick={() => openEventForm(t.id)}>编辑</button>
                  {/if}
                </div>
                <div class="text-xs mt-0.5" style="color: var(--q-muted);">
                  {TIMELINE_LABEL[t.type] || t.type} · {new Date(t.date).toLocaleDateString()}
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

{#if showEdit && person}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={() => (showEdit = false)}></button>
    <div class="relative w-full max-w-lg max-h-[90vh] overflow-auto rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">编辑联系人</h2>
        <button onclick={() => (showEdit = false)}><X size={18} /></button>
      </div>
      <PersonForm {person} {categories} {tags} onsave={() => { showEdit = false; load() }} onrelchange={() => load()} oncancel={() => (showEdit = false)} />
    </div>
  </div>
{/if}

{#if showEventForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={closeEventForm}></button>
    <div class="relative w-full max-w-lg max-h-[90vh] overflow-auto rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">{eventEditId ? '编辑往来' : '新建往来'}</h2>
        <button onclick={closeEventForm}><X size={18} /></button>
      </div>
      <EventForm editId={eventEditId} preset={eventPreset}
                 onsave={afterEventForm} oncancel={closeEventForm} onremove={removeEvent} />
    </div>
  </div>
{/if}
