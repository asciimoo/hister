<script lang="ts">
  import { Badge } from '@nogon/components/ui/badge';
  import * as Card from '@nogon/components/ui/card';
  import AlertTriangleIcon from '@lucide/svelte/icons/triangle-alert';

  type ConfigScope = 'client' | 'server';

  interface ConfigReferenceItem {
    name: string;
    type: string;
    defaultValue?: string;
    requirement?: string;
    description: string;
    scopes?: ConfigScope[];
  }

  let { items, scopes = [] }: { items: ConfigReferenceItem[]; scopes?: ConfigScope[] } = $props();
</script>

<div class="mb-7 flex flex-col gap-4">
  {#each items as item (item.name)}
    {@const optionScopes = item.scopes ?? scopes}
    <Card.Root
      aria-labelledby="config-option-{item.name}"
      color="nogon-indigo"
      class="bg-brutal-card min-w-0"
    >
      <Card.Header
        class="border-brutal-border items-center justify-between gap-4 border-b-[3px] bg-(--text-primary) px-5 py-3.5"
      >
        <Card.Title
          id="config-option-{item.name}"
          class="font-outfit text-xl font-extrabold text-(--brutal-bg)"
        >
          <code class="border-0! bg-transparent! p-0! text-xl! wrap-anywhere text-(--brutal-bg)"
            >{item.name}</code
          >
        </Card.Title>
        {#if optionScopes.length > 0}
          <div class="flex shrink-0 flex-wrap justify-end gap-1.5">
            {#each optionScopes as scope (scope)}
              <Badge
                variant="outline"
                class={scope === 'server'
                  ? 'font-space border-nogon-amber text-nogon-amber bg-transparent text-[9px] font-bold tracking-[1.25px] uppercase'
                  : 'font-space border-nogon-cyan text-nogon-cyan bg-transparent text-[9px] font-bold tracking-[1.25px] uppercase'}
              >
                {scope}
              </Badge>
            {/each}
          </div>
        {:else}
          <Badge
            variant="outline"
            class="font-space shrink-0 border-(--brutal-bg) bg-transparent text-[9px] font-semibold tracking-[1.25px] text-(--brutal-bg) uppercase opacity-70"
          >
            Config option
          </Badge>
        {/if}
      </Card.Header>

      <Card.Content class="px-5 py-4">
        <Card.Description class="font-inter m-0! text-sm leading-6 text-(--text-secondary)">
          {item.description}
        </Card.Description>
      </Card.Content>

      <Card.Footer
        class="bg-brutal-card border-border-brand-muted flex flex-col items-stretch border-t-2 p-0 sm:flex-row md:p-0"
      >
        <div class="flex min-w-0 items-baseline gap-2 px-4 py-2">
          <span
            class="font-space shrink-0 text-[9px] font-bold tracking-[1.25px] text-(--text-secondary) uppercase"
            >Type</span
          >
          <code
            class="border-0! bg-(--muted-surface)! px-1.5! py-0.5! text-sm whitespace-nowrap text-(--text-primary)"
            >{item.type}</code
          >
        </div>
        {#if item.defaultValue !== undefined}
          <div
            class="border-border-brand-muted flex min-w-0 items-baseline gap-2 border-t-2 px-4 py-2 sm:flex-1 sm:border-t-0 sm:border-l-2"
          >
            <span
              class="font-space shrink-0 text-[9px] font-bold tracking-[1.25px] text-(--text-secondary) uppercase"
              >Default</span
            >
            <code
              class="border-0! bg-(--muted-surface)! px-1.5! py-0.5! text-sm wrap-anywhere text-(--text-primary)"
              >{item.defaultValue}</code
            >
          </div>
        {/if}
        {#if item.requirement}
          <div
            class="border-border-brand-muted flex min-w-0 items-center justify-end border-t-2 px-4 py-2 sm:ml-auto sm:border-t-0 sm:border-l-2"
          >
            <Badge
              variant="outline"
              class="font-space border-nogon-amber bg-nogon-amber/10 border-2 px-2 py-1 text-[10px] font-bold tracking-[1px] text-(--text-primary) uppercase"
            >
              <AlertTriangleIcon aria-hidden="true" class="text-nogon-amber size-3.5!" />
              {item.requirement}
            </Badge>
          </div>
        {/if}
      </Card.Footer>
    </Card.Root>
  {/each}
</div>
