<script lang="ts">
  import { onMount } from 'svelte';
  import { navigate } from '../lib/router';
  import {
    API,
    gradeLabel,
    type BackupItem,
    type BackupStatus,
    type RhythmTier,
    type HuozhiStatus
  } from '../lib/api';
  import TermList from '../lib/TermList.svelte';
  import PageHeader from '../lib/ui/PageHeader.svelte';
  import {
    Download,
    Upload,
    Bell,
    Monitor,
    Sun,
    Moon,
    Check,
    AlertTriangle,
    SlidersHorizontal,
    Users,
    Database,
    KeyRound,
    Link2
  } from '@lucide/svelte';
  import {
    theme,
    setThemeMode,
    setThemeColor,
    setFontScale,
    setFontFamily,
    setStartPage,
    setMoneyMask,
    initTheme,
    isPreset,
    THEME_LABEL,
    THEME_PRESETS,
    FONT_SCALE_LABEL,
    FONT_FAMILY_LABEL,
    START_PAGES,
    TODAY_MODULES,
    setTodayModule,
    type ThemeMode,
    type FontScale,
    type FontFamily,
    type StartPage
  } from '../lib/theme.svelte';
  import PersonPicker from '../lib/PersonPicker.svelte';
  import { setSelf } from '../lib/self.svelte';
  import { dict, ensure, refresh, refreshAll } from '../lib/dict.svelte';
  import { toast } from '../lib/toast.svelte';
  import { ask } from '../lib/ask.svelte';

  let settings = $state<Record<string, string>>({});
  let appriseUrls = $state('');
  let pushHour = $state(9);

  // 货殖对接：地址与密钥都存在 settings 表里，密钥只在本机服务端使用
  let hzBase = $state('');
  let hzKey = $state('');
  let hzStatus = $state<HuozhiStatus | null>(null);
  let hzBusy = $state(false);

  // 这三个字典在本页编辑，改完必须显式刷新，否则别的页面还是旧名单
  const categories = $derived(dict.categories);
  const tags = $derived(dict.tags);
  const eventTypes = $derived(dict.eventTypes);
  let backups = $state<BackupItem[]>([]);
  let restoreInput: HTMLInputElement | undefined = $state();
  let restoring = $state(false);
  let token = $state(localStorage.getItem('q_token') || '');

  // 「我是谁」只是 settings 里一个指向 people 的指针，但关系图拼色、选人置顶、
  // 认识路径全挂在它身上，所以本页必须能设它——以前只能进某个人的详情点一个无文字图标。
  let selfId = $state('');
  let selfSaved = $state('');
  let selfSaving = $state(false);
  const selfDirty = $derived(selfId !== selfSaved);

  async function saveSelf() {
    selfSaving = true;
    try {
      await API.post('/api/v1/settings/bulk', { self_person_id: selfId });
    } catch (err) {
      toast.fail('保存本人失败', err);
      selfSaving = false;
      return;
    }
    selfSaving = false;
    selfSaved = selfId;
    settings = { ...settings, self_person_id: selfId };
    // 本页改了指针，别的页面上的下拉与图谱读的是共享状态，必须一并更新
    setSelf(selfId);
    toast.ok(selfId ? '已把 TA 设为你自己' : '已取消本人');
  }

  const WEEKDAYS = [
    { v: 0, n: '日' },
    { v: 1, n: '一' },
    { v: 2, n: '二' },
    { v: 3, n: '三' },
    { v: 4, n: '四' },
    { v: 5, n: '五' },
    { v: 6, n: '六' }
  ];

  // 主题三档按钮，顺序固定：先"跟随系统"（默认），再具体模式
  const THEME_TABS: { mode: ThemeMode; icon: typeof Monitor }[] = [
    { mode: 'system', icon: Monitor },
    { mode: 'light', icon: Sun },
    { mode: 'dark', icon: Moon }
  ];

  // 字号四档与字体三族，顺序即展示顺序（标准/默认在第二位，贴着语义）
  const FONT_SCALE_TABS = Object.keys(FONT_SCALE_LABEL) as FontScale[];
  const FONT_FAMILY_TABS = Object.keys(FONT_FAMILY_LABEL) as FontFamily[];

  // 自动备份
  let auto = $state<BackupStatus | null>(null);
  let autoSaving = $state(false);
  let autoRunning = $state(false);

  // 联系节奏（六档天数）：和自动备份的 schedule 一样，是这台机器上的偏好，
  // 后端存成 settings 里的一个 JSON 键，GET 永远回齐六档。
  let rhythm = $state<RhythmTier[]>([]);
  let rhythmSaving = $state(false);
  const RHYTHM_MAX = 3650;

  // 总开关就是 settings 里的一个键（后端 ContactUpcoming 认它），所以不另开端点。
  // 只有显式写过关闭值才算关：没配过的老库默认是开着。
  let rhythmOn = $state(true);
  let rhythmToggling = $state(false);

  async function toggleRhythm(on: boolean) {
    rhythmToggling = true;
    try {
      await API.post('/api/v1/settings/bulk', { contact_rhythm_enabled: on ? '1' : '0' });
    } catch (err) {
      // 写失败就把勾退回原样：屏幕上的开关必须和后端一致，不能看着关了其实还在催
      rhythmOn = !on;
      rhythmToggling = false;
      toast.fail(on ? '开启联系节奏失败' : '关闭联系节奏失败', err);
      return;
    }
    rhythmToggling = false;
    settings = { ...settings, contact_rhythm_enabled: on ? '1' : '0' };
    toast.ok(on ? '已开启联系节奏提醒' : '已关闭：不再派生待办，也不进每日推送');
  }

  async function loadRhythm() {
    const r = await API.get<RhythmTier[]>('/api/v1/contact-rhythm').catch((err) => {
      toast.fail('读取联系节奏失败', err);
      return null;
    });
    if (r) rhythm = r;
  }

  // 存完用服务端的回读覆盖本地：它才是归一化后的那份，六个数字要对得上
  async function saveRhythm() {
    rhythmSaving = true;
    try {
      rhythm = await API.put<RhythmTier[]>('/api/v1/contact-rhythm', rhythm);
      toast.ok('已保存联系节奏');
    } catch (err) {
      toast.fail('保存联系节奏失败', err);
    } finally {
      rhythmSaving = false;
    }
  }

  async function resetRhythm() {
    if (
      !(await ask({
        title: '恢复默认联系节奏？',
        detail: '六档天数会退回 7 / 7 / 30 / 30 / 90 / 90 天。',
        confirmLabel: '恢复默认'
      }))
    )
      return;
    rhythmSaving = true;
    try {
      rhythm = await API.delete<RhythmTier[]>('/api/v1/contact-rhythm');
      toast.ok('已恢复默认节奏');
    } catch (err) {
      toast.fail('恢复默认节奏失败', err);
    } finally {
      rhythmSaving = false;
    }
  }

  async function load() {
    // 读失败就把屏幕上已有的值留着：填成空再点保存会覆盖掉服务端的真配置
    const s = await API.get<Record<string, string>>('/api/v1/settings').catch((err) => {
      toast.fail('读取设置失败', err);
      return null;
    });
    if (s) {
      settings = s;
      appriseUrls = s['apprise_urls'] || '';
      pushHour = parseInt(s['push_time_hour'] || '9', 10);
      selfId = s['self_person_id'] || '';
      selfSaved = selfId;
      hzBase = s['huozhi_base_url'] || '';
      hzKey = s['huozhi_api_key'] || '';
      rhythmOn = !['0', 'false', 'off', 'no'].includes(
        (s['contact_rhythm_enabled'] || '').trim().toLowerCase()
      );
      trashDays = s['trash_retention_days'] || '0';
    }
    const b = await API.get<BackupItem[]>('/api/v1/backup/list').catch((err) => {
      toast.fail('读取备份列表失败', err);
      return null;
    });
    if (b) backups = b;
    await Promise.all([loadAuto(), loadRhythm()]);
    void checkHuozhi();
  }

  // checkHuozhi 只报告状态，不弹错：设置页开着的时候货殖没启动是常见情形，
  // 每次进页面都弹一次红条属于噪声。
  async function checkHuozhi() {
    hzStatus = await API.get<HuozhiStatus>('/api/v1/huozhi/status').catch(() => null);
  }

  async function saveHuozhi() {
    hzBusy = true;
    try {
      await API.post('/api/v1/settings/bulk', {
        huozhi_base_url: hzBase.trim(),
        huozhi_api_key: hzKey.trim()
      });
    } catch (err) {
      toast.fail('保存货殖配置失败', err);
      hzBusy = false;
      return;
    }
    // 保存之后立刻打一次真实请求，让「填了但连不通」当场暴露，而不是等关联账单时才报错
    await checkHuozhi();
    hzBusy = false;
    if (hzStatus?.ok) toast.ok('货殖连接正常');
    else toast.error(hzStatus?.error || '货殖连接失败，请检查地址与密钥');
  }

  async function loadAuto() {
    try {
      auto = (await API.get('/api/v1/backup/auto')) as BackupStatus;
    } catch {
      auto = null;
    }
  }

  // ===== 回收站自动清理 =====
  // 天数档存 settings 的 trash_retention_days，后端每小时巡一次，过期行真删除。0 = 关闭。
  const TRASH_DAYS_TABS = [
    { v: '0', n: '关闭' },
    { v: '7', n: '7 天' },
    { v: '14', n: '14 天' },
    { v: '30', n: '30 天' },
    { v: '90', n: '90 天' }
  ];
  let trashDays = $state('0');
  async function setTrashDays(v: string) {
    const prev = trashDays;
    trashDays = v;
    try {
      await API.post('/api/v1/settings/bulk', { trash_retention_days: v });
    } catch (err) {
      trashDays = prev;
      toast.fail('保存回收站清理设置失败', err);
    }
  }

  onMount(() => {
    initTheme();
    ensure('categories');
    ensure('tags');
    ensure('eventTypes');
    ensure('events');
    load();
  });

  async function saveNotify() {
    try {
      await API.post('/api/v1/settings/bulk', {
        apprise_urls: appriseUrls,
        push_time_hour: String(pushHour)
      });
    } catch (err) {
      toast.fail('保存推送设置失败', err);
      return;
    }
    toast.ok('已保存推送设置');
  }
  // 之前的「测试推送」只保存了配置却提示已触发，属于误导
  async function testNotify() {
    try {
      await API.post('/api/v1/settings/bulk', {
        apprise_urls: appriseUrls,
        push_time_hour: String(pushHour)
      });
    } catch (err) {
      toast.fail('保存推送设置失败', err);
      return;
    }
    try {
      await API.post('/api/v1/notify/test', {});
      toast.ok('已发送测试推送，请检查对应渠道');
    } catch (err) {
      toast.fail('测试推送失败', err);
    }
  }

  async function saveToken() {
    const t = token.trim();
    if (t) localStorage.setItem('q_token', t);
    else localStorage.removeItem('q_token');
    // 头像和下载链接靠会话 cookie 才认令牌：存完当场换一枚，否则要刷新页面才生效
    const ok = await API.ensureSession();
    if (ok) toast.ok('已保存访问令牌；若服务端未设置 QIANSI_TOKEN，请留空');
    else toast.error('令牌已保存，但服务端不认它，头像与导出仍会失败');
  }

  async function exportData() {
    try {
      await API.download('/api/v1/backup/export');
    } catch (err) {
      toast.fail('导出失败', err);
    }
  }

  // ===== 明细 CSV 与全量 JSON（N3）=====
  const eventOptions = $derived(dict.events);
  let csvWhat = $state<'events' | 'transactions' | 'memos'>('events');
  let csvYear = $state<number | ''>('');
  let csvPerson = $state('');
  let csvEvent = $state('');
  let ioMsg = $state('');
  let jsonInput: HTMLInputElement | undefined = $state();

  function say(msg: string) {
    // 失败都交给 toast；这条留在原地，是导入结果里需要看清的明细数字
    ioMsg = msg;
    setTimeout(() => (ioMsg = ''), 6000);
  }

  async function exportCsv() {
    const q = new URLSearchParams({ what: csvWhat });
    if (csvYear) q.set('year', String(csvYear));
    if (csvPerson) q.set('person_id', csvPerson);
    // 事件过滤只对金钱明细有意义（礼单对账）
    if (csvEvent && csvWhat === 'transactions') q.set('event_id', csvEvent);
    try {
      await API.download('/api/v1/export/csv?' + q.toString());
    } catch (err) {
      toast.fail('导出失败', err);
    }
  }

  async function exportJson() {
    try {
      await API.download('/api/v1/export/json');
    } catch (err) {
      toast.fail('全量导出失败', err);
    }
  }

  async function pickJson(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    let parsed: any;
    try {
      parsed = JSON.parse(await file.text());
    } catch {
      toast.error('这个文件读不出 JSON，确认选的是「全量导出」下来的文件');
      return;
    }
    // 先在本机看一眼再上传：选错文件不该让人等一趟几十 MB 的往返
    if (parsed?.app !== 'qiansi') {
      toast.error('这不是牵丝的全量导出文件');
      return;
    }
    if (
      !(await ask({
        title: '用这份 JSON 覆盖当前全部数据',
        detail: `文件导出于 ${fmtTime(parsed.exported_at || '')}。导入前会自动归档一份当前数据库到 backups/，出问题可以从那里捞回来。附件的图片文件不在 JSON 里，需要另外拷贝 uploads 目录。`,
        danger: true,
        confirmLabel: '覆盖导入'
      }))
    )
      return;
    try {
      const res = await API.post<{ tables: number; rows: number }>('/api/v1/import/json', parsed);
      say(`已导入 ${res.rows} 行，覆盖 ${res.tables} 张表`);
      await Promise.all([load(), refreshAll()]);
    } catch (err) {
      toast.fail('导入失败', err);
    }
  }

  async function snapshot() {
    try {
      await API.post('/api/v1/backup/snapshot', {});
      // 列表刷新失败只说明「没看到新条目」，不能说快照也失败
      const b = await API.get<BackupItem[]>('/api/v1/backup/list').catch((err) => {
        toast.fail('读取备份列表失败', err);
        return null;
      });
      if (b) backups = b;
      toast.ok('已生成一份归档快照');
    } catch (err) {
      toast.fail('生成快照失败', err);
    }
  }

  // ===== 自动备份 =====
  async function saveAuto() {
    if (!auto) return;
    autoSaving = true;
    try {
      auto = (await API.put('/api/v1/backup/auto', auto.schedule)) as BackupStatus;
      toast.ok('已保存自动备份设置');
    } catch (err) {
      toast.fail('保存失败', err);
    } finally {
      autoSaving = false;
    }
  }

  async function runAutoNow() {
    autoRunning = true;
    try {
      const res = (await API.post('/api/v1/backup/auto/run', {})) as { status: BackupStatus };
      auto = res.status;
      const b = await API.get<BackupItem[]>('/api/v1/backup/list').catch((err) => {
        toast.fail('读取备份列表失败', err);
        return null;
      });
      if (b) backups = b;
      toast.ok('已立即执行一次备份');
    } catch (err) {
      // 失败原因已由服务端落库，Status.last_error 会展示，这里只给即时提示
      toast.fail('备份失败', err);
      await loadAuto();
    } finally {
      autoRunning = false;
    }
  }

  // 把 RFC3339 转成"YYYY-MM-DD HH:MM"，直接用 toLocaleString 会被时区/locale 干扰
  function fmtTime(s: string): string {
    if (!s) return '—';
    const d = new Date(s);
    if (isNaN(d.getTime())) return '—';
    const p = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
  }

  async function restoreData(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    // file 已经拿到手，先把 input 清空：后面无论走哪条分支都不用再管它
    input.value = '';
    if (
      !(await ask({
        title: '用上传的数据库覆盖当前数据？',
        detail: '当前数据会先自动归档一份，但恢复过程中新写的记录会全部丢失。',
        danger: true,
        confirmLabel: '覆盖并恢复'
      }))
    )
      return;
    restoring = true;
    try {
      const fd = new FormData();
      fd.append('file', file);
      const res = await fetch('/api/v1/backup/restore', {
        method: 'POST',
        headers: API.authHeaders(),
        body: fd
      });
      if (!res.ok) {
        let msg = res.statusText;
        try {
          msg = (await res.json()).error || msg;
        } catch {
          /* 非 JSON 响应体，沿用 statusText */
        }
        throw new Error(msg);
      }
      toast.ok('恢复完成，即将重新加载页面');
      // alert 原来是阻塞的，toast 不会：晚一点刷新才看得见这句，期间按钮保持「恢复中」
      setTimeout(() => location.reload(), 1500);
    } catch (err) {
      toast.fail('恢复失败', err);
      restoring = false;
    }
  }
