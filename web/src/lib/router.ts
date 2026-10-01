import { writable } from 'svelte/store'

export type Route = { path: string; params: Record<string, string>; query: Record<string, string> }

export const route = writable<Route>(parse(location))

function parse(loc: Location): Route {
  // 查询串在 location.search 里；pathname 永远不含 '?'，从它身上切只会拿到空串
  const p = loc.pathname
  const qs = loc.search.replace(/^\?/, '')
  const query: Record<string, string> = {}
  for (const kv of qs.split('&')) {
    if (!kv) continue
    const [k, v = ''] = kv.split('=')
    query[decodeURIComponent(k)] = decodeURIComponent(v)
  }
  return { path: p || '/', params: {}, query }
}

export function navigate(to: string) {
  if (!to.startsWith('/')) to = '/' + to
  history.pushState({}, '', to)
  route.set(parse(location))
}

window.addEventListener('popstate', () => route.set(parse(location)))

// simple hash-less path matcher
export function match(pattern: string, path: string): Record<string, string> | null {
  const pp = pattern.split('/').filter(Boolean)
  const rp = path.split('/').filter(Boolean)
  if (pp.length !== rp.length) return null
  const params: Record<string, string> = {}
  for (let i = 0; i < pp.length; i++) {
    if (pp[i].startsWith(':')) params[pp[i].slice(1)] = decodeURIComponent(rp[i])
    else if (pp[i] !== rp[i]) return null
  }
  return params
}
