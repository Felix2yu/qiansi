<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Category, type Tag, type EventType } from '../lib/api'
  import { Plus, Trash2, Download, Upload, Palette, Moon, Sun, Bell } from '@lucide/svelte'

  let settings = $state<Record<string, string>>({})
  let appriseUrls = $state('')
  let pushHour = $state(9)
  let theme = $state(localStorage.getItem('q_theme') || '#6366f1')
  let dark = $state(localStorage.getItem('q_dark') === '1')

  let categories = $state<Category[]>([])
  let tags = $state<Tag[]>([])
  let eventTypes = $state<EventType[]>([])
  let newCat = $state<Category>({ id: 0, name: '', color: '#6366f1', icon: 'circle', sort_order: 0 })
  let newTag = $state<Tag>({ id: 0, name: '', color: '#6366f1' })
  let newType = $state<EventType>({ id: 0, name: '', color: '#6366f1', icon: 'calendar', is_default: false, sort_order: 0 })

  async function load() {
    settings = await API.get('/api/v1/settings') as any
    appriseUrls = settings['apprise_urls'] || ''
    pushHour = parseInt(settings['push_time_hour'] || '9', 10)
    categories = await API.get('/api/v1/categories') as Category[]
    tags = await API.get('/api/v1/tags') as Tag[]
    eventTypes = await API.get('/api/v1/event-types') as EventType[]
  }
  onMount(load)

  async function saveNotify() {
    await API.post('/api/v1/settings/bulk', {
      apprise_urls: appriseUrls,
      push_time_hour: String(pushHour),
    })
    alert('已保存推送设置')
  }
  async function testNotify() {
    await API.post('/api/v1/settings/bulk', { apprise_urls: appriseUrls, push_time_hour: String(pushHour) })
    alert('已触发测试推送（若配置正确渠道，稍后会收到）')
  }

  function setTheme() {
    localStorage.setItem('q_theme', theme)
    document.documentElement.style.setProperty('--q-theme', theme)
  }
  function setDark() {
    localStorage.setItem('q_dark', dark ? '1' : '0')
    document.documentElement.classList.toggle('dark', dark)
  }
  // apply on mount

  async function addCategory() { if (!newCat.name.trim()) return
    await API.post('/api/v1/categories', newCat); newCat = { id: 0, name: '', color: '#6366f1', icon: 'circle', sort_order: 0 }; await load() }
  async function addTag() { if (!newTag.name.trim()) return
    await API.post('/api/v1/tags', newTag); newTag = { id: 0, name: '', color: '#6366f1' }; await load() }
  async function addType() { if (!newType.name.trim()) return
    await API.post('/api/v1/event-types', newType); newType = { id: 0, name: '', color: '#6366f1', icon: 'calendar', is_default: false, sort_order: 0 }; await load() }
  async function delCategory(id: number) { if (confirm('删除圈子？')) { await API.delete(`/api/v1/categories/${id}`); await load() } }
  async function delTag(id: number) { if (confirm('删除标签？')) { await API.delete(`/api/v1/tags/${id}`); await load() } }
  async function delType(id: number) { if (confirm('删除类型？')) { await API.delete(`/api/v1/event-types/${id}`); await load() } }

  async function exportData() { window.location.href = '/api/v1/backup/export' }
