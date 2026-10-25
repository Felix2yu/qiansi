<script lang="ts">
  import { AlertTriangle, RotateCw } from '@lucide/svelte';

  /** 全站统一加载失败态：与空状态明确区分——「没有数据」和「没读到数据」
   *  是两回事，混为一谈会让用户以为记录丢了。 */
  let {
    text = '加载失败，请重试',
    onretry
  }: {
    text?: string;
    onretry?: () => void;
  } = $props();
</script>

<div
  class="q-empty"
  style="border-style: solid; border-color: color-mix(in srgb, var(--q-danger) 45%, var(--q-border));"
>
  <AlertTriangle
    class="q-empty__icon"
    size={26}
    strokeWidth={1.5}
    style="color: var(--q-danger);"
  />
  <div class="q-empty__text" style="color: var(--q-danger);">{text}</div>
  {#if onretry}
    <button class="q-btn q-btn--ghost q-btn--sm" onclick={() => onretry()}>
      <RotateCw size={14} /> 重试
    </button>
  {/if}
</div>
