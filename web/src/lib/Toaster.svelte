<script lang="ts">
  import { toasts, dismiss, runUndo } from './toast.svelte';
  import { Check, X, Info, AlertTriangle } from '@lucide/svelte';

  // 反馈条：成功/失败/可撤销都走这里，不再用原生 alert 打断整个页面。
  const ICON = { ok: Check, error: AlertTriangle, info: Info } as const;
  const COLOR = {
    ok: 'var(--q-ok)',
    error: 'var(--q-danger)',
    info: 'var(--q-theme)'
  } as const;
</script>

<div
  class="pointer-events-none fixed inset-x-0 bottom-4 z-[80] flex flex-col items-center gap-2 px-4"
  role="status"
  aria-live="polite"
>
  {#each toasts as t (t.id)}
    {@const Icon = ICON[t.kind]}
    <div
      class="toast-item pointer-events-auto flex w-full max-w-md items-start gap-2.5 rounded-[var(--q-radius)] px-3.5 py-2.5 text-sm"
      style="background: var(--q-surface); border: 1px solid var(--q-border); box-shadow: var(--q-shadow-pop);"
    >
      <span class="mt-0.5 shrink-0" style="color: {COLOR[t.kind]};"><Icon size={15} /></span>
      <span class="min-w-0 flex-1" style="word-break: break-word;">{t.text}</span>
      {#if t.onUndo}
        <button
          class="q-btn q-btn--sm q-btn--ghost shrink-0 !px-2 !py-0.5"
          onclick={() => runUndo(t)}>{t.undoLabel}</button
        >
      {/if}
      <button
        class="q-iconbtn !h-6 !w-6 shrink-0"
        title="关闭"
        aria-label="关闭提示"
        onclick={() => dismiss(t.id)}><X size={14} /></button
      >
    </div>
  {/each}
</div>

<style>
  .toast-item {
    animation: q-toast-in 0.18s cubic-bezier(0.22, 0.8, 0.3, 1);
  }
  @keyframes q-toast-in {
    from {
      opacity: 0;
      transform: translateY(10px);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }
</style>
