<script lang="ts">
  import { toasts, dismiss, runUndo } from './toast.svelte'
  import { Check, X, Info, AlertTriangle } from '@lucide/svelte'

  // 反馈条：成功/失败/可撤销都走这里，不再用原生 alert 打断整个页面。
  const ICON = { ok: Check, error: AlertTriangle, info: Info } as const
  const COLOR = {
    ok: 'var(--q-ok)',
    error: 'var(--q-danger)',
    info: 'var(--q-theme)',
  } as const
</script>

<div class="fixed inset-x-0 bottom-4 z-[60] flex flex-col items-center gap-2 px-4 pointer-events-none"
     role="status" aria-live="polite">
  {#each toasts as t (t.id)}
    {@const Icon = ICON[t.kind]}
    <div class="toast-item pointer-events-auto w-full max-w-md rounded-xl px-3 py-2 flex items-start gap-2 text-sm"
         style="background: var(--q-surface); border: 1px solid var(--q-border); box-shadow: 0 8px 24px rgba(0,0,0,.16);">
      <span class="shrink-0 mt-0.5" style="color: {COLOR[t.kind]};"><Icon size={15} /></span>
      <span class="flex-1 min-w-0" style="word-break: break-word;">{t.text}</span>
      {#if t.onUndo}
        <button class="shrink-0 px-2 py-0.5 rounded-md text-sm font-medium"
                style="color: var(--q-theme); background: var(--q-bg);"
                onclick={() => runUndo(t)}>{t.undoLabel}</button>
      {/if}
      <button class="shrink-0 p-0.5" style="color: var(--q-muted);" title="关闭" aria-label="关闭提示"
              onclick={() => dismiss(t.id)}><X size={14} /></button>
    </div>
  {/each}
</div>

<style>
  .toast-item { animation: q-toast-in .16s ease-out; }
  @keyframes q-toast-in {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: none; }
  }
</style>
