<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { MediaQuery } from 'svelte/reactivity';
  import { beforeNavigate, goto } from '$app/navigation';
  import { page } from '$app/state';
  import { Badge } from '@nogon/components/ui/badge';
  import { Button } from '@nogon/components/ui/button';
  import * as Tooltip from '@nogon/components/ui/tooltip';
  import * as Dialog from '@nogon/components/ui/dialog';
  import { TriangleAlert } from '@lucide/svelte';
  import { fetchConfig, resetConfig } from '#lib/api.js';
  import { redirectToAuth } from '#lib/auth-navigation.js';
  import SettingsField from '#lib/components/SettingsField.svelte';
  import SettingsAccess from '#lib/components/SettingsAccess.svelte';
  import {
    categoryLabels,
    groupSettings,
    settingsRequest,
    readSettingsReview,
    reloadConfiguration,
    sameSetting,
    canEditSetting,
    SettingsError,
    type SettingsState,
    type SettingsDraft,
    type SettingsReview,
    type SettingEdit,
    type SettingError,
    type SettingItemEditor,
    type SettingRemoval,
    type PendingAccess,
  } from '#lib/settings.js';

  const activityMessages = {
    loading: 'Loading settings…',
    refreshing: 'Refreshing settings…',
    reloading: 'Reloading configuration. Service may be briefly interrupted…',
    checking: 'Checking item…',
    validating: 'Validating settings…',
    applying: 'Applying settings. Service may be briefly interrupted…',
    reconnecting: 'Reconnecting and reading active settings…',
  };
  type SettingsActivity = keyof typeof activityMessages;
  type EditingPhase = 'editing' | 'refresh-required';
  type ReviewedDraft = SettingsReview & { draft: SettingsDraft };
  type AccessConfirmation = { destination: string; initial?: PendingAccess; warning?: string };
  type SettingsWorkflow =
    | { phase: EditingPhase | 'denied' | 'closed' }
    | { phase: 'working'; activity: SettingsActivity; resume: EditingPhase }
    | { phase: 'review'; review: ReviewedDraft }
    | { phase: 'access'; access: AccessConfirmation };

  let settings = $state<SettingsState | null>(null);
  let edits = $state<Record<string, SettingEdit>>({});
  let editor = $state<SettingItemEditor | null>(null);
  let removal = $state<SettingRemoval | null>(null);
  let conflicts = $state<Record<string, string>>({});
  let errors = $state<SettingError[]>([]);
  let workflow = $state<SettingsWorkflow>({
    phase: 'working',
    activity: 'loading',
    resume: 'editing',
  });
  let message = $state(activityMessages.loading);
  let problem = $state('');
  const busy = $derived(workflow.phase === 'working');
  const denied = $derived(workflow.phase === 'denied');
  const needsReload = $derived(
    workflow.phase === 'refresh-required' ||
      (workflow.phase === 'working' && workflow.resume === 'refresh-required'),
  );
  const review = $derived(workflow.phase === 'review' ? workflow.review : null);
  const access = $derived(workflow.phase === 'access' ? workflow.access : null);
  let destroyed = false;
  const sidebarLayout = new MediaQuery('(min-width: 64rem)');
  const categories = $derived([...new Set(settings?.fields.map((field) => field.category) ?? [])]);
  const settingsUrl = $derived(page.shallow?.url ?? page.url);
  const requestedCategory = $derived(settingsUrl.searchParams.get('tab') ?? 'app');
  const category = $derived(
    categories.includes(requestedCategory) ? requestedCategory : (categories[0] ?? 'app'),
  );
  const editedPaths = $derived(new Set([...Object.keys(edits), ...(editor ? [editor.path] : [])]));
  const editCount = $derived(editedPaths.size);
  const dirty = $derived(editCount > 0);
  const conflictCount = $derived(Object.keys(conflicts).length);
  const values = $derived(new Map(settings?.values.map((value) => [value.path, value]) ?? []));
  const pendingValues = $derived(
    new Map(settings?.pending?.values?.map((value) => [value.path, value]) ?? []),
  );
  const localConfigFilename = $derived(
    [...(settings?.pending?.values ?? []), ...(settings?.values ?? [])]
      .find((value) => value.source.kind === 'file' && value.source.name)
      ?.source.name?.split(/[\\/]/)
      .pop(),
  );
  const groups = $derived(groupSettings(settings?.fields ?? [], category));

  onMount(() => {
    void load();
  });
  onDestroy(() => {
    destroyed = true;
    clearDraft();
    workflow = { phase: 'closed' };
  });
  beforeNavigate((navigation) => {
    if (navigation.shallow && navigation.to?.url.pathname === page.url.pathname) return;
    if (
      (access || dirty || busy) &&
      !navigation.willUnload &&
      !window.confirm(
        access
          ? 'Leave Settings before confirming access? Any private confirmation code on this page will be lost. Without it, you may need to wait for automatic rollback instead of confirming or restoring settings.'
          : 'Leave Settings and discard unsaved changes?',
      )
    )
      navigation.cancel();
  });

  function beforeUnload(event: BeforeUnloadEvent) {
    if (!dirty && !access) return;
    event.preventDefault();
    event.returnValue = '';
  }

  function beginWork(activity: SettingsActivity) {
    workflow = {
      phase: 'working',
      activity,
      resume: needsReload ? 'refresh-required' : 'editing',
    };
    problem = '';
    showActivity(activity);
    const operation = workflow;
    // A completed request must not replace a review, access confirmation,
    // denied state, or a newer operation that has already taken over.
    return () => {
      if (workflow === operation) workflow = { phase: operation.resume };
    };
  }

  function showActivity(activity: SettingsActivity) {
    if (workflow.phase !== 'working') return;
    workflow.activity = activity;
    message = activityMessages[activity];
  }

  function requireRefresh(required = true) {
    const phase = required ? 'refresh-required' : 'editing';
    if (workflow.phase === 'working') workflow.resume = phase;
    else if (workflow.phase === 'editing' || workflow.phase === 'refresh-required')
      workflow = { phase };
  }

  function clearDraft() {
    edits = {};
    editor = null;
    removal = null;
    conflicts = {};
    errors = [];
  }

  function dismissReview() {
    if (workflow.phase === 'review') workflow = { phase: 'editing' };
  }

  function denyAccess() {
    settings = null;
    clearDraft();
    workflow = { phase: 'denied' };
    problem = 'You do not have permission to configure this instance.';
    message = '';
  }

  // Only reads are retried. A disconnected save may already have committed.
  async function retryRead<T>(read: () => Promise<T>): Promise<T> {
    for (let attempt = 0; ; attempt++) {
      if (destroyed) throw new Error('Settings closed');
      try {
        return await read();
      } catch (error) {
        const retryable =
          error instanceof TypeError ||
          (error instanceof SettingsError
            ? error.status === 503
            : error instanceof Error && error.cause === 503);
        if (!retryable || attempt >= 19 || destroyed) throw error;
        await new Promise((resolve) => setTimeout(resolve, 500));
      }
    }
  }

  function reconcile(next: SettingsState) {
    const previous = new Map(settings?.values.map((value) => [value.path, value]));
    const current = new Map(next.values.map((value) => [value.path, value]));
    for (const path of new Set([...Object.keys(edits), ...(editor ? [editor.path] : [])])) {
      const field = next.fields.find((field) => field.path === path);
      const active = current.get(path);
      if (!field || !active || !canEditSetting(field, active)) {
        conflicts[path] =
          'This setting is no longer editable. Use the current value to remove your draft.';
      } else if (!sameSetting(previous.get(path), active)) {
        conflicts[path] =
          field.schema.sensitivity === 'none'
            ? 'The active setting changed. Review your draft before saving.'
            : 'Stored secret references changed. Use the current value and reapply your edits.';
      }
    }
    settings = next;
    // A removal snapshot may contain references from the previous revision.
    removal = null;
  }

  async function readActive(preserveDraft = false) {
    const next = await retryRead(() => settingsRequest<SettingsState>());
    await retryRead(() => {
      resetConfig();
      return fetchConfig();
    });
    if (preserveDraft) reconcile(next);
    else settings = next;
    if (next.pending_access)
      startAccess({
        destination: next.pending_access.destination_url,
        initial: next.pending_access,
      });
  }

  function startAccess(confirmation: AccessConfirmation) {
    clearDraft();
    workflow = { phase: 'access', access: confirmation };
    problem = '';
    message = '';
  }

  async function finishAccess(outcome: 'confirmed' | 'cancelled' | 'ended') {
    const finish = beginWork('reconnecting');
    clearDraft();
    const result =
      outcome === 'confirmed'
        ? 'Settings confirmed and saved.'
        : outcome === 'cancelled'
          ? 'Previous settings restored.'
          : 'No access change is awaiting confirmation. Check the active settings before making another change.';
    try {
      await readActive();
      requireRefresh(false);
      problem = '';
      message = result;
    } catch {
      requireRefresh();
      message = '';
      problem = `${result} Reopen Nogon at ${outcome === 'confirmed' ? 'the confirmed' : 'the original'} address and sign in to read the active settings.`;
    } finally {
      finish();
    }
  }

  async function load() {
    const finish = beginWork('loading');
    try {
      const config = await retryRead(() => fetchConfig());
      if (!config.canConfigure) {
        if (!config.authenticated && config.authMode !== 'none') {
          redirectToAuth();
          return;
        }
        denyAccess();
        return;
      }
      settings = await retryRead(() => settingsRequest<SettingsState>());
      clearDraft();
      requireRefresh(false);
      if (settings.pending_access)
        startAccess({
          destination: settings.pending_access.destination_url,
          initial: settings.pending_access,
        });
      message = '';
    } catch (error) {
      handleError(error);
    } finally {
      finish();
    }
  }

  async function refreshSettings() {
    if (busy || denied) return;
    if (!settings) {
      resetConfig();
      await load();
      return;
    }
    const finish = beginWork('refreshing');
    try {
      await readActive(true);
      errors = [];
      requireRefresh(false);
      message = conflictCount
        ? 'Settings refreshed. Resolve the highlighted draft conflicts before saving.'
        : 'Settings refreshed. Your unsaved changes have been kept.';
    } catch (error) {
      handleError(error);
      requireRefresh();
    } finally {
      finish();
    }
  }

  async function applyConfiguration() {
    if (busy || !settings || settings.pending?.status !== 'changed') return;
    const finish = beginWork('reloading');
    let reloaded = false;
    try {
      await reloadConfiguration();
      reloaded = true;
      await readActive(true);
      errors = [];
      requireRefresh(false);
      message = conflictCount
        ? 'Configuration reloaded. Resolve the highlighted draft conflicts before saving.'
        : 'Configuration reloaded. Your unsaved changes have been kept.';
    } catch (error) {
      handleError(error);
      requireRefresh();
      if (denied) return;
      if (reloaded) {
        problem =
          'Configuration was reloaded, but active settings could not be refreshed. Refresh settings before saving. If the server address or sign-in settings changed, reopen Nogon using the new connection settings.';
      } else {
        if (!(error instanceof SettingsError)) {
          problem =
            'The reload response was lost. The configuration may have been applied. Refresh settings to check the active values before continuing.';
        }
        // A reload is never retried automatically, even when its response is lost.
        try {
          await readActive(true);
        } catch (readError) {
          if (readError instanceof SettingsError && [401, 403].includes(readError.status))
            handleError(readError);
          else
            problem +=
              ' Active settings could not be read. If the server address or sign-in settings changed, reopen Nogon using the new connection settings.';
        }
      }
    } finally {
      finish();
    }
  }

  function handleError(error: unknown) {
    if (error instanceof SettingsError) {
      if (error.status === 403 || error.status === 401) {
        denyAccess();
      } else if (error.status === 409) {
        requireRefresh();
        problem =
          error.code === 'sources_changed'
            ? 'Configuration sources changed or cannot be read. Refresh settings to check the file changes.'
            : 'Settings changed since you opened this page. Refresh settings before saving again.';
      } else {
        errors = error.fields;
        problem = error.fields.length ? 'Please correct the highlighted settings.' : error.message;
      }
    } else {
      problem = 'Could not reach the server. Please try again.';
    }
    message = '';
  }

  function update(path: string, edit?: SettingEdit) {
    dismissReview();
    if (editor?.path === path) editor = null;
    if (removal?.path === path) removal = null;
    if (edit) edits[path] = edit;
    else delete edits[path];
    delete conflicts[path];
    errors = errors.filter((error) => error.path !== path);
    if (!needsReload) problem = '';
    message = '';
  }

  async function changeEditor(next: SettingItemEditor | null) {
    const previous = editor;
    editor = next;
    if (!next && previous) {
      errors = errors.filter((error) => error.path !== previous.path);
      if (!edits[previous.path]) delete conflicts[previous.path];
      if (!needsReload) problem = '';
      await tick();
      document
        .getElementById(
          `setting-${previous.path}-${previous.index === null ? 'add' : `${previous.index}-edit`}`,
        )
        ?.focus();
    }
  }

  function collectionEdit(path: string, value: unknown): SettingEdit | undefined {
    return JSON.stringify(value) === JSON.stringify(values.get(path)?.value)
      ? undefined
      : { path, action: 'set', value };
  }

  async function finishItem(path: string, value: unknown) {
    if (
      !settings ||
      busy ||
      needsReload ||
      settings.pending ||
      conflicts[path] ||
      editor?.path !== path
    )
      return;
    const edit = collectionEdit(path, value);
    const draft = Object.values(edits).filter((edit) => edit.path !== path);
    if (edit) draft.push(edit);
    const finish = beginWork('checking');
    let otherErrors: SettingError[] = [];
    try {
      try {
        await settingsRequest('/validate', { revision: settings.revision, edits: draft });
      } catch (error) {
        // Other fields can be corrected after finishing this item.
        if (
          error instanceof SettingsError &&
          error.status === 422 &&
          error.fields.length &&
          error.fields.every((field) => field.path && field.path !== path)
        )
          otherErrors = error.fields;
        else throw error;
      }
      const finished = editor;
      update(path, edit);
      errors = otherErrors;
      message = 'Item ready. Save and apply activates your changes.';
      // The summary button must be enabled before returning keyboard focus.
      finish();
      await tick();
      document
        .getElementById(
          `setting-${path}-${finished?.index === null ? 'add' : `${finished?.index}-edit`}`,
        )
        ?.focus();
    } catch (error) {
      handleError(error);
    } finally {
      finish();
    }
  }

  function removeItem(path: string, value: unknown, previous: unknown) {
    update(path, collectionEdit(path, value));
    removal = { path, value: previous };
  }

  function undoRemoval() {
    if (!removal) return;
    update(removal.path, collectionEdit(removal.path, removal.value));
  }

  function discard() {
    if (busy || !dirty) return;
    dismissReview();
    clearDraft();
    if (!needsReload) problem = '';
    message = 'Unsaved changes discarded.';
  }

  function tabErrors(tab: string) {
    return [
      ...errors,
      ...Object.entries(conflicts).map(([path, message]) => ({ path, message })),
    ].filter((error) => error.path.split('.')[0] === tab);
  }

  function selectCategory(next: string) {
    const url = new URL(settingsUrl.href);
    if (next === 'app') url.searchParams.delete('tab');
    else url.searchParams.set('tab', next);
    if (url.href !== settingsUrl.href) void goto(url, { shallow: true });
  }

  function tabKeydown(event: KeyboardEvent, index: number) {
    let next = index;
    switch (event.key) {
      case sidebarLayout.current ? 'ArrowDown' : 'ArrowRight':
        next = (index + 1) % categories.length;
        break;
      case sidebarLayout.current ? 'ArrowUp' : 'ArrowLeft':
        next = (index + categories.length - 1) % categories.length;
        break;
      case 'Home':
        next = 0;
        break;
      case 'End':
        next = categories.length - 1;
        break;
      default:
        return;
    }
    event.preventDefault();
    selectCategory(categories[next]);
    document.getElementById(`tab-${categories[next]}`)?.focus();
  }

  function cancelReview() {
    if (!review) return;
    dismissReview();
    message = 'Sensitive changes were not applied. Your draft is still here.';
  }

  async function save(accepted?: ReviewedDraft) {
    if (
      !settings ||
      access ||
      busy ||
      !dirty ||
      editor ||
      needsReload ||
      settings.pending ||
      conflictCount
    )
      return;
    const draft: SettingsDraft = {
      revision: settings.revision,
      edits: JSON.parse(JSON.stringify(Object.values(edits))),
    };
    if (accepted && JSON.stringify(accepted.draft) !== JSON.stringify(draft)) {
      dismissReview();
      problem = 'The draft changed. Save and apply again to review the current changes.';
      return;
    }
    const finish = beginWork('validating');
    errors = [];
    try {
      const details = accepted ? undefined : await readSettingsReview(draft, settings.fields);
      if (details) {
        workflow = { phase: 'review', review: { ...details, draft } };
        message = 'Review the sensitive changes before applying this draft.';
        return;
      }
      showActivity('applying');
      const result = await settingsRequest<PendingAccess | { saved: boolean }>(
        '/apply',
        accepted ? { ...accepted.draft, acknowledgment: accepted.acknowledgment } : draft,
      );
      if ('deadline' in result) {
        startAccess({ destination: result.destination_url, initial: result });
        return;
      }
      clearDraft();
      showActivity('reconnecting');
      await readActive();
      message = 'Settings saved and applied.';
    } catch (error) {
      const activity = workflow.phase === 'working' ? workflow.activity : undefined;
      const submitted = activity === 'applying' || activity === 'reconnecting';
      const saved = activity === 'reconnecting';
      if (submitted && accepted?.mode === 'access' && !(error instanceof SettingsError)) {
        startAccess({
          destination: accepted.destination_url ?? page.url.href,
          warning:
            'The apply response was lost. Check the pending change at the reviewed address. Do not submit the draft again; unconfirmed changes roll back after two minutes.',
        });
        return;
      }
      handleError(error);
      if (error instanceof SettingsError && error.code === 'sources_changed') {
        try {
          await readActive(true);
          if (settings?.pending) problem = '';
        } catch (readError) {
          handleError(readError);
        }
        requireRefresh();
      }
      if (submitted && !(error instanceof SettingsError && error.status < 500)) {
        requireRefresh();
        if (saved)
          problem =
            'Settings were saved, but active settings could not be refreshed. Refresh settings before making another change.';
        else if (!(error instanceof SettingsError))
          problem =
            'The save response was lost. The change may have been applied. Refresh settings to check the active values before saving again.';
        else problem += ' Refresh settings before saving again.';
        // Show the active values alongside the retained draft, without accepting
        // a new revision for another save until the owner explicitly reloads.
        if (!saved) {
          try {
            await readActive(true);
          } catch (readError) {
            if (readError instanceof SettingsError && [401, 403].includes(readError.status))
              handleError(readError);
            else problem += ' The server could not be reached to read active settings.';
          }
        }
      }
    } finally {
      finish();
    }
  }
