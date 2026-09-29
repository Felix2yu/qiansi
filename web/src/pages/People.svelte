<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Person, type Category, type Tag } from '../lib/api'
  import PersonForm from '../lib/PersonForm.svelte'
  import { navigate } from '../lib/router'
  import { Search, Plus, Trash2, X, Upload, Download, Undo2 } from '@lucide/svelte'

  let { mode = 'list' }: { mode?: string } = $props()

  let list = $state<Person[]>([])
  let q = $state('')
  let categories = $state<Category[]>([])
  let tags = $state<Tag[]>([])
  let selectedCat = $state<number>(0)
  let selectedTag = $state<number>(0)
  // 归档的人默认不在列表里，靠这个筛选翻出来找回
  let onlyArchived = $state(false)
  function toggleArchived() {
    onlyArchived = !onlyArchived
    selectMode = false
    selectedIds = []
    load()
  }
  function tabStyle(active: boolean) {
    return active
      ? 'background: var(--q-theme); color: #fff;'
      : 'color: var(--q-muted);'
  }
  let showForm = $state(false)
  let editing = $state<Person | null>(null)
  let importing = $state(false)
  let vcardInput: HTMLInputElement | undefined = $state()

  // 批量选择态
  let selectMode = $state(false)
  let selectedIds = $state<string[]>([])
  function toggleSelect(id: string) {
    selectedIds = selectedIds.includes(id) ? selectedIds.filter((x) => x !== id) : [...selectedIds, id]
  }
  function allVisibleSelected() {
    return list.length > 0 && list.every((p) => selectedIds.includes(p.id))
  }
  function toggleAll() {
    if (allVisibleSelected()) {
      selectedIds = []
    } else {
      selectedIds = [...new Set([...selectedIds, ...list.map((p) => p.id)])]
    }
  }
  function toggleSelectMode() {
    selectMode = !selectMode
    if (!selectMode) selectedIds = []
  }
  // 批量把选中的人加进一个圈子（只增不减，移出仍回单人编辑弹窗）
  let bulkCat = $state(0)
  async function addSelectedToCircle() {
    if (selectedIds.length === 0 || bulkCat === 0) return
    const name = categories.find(c => c.id === bulkCat)?.name || ''
    try {
      const res = await API.post<{ added: number }>('/api/v1/people/bulk-categories', {
        ids: selectedIds, category_ids: [bulkCat],
      })
      bulkCat = 0
      await load()
      alert(res.added === selectedIds.length
        ? `已把 ${res.added} 位加入「${name}」`
        : `${selectedIds.length} 位里有 ${res.added} 位新加入「${name}」，其余原本就在圈子里`)
    } catch (err: any) {
      alert('加入圈子失败：' + (err?.message || err))
    }
  }
  async function deleteSelected() {
    if (selectedIds.length === 0) return
    if (!confirm(`确定删除选中的 ${selectedIds.length} 位联系人及其所有关联记录（往来/对话/记账/纪念日）？此操作不可恢复。`)) return
    try {
      const res = await API.delete<{ deleted: number }>('/api/v1/people', { ids: selectedIds })
      selectedIds = []
      await load()
      alert(`已删除 ${res.deleted} 位联系人`)
    } catch (err: any) {
      alert('删除失败：' + (err?.message || err))
    }
  }
  async function clearAll() {
    if (!confirm('确定清空全部联系人吗？此操作不可恢复，会一并删除所有往来、对话、记账与纪念日等关联数据。\n建议先点「导出 vCard」备份，再清空后重新导入。')) return
    try {
      const res = await API.delete<{ deleted: number }>('/api/v1/people', { ids: [] })
      selectedIds = []
      await load()
      alert(`已清空 ${res.deleted} 位联系人`)
    } catch (err: any) {
      alert('清空失败：' + (err?.message || err))
    }
  }

  // 分页：一次取一页，避免数据量上来后被静默截断
  const PAGE = 60
  let page = $state(0)
  let hasMore = $state(false)

  async function load(reset = true) {
    if (reset) page = 0
    const archived = onlyArchived ? '&archived=only' : ''
    const batch = await API.get(
      `/api/v1/people?q=${encodeURIComponent(q)}${archived}&category_id=${selectedCat}&tag_id=${selectedTag}&limit=${PAGE}&offset=${page * PAGE}`
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
      selectedIds = selectedIds.filter((id) => id !== p.id)
    }
  }
  async function unarchive(p: Person) {
    await API.delete(`/api/v1/people/${p.id}/archive`)
    await load()
    selectedIds = selectedIds.filter((id) => id !== p.id)
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
    <div class="min-w-0">
      <h1 class="text-2xl font-semibold">人物</h1>
      <p class="text-sm mt-1 truncate" style="color: var(--q-muted);">{onlyArchived ? '归档只是隐藏，取消归档即可找回' : '管理你的联系人档案'}</p>
    </div>
    <div class="flex items-center gap-2 shrink-0">
      <input type="file" accept=".vcf,.vcard,text/vcard,text/x-vcard" class="hidden" bind:this={vcardInput} onchange={importVCard} />
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm whitespace-nowrap disabled:opacity-50"
              style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);"
              disabled={importing} onclick={exportVCard}>
        <Download size={14} /> 导出 vCard
      </button>
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm whitespace-nowrap disabled:opacity-50"
              style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);"
              disabled={importing} onclick={() => vcardInput?.click()}>
        <Upload size={14} /> {importing ? '导入中…' : '导入 vCard'}
      </button>
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm whitespace-nowrap disabled:opacity-50"
              style={selectMode ? 'background: var(--q-theme); border: 1px solid var(--q-theme); color: #fff;' : 'background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);'}
              onclick={toggleSelectMode}>
        {selectMode ? '退出选择' : '批量'}
      </button>
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm whitespace-nowrap text-white" style="background: var(--q-theme);" onclick={openCreate}>
        <Plus size={14} /> 新建
      </button>
    </div>
  </header>

  <div class="flex flex-wrap gap-2">
    <div class="flex rounded-lg p-0.5 shrink-0" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <button class="px-3 py-1.5 rounded-md text-sm whitespace-nowrap"
              style={tabStyle(!onlyArchived)} onclick={() => { if (onlyArchived) toggleArchived() }}>联系人</button>
      <button class="px-3 py-1.5 rounded-md text-sm whitespace-nowrap"
              style={tabStyle(onlyArchived)} onclick={() => { if (!onlyArchived) toggleArchived() }}>已归档</button>
    </div>
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

  {#if selectMode}
    <div class="flex flex-wrap items-center gap-2 rounded-lg px-3 py-2" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <label class="flex items-center gap-1.5 text-sm cursor-pointer select-none" style="color: var(--q-text);">
        <input type="checkbox" checked={allVisibleSelected()} onchange={toggleAll} />
        全选本页
      </label>
      <span class="text-sm" style="color: var(--q-muted);">已选 {selectedIds.length} 位</span>
      <div class="flex-1"></div>
      <select bind:value={bulkCat} class="px-3 py-1.5 rounded-lg text-sm outline-none"
              style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);"
              disabled={selectedIds.length === 0}>
        <option value={0}>加入圈子…</option>
        {#each categories as c}<option value={c.id}>{c.name}</option>{/each}
      </select>
      <button class="px-3 py-1.5 rounded-lg text-sm" style="background: var(--q-theme); color: #fff;" disabled={selectedIds.length === 0 || bulkCat === 0} onclick={addSelectedToCircle}>
        加入
      </button>
      <button class="px-3 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" disabled={selectedIds.length === 0} onclick={deleteSelected}>
        删除选中
      </button>
      <button class="px-3 py-1.5 rounded-lg text-sm text-white" style="background: #dc2626;" onclick={clearAll}>
        清空全部
      </button>
    </div>
  {/if}

  <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
    {#each list as p}
      <div class="rounded-xl p-4 transition hover:-translate-y-0.5"
           style="background: var(--q-surface); border: 1px solid var(--q-border);">
        <div class="flex items-start gap-3">
          {#if selectMode}
            <input type="checkbox" class="mt-2.5 shrink-0" checked={selectedIds.includes(p.id)} onchange={() => toggleSelect(p.id)} />
          {/if}
          <a href={`/people/${p.id}`} class="flex items-start gap-3 flex-1 min-w-0"
             onclick={(e) => { e.preventDefault(); if (selectMode) { toggleSelect(p.id) } else { navigate(`/people/${p.id}`) } }}>
            <div class="w-11 h-11 rounded-full flex items-center justify-center text-white font-semibold shrink-0" style="background: var(--q-theme);">{p.name.slice(0,1)}</div>
            <div class="flex-1 min-w-0">
              <div class="font-medium truncate">{p.name}</div>
              {#if p.grade > 0 || (p.categories?.length ?? 0) > 0}
                <div class="flex flex-wrap items-center gap-1 mt-0.5 text-xs" style="color: var(--q-muted);">
                  {#if p.grade > 0}<span class="shrink-0">{'♥'.repeat(p.grade)}</span>{/if}
                  {#each p.categories ?? [] as c}
                    <span class="px-1.5 py-0.5 rounded-full whitespace-nowrap"
                          style={`background: color-mix(in srgb, ${c.color} 18%, transparent); color: ${c.color};`}>{c.name}</span>
                  {/each}
                </div>
              {/if}
              {#if p.notes}<div class="text-xs mt-1 line-clamp-2" style="color: var(--q-muted);">{p.notes}</div>{/if}
            </div>
          </a>
          <div class="flex gap-1">
            {#if onlyArchived}
              <button class="p-1 rounded hover:bg-black/5 dark:hover:bg-white/10" onclick={() => unarchive(p)} title="取消归档"><Undo2 size={14} /></button>
            {/if}
            <button class="p-1 rounded hover:bg-black/5 dark:hover:bg-white/10" onclick={() => openEdit(p)} title="编辑">✎</button>
            <button class="p-1 rounded hover:bg-black/5 dark:hover:bg-white/10" onclick={() => remove(p)} title="删除"><Trash2 size={14} /></button>
          </div>
        </div>
      </div>
    {:else}
      <div class="col-span-full text-center py-12 text-sm" style="color: var(--q-muted);">
        {#if onlyArchived}没有已归档的联系人{:else}暂无联系人，点击右上角新建，或导入 vCard 通讯录文件{/if}
      </div>
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
      <PersonForm person={editing} {categories} {tags} onsave={onSaved} onrelchange={() => load()} oncancel={() => (showForm = false)} />
    </div>
  </div>
{/if}
