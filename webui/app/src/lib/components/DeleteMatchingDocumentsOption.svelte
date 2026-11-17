<script lang="ts">
  interface Props {
    checked: boolean;
    class?: string;
  }

  let { checked = $bindable(), class: className = '' }: Props = $props();
</script>

<label
  class="font-inter text-text-brand-secondary flex cursor-pointer items-center gap-2 text-xs select-none {className}"
>
  <input type="checkbox" bind:checked class="accent-nogon-rose size-3.5 cursor-pointer" />
  Delete matching documents already in the index
</label>
