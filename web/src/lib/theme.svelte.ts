// 主题外观：浅色 / 深色 / 跟随系统。
//
// 偏好持久化在两处，各有职责：
//   - localStorage：同步可读，首屏渲染前就能应用，避免刷新时白闪；
//   - 服务端 settings 表：跨设备/跨浏览器共享，且能随数据库备份一起带走。
// 启动时以服务端为准（若已拉取），否则用本地值；用户切换时两边同时写。

import { API } from './api';
import { applyStartPage } from './router';

export type ThemeMode = 'light' | 'dark' | 'system';
export const THEME_MODES: ThemeMode[] = ['system', 'light', 'dark'];

export const THEME_LABEL: Record<ThemeMode, string> = {
  system: '跟随系统',
  light: '浅色',
  dark: '深色'
};

// 推荐主题色：主题色在界面里主要当按钮/选中态的底色用，上面压白字，
// 所以一律挑深色（对白字对比度 ≥ 3.9），浅粉浅黄这类点下去按钮就看不清了。
export const THEME_PRESETS = [
  { name: '沉绛（默认）', color: '#9E2A2B' },
  { name: '紫罗兰', color: '#7c3aed' },
  { name: '海蓝', color: '#0284c7' },
  { name: '青碧', color: '#0d9488' },
  { name: '松绿', color: '#059669' },
  { name: '赤红', color: '#dc2626' },
  { name: '玫红', color: '#db2777' },
  { name: '石墨', color: '#475569' }
] as const;

/** 当前主题色是否就是某个推荐色（取色器可能存成 #RRGGBB 大写，先归一） */
export function isPreset(color: string): boolean {
  const c = color.trim().toLowerCase();
  return THEME_PRESETS.some((p) => p.color === c);
}

// ---------- 字号档位 ----------
// 以根字号（html font-size）整体缩放：全站文字与间距都是 rem，
// 等比缩放不会破坏对齐。标准档 = 16px（正文 14px），各档上下浮动。
export type FontScale = 's' | 'm' | 'l' | 'xl';

export const FONT_SCALE_LABEL: Record<FontScale, string> = {
  s: '小',
  m: '标准',
  l: '大',
  xl: '特大'
};

const FONT_SCALE_PX: Record<FontScale, string> = {
  s: '15px',
  m: '16px',
  l: '17px',
  xl: '18px'
};

const VALID_SCALE: FontScale[] = ['s', 'm', 'l', 'xl'];

// ---------- 字体族 ----------
// default = 设计默认（标题衬线 / 正文非衬线）；serif / sans 把两者统一成一种。
// 实现走 CSS 变量重定向：衬线模式下让 --font-sans 指向衬线栈，
// 非衬线模式下让 --font-serif 指向非衬线栈，组件代码零改动。
export type FontFamily = 'default' | 'serif' | 'sans';

export const FONT_FAMILY_LABEL: Record<FontFamily, string> = {
  default: '默认',
  serif: '衬线',
  sans: '非衬线'
};

const VALID_FAMILY: FontFamily[] = ['default', 'serif', 'sans'];

// ---------- 启动页 ----------
// 打开牵丝落在哪个页面。白名单收口，防存储脏值把路由带进未知路径。
export const START_PAGES = [
  { path: '/', label: '今日' },
  { path: '/people', label: '人物' },
  { path: '/events', label: '往来' },
  { path: '/money', label: '金钱' },
  { path: '/timeline', label: '时间线' },
  { path: '/analytics', label: '统计' }
] as const;

export type StartPage = (typeof START_PAGES)[number]['path'];

const START_PATHS: string[] = START_PAGES.map((p) => p.path);

function readStartPage(): string {
  const raw = localStorage.getItem('q_start_page');
  return raw && START_PATHS.includes(raw) ? raw : '/';
}

// ---------- 今日页模块 ----------
// 首页卡片可整块收起：不是每块每人都看。键名白名单收口，防存储脏值。
export const TODAY_MODULES = [
  { key: 'stats', label: '数据统计' },
  { key: 'money', label: '借出待还' },
  { key: 'suggestions', label: '智能建议' },
  { key: 'reminders', label: '近期待办' }
] as const;

