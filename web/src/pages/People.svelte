<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Person, type Category, type Tag } from '../lib/api'
  import { navigate } from '../lib/router'
  import { Search, Plus, Trash2, X } from '@lucide/svelte'

  let { mode = 'list' }: { mode?: string } = $props()

  let list = $state<Person[]>([])
  let q = $state('')
  let categories = $state<Category[]>([])
  let tags = $state<Tag[]>([])
  let selectedCat = $state<number>(0)
  let selectedTag = $state<number>(0)
  let showForm = $state(false)
  let editing = $state<Person | null>(null)

  let form = $state({ name: '', gender: '', grade: 3, birthday: '', birthday_is_lunar: false, phone: '', wechat: '', location: '', notes: '', category_id: 0 })

  async function load() {
    list = await API.get(`/api/v1/people?q=${encodeURIComponent(q)}&category_id=${selectedCat}&tag_id=${selectedTag}&limit=200`) as Person[]
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
    form = { name: '', gender: '', grade: 3, birthday: '', birthday_is_lunar: false, phone: '', wechat: '', location: '', notes: '', category_id: 0 }
    showForm = true
  }
  function openEdit(p: Person) {
    editing = p
    form = {
      name: p.name, gender: p.gender || '', grade: p.grade, birthday: p.birthday || '',
      birthday_is_lunar: !!p.birthday_is_lunar, phone: p.phone || '', wechat: p.wechat || '',
      location: p.location || '', notes: p.notes || '', category_id: p.category_id || 0,
    }
    showForm = true
  }
  async function submit() {
    if (!form.name.trim()) { alert('名字必填'); return }
    const body: any = { ...form }
    if (!body.category_id) delete body.category_id
    let saved: any
    if (editing) {
      saved = await API.put(`/api/v1/people/${editing.id}`, body)
    } else {
      saved = await API.post('/api/v1/people', body)
    }
    showForm = false; await load()
    if (mode === 'new' && saved?.id) { navigate(`/people/${saved.id}`) }
  }
  async function remove(p: Person) {
    if (confirm(`删除联系人「${p.name}」及其所有关联记录？`)) {
      await API.delete(`/api/v1/people/${p.id}`); await load()
    }
  }
</script>

<div class="space-y-4">
  <header class="flex items-center justify-between gap-3">
    <div>
      <h1 class="text-2xl font-semibold">人物</h1>
      <p class="text-sm mt-1" style="color: var(--q-muted);">管理你的联系人档案</p>
    </div>
    <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={openCreate}>
      <Plus size={14} /> 新建
    </button>
  </header>

  <div class="flex flex-wrap gap-2">
    <div class="relative flex-1 min-w-[200px]">
      <Search size={14} class="absolute left-3 top-1/2 -translate-y-1/2" style="color: var(--q-muted);" />
      <input class="w-full pl-9 pr-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);">
             placeholder="搜索…" bind:value={q} onkeydown={(e) => e.key === 'Enter' && load()} />
    </div>
    <select bind:value={selectedCat} onchange={load} class="px-3 py-2 rounded-lg text-sm outline-none"
            style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);">
      <option value={0}>全部圈子</option>
      {#each categories as c}<option value={c.id}>{c.name}</option>{/each}
    </select>
    <select bind:value={selectedTag} onchange={load} class="px-3 py-2 rounded-lg text-sm outline-none"
            style="background: var(--q-surface); border: 1px solid var(--q-border); color: var(--q-text);">
      <option value={0}>全部标签</option>
      {#each tags as t}<option value={t.id}>{t.name}</option>{/each}
    </select>
  </div>

  <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
    {#each list as p}
      <div class="rounded-xl p-4 cursor-pointer transition hover:-translate-y-0.5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
           onclick={() => navigate(`/people/${p.id}`)}>
        <div class="flex items-start gap-3">
          <div class="w-11 h-11 rounded-full flex items-center justify-center text-white font-semibold shrink-0" style="background: var(--q-theme);">{p.name.slice(0,1)}</div>
          <div class="flex-1 min-w-0">
            <div class="font-medium truncate">{p.name}</div>
            <div class="text-xs mt-0.5" style="color: var(--q-muted);">
              Grade {'★'.repeat(p.grade)}{p.category_name && ` · ${p.category_name}`}
            </div>
            {#if p.notes}<div class="text-xs mt-1 line-clamp-2" style="color: var(--q-muted);">{p.notes}</div>{/if}
          </div>
          <div class="flex gap-1">
            <button class="p-1 rounded hover:bg-black/5 dark:hover:bg-white/10" onclick={() => openEdit(p)} title="编辑">✎</button>
            <button class="p-1 rounded hover:bg-black/5 dark:hover:bg-white/10" onclick={() => remove(p)} title="删除"><Trash2 size={14} /></button>
          </div>
        </div>
      </div>
    {:else}
      <div class="col-span-full text-center py-12 text-sm" style="color: var(--q-muted);">暂无联系人，点击右上角新建</div>
    {/each}
  </div>
</div>

{#if showForm}
  <div class="fixed inset-0 z-40 flex items-center justify-center p-4" style="background: rgba(0,0,0,0.3);" onclick={() => showForm = false}>
    <div class="w-full max-w-lg rounded-2xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
      <div class="flex items-center justify-between mb-4">
        <h2 class="font-semibold">{editing ? '编辑' : '新建'}联系人</h2>
        <button class="p-1 rounded" onclick={() => showForm = false}><X size={18} /></button>
      </div>
      <div class="space-y-3">
        <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
               placeholder="姓名 *" bind:value={form.name} />
        <div class="grid grid-cols-2 gap-3">
          <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
                 placeholder="昵称" bind:value={form.nickname || ''} />
          <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
                 placeholder="性别" bind:value={form.gender} />
        </div>
        <div class="grid grid-cols-2 gap-3 items-center">
          <input type="date" class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" bind:value={form.birthday} />
          <label class="text-sm flex items-center gap-2" style="color: var(--q-muted);"><input type="checkbox" bind:checked={form.birthday_is_lunar} /> 农历</label>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
                 placeholder="电话" bind:value={form.phone} />
          <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
                 placeholder="微信" bind:value={form.wechat} />
        </div>
        <input class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
               placeholder="位置" bind:value={form.location} />
        <div>
          <div class="text-xs mb-1" style="color: var(--q-muted);">亲密度分级 1–5</div>
          <div class="flex gap-1">
            {#each [1,2,3,4,5] as g}
              <button class="w-8 h-8 rounded-md text-sm font-semibold transition"
                      style={form.grade >= g ? 'background: var(--q-theme); color: white;' : 'background: var(--q-bg); color: var(--q-muted); border: 1px solid var(--q-border);'}
                      onclick={() => form.grade = g}>{g}</button>
            {/each}
          </div>
        </div>
        <select bind:value={form.category_id} class="w-full px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
          <option value={0}>不选圈子</option>
          {#each categories as c}<option value={c.id}>{c.name}</option>{/each}
        </select>
        <textarea class="w-full px-3 py-2 rounded-lg text-sm outline-none min-h-[80px]" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
                  placeholder="备注、爱好、口味…" bind:value={form.notes}></textarea>
      </div>
      <div class="flex justify-end gap-2 mt-5">
        <button class="px-4 py-2 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" onclick={() => showForm = false}>取消</button>
        <button class="px-4 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={submit}>保存</button>
      </div>
    </div>
  </div>
{/if}
