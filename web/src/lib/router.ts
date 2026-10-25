import { writable } from 'svelte/store';

export type Route = { path: string; params: Record<string, string>; query: Record<string, string> };

export const route = writable<Route>(parse(location));

// URL 里出现未转义的 '%'（手输 ?q=100%、IM 粘贴的坏链接）时
// decodeURIComponent 会抛 URIError；它在模块顶层就执行，炸了整站白屏。
function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

function parse(loc: Location): Route {
  // 查询串在 location.search 里；pathname 永远不含 '?'，从它身上切只会拿到空串
  const p = loc.pathname;
  const qs = loc.search.replace(/^\?/, '');
  const query: Record<string, string> = {};
  for (const kv of qs.split('&')) {
    if (!kv) continue;
    const [k, v = ''] = kv.split('=');
    query[safeDecode(k)] = safeDecode(v);
  }
  return { path: p || '/', params: {}, query };
}

export function navigate(to: string) {
  if (!to.startsWith('/')) to = '/' + to;
  history.pushState({}, '', to);
  route.set(parse(location));
}

window.addEventListener('popstate', () => route.set(parse(location)));

/**
 * 启动页重定向：用户把「今日」以外的页面设为启动落点时，
 * 在首帧渲染前把 / 悄悄换成目标路径（replaceState，不产生历史记录）。
 * 首次进站（本地无偏好）时由 theme 的服务端同步再调一次。
 */
export function applyStartPage(path: string | null | undefined) {
  if (!path || path === '/') return;
  if (location.pathname !== '/' && location.pathname !== '') return;
  history.replaceState({}, '', path);
  route.set(parse(location));
}

// simple hash-less path matcher
export function match(pattern: string, path: string): Record<string, string> | null {
  const pp = pattern.split('/').filter(Boolean);
  const rp = path.split('/').filter(Boolean);
  if (pp.length !== rp.length) return null;
  const params: Record<string, string> = {};
  for (let i = 0; i < pp.length; i++) {
    if (pp[i].startsWith(':')) params[pp[i].slice(1)] = safeDecode(rp[i]);
    else if (pp[i] !== rp[i]) return null;
  }
  return params;
}