export type TodayModuleKey = (typeof TODAY_MODULES)[number]['key'];

const TODAY_KEYS: string[] = TODAY_MODULES.map((m) => m.key);

/** 服务端/本地存的是逗号分隔的隐藏键，解析时丢掉不认识的 */
export function parseTodayHidden(raw: string | null | undefined): string[] {
  if (!raw) return [];
  return raw.split(',').filter((k) => TODAY_KEYS.includes(k));
}

// localStorage 键。沿用既有的 q_dark（历史值 '1'/'0'）以兼容老用户：
// 迁移时若没有 q_theme_mode，但有 q_dark，则按其值折算成 light/dark。
const KEY_MODE = 'q_theme_mode';
const KEY_COLOR = 'q_theme';
const KEY_SCALE = 'q_font_scale';
const KEY_FAMILY = 'q_font_family';
const KEY_START = 'q_start_page';
const KEY_MASK = 'q_money_mask';
const KEY_TODAY = 'q_today_hidden';
const KEY_ONBOARD = 'q_onboard_off';

const VALID: ThemeMode[] = ['light', 'dark', 'system'];

function readMode(): ThemeMode {
  const raw = localStorage.getItem(KEY_MODE);
  if (raw && (VALID as string[]).includes(raw)) return raw as ThemeMode;
  const legacy = localStorage.getItem('q_dark');
  if (legacy === '1') return 'dark';
  if (legacy === '0') return 'light';
  return 'system';
}

function readScale(): FontScale {
  const raw = localStorage.getItem(KEY_SCALE);
  return raw && (VALID_SCALE as string[]).includes(raw) ? (raw as FontScale) : 'm';
}

function readFamily(): FontFamily {
  const raw = localStorage.getItem(KEY_FAMILY);
  return raw && (VALID_FAMILY as string[]).includes(raw) ? (raw as FontFamily) : 'default';
}

export const theme = $state<{
  mode: ThemeMode;
  color: string;
  fontScale: FontScale;
  fontFamily: FontFamily;
  startPage: string;
  maskMoney: boolean;
  todayHidden: string[];
  onboardHidden: boolean;
}>({
  mode: readMode(),
  color: localStorage.getItem(KEY_COLOR) || '#9E2A2B',
  fontScale: readScale(),
  fontFamily: readFamily(),
  startPage: readStartPage(),
  maskMoney: localStorage.getItem(KEY_MASK) === '1',
  todayHidden: parseTodayHidden(localStorage.getItem(KEY_TODAY)),
  // 首启引导的「不用了」：只写本机的话，换台设备它又赖回首屏
  onboardHidden: localStorage.getItem(KEY_ONBOARD) === '1'
});

const mql = typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null;

/** 当前是否处于深色（mode=system 时取系统值） */
export function isDark(): boolean {
  if (theme.mode === 'system') return !!mql?.matches;
  return theme.mode === 'dark';
}

/**
 * 暗色模式下把主题色提亮一档：沉绛 #9E2A2B 压在墨色暗底（#15120e）上可见度偏低，
 * 换成手调的亮绛 #C0392B 才看得清选中态/按钮描边。其他色暂沿用原值。
 */
function darkAccent(color: string): string {
  return color.trim().toLowerCase() === '#9e2a2b' ? '#c0392b' : color;
}

/** 把当前状态写进 DOM。首屏与每次切换都调它，逻辑只有这一份。 */
export function applyTheme() {
  const root = document.documentElement;
  root.classList.toggle('dark', isDark());
  root.style.colorScheme = isDark() ? 'dark' : 'light';
  root.style.setProperty('--q-theme', isDark() ? darkAccent(theme.color) : theme.color);
  // 字号：标准档清掉内联值，交给样式表的默认；其余档位整体缩放
  if (theme.fontScale === 'm') root.style.removeProperty('font-size');
  else root.style.fontSize = FONT_SCALE_PX[theme.fontScale];
  // 字体族：重定向 CSS 变量，标题/正文一起换，不重复维护字体栈
  if (theme.fontFamily === 'serif') {
    root.style.setProperty('--font-sans', 'var(--font-serif)');
    root.style.removeProperty('--font-serif');
  } else if (theme.fontFamily === 'sans') {
    root.style.setProperty('--font-serif', 'var(--font-sans)');
    root.style.removeProperty('--font-sans');
  } else {
    root.style.removeProperty('--font-serif');
    root.style.removeProperty('--font-sans');
  }
  // 金额打码：blur 由 CSS 按 .q-money 生效
  root.classList.toggle('q-money-mask', theme.maskMoney);
  // 移动端浏览器地址栏/状态栏跟随页面配色
  const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  if (meta) meta.content = isDark() ? '#15120e' : theme.color;
}

