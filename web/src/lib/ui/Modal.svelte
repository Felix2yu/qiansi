<script lang="ts">
  import X from '@lucide/svelte/icons/x';
  import type { Snippet } from 'svelte';

  /**
   * 全站统一弹窗：半透明宣纸遮罩 + 纸卡面板 + 宋体标题。
   * - title：标准标题行（自带关闭按钮）
   * - head：自定义标题区（传入后不渲染关闭按钮，由调用方自行处理）
   * - foot：底部操作区（带上分隔线）
   * Esc / 点击遮罩统一触发 onclose
   * 焦点圈在面板内：Tab 不会绕到背后的页面上（ aria-modal 已经这么承诺了）
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

  let panel = $state<HTMLDivElement | null>(null);

  // 可聚焦元素都算，包含 disabled 之外的隐藏项要剔掉（hidden 的 tab 不该停）
  const FOCUSABLE =
    'a[href],button:not([disabled]),input:not([disabled]):not([type="hidden"]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

  function onKey(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === 'Escape') {
      onclose?.();
      return;
    }
    if (e.key !== 'Tab' || !panel) return;
    const items = [...panel.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(
      (el) => el.offsetParent !== null
    );
    if (items.length === 0) return;
    const first = items[0];
    const last = items[items.length - 1];
    const active = document.activeElement as HTMLElement | null;
    // 焦点在面板外（或干脆在 body 上）时，Tab 从第一个开始，别放它跑回页面
    if (!active || !panel.contains(active)) {
      e.preventDefault();
      (e.shiftKey ? last : first).focus();
      return;
    }
    if (e.shiftKey && active === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
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
    <div class="q-modal__panel" role="dialog" aria-modal="true" bind:this={panel} tabindex="-1">
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
