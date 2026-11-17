<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { tick } from 'svelte';
  import type { ResultState } from '#lib/result-state.svelte.js';
  import { SkipRuleActions } from '@nogon/components';
  import { Button } from '@nogon/components/ui/button';
  import { Input } from '@nogon/components/ui/input';
  import { Label } from '@nogon/components/ui/label';
  import * as Dialog from '@nogon/components/ui/dialog';
  import {
    ArrowLeft,
    Ban,
    Copy,
    MoreVertical,
    Pin,
    PinOff,
    Tag,
    Trash2,
    Unlink,
    X,
  } from '@lucide/svelte';

  let {
    open = $bindable(false),
    url,
    title,
    query,
    pinned,
    historyResult,
    canWrite,
    resultState,
    onAddSkipRule,
    onForget,
    onDelete,
  }: {
    open: boolean;
    url: string;
    title: string;
    query: string;
    pinned: boolean;
    historyResult: boolean;
    canWrite: boolean;
    resultState: ResultState;
    onAddSkipRule: (type: 'url' | 'domain', deleteMatches: boolean) => Promise<void>;
    onForget: () => Promise<void>;
    onDelete?: () => void;
  } = $props();

  type View = 'actions' | 'pin' | 'label' | 'skip';
  const titles = {
    actions: 'Result actions',
    pin: 'Pin result',
    label: 'Edit label',
    skip: 'Disable indexing',
  };
  const id = $props.id();
  const actionClass =
    'min-h-11 w-full justify-start whitespace-normal border-[2px] px-3 py-2 text-left text-sm';
  let view = $state<View>('actions');
  let pending = $state(false);
  let editorInput = $state<HTMLInputElement | null>(null);
  let heading = $state<HTMLHeadingElement | null>(null);
  let pinButton = $state<HTMLButtonElement | null>(null);
  let labelButton = $state<HTMLButtonElement | null>(null);
  let skipButton = $state<HTMLButtonElement | null>(null);

  async function showEditor(next: View) {
    resultState.onOpen();
    view = next;
    await tick();
    if (next === 'skip') heading?.focus();
    else editorInput?.focus();
  }

  async function backToActions() {
    const previous = view;
    view = 'actions';
    await tick();
    ({ pin: pinButton, label: labelButton, skip: skipButton, actions: null })[previous]?.focus();
  }

  async function perform(action: () => void | Promise<void>) {
    if (pending) return;
    pending = true;
    try {
      await action();
    } catch {
      resultState.actionsMessage = 'Action failed. Please try again.';
      resultState.actionsError = true;
    } finally {
      pending = false;
    }
  }
</script>

<Dialog.Root
  bind:open
  onOpenChange={(isOpen) => {
    if (!isOpen) return;
    view = 'actions';
    resultState.onOpen();
    resultState.actionsQuery = query;
    resultState.labelInput = resultState.displayLabel ?? '';
  }}
