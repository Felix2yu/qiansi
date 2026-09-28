<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Person, type Category, type Tag } from '../lib/api'
  import PersonForm from '../lib/PersonForm.svelte'
  import { navigate } from '../lib/router'
  import { Search, Plus, Trash2, X, Upload, Download } from '@lucide/svelte'

  let { mode = 'list' }: { mode?: string } = $props()

  let list = $state<Person[]>([])
  let q = $state('')
  let categories = $state<Category[]>([])
  let tags = $state<Tag[]>([])
  let selectedCat = $state<number>(0)
  let selectedTag = $state<number>(0)
  let showForm = $state(false)
  let editing = $state<Person | null>(null)
  let importing = $state(false)
  let vcardInput: HTMLInputElement | undefined = $state()

  // 分页：一次取一页，避免数据量上来后被静默截断
  const PAGE = 60
  let page = $state(0)
  let hasMore = $state(false)

  async function load(reset = true) {
    if (reset) page = 0
    const batch = await API.get(
      `/api/v1/people?q=${encodeURIComponent(q)}&category_id=${selectedCat}&tag_id=${selectedTag}&limit=${PAGE}&offset=${page * PAGE}`
    ) as Person[]
    hasMore = batch.length === PAGE
    list = reset ? batch : [...list, ...batch]
  }
  async function loadMore() {
    page += 1
    await load(false)
  }
  async function init() {
    categories = await API.get('/api/v1/categories') as Category[]
    tags = await API.get('/api/v1/tags') as Tag[]
    await load()
    if (mode === 'new') { openCreate() }
  }
  onMount(init)

  function openCreate() {
    editing = null
    showForm = true
  }
  function openEdit(p: Person) {
    editing = p
    showForm = true
  }
  async function onSaved(saved: Person) {
    showForm = false
    await load()
    if (mode === 'new' && saved?.id) { navigate(`/people/${saved.id}`) }
  }
  async function remove(p: Person) {
    if (confirm(`删除联系人「${p.name}」及其所有关联记录？`)) {
      await API.delete(`/api/v1/people/${p.id}`); await load()
    }
  }

  async function importVCard(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    importing = true
    try {
      const text = await file.text()
      const res = await API.post<{ total: number; imported: number; updated: number; skipped: number }>('/api/v1/people/import/vcard', { text })
      const parts = [`成功导入 ${res.imported} 人`]
      if (res.updated > 0) parts.push(`更新姓名 ${res.updated} 人（与现有联系人按通讯录 ID 匹配）`)
      if (res.skipped > 0) parts.push(`跳过 ${res.skipped} 人（与现有联系人重复或缺少姓名）`)
      alert(`vCard 导入完成：${parts.join('，')}`)
      await load()
    } catch (err: any) {
      alert('导入失败：' + (err?.message || err))
    } finally {
      input.value = ''
      importing = false
    }
  }

  async function exportVCard() {
    try {
      const res = await fetch('/api/v1/people/export/vcard')
      if (!res.ok) {
        let msg = res.statusText
        try { msg = (await res.json()).error || msg } catch {}
        throw new Error(msg)
      }
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `qiansi-contacts-${new Date().toISOString().slice(0, 10)}.vcf`
      a.click()
      URL.revokeObjectURL(url)
    } catch (err: any) {
      alert('导出失败：' + (err?.message || err))
    }
  }
</script>

<div class="space-y-4">
  <header class="flex items-center justify-between gap-3">
    <div>
      <h1 class="text-2xl font-semibold">人物</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">管理你的联系人档案</p>
    </div>
    <div class="flex items-center gap-2">
      <input type="file" accept=".vcf,.vcard,text/vcard,text/x-vcard" class="hidden" bind:this={vcardInput} onchange={importVCard} />
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm disabled:opacity-50"
              style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);"
              disabled={importing} onclick={exportVCard}>
        <Download size={14} /> 导出 vCard
      </button>
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm disabled:opacity-50"
              style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);"
              disabled={importing} onclick={() => vcardInput?.click()}>
        <Upload size={14} /> {importing ? '导入中…' : '导入 vCard'}
      </button>
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={openCreate}>
        <Plus size={14} /> 新建
      </button>
    </div>
  </header>

  <div class="flex flex-wrap gap-2">
    <div class="relative flex-1 min-w-[200px]">
      <Search size={14} class="absolute left-3 top-1/2 -translate-y-1/2" style="color: var(--q-muted);" />
      <input class="w-full pl-9 pr-3 py-2 rounded-lg text-sm outline-none" placeholder="搜索…"
             style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);"
             bind:value={q} onkeydown={(e) => e.key === 'Enter' && load()} />
    </div>
    <select bind:value={selectedCat} onchange={() => load()} class="px-3 py-2 rounded-lg text-sm outline-none"
            style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);">
      <option value={0}>全部圈子</option>
      {#each categories as c}<option value={c.id}>{c.name}</option>{/each}
    </select>
    <select bind:value={selectedTag} onchange={() => load()} class="px-3 py-2 rounded-lg text-sm outline-none"
            style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);">
      <option value={0}>全部标签</option>
      {#each tags as t}<option value={t.id}>{t.name}</option>{/each}
    </select>
  </div>

  <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
    {#each list as p}
      <div class="rounded-xl p-4 transition hover:-translate-y-0.5"
           style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-start gap-3">
          <a href={`/people/${p.id}`} class="flex items-start gap-3 flex-1 min-w-0"
             onclick={(e) => { e.preventDefault(); navigate(`/people/${p.id}`) }}>
            <div class="w-11 h-11 rounded-full flex items-center justify-center text-white font-semibold shrink-0" style="background: var(--q-theme);">{p.name.slice(0,1)}</div>
            <div class="flex-1 min-w-0">
              <div class="font-medium truncate">{p.name}</div>
              <div class="text-xs mt-0.5" style="color: var(--q-muted);">
                Grade {'★'.repeat(p.grade)}{p.category_name && ` · ${p.category_name}`}
              </div>
              {#if p.notes}<div class="text-xs mt-1 line-clamp-2" style="color: var(--q-muted);">{p.notes}</div>{/if}
            </div>
          </a>
          <div class="flex gap-1">
            <button class="p-1 rounded hover:bg-black/5 dark:hover:bg-white/10" onclick={() => openEdit(p)} title="编辑">✎</button>
            <button class="p-1 rounded hover:bg-black/5 dark:hover:bg-white/10" onclick={() => remove(p)} title="删除"><Trash2 size={14} /></button>
          </div>
        </div>
      </div>
    {:else}
      <div class="col-span-full text-center py-12 text-sm" style="color: var(--q-muted);">暂无联系人，点击右上角新建，或导入 vCard 通讯录文件</div>
    {/each}
  </div>
  {#if hasMore}
    <div class="text-center">
      <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-muted);" onclick={loadMore}>
        加载更多（已显示 {list.length} 位）
      </button>
    </div>
  {/if}
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4">
    <button type="button" aria-label="关闭弹窗" class="absolute inset-0 cursor-default" style="background: rgba(0,0,0,0.3); border: 0;" onclick={() => (showForm = false)}></button>
    <div class="relative w-full max-w-lg max-h-[90vh] overflow-auto rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">{editing ? '编辑' : '新建'}联系人</h2>
        <button class="p-1 rounded" onclick={() => showForm = false}><X size={18} /></button>
      </div>
      <PersonForm person={editing} {categories} {tags} onsave={onSaved} oncancel={() => (showForm = false)} />
    </div>
  </div>
{/if}