</script>

<svelte:head><title>Settings | Nogon</title></svelte:head>
<svelte:window onbeforeunload={beforeUnload} />

<div class="bg-page-bg text-text-brand flex min-h-0 flex-1 flex-col">
  <div class="flex min-h-0 w-full flex-1 flex-col pt-5">
    <div class="flex shrink-0 flex-col px-4 md:px-6">
      <div class="flex items-center justify-between gap-3">
        <h2 class="font-space text-2xl font-bold tracking-tight">Settings</h2>
        {#if !denied && !access}<Button
            type="button"
            variant="outline"
            disabled={busy}
            onclick={refreshSettings}
            size="sm">Refresh settings</Button
          >{/if}
      </div>
      <p class="text-text-brand-muted my-2 text-sm">
        Configure appearance, search behavior, logging, indexing, crawling, extraction and web
        shortcuts. Other settings are shown for reference.
      </p>
      {#if problem}<div role="alert" class="text-nogon-rose my-2 text-sm">
          <p>{problem}{needsReload && dirty ? ' Your draft is still here.' : ''}</p>
          {#each errors.filter((error) => !error.path || !categories.includes(error.path.split('.')[0])) as error}<p
            >
              {error.message}
            </p>{/each}
        </div>{/if}
      {#if !settings || denied}
        <p role="status" class="text-text-brand-muted my-2 text-sm">{message}</p>
      {/if}
    </div>
    {#if access}
      <SettingsAccess {...access} onFinished={finishAccess} />
    {:else if settings && !denied}
      <div class="border-border-brand-muted flex min-h-0 flex-1 flex-col lg:flex-row lg:border-t">
        <div
          role="tablist"
          aria-label="Configuration categories"
          aria-orientation={sidebarLayout.current ? 'vertical' : 'horizontal'}
          class="border-border-brand-muted flex shrink-0 gap-1 overflow-x-auto border-b px-4 py-2 md:px-6 lg:w-56 lg:flex-col lg:overflow-x-hidden lg:overflow-y-auto lg:border-r lg:border-b-0 lg:p-3"
        >
          {#each categories as item, index}
            {@const count = tabErrors(item).length}
            {@const unsavedCount = settings.fields.filter(
              (field) => field.category === item && editedPaths.has(field.path),
            ).length}
            {@const unsavedDescription = `Number of unsaved items: ${unsavedCount}`}
            <div class="relative shrink-0">
              <button
                type="button"
                role="tab"
                id={`tab-${item}`}
                aria-controls={`panel-${item}`}
                aria-selected={category === item}
                aria-describedby={unsavedCount ? `tab-${item}-unsaved` : undefined}
                tabindex={category === item ? 0 : -1}
                onclick={() => selectCategory(item)}
                onkeydown={(event) => tabKeydown(event, index)}
                class="font-inter hover:bg-muted-surface w-full cursor-pointer border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors lg:border-b-0 lg:border-l-2 lg:py-3 lg:text-left lg:whitespace-normal {unsavedCount
                  ? 'pr-12'
                  : ''} {category === item
                  ? 'border-nogon-indigo text-text-brand lg:bg-nogon-indigo/10 font-semibold'
                  : 'text-text-brand-muted border-transparent font-medium'}"
                >{categoryLabels[item] ?? item}{count
                  ? ` (${count} ${count === 1 ? 'error' : 'errors'})`
                  : ''}</button
              >
              {#if unsavedCount}
                <span id={`tab-${item}-unsaved`} hidden>{unsavedDescription}</span>
                <Tooltip.Provider delayDuration={0}>
                  <Tooltip.Root>
                    <Tooltip.Trigger
                      aria-label={`Unsaved items in ${categoryLabels[item] ?? item}`}
                      aria-describedby={`tab-${item}-unsaved`}
                      tabindex={category === item ? 0 : -1}
                      class="focus-visible:outline-ring absolute top-1/2 right-3 -translate-y-1/2 cursor-help outline-none focus-visible:outline-2 focus-visible:outline-offset-2"
                    >
                      <Badge
                        variant="outline"
                        aria-hidden="true"
                        class="border-nogon-indigo/30 bg-nogon-indigo/10 text-text-brand min-w-5 px-1.5 tabular-nums"
                        >{unsavedCount}</Badge
                      >
                    </Tooltip.Trigger>
                    <Tooltip.Content role="tooltip" side="bottom" sideOffset={6}>
                      {unsavedDescription}
                    </Tooltip.Content>
                  </Tooltip.Root>
                </Tooltip.Provider>
              {/if}
            </div>
          {/each}
        </div>
        <form
          class="flex min-h-0 min-w-0 flex-1 flex-col"
          onsubmit={(event) => {
            event.preventDefault();
            void save();
          }}
          novalidate
        >
          <div
            role="tabpanel"
            id={`panel-${category}`}
            aria-labelledby={`tab-${category}`}
            tabindex="0"
            class="bg-page-bg min-h-0 flex-1 overflow-y-auto p-4 focus:outline-none md:p-6"
          >
            <div class="w-full max-w-5xl">
              {#if settings?.pending}
                <div
                  role="alert"
                  aria-labelledby="configuration-warning-title"
                  class="mb-4 flex items-start gap-3 border border-l-4 border-amber-500 bg-amber-50 p-4 text-sm text-amber-950 dark:bg-amber-950/40 dark:text-amber-100"
                >
                  <TriangleAlert
                    class="mt-0.5 size-5 shrink-0 text-amber-700 dark:text-amber-400"
                    aria-hidden="true"
                  />
                  <div class="min-w-0 space-y-2">
                    <h3 id="configuration-warning-title" class="text-base font-semibold">
                      {settings.pending.status === 'changed'
                        ? 'Configuration changes not applied'
                        : 'Configuration could not be loaded'}
                    </h3>
                    {#if settings.pending.status === 'changed'}
                      <p>
                        The local configuration file{#if localConfigFilename}
                          {' '}(<code>{localConfigFilename}</code>){/if} has changed and needs to be reloaded.
                      </p>
                      <Button
                        type="button"
                        class="bg-amber-400 text-amber-950 hover:bg-amber-300"
                        disabled={busy}
                        onclick={applyConfiguration}>Reload configuration</Button
                      >
                    {:else}
                      <p>
                        {settings.pending.status === 'unreadable'
                          ? 'The configuration file cannot be read.'
                          : 'The configuration file contains invalid settings.'} Nogon is still using
                        the previously loaded settings. Fix the file, then refresh settings.
                      </p>
                    {/if}
                  </div>
                </div>
              {/if}
              {#each tabErrors(category).filter((error) => !settings?.fields.some((field) => field.path === error.path)) as error}<p
                  class="text-nogon-rose my-2 text-sm"
                >
                  {error.path}: {error.message}
                </p>{/each}
              <div class="space-y-6">
                {#if category === 'semantic_search'}
                  <p class="text-text-brand-muted text-sm">
                    Saving does not start reindexing or test the embedding endpoint. Run <code
                      >nogon reindex</code
                    > to rebuild existing embeddings when needed. Ranking and query settings apply without
                    reindexing.
                  </p>
                {:else if category === 'extractors'}
                  <p class="text-text-brand-muted text-sm">
                    Changes apply to future extraction. Fetch documents again to update previously
                    extracted content.
                  </p>
                {/if}
                {#each groups as group, index (group.label)}
                  <section
                    aria-labelledby={group.label ? `${category}-group-${index}` : undefined}
                    class="border-border-brand-muted bg-card-surface border"
                  >
                    {#if group.label}
                      <div class="border-border-brand-muted border-b px-4 py-4 md:px-6">
                        <h3
                          id={`${category}-group-${index}`}
                          class="font-space text-text-brand text-lg font-semibold tracking-tight"
                        >
                          {group.label}
                        </h3>
                      </div>
                    {/if}
                    <div>
                      {#each group.fields as field (field.path)}
                        {@const active = values.get(field.path)}
                        {@const pending = pendingValues.get(field.path)}
                        {#if active}<SettingsField
                            {field}
                            {active}
                            edit={edits[field.path]}
                            conflict={conflicts[field.path]}
                            errors={errors
                              .filter((error) => error.path === field.path)
                              .map((error) => error.message)}
                            disabled={busy}
                            {editor}
                            {removal}
                            blocked={needsReload || !!settings.pending}
                            onEditor={changeEditor}
                            onDone={(value) => finishItem(field.path, value)}
                            onRemove={(value, previous) => removeItem(field.path, value, previous)}
                            onUndoRemoval={undoRemoval}
                            onEdit={(edit) => update(field.path, edit)}
                          />{/if}
                        {#if pending && !sameSetting(active, pending)}
                          <SettingsField {field} active={pending} preview onEdit={() => {}} />
                        {/if}
                      {/each}
                    </div>
                  </section>
                {/each}
              </div>
            </div>
          </div>
          <div
            role="group"
            aria-label="Settings actions"
            class="shrink-0 border-t px-4 py-3 md:px-6 {dirty
              ? 'border-nogon-indigo/50 bg-nogon-indigo/5'
              : 'border-border-brand-muted bg-page-bg'}"
          >
            <div
              class="flex w-full max-w-5xl flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
            >
              <div class="min-w-0 sm:flex-1">
                {#if editor}
                  <p class="text-text-brand text-sm">
                    Finish or cancel the open item before saving.
                  </p>
                  <div class="flex flex-wrap gap-2">
                    <Button
                      type="button"
                      variant="link"
                      size="sm"
                      disabled={busy}
                      onclick={async () => {
                        if (!editor) return;
                        selectCategory(editor.path.split('.')[0]);
                        await tick();
                        document
                          .getElementById(`setting-${editor.path}`)
                          ?.scrollIntoView({ block: 'nearest' });
                      }}>Continue editing</Button
                    >
                    <Button
                      type="button"
                      variant="link"
                      size="sm"
                      disabled={busy}
                      onclick={() => changeEditor(null)}>Cancel edit</Button
                    >
                  </div>
                {/if}
                <p
                  aria-live="polite"
                  aria-atomic="true"
                  class="text-sm {dirty ? 'text-text-brand font-medium' : 'text-text-brand-muted'}"
                >
                  {dirty
                    ? `${editCount} unsaved ${editCount === 1 ? 'change' : 'changes'}`
                    : 'No unsaved changes'}
                </p>
                <p
                  role="status"
                  class="text-text-brand-muted text-xs break-words {message ? 'mt-1' : ''}"
                >
                  {message}
                </p>
              </div>
              <div class="flex flex-wrap items-center justify-end gap-2 sm:shrink-0">
                <Button
                  type="button"
                  variant="ghost"
                  disabled={busy || !dirty}
                  onclick={discard}
                  class="text-text-brand-secondary px-2 sm:px-3">Discard changes</Button
                >
                <Button
                  id="save-settings"
                  type="submit"
                  disabled={busy ||
                    !dirty ||
                    !!editor ||
                    needsReload ||
                    !!settings.pending ||
                    conflictCount > 0}
                  class="px-2 sm:px-4">Save and apply</Button
                >
              </div>
            </div>
          </div>
        </form>
      </div>
    {/if}
  </div>
</div>

<Dialog.Root
  open={review !== null}
  onOpenChange={(open) => {
    if (!open) cancelReview();
  }}
>
  <Dialog.Content
    showCloseButton={false}
    class="border-border-brand bg-card-surface max-h-[calc(100dvh-2rem)] overflow-y-auto"
    onOpenAutoFocus={(event) => {
      event.preventDefault();
      document.getElementById('cancel-sensitive-settings')?.focus();
    }}
    onCloseAutoFocus={(event) => {
      event.preventDefault();
      document.getElementById('save-settings')?.focus();
    }}
  >
    <Dialog.Header>
      <Dialog.Title>Review sensitive changes</Dialog.Title>
      <Dialog.Description>
        Review the complete draft before applying it.
        {#if review?.mode !== 'acknowledge'}You will need to confirm working access separately after
          activation.{/if}
      </Dialog.Description>
    </Dialog.Header>
    {#if review}
      <dl class="space-y-3 text-sm">
        {#each review.changes as change}
          <div class="border-border-brand-muted space-y-1 border-b pb-3 [overflow-wrap:anywhere]">
            <dt class="font-medium">
              {settings?.fields.find((field) => field.path === change.path)?.label ?? change.path}
            </dt>
            <dd>Before: {change.before}</dd>
            <dd>After: {change.after}</dd>
          </div>
        {/each}
      </dl>
      <ul class="list-disc space-y-2 pl-5 text-sm [overflow-wrap:anywhere]">
        {#each review.consequences as consequence}<li>{consequence}</li>{/each}
      </ul>
      {#if review.destination_url}
        <p class="text-sm [overflow-wrap:anywhere]">Destination: {review.destination_url}</p>
      {/if}
    {/if}
    <Dialog.Footer>
      <Button id="cancel-sensitive-settings" type="button" variant="outline" onclick={cancelReview}>
        Cancel
      </Button>
      <Button
        type="button"
        onclick={() => {
          if (review) void save(review);
        }}
      >
        Confirm sensitive changes
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