>
  <Dialog.Trigger>
    {#snippet child({ props })}
      <Button
        {...props}
        variant="ghost"
        size="icon-lg"
        class="text-text-brand-muted hover:text-text-brand shrink-0 self-start"
        aria-label={`Actions for ${title || url}`}
      >
        <MoreVertical class="size-5" />
      </Button>
    {/snippet}
  </Dialog.Trigger>
  <Dialog.Content
    showCloseButton={false}
    class="border-brutal-border bg-card-surface top-auto bottom-0 left-0 max-h-[85dvh] w-full max-w-none translate-x-0 translate-y-0 gap-4 overflow-y-auto overscroll-contain rounded-none border-[3px] p-4 pb-[max(1rem,env(safe-area-inset-bottom))] sm:max-w-none"
    onkeydown={(event) => {
      if (event.key !== 'Escape') event.stopPropagation();
    }}
  >
    <div class="flex items-center justify-between gap-2">
      {#if view !== 'actions'}
        <Button variant="ghost" class="min-h-11 px-2" disabled={pending} onclick={backToActions}>
          <ArrowLeft class="size-4" />
          Back to actions
        </Button>
      {/if}
      <Dialog.Close>
        {#snippet child({ props })}
          <Button
            {...props}
            variant="ghost"
            size="icon-lg"
            class="ml-auto"
            aria-label="Close result actions"
          >
            <X class="size-5" />
          </Button>
        {/snippet}
      </Dialog.Close>
    </div>
    <Dialog.Header class="min-w-0 text-left">
      <Dialog.Title
        bind:ref={heading}
        tabindex="-1"
        class="font-outfit text-text-brand text-xl font-bold"
      >
        {titles[view]}
      </Dialog.Title>
      <Dialog.Description class="font-inter text-text-brand-muted line-clamp-2 text-sm break-all">
        {title || url}
      </Dialog.Description>
    </Dialog.Header>

    {#if view === 'actions'}
      <fieldset disabled={pending} class="flex min-w-0 flex-col gap-2">
        <Button
          variant="outline"
          class={actionClass}
          onclick={() => perform(() => resultState.copyURL(url))}
        >
          <Copy class="size-4" /> Copy URL
        </Button>
        {#if canWrite}
          {#if pinned}
            <Button
              variant="outline"
              class={actionClass}
              onclick={() => perform(() => resultState.pin(url, title, query, true))}
            >
              <PinOff class="size-4" /> Unpin
            </Button>
          {:else}
            <Button
              bind:ref={pinButton}
              variant="outline"
              class={actionClass}
              onclick={() => showEditor('pin')}
            >
              <Pin class="size-4" /> Pin result
            </Button>
          {/if}
          <Button
            bind:ref={labelButton}
            variant="outline"
            class={actionClass}
            onclick={() => showEditor('label')}
          >
            <Tag class="size-4" /> Edit label
          </Button>
          <Button
            bind:ref={skipButton}
            variant="outline"
            class={actionClass}
            onclick={() => showEditor('skip')}
          >
            <Ban class="size-4" /> Disable indexing
          </Button>
          {#if historyResult}
            <Button variant="outline" class={actionClass} onclick={() => perform(onForget)}>
              <Unlink class="size-4" /> Forget for this query
            </Button>
            <p class="font-inter text-text-brand-muted text-xs">
              Stops prioritizing this result for “{query}”. The document remains indexed.
            </p>
          {/if}
          {#if !pinned}
            <hr class="border-border-brand-muted my-1" />
            <Button
              variant="outline"
              class="{actionClass} border-nogon-rose text-nogon-rose"
              onclick={() => {
                open = false;
                onDelete?.();
              }}
            >
              <Trash2 class="size-4" /> Delete result
            </Button>
          {/if}
        {/if}
      </fieldset>
    {:else if canWrite && view === 'pin'}
      <form
        onsubmit={(event) => {
          event.preventDefault();
          void perform(() => resultState.pin(url, title, query));
        }}
      >
        <fieldset disabled={pending} class="min-w-0 space-y-3">
          <p class="font-inter text-text-brand-secondary text-sm" id="{id}-pin-help">
            Show this result first when searching for this query.
          </p>
          <Label for="{id}-query">Search query</Label>
          <Input
            id="{id}-query"
            bind:ref={editorInput}
            bind:value={resultState.actionsQuery}
            aria-describedby="{id}-pin-help"
            class="h-11 text-base"
            required
          />
          <Button type="submit" class="min-h-11 w-full" disabled={!resultState.actionsQuery.trim()}>
            {pending ? 'Saving…' : 'Pin result'}
          </Button>
        </fieldset>
      </form>
    {:else if canWrite && view === 'label'}
      <form
        onsubmit={(event) => {
          event.preventDefault();
          void perform(() => resultState.updateLabel(url));
        }}
      >
        <fieldset disabled={pending} class="min-w-0 space-y-3">
          <Label for="{id}-label">Label</Label>
          <Input
            id="{id}-label"
            bind:ref={editorInput}
            bind:value={resultState.labelInput}
            aria-describedby="{id}-label-help"
            class="h-11 text-base"
          />
          <p class="font-inter text-text-brand-muted text-sm" id="{id}-label-help">
            Leave empty to remove the label.
          </p>
          <Button type="submit" class="min-h-11 w-full">{pending ? 'Saving…' : 'Save label'}</Button
          >
        </fieldset>
      </form>
      {#if resultState.labelMessage}
        <p
          role={resultState.labelError ? 'alert' : 'status'}
          class="font-inter text-sm {resultState.labelError
            ? 'text-nogon-rose'
            : 'text-nogon-teal'}"
        >
          {resultState.labelMessage}
        </p>
      {/if}
    {:else if canWrite && view === 'skip'}
      <p class="font-inter text-text-brand-secondary text-sm break-all">
        Choose whether to stop indexing this URL or all pages on its domain.
      </p>
      <p class="font-inter text-text-brand-muted text-xs break-all">{url}</p>
      <fieldset disabled={pending} class="min-w-0">
        <SkipRuleActions
          class="[&_button]:min-h-11 [&_input]:size-5 [&_label]:min-h-11"
          onAddSkipRule={(type, deleteMatches) => perform(() => onAddSkipRule(type, deleteMatches))}
        />
      </fieldset>
    {/if}
    {#if resultState.actionsMessage}
      <p
        role={resultState.actionsError ? 'alert' : 'status'}
        class="font-inter text-sm {resultState.actionsError
          ? 'text-nogon-rose'
          : 'text-nogon-teal'}"
      >
        {resultState.actionsMessage}
      </p>
    {/if}
  </Dialog.Content>
</Dialog.Root>
