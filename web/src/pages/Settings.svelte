<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Category, type Tag, type EventType, type BackupItem } from '../lib/api'
  import TermList from '../lib/TermList.svelte'
  import { Download, Upload, Palette, Moon, Sun, Bell } from '@lucide/svelte'

  let settings = $state<Record<string, string>>({})
  let appriseUrls = $state('')
  let pushHour = $state(9)
  let theme = $state(localStorage.getItem('q_theme') || '#6366f1')
  let dark = $state(localStorage.getItem('q_dark') === '1')

  let categories = $state<Category[]>([])
  let tags = $state<Tag[]>([])
  let eventTypes = $state<EventType[]>([])
  let backups = $state<BackupItem[]>([])
  let restoreInput: HTMLInputElement | undefined = $state()
  let restoring = $state(false)
  let token = $state(localStorage.getItem('q_token') || '')

  async function load() {
    settings = await API.get('/api/v1/settings') as any
    appriseUrls = settings['apprise_urls'] || ''
    pushHour = parseInt(settings['push_time_hour'] || '9', 10)
    ;[categories, tags, eventTypes, backups] = await Promise.all([
      API.get('/api/v1/categories') as Promise<Category[]>,
      API.get('/api/v1/tags') as Promise<Tag[]>,
      API.get('/api/v1/event-types') as Promise<EventType[]>,
      API.get('/api/v1/backup/list') as Promise<BackupItem[]>,
    ])
  }
  onMount(load)

  async function saveNotify() {
    await API.post('/api/v1/settings/bulk', {
      apprise_urls: appriseUrls,
      push_time_hour: String(pushHour),
    })
    alert('已保存推送设置')
  }
  // 之前的「测试推送」只保存了配置却提示已触发，属于误导
  async function testNotify() {
    await API.post('/api/v1/settings/bulk', { apprise_urls: appriseUrls, push_time_hour: String(pushHour) })
    try {
      await API.post('/api/v1/notify/test', {})
      alert('已发送测试推送，请检查对应渠道')
    } catch (err: any) {
      alert('测试推送失败：' + (err?.message || err))
    }
  }

  function setTheme() {
    localStorage.setItem('q_theme', theme)
    document.documentElement.style.setProperty('--q-theme', theme)
  }
  function setDark() {
    localStorage.setItem('q_dark', dark ? '1' : '0')
    document.documentElement.classList.toggle('dark', dark)
  }
  function applySaved() {
    document.documentElement.style.setProperty('--q-theme', theme)
    document.documentElement.classList.toggle('dark', dark)
  }
  // 保存的设置要在刷新后仍然生效
  onMount(applySaved)

  function saveToken() {
    if (token.trim()) localStorage.setItem('q_token', token.trim())
    else localStorage.removeItem('q_token')
    alert('已保存访问令牌；若服务端未设置 QIANSI_TOKEN，请留空')
  }

  async function exportData() { window.location.href = '/api/v1/backup/export' }

  async function snapshot() {
    await API.post('/api/v1/backup/snapshot', {})
    backups = await API.get('/api/v1/backup/list') as BackupItem[]
    alert('已生成一份归档快照')
  }

  async function restoreData(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    if (!confirm('恢复会用上传的数据库覆盖当前数据，当前数据会先自动归档一份。确定继续？')) {
      input.value = ''
      return
    }
    restoring = true
    try {
      const fd = new FormData()
      fd.append('file', file)
      const res = await fetch('/api/v1/backup/restore', {
        method: 'POST',
        headers: (localStorage.getItem('q_token') || '') ? { Authorization: 'Bearer ' + localStorage.getItem('q_token') } : undefined,
        body: fd,
      })
      if (!res.ok) {
        let msg = res.statusText
        try { msg = (await res.json()).error || msg } catch {}
        throw new Error(msg)
      }
      alert('恢复完成，即将重新加载页面')
      location.reload()
    } catch (err: any) {
      alert('恢复失败：' + (err?.message || err))
    } finally {
      input.value = ''
      restoring = false
    }
  }
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
    <TermList items={categories} endpoint="/api/v1/categories" noun="圈子" placeholder="圈子名"
              defaults={{ icon: 'circle', sort_order: 0 }} onchange={load} />
  </section>

  <!-- 标签 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">标签</h2>
    <TermList items={tags} endpoint="/api/v1/tags" noun="标签" placeholder="标签名" pill onchange={load} />
  </section>

  <!-- 事件类型 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">往来事件类型</h2>
    <TermList items={eventTypes} endpoint="/api/v1/event-types" noun="类型" placeholder="类型名"
              defaults={{ icon: 'calendar', is_default: false, sort_order: 0 }} onchange={load} />
  </section>

  <!-- 通知 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3 flex items-center gap-2"><Bell size={14} /> 多渠道通知（apprise URL）</h2>
    <textarea bind:value={appriseUrls} class="w-full h-28 px-3 py-2 rounded-lg text-sm outline-none font-mono" style="background: var(--q-bg); border: 1px solid var(--q-border);"
      placeholder='每行一个 URL，如：&#10;bark://host/key&#10;feishu://...&#10;tgram://token/chat'></textarea>
    <div class="flex items-center gap-3 mt-3">
      <span class="text-sm" style="color: var(--q-muted);">每日推送时间</span>
      <input type="number" min={0} max={23} bind:value={pushHour} aria-label="每日推送时间（0-23 时）" class="w-20 px-3 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
      <button class="px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={saveNotify}>保存</button>
      <button class="px-3 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={testNotify}>测试推送</button>
    </div>
    <p class="text-xs mt-2" style="color: var(--q-muted);">
      支持 bark、feishu、telegram、discord、smtp 等 100+ 渠道；一行一个 URL，逗号或换行分隔。
    </p>
  </section>

  <!-- 访问令牌 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">访问令牌（可选）</h2>
    <div class="flex gap-2">
      <input bind:value={token} type="password" placeholder="服务端 QIANSI_TOKEN，未设置则留空"
             class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
      <button class="px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={saveToken}>保存</button>
    </div>
    <p class="text-xs mt-2" style="color: var(--q-muted);">
      服务端设置 QIANSI_TOKEN 后，所有接口都需要此令牌；跨站访问默认已被 CORS 拒绝。
    </p>
  </section>

  <!-- 数据管理 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3">数据备份</h2>
    <div class="flex flex-wrap gap-2">
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={exportData}><Download size={14} /> 导出数据库</button>
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={snapshot}>生成归档快照</button>
      <input type="file" accept=".db,.sqlite,.sqlite3" class="hidden" bind:this={restoreInput} onchange={restoreData} />
      <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm disabled:opacity-50"
              style="background: var(--q-bg); border: 1px solid var(--q-border); color: #ef4444;"
              disabled={restoring} onclick={() => restoreInput?.click()}>
        <Upload size={14} /> {restoring ? '恢复中…' : '从备份恢复'}
      </button>
    </div>
    <p class="text-xs mt-2" style="color: var(--q-muted);">
      导出会生成一份一致性快照（不含未落盘的 WAL 残留）；系统每日 04:00 自动归档一份，最多保留 7 份。恢复前会自动归档当前数据。
    </p>
    {#if backups.length > 0}
      <ul class="mt-3 space-y-1">
        {#each backups as b}
          <li class="flex items-center gap-2 text-xs px-2 py-1 rounded" style="background: var(--q-bg); color: var(--q-muted);">
            <span class="truncate">{b.name}</span>
            <span class="ml-auto shrink-0">{(b.size / 1024).toFixed(0)} KB · {b.time}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
</div>
