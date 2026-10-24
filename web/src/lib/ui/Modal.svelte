<script lang="ts">
  import X from '@lucide/svelte/icons/x';
  import type { Snippet } from 'svelte';

  /**
   * 全站统一弹窗：半透明宣纸遮罩 + 纸卡面板 + 宋体标题。
   * - title：标准标题行（自带关闭按钮）
   * - head：自定义标题区（传入后不渲染关闭按钮，由调用方自行处理）
   * - foot：底部操作区（带上分隔线）
   * Esc / 点击遮罩统一触发 onclose
   */
  let {
    open = true,
    onclose,
    title = '',
    wide = false,
    head = undefined,
    foot = undefined,
    children
  }: {
    open?: boolean;
    onclose?: () => void;
    title?: string;
    wide?: boolean;
    head?: Snippet;
    foot?: Snippet;
    children: Snippet;
  } = $props();

  function onKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') onclose?.();
  }

  $effect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = prev;
    };
  });
</script>

<svelte:window onkeydown={onKey} />

{#if open}
  <div class="q-modal" class:q-modal--wide={wide} role="presentation">
    <button type="button" class="q-modal__veil" aria-label="关闭弹窗" onclick={() => onclose?.()}
    ></button>
    <div class="q-modal__panel" role="dialog" aria-modal="true">
      {#if head}
        <div class="q-modal__head">{@render head()}</div>
      {:else if title}
        <div class="q-modal__head">
          <h2 class="q-modal__title">{title}</h2>
          <button type="button" class="q-iconbtn" aria-label="关闭弹窗" onclick={() => onclose?.()}>
            <X size={17} />
          </button>
        </div>
      {/if}
      <div class="q-modal__body">
        {@render children()}
      </div>
      {#if foot}
        <div class="q-modal__foot">{@render foot()}</div>
      {/if}
    </div>
  </div>
{/if}
