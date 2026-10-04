<script lang="ts">
  import { onMount } from 'svelte'
  import { API, type Category, type Tag, type EventType, type BackupItem, type BackupStatus } from '../lib/api'
  import TermList from '../lib/TermList.svelte'
  import { Download, Upload, Palette, Bell, Monitor, Sun, Moon, Clock, Check, AlertTriangle } from '@lucide/svelte'
  import { theme, setThemeMode, setThemeColor, initTheme, THEME_LABEL, type ThemeMode } from '../lib/theme.svelte'

  let settings = $state<Record<string, string>>({})
  let appriseUrls = $state('')
  let pushHour = $state(9)

  let categories = $state<Category[]>([])
  let tags = $state<Tag[]>([])
  let eventTypes = $state<EventType[]>([])
  let backups = $state<BackupItem[]>([])
  let restoreInput: HTMLInputElement | undefined = $state()
  let restoring = $state(false)
  let token = $state(localStorage.getItem('q_token') || '')

  const WEEKDAYS = [{ v: 0, n: '日' }, { v: 1, n: '一' }, { v: 2, n: '二' }, { v: 3, n: '三' },
                    { v: 4, n: '四' }, { v: 5, n: '五' }, { v: 6, n: '六' }]

  // 主题三档按钮，顺序固定：先"跟随系统"（默认），再具体模式
  const THEME_TABS: { mode: ThemeMode; icon: typeof Monitor }[] = [
    { mode: 'system', icon: Monitor },
    { mode: 'light', icon: Sun },
    { mode: 'dark', icon: Moon },
  ]

  // 自动备份
  let auto = $state<BackupStatus | null>(null)
  let autoSaving = $state(false)
  let autoRunning = $state(false)
  let autoMsg = $state('')

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
    await loadAuto()
  }

  async function loadAuto() {
    try {
      auto = await API.get('/api/v1/backup/auto') as BackupStatus
    } catch {
      auto = null
    }
  }

  onMount(() => { initTheme(); load() })


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

  function saveToken() {
    if (token.trim()) localStorage.setItem('q_token', token.trim())
    else localStorage.removeItem('q_token')
    alert('已保存访问令牌；若服务端未设置 QIANSI_TOKEN，请留空')
  }

  async function exportData() { window.location.href = '/api/v1/backup/export' }

  async function snapshot() {
    try {
      await API.post('/api/v1/backup/snapshot', {})
      backups = await API.get('/api/v1/backup/list') as BackupItem[]
      autoMsg = '已生成一份归档快照'
    } catch (err: any) {
      autoMsg = '生成快照失败：' + (err?.message || err)
    }
    setTimeout(() => autoMsg = '', 4000)
  }

  // ===== 自动备份 =====
  async function saveAuto() {
    if (!auto) return
    autoSaving = true
    try {
      auto = await API.put('/api/v1/backup/auto', auto.schedule) as BackupStatus
      autoMsg = '已保存自动备份设置'
    } catch (err: any) {
      autoMsg = '保存失败：' + (err?.message || err)
    } finally {
      autoSaving = false
      setTimeout(() => autoMsg = '', 4000)
    }
  }

  async function runAutoNow() {
    autoRunning = true
    try {
      const res = await API.post('/api/v1/backup/auto/run', {}) as { status: BackupStatus }
      auto = res.status
      backups = await API.get('/api/v1/backup/list') as BackupItem[]
      autoMsg = '已立即执行一次备份'
    } catch (err: any) {
      // 失败原因已由服务端落库，Status.last_error 会展示，这里只给即时提示
      autoMsg = '备份失败：' + (err?.message || err)
      await loadAuto()
    } finally {
      autoRunning = false
      setTimeout(() => autoMsg = '', 4000)
    }
  }

  // 把 RFC3339 转成"YYYY-MM-DD HH:MM"，直接用 toLocaleString 会被时区/locale 干扰
  function fmtTime(s: string): string {
    if (!s) return '—'
    const d = new Date(s)
    if (isNaN(d.getTime())) return '—'
    const p = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
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
    <div class="flex flex-wrap items-center gap-4">
      <!-- 外观三档：跟随系统 / 浅色 / 深色 -->
      <div class="flex items-center gap-0.5 rounded-lg p-0.5" style="background: var(--q-bg); border: 1px solid var(--q-border);">
        {#each THEME_TABS as t}
          <button onclick={() => setThemeMode(t.mode)} aria-pressed={theme.mode === t.mode}
                  class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs transition-colors"
                  style="color: {theme.mode === t.mode ? '#fff' : 'var(--q-muted)'}; background: {theme.mode === t.mode ? 'var(--q-theme)' : 'transparent'};">
            <t.icon size={13} />
            {THEME_LABEL[t.mode]}
          </button>
        {/each}
      </div>
      <span class="text-xs" style="color: var(--q-muted);">
        {theme.mode === 'system' ? '自动匹配系统外观，系统切换时实时跟随' : '已手动指定，不随系统变化'}
      </span>
      <div class="flex items-center gap-2 ml-auto">
        <span class="text-xs" style="color: var(--q-muted);">主题色</span>
        <input type="color" value={theme.color} oninput={(e) => setThemeColor(e.currentTarget.value)}
               aria-label="主题色" class="w-9 h-9 rounded-lg border cursor-pointer" style="border-color: var(--q-border);" />
        <span class="text-xs font-mono" style="color: var(--q-muted);">{theme.color}</span>
      </div>
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
      导出会生成一份一致性快照（不含未落盘的 WAL 残留）；恢复前会自动归档当前数据。
    </p>
    {#if autoMsg}
      <p class="text-xs mt-2 flex items-center gap-1" style="color: var(--q-muted);"><Check size={12} /> {autoMsg}</p>
    {/if}
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

  <!-- 定时自动备份 -->
  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-medium mb-3 flex items-center gap-2"><Clock size={14} /> 定时自动备份</h2>
    {#if auto}
      <div class="space-y-3">
        <!-- 开关 + 概览 -->
        <div class="flex flex-wrap items-center gap-3">
          <label class="flex items-center gap-2 text-sm cursor-pointer">
            <input type="checkbox" bind:checked={auto.schedule.enabled} class="w-4 h-4 accent-indigo-500" />
            {auto.schedule.enabled ? '已开启' : '已关闭'}
          </label>
          <span class="text-xs" style="color: var(--q-muted);">{auto.label}</span>
          <button class="ml-auto px-3 py-1.5 rounded-lg text-sm text-white disabled:opacity-50"
                  style="background: var(--q-theme);" disabled={autoRunning} onclick={runAutoNow}>
            {autoRunning ? '执行中…' : '立即执行一次'}
          </button>
        </div>

        <!-- 周期与时间点 -->
        <div class="flex flex-wrap items-center gap-3">
          <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
            周期
            <select bind:value={auto.schedule.frequency} aria-label="备份周期"
                    class="px-2 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
              <option value="daily">每日</option>
              <option value="weekly">每周</option>
              <option value="custom">自定义间隔</option>
            </select>
          </label>

          {#if auto.schedule.frequency === 'weekly'}
            <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
              星期
              <select bind:value={auto.schedule.weekday} aria-label="星期"
                      class="px-2 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
                {#each WEEKDAYS as w}
                  <option value={w.v}>周{w.n}</option>
                {/each}
              </select>
            </label>
          {/if}

          {#if auto.schedule.frequency === 'custom'}
            <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
              间隔（分钟）
              <input type="number" min={1} max={43200} bind:value={auto.schedule.every_min} aria-label="自定义间隔（分钟）"
                     class="w-24 px-2 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
            </label>
          {:else}
            <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
              时间
              <input type="time" bind:value={auto.schedule.at} aria-label="执行时间"
                     class="px-2 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
            </label>
          {/if}

          <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
            保留
            <input type="number" min={1} max={200} bind:value={auto.schedule.keep} aria-label="保留份数"
                   class="w-20 px-2 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
            份
          </label>

          <button class="px-3 py-1.5 rounded-lg text-sm text-white disabled:opacity-50"
                  style="background: var(--q-theme);" disabled={autoSaving} onclick={saveAuto}>
            {autoSaving ? '保存中…' : '保存'}
          </button>
        </div>

        <!-- 执行状态 -->
        <div class="text-xs space-y-1" style="color: var(--q-muted);">
          {#if auto.enabled && auto.next_run}
            <p>下次执行：<span class="font-mono">{fmtTime(auto.next_run)}</span>（{auto.next_run_in}）· {auto.next_hint}</p>
          {:else}
            <p>下次执行：已关闭</p>
          {/if}
          <p>上次执行：{auto.last_run ? fmtTime(auto.last_run) : '尚未执行'}</p>
          {#if auto.last_error}
            <p class="flex items-start gap-1" style="color: #ef4444;">
              <AlertTriangle size={12} class="mt-0.5 shrink-0" />
              <span>上次备份失败：{auto.last_error}</span>
            </p>
          {/if}
          <p>归档目录：<span class="font-mono">{auto.backups_dir}</span></p>
        </div>
      </div>
    {:else}
      <p class="text-xs" style="color: var(--q-muted);">读取自动备份配置失败，请检查服务是否运行。</p>
    {/if}
  </section>
</div>