</script>

<div class="q-page">
  <PageHeader title="设置" subtitle="偏好、名单、联系提醒、数据与访问" />

  <div class="q-stack">
    <section class="q-card">
      <h2 class="q-section-title">
        <SlidersHorizontal size={15} /> 偏好
      </h2>
      <h3 class="q-label">我是谁</h3>
      <p class="q-hint mb-3">
        牵丝以你为中心：设好本人之后，关系图按「离我几步」拼色，选人下拉把「我」置顶，认识路径才算得出来。
      </p>
      <div class="flex flex-wrap items-center gap-2">
        <div class="w-56 max-w-full">
          <PersonPicker bind:value={selfId} placeholder="选出哪一条记录是你自己" />
        </div>
        <button class="q-btn q-btn--primary" disabled={!selfDirty || selfSaving} onclick={saveSelf}>
          {selfSaving ? '保存中…' : '保存'}
        </button>
        {#if selfSaved}
          <span class="text-xs" style="color: var(--q-muted);"
            >清除左边那个人后保存，就是不设本人</span
          >
        {/if}
      </div>
      <div class="q-divider"></div>
      <h3 class="q-label">主题</h3>
      <div class="flex flex-wrap items-center gap-4">
        <!-- 外观三档：跟随系统 / 浅色 / 深色 -->
        <div class="q-seg q-seg--fill">
          {#each THEME_TABS as t, i (i)}
            <button
              onclick={() => setThemeMode(t.mode)}
              aria-pressed={theme.mode === t.mode}
              class="q-seg__btn"
            >
              <t.icon size={13} />
              {THEME_LABEL[t.mode]}
            </button>
          {/each}
        </div>
        <span class="text-xs" style="color: var(--q-muted);">
          {theme.mode === 'system'
            ? '自动匹配系统外观，系统切换时实时跟随'
            : '已手动指定，不随系统变化'}
        </span>
      </div>
      <!-- 主题色：推荐色和取色器同一行，拆成两行看着像两组设置；窄屏宁可横向滚也不换行 -->
      <div class="mt-3 flex flex-nowrap items-center gap-2 overflow-x-auto pb-1">
        <span class="shrink-0 text-xs" style="color: var(--q-muted);">主题色</span>
        {#each THEME_PRESETS as p, i (i)}
          {@const active = theme.color.trim().toLowerCase() === p.color}
          <button
            onclick={() => setThemeColor(p.color)}
            title={p.name}
            aria-label={p.name}
            aria-pressed={active}
            class="h-6 w-6 shrink-0 rounded-full transition-transform hover:scale-110"
            style={`background: ${p.color}; ${active ? `box-shadow: 0 0 0 2px var(--q-surface), 0 0 0 4px ${p.color};` : 'border: 1px solid var(--q-border);'}`}
          ></button>
        {/each}
        <input
          type="color"
          value={theme.color}
          oninput={(e) => setThemeColor(e.currentTarget.value)}
          aria-label="自定义主题色"
          class="h-9 w-9 shrink-0 cursor-pointer rounded-lg border"
          style="border-color: var(--q-border);"
        />
        <span class="q-num shrink-0 text-xs" style="color: var(--q-muted);">{theme.color}</span>
        {#if !isPreset(theme.color)}
          <span class="shrink-0 text-xs" style="color: var(--q-muted);">当前是自定义色</span>
        {/if}
      </div>
      <!-- 字号档位与字体族：两组分段控件同一行，右侧按当前选择给一句说明 -->
      <div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
        <div class="flex items-center gap-2">
          <span class="shrink-0 text-xs" style="color: var(--q-muted);">字号</span>
          <div class="q-seg q-seg--fill">
            {#each FONT_SCALE_TABS as f (f)}
              <button
                onclick={() => setFontScale(f)}
                aria-pressed={theme.fontScale === f}
                class="q-seg__btn"
              >
                {FONT_SCALE_LABEL[f]}
              </button>
            {/each}
          </div>
        </div>
        <div class="flex items-center gap-2">
          <span class="shrink-0 text-xs" style="color: var(--q-muted);">字体</span>
          <div class="q-seg q-seg--fill">
            {#each FONT_FAMILY_TABS as f (f)}
              <button
                onclick={() => setFontFamily(f)}
                aria-pressed={theme.fontFamily === f}
                class="q-seg__btn"
              >
                {FONT_FAMILY_LABEL[f]}
              </button>
            {/each}
          </div>
        </div>
        <span class="text-xs" style="color: var(--q-muted);">
          {#if theme.fontFamily === 'serif'}
            全界面衬线（宋体）
          {:else if theme.fontFamily === 'sans'}
            全界面非衬线（黑体）
          {:else}
            标题衬线、正文黑体
          {/if}；字号整体缩放，跨设备同步
        </span>
      </div>
      <!-- 启动页与金额打码：偏好区最后两组，与上面的档位控件同一套分段样式 -->
      <div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
        <div class="flex items-center gap-2">
          <span class="shrink-0 text-xs" style="color: var(--q-muted);">启动页</span>
          <div class="q-seg q-seg--fill">
            {#each START_PAGES as p (p.path)}
              <button
                onclick={() => setStartPage(p.path)}
                aria-pressed={theme.startPage === p.path}
                class="q-seg__btn"
              >
                {p.label}
              </button>
            {/each}
          </div>
        </div>
        <div class="flex items-center gap-2">
          <span class="shrink-0 text-xs" style="color: var(--q-muted);">金额</span>
          <div class="q-seg q-seg--fill">
            <button onclick={() => setMoneyMask(false)} aria-pressed={!theme.maskMoney} class="q-seg__btn">
              明文
            </button>
            <button onclick={() => setMoneyMask(true)} aria-pressed={theme.maskMoney} class="q-seg__btn">
              打码
            </button>
          </div>
        </div>
        <span class="text-xs" style="color: var(--q-muted);">
          启动页在下次进站生效；金额打码后侧栏眼睛图标可随时切换
        </span>
      </div>
      <!-- 今日页模块显隐：点亮的卡片显示在首页，整块收起不留空位 -->
      <div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
        <div class="flex items-center gap-2">
          <span class="shrink-0 text-xs" style="color: var(--q-muted);">今日页</span>
          <div class="q-seg q-seg--fill">
            {#each TODAY_MODULES as m (m.key)}
              <button
                onclick={() => setTodayModule(m.key, theme.todayHidden.includes(m.key))}
                aria-pressed={!theme.todayHidden.includes(m.key)}
                class="q-seg__btn"
              >
                {m.label}
              </button>
            {/each}
          </div>
        </div>
        <span class="text-xs" style="color: var(--q-muted);">
          点亮的显示在首页；首启引导不受这里控制
        </span>
      </div>
    </section>

    <section class="q-card">
      <h2 class="q-section-title">
        <Users size={15} /> 名单与口径
      </h2>
      <h3 class="q-label">圈子（分组）</h3>
      <TermList
        items={categories}
        endpoint="/api/v1/categories"
        noun="圈子"
        placeholder="圈子名"
        defaults={{ icon: 'circle', sort_order: 0 }}
        onchange={() => refresh('categories')}
      />
      <div class="q-divider"></div>
      <h3 class="q-label">标签</h3>
      <TermList
        items={tags}
        endpoint="/api/v1/tags"
        noun="标签"
        placeholder="标签名"
        pill
        onchange={() => refresh('tags')}
      />
      <div class="q-divider"></div>
      <h3 class="q-label">往来事件类型</h3>
      <TermList
        items={eventTypes}
        endpoint="/api/v1/event-types"
        noun="类型"
        placeholder="类型名"
        defaults={{ icon: 'calendar', is_default: false, sort_order: 0 }}
        onchange={() => refresh('eventTypes')}
      />
    </section>

    <section class="q-card">
      <h2 class="q-section-title">
        <Bell size={15} /> 联系与提醒
      </h2>
      <div class="mb-2.5 flex items-center justify-between gap-3">
        <h3 class="q-label !mb-0">联系节奏</h3>
        <label class="flex shrink-0 cursor-pointer items-center gap-2 text-xs">
          <input
            type="checkbox"
            bind:checked={rhythmOn}
            disabled={rhythmToggling}
            onchange={() => toggleRhythm(rhythmOn)}
            aria-label="按联系节奏派生待办与推送"
          />
          {rhythmOn ? '按节奏提醒' : '已关闭提醒'}
        </label>
      </div>
      <p class="q-hint mb-4">
        按亲密度设定「多久该联系一次」。距最近一次往来、对话或「联系过了」打卡超过这个天数，就自动派生一条待办并随每日推送发出；
        勾掉待办就等于打过招呼，下一次到期日自动推后。
      </p>
      {#if rhythm.length > 0}
        {@const off = !rhythmOn}
        <!-- 五颗心那档实测要 173px：手机一列、中屏两列，三列要到 lg 才放得下，
           否则「天」会压到下一档的亲密度上 -->
        <div
          class="grid max-w-60 grid-cols-1 gap-x-6 gap-y-2 transition-opacity sm:max-w-2xl sm:grid-cols-2 lg:grid-cols-3"
          style="opacity: {off ? 0.45 : 1};"
        >
          {#each rhythm as t (t.grade)}
            <label class="flex items-center justify-between gap-2 text-sm">
              <span class="shrink-0" style="color: var(--q-muted);">{gradeLabel(t.grade)}</span>
              <span class="flex items-center gap-1.5">
                <input
                  type="number"
                  min={1}
                  max={RHYTHM_MAX}
                  bind:value={t.days}
                  disabled={off}
                  aria-label={`${gradeLabel(t.grade)}的联系天数`}
                  class="q-input w-20 !py-1 text-right"
                />
                <span class="text-xs" style="color: var(--q-muted);">天</span>
              </span>
            </label>
          {/each}
        </div>
        <div class="mt-4 flex items-center gap-2">
          <button class="q-btn q-btn--primary" disabled={rhythmSaving || off} onclick={saveRhythm}>
            {rhythmSaving ? '保存中…' : '保存'}
          </button>
          <button class="q-btn q-btn--ghost" disabled={rhythmSaving || off} onclick={resetRhythm}
            >恢复默认</button
          >
          <button class="q-btn q-btn--subtle q-btn--sm ml-auto" onclick={() => navigate('/drift')}
            >看渐远名单</button
          >
        </div>
        {#if off}
          <p class="q-hint mt-3">
            关闭只是不再派生待办、不再进每日推送：天数原样留着，重新开启就按这份节奏继续催。渐远名单和「联系过了」打卡不受影响。
          </p>
        {/if}
      {/if}
      <div class="q-divider"></div>
      <h3 class="q-label">多渠道通知（apprise URL）</h3>
      <textarea
        bind:value={appriseUrls}
        class="q-textarea h-28 font-mono"
        placeholder="每行一个 URL，如：&#10;bark://host/key&#10;feishu://...&#10;tgram://token/chat"
      ></textarea>
      <!-- 窄屏放不下整行时宁可让按钮整块换行，也不能压进按钮里把「测试推送」拆成两行 -->
      <div class="mt-3 flex flex-wrap items-center gap-2">
        <span class="shrink-0 text-sm whitespace-nowrap" style="color: var(--q-muted);"
          >每日推送时间</span
        >
        <input
          type="number"
          min={0}
          max={23}
          bind:value={pushHour}
          aria-label="每日推送时间（0-23 时）"
          class="q-input w-16 shrink-0"
        />
        <button class="q-btn q-btn--primary shrink-0" onclick={saveNotify}>保存</button>
        <button class="q-btn q-btn--ghost shrink-0" onclick={testNotify}>测试推送</button>
      </div>
      <p class="q-hint">
        支持 bark、feishu、telegram、discord、smtp 等 100+ 渠道；一行一个 URL，逗号或换行分隔。
      </p>
    </section>

    <section class="q-card">
      <h2 class="q-section-title"><Link2 size={15} /> 货殖账单</h2>
      <p class="q-hint mb-3">
        关联后，一次往来的开销可以来自货殖里的多张账单，往来卡片按账单汇总。
        牵丝只读货殖的账单（不增、不改、不删），密钥只保存在本机服务端。
        <strong style="color: var(--q-text);">请使用货殖里的「只读」密钥</strong>——读写密钥连带 MCP
        的删改能力，牵丝用不上，没必要把这份权限交出来。
      </p>
      <div class="q-stack--tight flex flex-col">
        <div class="flex flex-wrap items-center gap-2">
          <input
            bind:value={hzBase}
            placeholder="货殖地址，如 https://huozhi.example.com"
            class="q-input min-w-[220px] flex-1"
          />
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <input
            bind:value={hzKey}
            type="password"
            autocomplete="off"
            placeholder="API Key（货殖 → 设置 → API 密钥）"
            class="q-input min-w-[220px] flex-1 font-mono"
          />
          <button
            class="q-btn q-btn--primary shrink-0 whitespace-nowrap"
            disabled={hzBusy}
            onclick={saveHuozhi}>{hzBusy ? '检查中…' : '保存并测试'}</button
          >
        </div>
      </div>
      {#if hzStatus}
        <p class="q-hint" style="color: {hzStatus.ok ? 'var(--q-ok)' : 'var(--q-danger)'};">
          {#if !hzStatus.configured}
            还没有配置。填好地址与密钥后点「保存并测试」。
          {:else if hzStatus.ok}
            连接正常{hzStatus.base ? ` · ${hzStatus.base}` : ''}
          {:else}
            连接失败：{hzStatus.error || '未知错误'}
          {/if}
        </p>
      {/if}
    </section>

    <section class="q-card">
      <h2 class="q-section-title"><Database size={15} /> 数据</h2>
      <h3 class="q-label">数据备份</h3>
      <div class="flex flex-wrap gap-2">
        <button class="q-btn q-btn--primary" onclick={exportData}>
          <Download size={14} /> 导出数据库
        </button>
        <button class="q-btn q-btn--ghost" onclick={snapshot}>生成归档快照</button>
        <input
          type="file"
          accept=".db,.sqlite,.sqlite3"
          class="hidden"
          bind:this={restoreInput}
          onchange={restoreData}
        />
        <button
          class="q-btn q-btn--danger-ghost"
          disabled={restoring}
          onclick={() => restoreInput?.click()}
        >
          <Upload size={14} />
          {restoring ? '恢复中…' : '从备份恢复'}
        </button>
      </div>
      <p class="q-hint">
        导出会生成一份一致性快照（不含未落盘的 WAL 残留）；恢复前会自动归档当前数据。
      </p>
      {#if backups.length > 0}
        <ul class="q-stack--tight mt-3 flex flex-col">
          {#each backups as b, i (i)}
            <li class="q-well q-num flex items-center gap-2 !py-1.5 text-xs">
              <span class="truncate">{b.name}</span>
              <span class="ml-auto shrink-0">{(b.size / 1024).toFixed(0)} KB · {b.time}</span>
            </li>
          {/each}
        </ul>
      {/if}
      <div class="q-divider"></div>
      <h3 class="q-label">定时自动备份</h3>
      {#if auto}
        {@const status = auto}
        <div class="q-stack">
          <!-- 开关 + 概览 -->
          <div class="flex flex-wrap items-center gap-3">
            <label class="flex cursor-pointer items-center gap-2 text-sm">
              <input type="checkbox" bind:checked={status.schedule.enabled} />
              {auto.schedule.enabled ? '已开启' : '已关闭'}
            </label>
            <span class="text-xs" style="color: var(--q-muted);">{auto.label}</span>
            <button
              class="q-btn q-btn--primary q-btn--sm ml-auto"
              disabled={autoRunning}
              onclick={runAutoNow}
            >
              {autoRunning ? '执行中…' : '立即执行一次'}
            </button>
          </div>

          <!-- 周期与时间点 -->
          <div class="flex flex-wrap items-center gap-3">
            <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
              周期
              <select bind:value={status.schedule.frequency} aria-label="备份周期" class="q-select">
                <option value="daily">每日</option>
                <option value="weekly">每周</option>
                <option value="custom">自定义间隔</option>
              </select>
            </label>

            {#if auto.schedule.frequency === 'weekly'}
              <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
                星期
                <select bind:value={status.schedule.weekday} aria-label="星期" class="q-select">
                  {#each WEEKDAYS as w, i (i)}
                    <option value={w.v}>周{w.n}</option>
                  {/each}
                </select>
              </label>
            {/if}

            {#if auto.schedule.frequency === 'custom'}
              <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
                间隔（分钟）
                <input
                  type="number"
                  min={1}
                  max={43200}
                  bind:value={status.schedule.every_min}
                  aria-label="自定义间隔（分钟）"
                  class="q-input w-24"
                />
              </label>
            {:else}
              <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
                时间
                <input
                  type="time"
                  bind:value={status.schedule.at}
                  aria-label="执行时间"
                  class="q-input"
                />
              </label>
            {/if}

            <label class="flex items-center gap-1.5 text-xs" style="color: var(--q-muted);">
              保留
              <input
                type="number"
                min={1}
                max={200}
                bind:value={status.schedule.keep}
                aria-label="保留份数"
                class="q-input w-20"
              />
              份
            </label>

            <button class="q-btn q-btn--primary" disabled={autoSaving} onclick={saveAuto}>
              {autoSaving ? '保存中…' : '保存'}
            </button>
          </div>

          <!-- 执行状态 -->
          <div class="q-stack--tight flex flex-col text-xs" style="color: var(--q-muted);">
            {#if auto.enabled && auto.next_run}
              <p>
                下次执行：<span class="font-mono">{fmtTime(auto.next_run)}</span
                >（{auto.next_run_in}）· {auto.next_hint}
              </p>
            {:else}
              <p>下次执行：已关闭</p>
            {/if}
            <p>上次执行：{auto.last_run ? fmtTime(auto.last_run) : '尚未执行'}</p>
            {#if auto.last_error}
              <p class="q-badge q-badge--danger w-fit">
                <AlertTriangle size={12} />
                <span>上次备份失败：{auto.last_error}</span>
              </p>
            {/if}
            <p>归档目录：<span class="font-mono">{auto.backups_dir}</span></p>
          </div>
        </div>
      {:else}
        <p class="q-hint !mt-0">读取自动备份配置失败，请检查服务是否运行。</p>
      {/if}
      <div class="q-divider"></div>
      <h3 class="q-label">回收站自动清理</h3>
      <div class="flex flex-wrap items-center gap-3">
        <div class="q-seg q-seg--fill">
          {#each TRASH_DAYS_TABS as t (t.v)}
            <button
              onclick={() => setTrashDays(t.v)}
              aria-pressed={trashDays === t.v}
              class="q-seg__btn"
            >
              {t.n}
            </button>
          {/each}
        </div>
        <span class="text-xs" style="color: var(--q-muted);">
          {#if trashDays === '0'}
            关闭中：回收站里的记录一直留着，直到手动清空
          {:else}
            软删除超过 {trashDays} 天的记录每小时自动真删除，未过期的仍可恢复
          {/if}
        </span>
      </div>
      <div class="q-divider"></div>
      <h3 class="q-label">明细导出与全量迁移</h3>
      <div class="flex flex-wrap items-center gap-2">
        <select bind:value={csvWhat} class="q-select">
          <option value="events">往来明细</option>
          <option value="transactions">金钱明细</option>
          <option value="memos">对话明细</option>
        </select>
        <input
          type="number"
          min="1900"
          max="2200"
          bind:value={csvYear}
          placeholder="年份（全部）"
          class="q-input w-28"
        />
        <div class="w-40">
          <PersonPicker bind:value={csvPerson} placeholder="全部人物" compact={true} />
        </div>
        {#if csvWhat === 'transactions'}
          <select bind:value={csvEvent} class="q-select max-w-[12rem]">
            <option value="">全部往来</option>
            {#each eventOptions as ev (ev.id)}<option value={ev.id}>{ev.title}</option>{/each}
          </select>
        {/if}
        <button class="q-btn q-btn--primary" onclick={exportCsv}>
          <Download size={14} /> 导出 CSV
        </button>
      </div>
      <p class="q-hint">
        CSV 带 BOM，Excel 双击打开不乱码；金额按「元」导出。选一场往来即可只导它的礼单。
      </p>
      <div class="mt-3 flex flex-wrap gap-2">
        <button class="q-btn q-btn--ghost" onclick={exportJson}>
          <Download size={14} /> 全量导出 JSON
        </button>
        <input
          type="file"
          accept=".json,application/json"
          class="hidden"
          bind:this={jsonInput}
          onchange={pickJson}
        />
        <button class="q-btn q-btn--danger-ghost" onclick={() => jsonInput?.click()}>
          <Upload size={14} /> 从 JSON 导入（覆盖全库）
        </button>
      </div>
      {#if ioMsg}
        <p class="q-hint flex items-center gap-1">
          <Check size={12} />
          {ioMsg}
        </p>
      {/if}
    </section>

    <section class="q-card">
      <h2 class="q-section-title">
        <KeyRound size={15} /> 访问与安全
      </h2>
      <h3 class="q-label">访问令牌（可选）</h3>
      <div class="flex gap-2">
        <input
          bind:value={token}
          type="password"
          placeholder="服务端 QIANSI_TOKEN，未设置则留空"
          class="q-input flex-1"
        />
        <button class="q-btn q-btn--primary" onclick={saveToken}>保存</button>
      </div>
      <p class="q-hint">
        服务端设置 QIANSI_TOKEN 后，所有接口都需要此令牌；跨站访问默认已被 CORS 拒绝。
      </p>
    </section>
  </div>
</div>
