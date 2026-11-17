<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import { type WithElementRef } from '@nogon/components/utils';
  import Callout from './callout.svelte';

  let {
    ref = $bindable(null),
    title,
    children,
    ...restProps
  }: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
    title?: string;
    children?: Snippet;
  } = $props();
</script>

<Callout bind:ref variant="tip" {title} {...restProps}>
  {@render children?.()}
</Callout>
