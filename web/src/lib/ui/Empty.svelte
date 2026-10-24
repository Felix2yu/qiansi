<script lang="ts">
  import type { Component } from 'svelte';
  import type { Snippet } from 'svelte';

  /** 全站统一空状态：虚线纸卡 + 淡图标 + 说明 + 可选操作 */
  let {
    icon = undefined,
    text,
    actions
  }: {
    icon?: Component;
    text: string;
    actions?: Snippet;
  } = $props();
</script>

<div class="q-empty">
  {#if icon}
    {@const Icon = icon}
    <Icon class="q-empty__icon" size={26} strokeWidth={1.5} />
  {/if}
  <div class="q-empty__text">{text}</div>
  {#if actions}
    <div class="mt-1 flex flex-wrap justify-center gap-2">
      {@render actions()}
    </div>
  {/if}
</div>
