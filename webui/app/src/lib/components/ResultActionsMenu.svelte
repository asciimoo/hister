<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { onMount } from 'svelte';
  import type { ResultState } from '#lib/result-state.svelte.js';
  import ResultActionsSheet from '#lib/components/ResultActionsSheet.svelte';
  import DeleteMatchingDocumentsDialog from '#lib/components/DeleteMatchingDocumentsDialog.svelte';
  import { SkipRuleActions } from '@nogon/components';
  import { Input } from '@nogon/components/ui/input';
  import { Button } from '@nogon/components/ui/button';
  import * as DropdownMenu from '@nogon/components/ui/dropdown-menu';
  import { Copy, MoreVertical, Pin, PinOff, Tag, Trash2, Unlink } from '@lucide/svelte';

  interface Props {
    url: string;
    title: string;
    domain: string;
    resultState: ResultState;
    query: string;
    pinned?: boolean;
    historyResult?: boolean;
    canWrite?: boolean;
    onForget?: () => void;
    onDelete?: () => void;
    removeResult: (url: string) => void;
    removeResultsByDomain: (domain: string) => void;
  }

  let {
    url,
    title,
    domain,
    resultState,
    query,
    pinned = false,
    historyResult = false,
    canWrite = true,
    onForget,
    onDelete,
    removeResult,
    removeResultsByDomain,
  }: Props = $props();

  let open = $state(false);
  let mobile = $state(false);
  let deleteConfirmOpen = $state(false);
  let deleteConfirmMatched = $state(0);
  let resolveDeleteConfirmation: ((confirmed: boolean) => void) | null = null;

  onMount(() => {
    const query = window.matchMedia('(max-width: 767px)');
    mobile = query.matches;
    const update = () => {
      open = false;
      mobile = query.matches;
    };
    query.addEventListener('change', update);
    return () => query.removeEventListener('change', update);
  });

  async function forgetResult() {
    if (await resultState.forgetForQuery(url, query)) {
      open = false;
      onForget?.();
    }
  }

  async function addSkipRule(type: 'url' | 'domain', deleteMatches: boolean) {
    await resultState.addSkipRule({
      url,
      domain,
      type,
      deleteMatches,
      removeResult,
      removeResultsByDomain,
      confirmDeletion,
    });
    if (deleteMatches) open = false;
  }

  function confirmDeletion(matched: number): Promise<boolean> {
    open = false;
    deleteConfirmMatched = matched;
    deleteConfirmOpen = true;
    return new Promise((resolve) => {
      resolveDeleteConfirmation = resolve;
    });
  }

  function finishDeleteConfirmation(confirmed: boolean) {
    deleteConfirmOpen = false;
    deleteConfirmMatched = 0;
    resolveDeleteConfirmation?.(confirmed);
    resolveDeleteConfirmation = null;
  }
</script>

