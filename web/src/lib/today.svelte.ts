// 「今天」的共享响应式源：跨午夜后所有引用处自动跟上。
//
// 之前 Today/Reminders 各自在组件里 const today = todayLocal()，
// 长开的标签页过完午夜还拿着昨天的日期分桶「逾期/今天」，对提醒类应用是口径错误。
// 每分钟对一次表 + 回到前台时立即对表，翻转时 $derived 自动重算。
import { todayLocal } from './api';

let today = $state(todayLocal());

function refresh() {
  const now = todayLocal();
  if (now !== today) today = now;
}

if (typeof window !== 'undefined') {
  setInterval(refresh, 60_000);
  document.addEventListener('visibilitychange', refresh);
}

/** 今天的 YYYY-MM-DD（响应式：跨午夜自动更新） */
export function todayStr(): string {
  return today;
}
