<script lang="ts">
  import { onMount } from 'svelte'
  import { navigate } from '../lib/router'
  import { API, gradeLabel, type BackupItem, type BackupStatus, type RhythmTier } from '../lib/api'
  import TermList from '../lib/TermList.svelte'
  import { Download, Upload, Bell, Monitor, Sun, Moon, Check, AlertTriangle, SlidersHorizontal, Users, Database, KeyRound } from '@lucide/svelte'
  import { theme, setThemeMode, setThemeColor, initTheme, isPreset, THEME_LABEL, THEME_PRESETS, type ThemeMode } from '../lib/theme.svelte'
  import PersonPicker from '../lib/PersonPicker.svelte'
  import { setSelf } from '../lib/self.svelte'
  import { dict, ensure, refresh, refreshAll } from '../lib/dict.svelte'
  import { toast } from '../lib/toast.svelte'
  import { ask } from '../lib/ask.svelte'

  let settings = $state<Record<string, string>>({})
  let appriseUrls = $state('')
  let pushHour = $state(9)

  // 这三个字典在本页编辑，改完必须显式刷新，否则别的页面还是旧名单
  const categories = $derived(dict.categories)
  const tags = $derived(dict.tags)
  const eventTypes = $derived(dict.eventTypes)
  let backups = $state<BackupItem[]>([])
  let restoreInput: HTMLInputElement | undefined = $state()
  let restoring = $state(false)
  let token = $state(localStorage.getItem('q_token') || '')

  // 「我是谁」只是 settings 里一个指向 people 的指针，但关系图拼色、选人置顶、
  // 认识路径全挂在它身上，所以本页必须能设它——以前只能进某个人的详情点一个无文字图标。
  let selfId = $state('')
  let selfSaved = $state('')
  let selfSaving = $state(false)
  const selfDirty = $derived(selfId !== selfSaved)

  async function saveSelf() {
    selfSaving = true
    try {
      await API.post('/api/v1/settings/bulk', { self_person_id: selfId })
    } catch (err) {
      toast.fail('保存本人失败', err)
      selfSaving = false
      return
    }
    selfSaving = false
    selfSaved = selfId
    settings = { ...settings, self_person_id: selfId }
    // 本页改了指针，别的页面上的下拉与图谱读的是共享状态，必须一并更新
    setSelf(selfId)
    toast.ok(selfId ? '已把 TA 设为你自己' : '已取消本人')
  }

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

  // 联系节奏（六档天数）：和自动备份的 schedule 一样，是这台机器上的偏好，
  // 后端存成 settings 里的一个 JSON 键，GET 永远回齐六档。
  let rhythm = $state<RhythmTier[]>([])
  let rhythmSaving = $state(false)
  const RHYTHM_MAX = 3650

  // 总开关就是 settings 里的一个键（后端 ContactUpcoming 认它），所以不另开端点。
  // 只有显式写过关闭值才算关：没配过的老库默认是开着。
  let rhythmOn = $state(true)
  let rhythmToggling = $state(false)

  async function toggleRhythm(on: boolean) {
    rhythmToggling = true
    try {
      await API.post('/api/v1/settings/bulk', { contact_rhythm_enabled: on ? '1' : '0' })
    } catch (err) {
      // 写失败就把勾退回原样：屏幕上的开关必须和后端一致，不能看着关了其实还在催
      rhythmOn = !on
      rhythmToggling = false
      toast.fail(on ? '开启联系节奏失败' : '关闭联系节奏失败', err)
      return
    }
    rhythmToggling = false
    settings = { ...settings, contact_rhythm_enabled: on ? '1' : '0' }
    toast.ok(on ? '已开启联系节奏提醒' : '已关闭：不再派生待办，也不进每日推送')
  }

  async function loadRhythm() {
    const r = await API.get<RhythmTier[]>('/api/v1/contact-rhythm').catch((err) => {
      toast.fail('读取联系节奏失败', err)
      return null
    })
    if (r) rhythm = r
  }

  // 存完用服务端的回读覆盖本地：它才是归一化后的那份，六个数字要对得上
  async function saveRhythm() {
    rhythmSaving = true
    try {
      rhythm = await API.put<RhythmTier[]>('/api/v1/contact-rhythm', rhythm)
      toast.ok('已保存联系节奏')
    } catch (err) {
      toast.fail('保存联系节奏失败', err)
    } finally {
      rhythmSaving = false
    }
  }

  async function resetRhythm() {
    if (!(await ask({
      title: '恢复默认联系节奏？',
      detail: '六档天数会退回 7 / 7 / 30 / 30 / 90 / 90 天。',
      confirmLabel: '恢复默认',
    }))) return
    rhythmSaving = true
    try {
      rhythm = await API.delete<RhythmTier[]>('/api/v1/contact-rhythm')
      toast.ok('已恢复默认节奏')
    } catch (err) {
      toast.fail('恢复默认节奏失败', err)
    } finally {
      rhythmSaving = false
    }
  }

  async function load() {
    // 读失败就把屏幕上已有的值留着：填成空再点保存会覆盖掉服务端的真配置
    const s = await API.get<Record<string, string>>('/api/v1/settings').catch((err) => {
      toast.fail('读取设置失败', err)
      return null
    })
    if (s) {
      settings = s
      appriseUrls = s['apprise_urls'] || ''
      pushHour = parseInt(s['push_time_hour'] || '9', 10)
      selfId = s['self_person_id'] || ''
      selfSaved = selfId
      rhythmOn = !['0', 'false', 'off', 'no'].includes((s['contact_rhythm_enabled'] || '').trim().toLowerCase())
    }
    const b = await API.get<BackupItem[]>('/api/v1/backup/list').catch((err) => {
      toast.fail('读取备份列表失败', err)
      return null
    })
    if (b) backups = b
    await Promise.all([loadAuto(), loadRhythm()])
  }

  async function loadAuto() {
    try {
      auto = await API.get('/api/v1/backup/auto') as BackupStatus
    } catch {
      auto = null
    }
  }

  onMount(() => { initTheme(); ensure('categories'); ensure('tags'); ensure('eventTypes'); ensure('events'); load() })


  async function saveNotify() {
    try {
      await API.post('/api/v1/settings/bulk', {
        apprise_urls: appriseUrls,
        push_time_hour: String(pushHour),
      })
    } catch (err) {
      toast.fail('保存推送设置失败', err)
      return
    }
    toast.ok('已保存推送设置')
  }
  // 之前的「测试推送」只保存了配置却提示已触发，属于误导
  async function testNotify() {
    try {
      await API.post('/api/v1/settings/bulk', { apprise_urls: appriseUrls, push_time_hour: String(pushHour) })
    } catch (err) {
      toast.fail('保存推送设置失败', err)
      return
    }
    try {
      await API.post('/api/v1/notify/test', {})
      toast.ok('已发送测试推送，请检查对应渠道')
    } catch (err: any) {
      toast.fail('测试推送失败', err)
    }
  }

  async function saveToken() {
    const t = token.trim()
    if (t) localStorage.setItem('q_token', t)
    else localStorage.removeItem('q_token')
    // 头像和下载链接靠会话 cookie 才认令牌：存完当场换一枚，否则要刷新页面才生效
    const ok = await API.ensureSession()
    if (ok) toast.ok('已保存访问令牌；若服务端未设置 QIANSI_TOKEN，请留空')
    else toast.error('令牌已保存，但服务端不认它，头像与导出仍会失败')
  }

  async function exportData() {
    try {
      await API.download('/api/v1/backup/export')
    } catch (err: any) {
      toast.fail('导出失败', err)
    }
  }

  // ===== 明细 CSV 与全量 JSON（N3）=====
  const eventOptions = $derived(dict.events)
  let csvWhat = $state<'events' | 'transactions' | 'memos'>('events')
  let csvYear = $state<number | ''>('')
  let csvPerson = $state('')
  let csvEvent = $state('')
  let ioMsg = $state('')
  let jsonInput: HTMLInputElement | undefined = $state()

  function say(msg: string) {
    // 失败都交给 toast；这条留在原地，是导入结果里需要看清的明细数字
    ioMsg = msg
    setTimeout(() => ioMsg = '', 6000)
  }

  async function exportCsv() {
    const q = new URLSearchParams({ what: csvWhat })
    if (csvYear) q.set('year', String(csvYear))
    if (csvPerson) q.set('person_id', csvPerson)
    // 事件过滤只对金钱明细有意义（礼单对账）
    if (csvEvent && csvWhat === 'transactions') q.set('event_id', csvEvent)
    try {
      await API.download('/api/v1/export/csv?' + q.toString())
    } catch (err: any) {
      toast.fail('导出失败', err)
    }
  }

  async function exportJson() {
    try {
      await API.download('/api/v1/export/json')
    } catch (err: any) {
      toast.fail('全量导出失败', err)
    }
  }

  async function pickJson(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file) return
    let parsed: any
    try {
      parsed = JSON.parse(await file.text())
    } catch {
      toast.error('这个文件读不出 JSON，确认选的是「全量导出」下来的文件')
      return
    }
    // 先在本机看一眼再上传：选错文件不该让人等一趟几十 MB 的往返
    if (parsed?.app !== 'qiansi') {
      toast.error('这不是牵丝的全量导出文件')
      return
    }
    if (!(await ask({
      title: '用这份 JSON 覆盖当前全部数据',
      detail: `文件导出于 ${fmtTime(parsed.exported_at || '')}。导入前会自动归档一份当前数据库到 backups/，出问题可以从那里捞回来。附件的图片文件不在 JSON 里，需要另外拷贝 uploads 目录。`,
      danger: true,
      confirmLabel: '覆盖导入',
    }))) return
    try {
      const res = await API.post<{ tables: number; rows: number }>('/api/v1/import/json', parsed)
      say(`已导入 ${res.rows} 行，覆盖 ${res.tables} 张表`)
      await Promise.all([load(), refreshAll()])
    } catch (err: any) {
      toast.fail('导入失败', err)
    }
  }

  async function snapshot() {
    try {
      await API.post('/api/v1/backup/snapshot', {})
      // 列表刷新失败只说明「没看到新条目」，不能说快照也失败
      const b = await API.get<BackupItem[]>('/api/v1/backup/list').catch((err) => {
        toast.fail('读取备份列表失败', err)
        return null
      })
      if (b) backups = b
      toast.ok('已生成一份归档快照')
    } catch (err: any) {
      toast.fail('生成快照失败', err)
    }
  }

  // ===== 自动备份 =====
  async function saveAuto() {
    if (!auto) return
    autoSaving = true
    try {
      auto = await API.put('/api/v1/backup/auto', auto.schedule) as BackupStatus
      toast.ok('已保存自动备份设置')
    } catch (err: any) {
      toast.fail('保存失败', err)
    } finally {
      autoSaving = false
    }
  }

  async function runAutoNow() {
    autoRunning = true
    try {
      const res = await API.post('/api/v1/backup/auto/run', {}) as { status: BackupStatus }
      auto = res.status
      const b = await API.get<BackupItem[]>('/api/v1/backup/list').catch((err) => {
        toast.fail('读取备份列表失败', err)
        return null
      })
      if (b) backups = b
      toast.ok('已立即执行一次备份')
    } catch (err: any) {
      // 失败原因已由服务端落库，Status.last_error 会展示，这里只给即时提示
      toast.fail('备份失败', err)
      await loadAuto()
    } finally {
      autoRunning = false
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
    // file 已经拿到手，先把 input 清空：后面无论走哪条分支都不用再管它
    input.value = ''
    if (!(await ask({
      title: '用上传的数据库覆盖当前数据？',
      detail: '当前数据会先自动归档一份，但恢复过程中新写的记录会全部丢失。',
      danger: true,
      confirmLabel: '覆盖并恢复',
    }))) return
    restoring = true
    try {
      const fd = new FormData()
      fd.append('file', file)
      const res = await fetch('/api/v1/backup/restore', {
        method: 'POST',
        headers: API.authHeaders(),
        body: fd,
      })
      if (!res.ok) {
        let msg = res.statusText
        try { msg = (await res.json()).error || msg } catch {}
        throw new Error(msg)
      }
      toast.ok('恢复完成，即将重新加载页面')
      // alert 原来是阻塞的，toast 不会：晚一点刷新才看得见这句，期间按钮保持「恢复中」
      setTimeout(() => location.reload(), 1500)
    } catch (err: any) {
      toast.fail('恢复失败', err)
      restoring = false
    }
  }
</script>
<div class="space-y-6">
  <header><h1 class="text-2xl font-semibold">设置</h1><p class="text-sm mt-1" style="color: var(--q-muted);">偏好、名单、联系提醒、数据与访问</p></header>

  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-semibold mb-4 flex items-center gap-2"><SlidersHorizontal size={14} /> 偏好</h2>
    <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">我是谁</h3>
    <p class="text-xs mb-3 leading-relaxed" style="color: var(--q-muted);">
      牵丝以你为中心：设好本人之后，关系图按「离我几步」拼色，选人下拉把「我」置顶，认识路径才算得出来。
    </p>
    <div class="flex flex-wrap items-center gap-2">
      <div class="w-56 max-w-full"><PersonPicker bind:value={selfId} placeholder="选出哪一条记录是你自己" /></div>
      <button class="px-3 py-1.5 rounded-lg text-sm text-white disabled:opacity-50"
              style="background: var(--q-theme);" disabled={!selfDirty || selfSaving} onclick={saveSelf}>
        {selfSaving ? '保存中…' : '保存'}
      </button>
      {#if selfSaved}
        <span class="text-xs" style="color: var(--q-muted);">清除左边那个人后保存，就是不设本人</span>
      {/if}
    </div>
    <div class="mt-5 pt-5 border-t" style="border-color: var(--q-border);">
      <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">主题</h3>
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
      </div>
      <!-- 主题色：推荐色和取色器同一行，拆成两行看着像两组设置；窄屏宁可横向滚也不换行 -->
      <div class="flex items-center gap-2 mt-3 flex-nowrap overflow-x-auto pb-1">
        <span class="text-xs shrink-0" style="color: var(--q-muted);">主题色</span>
        {#each THEME_PRESETS as p}
          {@const active = theme.color.trim().toLowerCase() === p.color}
          <button onclick={() => setThemeColor(p.color)} title={p.name} aria-label={p.name} aria-pressed={active}
                  class="w-6 h-6 shrink-0 rounded-full transition-transform hover:scale-110"
                  style={`background: ${p.color}; ${active ? `box-shadow: 0 0 0 2px var(--q-surface), 0 0 0 4px ${p.color};` : 'border: 1px solid var(--q-border);'}`}></button>
        {/each}
        <input type="color" value={theme.color} oninput={(e) => setThemeColor(e.currentTarget.value)}
               aria-label="自定义主题色" class="w-9 h-9 shrink-0 rounded-lg border cursor-pointer" style="border-color: var(--q-border);" />
        <span class="text-xs font-mono shrink-0" style="color: var(--q-muted);">{theme.color}</span>
        {#if !isPreset(theme.color)}
          <span class="text-xs shrink-0" style="color: var(--q-muted);">当前是自定义色</span>
        {/if}
      </div>
    </div>
  </section>

  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-semibold mb-4 flex items-center gap-2"><Users size={14} /> 名单与口径</h2>
    <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">圈子（分组）</h3>
    <TermList items={categories} endpoint="/api/v1/categories" noun="圈子" placeholder="圈子名"
              defaults={{ icon: 'circle', sort_order: 0 }} onchange={() => refresh('categories')} />
    <div class="mt-5 pt-5 border-t" style="border-color: var(--q-border);">
      <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">标签</h3>
      <TermList items={tags} endpoint="/api/v1/tags" noun="标签" placeholder="标签名" pill onchange={() => refresh('tags')} />
    </div>
    <div class="mt-5 pt-5 border-t" style="border-color: var(--q-border);">
      <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">往来事件类型</h3>
      <TermList items={eventTypes} endpoint="/api/v1/event-types" noun="类型" placeholder="类型名"
                defaults={{ icon: 'calendar', is_default: false, sort_order: 0 }} onchange={() => refresh('eventTypes')} />
    </div>
  </section>

  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-semibold mb-4 flex items-center gap-2"><Bell size={14} /> 联系与提醒</h2>
    <div class="flex items-center justify-between gap-3 mb-2.5">
      <h3 class="text-xs font-medium" style="color: var(--q-muted);">联系节奏</h3>
      <label class="flex items-center gap-2 text-xs cursor-pointer shrink-0">
        <input type="checkbox" bind:checked={rhythmOn} disabled={rhythmToggling}
               onchange={() => toggleRhythm(rhythmOn)}
               aria-label="按联系节奏派生待办与推送"
               class="w-4 h-4 accent-indigo-500" />
        {rhythmOn ? '按节奏提醒' : '已关闭提醒'}
      </label>
    </div>
    <p class="text-xs mb-4 leading-relaxed" style="color: var(--q-muted);">
      按亲密度设定「多久该联系一次」。距最近一次往来、对话或「联系过了」打卡超过这个天数，就自动派生一条待办并随每日推送发出；
      勾掉待办就等于打过招呼，下一次到期日自动推后。
    </p>
    {#if rhythm.length > 0}
      {@const off = !rhythmOn}
      <!-- 五颗心那档实测要 173px：手机一列、中屏两列，三列要到 lg 才放得下，
           否则「天」会压到下一档的亲密度上 -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-x-6 gap-y-2 max-w-60 sm:max-w-2xl transition-opacity" style="opacity: {off ? 0.45 : 1};">
        {#each rhythm as t (t.grade)}
          <label class="flex items-center justify-between gap-2 text-sm">
            <span class="shrink-0" style="color: var(--q-muted);">{gradeLabel(t.grade)}</span>
            <span class="flex items-center gap-1.5">
              <input type="number" min={1} max={RHYTHM_MAX} bind:value={t.days} disabled={off}
                     aria-label={`${gradeLabel(t.grade)}的联系天数`}
                     class="w-20 px-2 py-1 rounded-lg text-sm outline-none text-right"
                     style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
              <span class="text-xs" style="color: var(--q-muted);">天</span>
            </span>
          </label>
        {/each}
      </div>
      <div class="flex items-center gap-2 mt-4">
        <button class="px-3 py-1.5 rounded-lg text-sm text-white disabled:opacity-50"
                style="background: var(--q-theme);" disabled={rhythmSaving || off} onclick={saveRhythm}>
          {rhythmSaving ? '保存中…' : '保存'}
        </button>
        <button class="px-3 py-1.5 rounded-lg text-sm disabled:opacity-50"
                style="background: var(--q-bg); border: 1px solid var(--q-border);" disabled={rhythmSaving || off} onclick={resetRhythm}>恢复默认</button>
        <button class="text-xs ml-auto" style="color: var(--q-theme);" onclick={() => navigate('/drift')}>看渐远名单</button>
      </div>
      {#if off}
        <p class="text-xs mt-3 leading-relaxed" style="color: var(--q-muted);">
          关闭只是不再派生待办、不再进每日推送：天数原样留着，重新开启就按这份节奏继续催。渐远名单和「联系过了」打卡不受影响。
        </p>
      {/if}
    {/if}
    <div class="mt-5 pt-5 border-t" style="border-color: var(--q-border);">
      <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">多渠道通知（apprise URL）</h3>
      <textarea bind:value={appriseUrls} class="w-full h-28 px-3 py-2 rounded-lg text-sm outline-none font-mono" style="background: var(--q-bg); border: 1px solid var(--q-border);"
        placeholder='每行一个 URL，如：&#10;bark://host/key&#10;feishu://...&#10;tgram://token/chat'></textarea>
      <!-- 窄屏放不下整行时宁可让按钮整块换行，也不能压进按钮里把「测试推送」拆成两行 -->
      <div class="flex flex-wrap items-center gap-2 mt-3">
        <span class="text-sm shrink-0 whitespace-nowrap" style="color: var(--q-muted);">每日推送时间</span>
        <input type="number" min={0} max={23} bind:value={pushHour} aria-label="每日推送时间（0-23 时）" class="w-16 shrink-0 px-3 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
        <button class="px-3 py-1.5 rounded-lg text-sm text-white shrink-0 whitespace-nowrap" style="background: var(--q-theme);" onclick={saveNotify}>保存</button>
        <button class="px-3 py-1.5 rounded-lg text-sm shrink-0 whitespace-nowrap" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={testNotify}>测试推送</button>
      </div>
      <p class="text-xs mt-2" style="color: var(--q-muted);">
        支持 bark、feishu、telegram、discord、smtp 等 100+ 渠道；一行一个 URL，逗号或换行分隔。
      </p>
    </div>
  </section>

  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-semibold mb-4 flex items-center gap-2"><Database size={14} /> 数据</h2>
    <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">数据备份</h3>
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
    <div class="mt-5 pt-5 border-t" style="border-color: var(--q-border);">
      <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">定时自动备份</h3>
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
    </div>
    <div class="mt-5 pt-5 border-t" style="border-color: var(--q-border);">
      <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">明细导出与全量迁移</h3>
      <div class="flex flex-wrap items-center gap-2">
        <select bind:value={csvWhat} class="px-3 py-1.5 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
          <option value="events">往来明细</option>
          <option value="transactions">金钱明细</option>
          <option value="memos">对话明细</option>
        </select>
        <input type="number" min="1900" max="2200" bind:value={csvYear} placeholder="年份（全部）"
               class="w-28 px-3 py-1.5 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);" />
        <div class="w-40">
          <PersonPicker bind:value={csvPerson} placeholder="全部人物" compact={true} />
        </div>
        {#if csvWhat === 'transactions'}
          <select bind:value={csvEvent} class="max-w-[12rem] px-3 py-1.5 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border); color: var(--q-text);">
            <option value="">全部往来</option>
            {#each eventOptions as ev}<option value={ev.id}>{ev.title}</option>{/each}
          </select>
        {/if}
        <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={exportCsv}>
          <Download size={14} /> 导出 CSV
        </button>
      </div>
      <p class="text-xs mt-2" style="color: var(--q-muted);">
        CSV 带 BOM，Excel 双击打开不乱码；金额按「元」导出。选一场往来即可只导它的礼单。
      </p>
      <div class="flex flex-wrap gap-2 mt-3">
        <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm" style="background: var(--q-bg); border: 1px solid var(--q-border);" onclick={exportJson}>
          <Download size={14} /> 全量导出 JSON
        </button>
        <input type="file" accept=".json,application/json" class="hidden" bind:this={jsonInput} onchange={pickJson} />
        <button class="flex items-center gap-1 px-3 py-1.5 rounded-lg text-sm"
                style="background: var(--q-bg); border: 1px solid var(--q-border); color: #ef4444;" onclick={() => jsonInput?.click()}>
          <Upload size={14} /> 从 JSON 导入（覆盖全库）
        </button>
      </div>
      {#if ioMsg}
        <p class="text-xs mt-2 flex items-center gap-1" style="color: var(--q-muted);"><Check size={12} /> {ioMsg}</p>
      {/if}
    </div>
  </section>

  <section class="rounded-xl p-5" style="background: var(--q-surface); border: 1px solid var(--q-border);">
    <h2 class="text-sm font-semibold mb-4 flex items-center gap-2"><KeyRound size={14} /> 访问与安全</h2>
    <h3 class="text-xs font-medium mb-2.5" style="color: var(--q-muted);">访问令牌（可选）</h3>
    <div class="flex gap-2">
      <input bind:value={token} type="password" placeholder="服务端 QIANSI_TOKEN，未设置则留空"
             class="flex-1 px-3 py-2 rounded-lg text-sm outline-none" style="background: var(--q-bg); border: 1px solid var(--q-border);" />
      <button class="px-3 py-1.5 rounded-lg text-sm text-white" style="background: var(--q-theme);" onclick={saveToken}>保存</button>
    </div>
    <p class="text-xs mt-2" style="color: var(--q-muted);">
      服务端设置 QIANSI_TOKEN 后，所有接口都需要此令牌；跨站访问默认已被 CORS 拒绝。
    </p>
  </section>

</div>
