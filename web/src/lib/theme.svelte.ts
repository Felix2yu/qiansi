// 主题外观：浅色 / 深色 / 跟随系统。
//
// 偏好持久化在两处，各有职责：
//   - localStorage：同步可读，首屏渲染前就能应用，避免刷新时白闪；
//   - 服务端 settings 表：跨设备/跨浏览器共享，且能随数据库备份一起带走。
// 启动时以服务端为准（若已拉取），否则用本地值；用户切换时两边同时写。

import { API } from './api'

export type ThemeMode = 'light' | 'dark' | 'system'
export const THEME_MODES: ThemeMode[] = ['system', 'light', 'dark']

export const THEME_LABEL: Record<ThemeMode, string> = {
  system: '跟随系统',
  light: '浅色',
  dark: '深色',
}

// 推荐主题色：主题色在界面里主要当按钮/选中态的底色用，上面压白字，
// 所以一律挑深色（对白字对比度 ≥ 3.9），浅粉浅黄这类点下去按钮就看不清了。
export const THEME_PRESETS = [
  { name: '靛蓝（默认）', color: '#6366f1' },
  { name: '紫罗兰', color: '#7c3aed' },
  { name: '海蓝', color: '#0284c7' },
  { name: '青碧', color: '#0d9488' },
  { name: '松绿', color: '#059669' },
  { name: '赤红', color: '#dc2626' },
  { name: '玫红', color: '#db2777' },
  { name: '石墨', color: '#475569' },
] as const

/** 当前主题色是否就是某个推荐色（取色器可能存成 #RRGGBB 大写，先归一） */
export function isPreset(color: string): boolean {
  const c = color.trim().toLowerCase()
  return THEME_PRESETS.some(p => p.color === c)
}

// localStorage 键。沿用既有的 q_dark（历史值 '1'/'0'）以兼容老用户：
// 迁移时若没有 q_theme_mode，但有 q_dark，则按其值折算成 light/dark。
const KEY_MODE = 'q_theme_mode'
const KEY_COLOR = 'q_theme'

const VALID: ThemeMode[] = ['light', 'dark', 'system']

function readMode(): ThemeMode {
  const raw = localStorage.getItem(KEY_MODE)
  if (raw && (VALID as string[]).includes(raw)) return raw as ThemeMode
  const legacy = localStorage.getItem('q_dark')
  if (legacy === '1') return 'dark'
  if (legacy === '0') return 'light'
  return 'system'
}

export const theme = $state<{ mode: ThemeMode; color: string }>({
  mode: readMode(),
  color: localStorage.getItem(KEY_COLOR) || '#6366f1',
})

const mql = typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null

/** 当前是否处于深色（mode=system 时取系统值） */
export function isDark(): boolean {
  if (theme.mode === 'system') return !!mql?.matches
  return theme.mode === 'dark'
}

/** 把当前状态写进 DOM。首屏与每次切换都调它，逻辑只有这一份。 */
export function applyTheme() {
  const root = document.documentElement
  root.classList.toggle('dark', isDark())
  root.style.colorScheme = isDark() ? 'dark' : 'light'
  root.style.setProperty('--q-theme', theme.color)
  // 移动端浏览器地址栏/状态栏跟随页面配色
  const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
  if (meta) meta.content = isDark() ? '#0b1020' : theme.color
}

/**
 * 启动主题：先按本地值立即应用（同步，消除首屏闪白），
 * 再从服务端拉取偏好并覆盖（跨设备一致）。
 */
export async function initTheme() {
  applyTheme()
  // 系统外观变化时实时跟随（仅 system 模式）
  mql?.addEventListener?.('change', () => {
    if (theme.mode === 'system') applyTheme()
  })
  try {
    const st = await API.get<Record<string, string>>('/api/v1/settings')
    const mode = st['theme_mode']
    const color = st['theme_color']
    if (mode && (VALID as string[]).includes(mode)) theme.mode = mode as ThemeMode
    if (color) theme.color = color
    applyTheme()
  } catch {
    // 后端不可达时保持本地偏好，不阻塞渲染
  }
}

/** 下一个外观档位：system → light → dark → system。移动端单击循环用。 */
export function nextMode(mode: ThemeMode): ThemeMode {
  const i = THEME_MODES.indexOf(mode)
  return THEME_MODES[(i + 1) % THEME_MODES.length]
}

/** 切换外观并持久化（本地 + 服务端）。 */
export async function setThemeMode(mode: ThemeMode) {
  theme.mode = mode
  localStorage.setItem(KEY_MODE, mode)
  // 同步旧的 q_dark，避免旧逻辑（如历史代码）读出矛盾状态
  localStorage.setItem('q_dark', isDark() ? '1' : '0')
  applyTheme()
  try {
    await API.put('/api/v1/settings/theme_mode', { value: mode })
  } catch {
    // 本地已生效；服务端失败仅影响跨设备同步，不回滚当前选择
  }
}

/** 修改主题色并持久化。 */
export async function setThemeColor(color: string) {
  theme.color = color
  localStorage.setItem(KEY_COLOR, color)
  applyTheme()
  try {
    await API.put('/api/v1/settings/theme_color', { value: color })
  } catch {
    // 同上，本地已生效
  }
}