{#if mobile}
  <ResultActionsSheet
    bind:open
    {url}
    {title}
    {query}
    {pinned}
    {historyResult}
    {canWrite}
    {resultState}
    onAddSkipRule={addSkipRule}
    onForget={forgetResult}
    {onDelete}
  />
{:else}
  <DropdownMenu.Root
    bind:open
    onOpenChange={(isOpen) => {
      if (!isOpen) return;
      resultState.onOpen();
    }}
  >
    <DropdownMenu.Trigger>
      {#snippet child({ props })}
        <Button
          {...props}
          variant="ghost"
          size="icon-sm"
          aria-label={`Actions for ${title || url}`}
          class="text-text-brand-muted hover:text-text-brand shrink-0 cursor-pointer self-start"
        >
          <MoreVertical class="size-4" />
        </Button>
      {/snippet}
    </DropdownMenu.Trigger>
    <DropdownMenu.Content
      class="border-brutal-border bg-card-surface {pinned || !canWrite
        ? 'w-72'
        : 'w-100'} rounded-none border-[3px] p-3 shadow-[4px_4px_0_var(--brutal-shadow)]"
    >
      <div class="space-y-3">
        <Button
          variant="outline"
          size="sm"
          class="w-full border-[2px] text-xs"
          onclick={() => resultState.copyURL(url)}
        >
          <Copy class="size-3.5" />
          Copy URL
        </Button>
        {#if canWrite}
          <hr />
          <div class="space-y-2">
            {#if pinned}
              <p
                class="font-outfit text-text-brand-muted mb-1 text-xs font-bold tracking-widest uppercase"
              >
                Priority
              </p>
              <Button
                variant="outline"
                size="sm"
                class="border-nogon-rose text-nogon-rose hover:bg-nogon-rose/10 w-full border-[2px] text-xs"
                onclick={() => resultState.pin(url, title, query, true)}
              >
                <PinOff class="size-3.5" />
                Unpin
              </Button>
            {:else}
              <p class="font-outfit mb-1 text-xs font-bold tracking-widest uppercase">
                Prioritize this result in query:
              </p>
              <div class="flex items-center gap-2">
                <Input
                  bind:value={resultState.actionsQuery}
                  placeholder="Query.."
                  size="sm"
                  class="font-inter border-border-brand-muted focus-visible:border-nogon-indigo flex-1 border-[2px] text-sm shadow-none focus-visible:ring-0"
                />
                <Button
                  variant="outline"
                  size="sm"
                  class="border-nogon-indigo text-nogon-indigo border-[2px] text-xs"
                  onclick={() => resultState.pin(url, title, query)}
                >
                  <Pin class="size-3.5" />
                  Pin
                </Button>
              </div>
              <hr />
            {/if}
            {#if historyResult}
              <p class="font-inter text-text-brand-muted text-xs">
                Stops prioritizing this result for “{query}”. The document remains indexed.
              </p>
              <Button
                variant="outline"
                size="sm"
                class="border-nogon-rose text-nogon-rose hover:bg-nogon-rose/10 w-full border-[2px] text-xs"
                onclick={forgetResult}
              >
                <Unlink class="size-3.5" />
                Forget for this query
              </Button>
            {/if}
          </div>
          <SkipRuleActions onAddSkipRule={addSkipRule} />
          <hr />
          <div class="space-y-2">
            <p class="font-outfit mb-1 text-xs font-bold tracking-widest uppercase">Label:</p>
            <div class="flex items-center gap-2">
              <Input
                bind:value={resultState.labelInput}
                placeholder="Add a label…"
                size="sm"
                class="font-inter border-border-brand-muted focus-visible:border-nogon-amber flex-1 border-[2px] text-sm shadow-none focus-visible:ring-0"
              />
              <Button
                variant="outline"
                size="sm"
                class="border-[2px] text-xs"
                onclick={() => resultState.updateLabel(url)}
              >
                <Tag class="size-3.5" />
                Save
              </Button>
            </div>
            {#if resultState.labelMessage}
              <p
                class="font-inter text-xs {resultState.labelError
                  ? 'text-nogon-rose'
                  : 'text-nogon-teal'}"
              >
                {resultState.labelMessage}
              </p>
            {/if}
          </div>
          {#if !pinned}
            <hr />
            <Button
              variant="outline"
              size="sm"
              class="border-nogon-rose text-nogon-rose hover:bg-nogon-rose/10 w-full border-[2px] text-xs"
              onclick={() => {
                open = false;
                onDelete?.();
              }}
            >
              <Trash2 class="size-3.5" />
              Delete result
            </Button>
          {/if}
        {/if}
        {#if resultState.actionsMessage}
          <p
            role={resultState.actionsError ? 'alert' : 'status'}
            class="font-inter text-xs {resultState.actionsError
              ? 'text-nogon-rose'
              : 'text-nogon-teal'}"
          >
            {resultState.actionsMessage}
          </p>
        {/if}
      </div>
    </DropdownMenu.Content>
  </DropdownMenu.Root>
{/if}
{#if canWrite}
  <DeleteMatchingDocumentsDialog
    bind:open={deleteConfirmOpen}
    matched={deleteConfirmMatched}
    onCancel={() => finishDeleteConfirmation(false)}
    onConfirm={() => finishDeleteConfirmation(true)}
  />
{/if}