</script>
<div class="space-y-6">
  <header><h1 class="text-2xl font-semibold">设置</h1><p class="text-sm mt-1" style="color: var(--q-muted);">偏好、数据、通知渠道</p></header>

  <!-- 主题 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3 flex items-center gap-2"><Palette size={14} /> 主题</h2>
    <div class="flex items-center gap-4">
      <input type="color" bind:value={theme} oninput={setTheme} class="w-10 h-10 rounded-lg border cursor-pointer" />
      <span class="text-xs" style="color: var(--q-muted);">{theme}</span>
      <button class="p-2 rounded-lg border" style="border-color: var(--q-border);" onclick={() => { dark = !dark; setDark() }}>
        {#if dark}<Sun size={16} />{:else}<Moon size={16} />{/if}
      </button>
      <span class="text-xs" style="color: var(--q-muted);">{dark ? '暗色' : '亮色'}</span>
    </div>
  </section>

  <!-- 圈子 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">圈子（分组）</h2>
    <ul class="space-y-1 mb-3">
      {#each categories as c}
        <li class="flex items-center gap-2 text-sm px-2 py-1 rounded-md" style="background: var(--q-bg);">
          <span class="w-3 h-3 rounded-full" style="background: {c.color};"></span>{c.name}
          <button class="ml-auto text-xs" style="color: var(--q-muted);" onclick={() => delCategory(c.id)}><Trash2 size={14} /></button>
        </li>
      {/each}
    </ul>
    <div class="flex gap-2">
      <input bind:value={newCat.name} class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="圈子名" />
      <input type="color" bind:value={newCat.color} class="w-10 h-10 rounded-lg border" />
      <button class="flex items-center gap-1 px-3 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={addCategory}><Plus size={14} /> 添加</button>
    </div>
  </section>

  <!-- 标签 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">标签</h2>
    <ul class="flex flex-wrap gap-2 mb-3">
      {#each tags as t}
        <li class="flex items-center gap-1 text-xs px-2 py-1 rounded-full" style="background: color-mix(in srgb, {t.color} 15%, transparent); color: {t.color};">
          {t.name} <button onclick={() => delTag(t.id)}><Trash2 size={12} /></button>
        </li>
      {/each}
    </ul>
    <div class="flex gap-2">
      <input bind:value={newTag.name} class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="标签名" />
      <input type="color" bind:value={newTag.color} class="w-10 h-10 rounded-lg border" />
      <button class="flex items-center gap-1 px-3 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={addTag}><Plus size={14} /> 添加</button>
    </div>
  </section>

  <!-- 事件类型 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">往来事件类型</h2>
    <ul class="space-y-1 mb-3">
      {#each eventTypes as e}
        <li class="flex items-center gap-2 text-sm px-2 py-1 rounded-md" style="background: var(--q-bg);">
          <span class="w-3 h-3 rounded-full" style="background: {e.color};"></span>{e.name}
          <button class="ml-auto text-xs" style="color: var(--q-muted);" onclick={() => delType(e.id)}><Trash2 size={14} /></button>
        </li>
      {/each}
    </ul>
    <div class="flex gap-2">
      <input bind:value={newType.name} class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" placeholder="类型名" />
      <input type="color" bind:value={newType.color} class="w-10 h-10 rounded-lg border" />
      <button class="flex items-center gap-1 px-3 py-2 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={addType}><Plus size={14} /> 添加</button>
    </div>
  </section>

  <!-- 通知 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3 flex items-center gap-2"><Bell size={14} /> 多渠道通知（apprise URL）</h2>
    <textarea bind:value={appriseUrls} class="w-full h-28 px-3 py-2 rounded-lg text-sm outline-none font-mono" style="background: var(--q-bg); border: 1px solid var(--q-border);"
      placeholder='每行一个 URL，如：&#10;bark://host/key&#10;feishu://...&#10;tgram://token/chat'></textarea>
    <div class="flex items-center gap-3 mt-3">
      <label class="text-sm" style="color: var(--q-muted);">每日推送时间</label>
      <input type="number" min={0} max={23} bind:value={pushHour} class="w-20 px-3 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
      <button class="px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={saveNotify}>保存</button>
      <button class="px-3 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={testNotify}>测试推送</button>
    </div>
    <p class="text-xs mt-2" style="color: var(--q-muted);">
      支持 bark、feishu、telegram、discord、smtp 等 100+ 渠道；一行一个 URL，逗号或换行分隔。
    </p>
  </section>

  <!-- 数据管理 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">数据备份</h2>
    <div class="flex gap-2">
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={exportData}><Download size={14} /> 导出数据库</button>
    </div>
    <p class="text-xs mt-2" style="color: var(--q-muted);">
      Docker 部署时，实际数据位于 data 挂载目录；可直接拷贝 qiansi.db + uploads/ + backups/。
    </p>
  </section>
</div>