// initTheme 的竞态防护：请求发出后用户又改过的键，响应回来不再覆盖。
// 没有这层守卫，「点侧栏眼睛 → GET 返回旧的服务端值 → 界面弹回旧值」
// 在慢网络与设置页（每次 mount 都调一次 initTheme）下稳定复现。
// 集合按整个页面会话累计：用户表达过一次意图就不再跟 TA 抢；
// 每次 PUT 成功后服务端值已同步为用户所选，跳过应用也不会造成偏差。
const userTouched = new Set<string>();

let systemThemeBound = false;

/**
 * 启动主题：先按本地值立即应用（同步，消除首屏闪白），
 * 再从服务端拉取偏好并覆盖（跨设备一致）。
 */
export async function initTheme() {
  applyTheme();
  // 系统外观变化时实时跟随（仅 system 模式）。App 与设置页都会调 initTheme，
  // 不加幂等守卫的话每进一次设置页就多挂一个 listener。
  if (!systemThemeBound) {
    systemThemeBound = true;
    mql?.addEventListener?.('change', () => {
      if (theme.mode === 'system') applyTheme();
    });
  }
  try {
    const st = await API.get<Record<string, string>>('/api/v1/settings');
    const mode = st['theme_mode'];
    const color = st['theme_color'];
    const scale = st['font_scale'];
    const family = st['font_family'];
    const start = st['start_page'];
    const mask = st['money_mask'];
    const todayHidden = st['today_hidden'];
    const onboardHidden = st['onboard_hidden'];
    // 应用服务端值的同时回写 localStorage：下次刷新首屏直接用新值渲染，
    // 不再「先按旧本地值出一帧、再被服务端值纠正」地闪一下
    if (!userTouched.has('mode') && mode && (VALID as string[]).includes(mode)) {
      theme.mode = mode as ThemeMode;
      localStorage.setItem(KEY_MODE, mode);
    }
    if (!userTouched.has('color') && color) {
      theme.color = color;
      localStorage.setItem(KEY_COLOR, color);
    }
    if (!userTouched.has('fontScale') && scale && (VALID_SCALE as string[]).includes(scale)) {
      theme.fontScale = scale as FontScale;
      localStorage.setItem(KEY_SCALE, scale);
    }
    if (!userTouched.has('fontFamily') && family && (VALID_FAMILY as string[]).includes(family)) {
      theme.fontFamily = family as FontFamily;
      localStorage.setItem(KEY_FAMILY, family);
    }
    if (!userTouched.has('startPage') && start && START_PATHS.includes(start)) {
      theme.startPage = start;
      localStorage.setItem(KEY_START, start);
    }
    if (!userTouched.has('maskMoney') && (mask === '1' || mask === '0')) {
      theme.maskMoney = mask === '1';
      localStorage.setItem(KEY_MASK, mask);
    }
    if (!userTouched.has('todayHidden') && todayHidden !== undefined) {
      theme.todayHidden = parseTodayHidden(todayHidden);
      const joined = theme.todayHidden.join(',');
      if (joined) localStorage.setItem(KEY_TODAY, joined);
      else localStorage.removeItem(KEY_TODAY);
    }
    if (!userTouched.has('onboardHidden') && (onboardHidden === '1' || onboardHidden === '0')) {
      theme.onboardHidden = onboardHidden === '1';
      localStorage.setItem(KEY_ONBOARD, onboardHidden);
    }
    applyTheme();
    // 服务端带来的启动页若与本地不同，且用户还停在 / 没动过，补一次重定向
    applyStartPage(theme.startPage);
  } catch {
    // 后端不可达时保持本地偏好，不阻塞渲染
  }
}

/** 下一个外观档位：system → light → dark → system。移动端单击循环用。 */
export function nextMode(mode: ThemeMode): ThemeMode {
  const i = THEME_MODES.indexOf(mode);
  return THEME_MODES[(i + 1) % THEME_MODES.length];
}

/** 切换外观并持久化（本地 + 服务端）。 */
export async function setThemeMode(mode: ThemeMode) {
  userTouched.add('mode');
  theme.mode = mode;
  localStorage.setItem(KEY_MODE, mode);
  // 同步旧的 q_dark，避免旧逻辑（如历史代码）读出矛盾状态
  localStorage.setItem('q_dark', isDark() ? '1' : '0');
  applyTheme();
  try {
    await API.put('/api/v1/settings/theme_mode', { value: mode });
  } catch {
    // 本地已生效；服务端失败仅影响跨设备同步，不回滚当前选择
  }
}

/** 修改主题色并持久化。 */
export async function setThemeColor(color: string) {
  userTouched.add('color');
  theme.color = color;
  localStorage.setItem(KEY_COLOR, color);
  applyTheme();
  try {
    await API.put('/api/v1/settings/theme_color', { value: color });
  } catch {
    // 同上，本地已生效
  }
}

/** 切换字号档位并持久化（本地 + 服务端）。 */
export async function setFontScale(scale: FontScale) {
  userTouched.add('fontScale');
  theme.fontScale = scale;
  localStorage.setItem(KEY_SCALE, scale);
  applyTheme();
  try {
    await API.put('/api/v1/settings/font_scale', { value: scale });
  } catch {
    // 本地已生效；服务端失败仅影响跨设备同步，不回滚当前选择
  }
}

/** 切换字体族并持久化（本地 + 服务端）。 */
export async function setFontFamily(family: FontFamily) {
  userTouched.add('fontFamily');
  theme.fontFamily = family;
  localStorage.setItem(KEY_FAMILY, family);
  applyTheme();
  try {
    await API.put('/api/v1/settings/font_family', { value: family });
  } catch {
    // 同上，本地已生效
  }
}

/** 切换启动页并持久化。非「今日」落点且当前停在 / 时当场跳转；改回「今日」则下次进站生效。 */
export async function setStartPage(path: StartPage) {
  userTouched.add('startPage');
  theme.startPage = path;
  localStorage.setItem(KEY_START, path);
  try {
    await API.put('/api/v1/settings/start_page', { value: path });
  } catch {
    // 本地已生效；服务端失败仅影响跨设备同步
  }
  applyStartPage(path);
}

/** 切换金额打码并持久化（本地 + 服务端）。顶栏眼睛图标与设置页共用。 */
export async function setMoneyMask(mask: boolean) {
  userTouched.add('maskMoney');
  theme.maskMoney = mask;
  localStorage.setItem(KEY_MASK, mask ? '1' : '0');
  applyTheme();
  try {
    await API.put('/api/v1/settings/money_mask', { value: mask ? '1' : '0' });
  } catch {
    // 同上，本地已生效
  }
}

/** 收起/放回今日页的一块卡片并持久化（本地 + 服务端）。 */
export async function setTodayModule(key: TodayModuleKey, visible: boolean) {
  userTouched.add('todayHidden');
  const next = theme.todayHidden.filter((k) => k !== key);
  if (!visible) next.push(key);
  theme.todayHidden = next;
  const joined = next.join(',');
  if (joined) localStorage.setItem(KEY_TODAY, joined);
  else localStorage.removeItem(KEY_TODAY);
  try {
    await API.put('/api/v1/settings/today_hidden', { value: joined });
  } catch {
    // 同上，本地已生效
  }
}

/** 首启引导「不用了」：本机先收，再同步服务端，换设备不再重现。 */
export async function setOnboardHidden(hidden: boolean) {
  userTouched.add('onboardHidden');
  theme.onboardHidden = hidden;
  if (hidden) localStorage.setItem(KEY_ONBOARD, '1');
  else localStorage.removeItem(KEY_ONBOARD);
  try {
    await API.put('/api/v1/settings/onboard_hidden', { value: hidden ? '1' : '' });
  } catch {
    // 同上，本地已生效
  }
}
