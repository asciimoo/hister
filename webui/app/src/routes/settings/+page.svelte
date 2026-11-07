<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { MediaQuery } from 'svelte/reactivity';
  import { beforeNavigate } from '$app/navigation';
  import { Button } from '@hister/components/ui/button';
  import { fetchConfig, resetConfig } from '#lib/api.js';
  import { redirectToAuth } from '#lib/auth-navigation.js';
  import SettingsField from '#lib/components/SettingsField.svelte';
  import {
    categoryLabels,
    groupSettings,
    settingsRequest,
    SettingsError,
    type SettingsState,
    type SettingEdit,
    type SettingError,
  } from '#lib/settings.js';

  let state = $state<SettingsState | null>(null);
  let edits = $state<Record<string, SettingEdit>>({});
  let errors = $state<SettingError[]>([]);
  let category = $state('app');
  let busy = $state(true);
  let denied = $state(false);
  let message = $state('Loading settings…');
  let problem = $state('');
  let needsReload = $state(false);
  let destroyed = false;
  const sidebarLayout = new MediaQuery('(min-width: 64rem)');
  const categories = $derived([...new Set(state?.fields.map((field) => field.category) ?? [])]);
  const editCount = $derived(Object.keys(edits).length);
  const dirty = $derived(editCount > 0);
  const values = $derived(new Map(state?.values.map((value) => [value.path, value]) ?? []));
  const groups = $derived(groupSettings(state?.fields ?? [], category));

  onMount(() => {
    void load();
  });
  onDestroy(() => {
    destroyed = true;
  });
  beforeNavigate((navigation) => {
    if (
      (dirty || busy) &&
      !navigation.willUnload &&
      !window.confirm('Leave Settings and discard unsaved changes?')
    )
      navigation.cancel();
  });

  function beforeUnload(event: BeforeUnloadEvent) {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = '';
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

  async function readActive() {
    state = await retryRead(() => settingsRequest<SettingsState>());
    await retryRead(() => {
      resetConfig();
      return fetchConfig();
    });
  }

  async function load() {
    busy = true;
    problem = '';
    message = 'Loading settings…';
    try {
      const config = await retryRead(() => fetchConfig());
      if (!config.canConfigure) {
        if (!config.authenticated && config.authMode !== 'none') {
          redirectToAuth();
          return;
        }
        denied = true;
        problem = 'You do not have permission to configure this instance.';
        message = '';
        return;
      }
      state = await retryRead(() => settingsRequest<SettingsState>());
      edits = {};
      errors = [];
      needsReload = false;
      denied = false;
      message = '';
    } catch (error) {
      handleError(error);
    } finally {
      busy = false;
    }
  }

  async function reload() {
    if (busy || (dirty && !window.confirm('Reload settings and discard your unsaved draft?')))
      return;
    resetConfig();
    await load();
  }

  function handleError(error: unknown) {
    if (error instanceof SettingsError) {
      if (error.status === 403 || error.status === 401) {
        denied = true;
        state = null;
        edits = {};
        problem = 'You do not have permission to configure this instance.';
      } else if (error.status === 409) {
        needsReload = true;
        problem =
          error.code === 'sources_changed'
            ? 'Configuration sources changed or cannot be read. Reload the server configuration, then reload settings.'
            : 'Settings changed since you opened this page. Reload settings before saving again.';
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
    if (edit) edits[path] = edit;
    else delete edits[path];
    errors = errors.filter((error) => error.path !== path);
    if (!needsReload) problem = '';
    message = '';
  }

  function discard() {
    if (busy || !dirty) return;
    edits = {};
    errors = [];
    if (!needsReload) problem = '';
    message = 'Unsaved changes discarded.';
  }

  function tabErrors(tab: string) {
    return errors.filter((error) => error.path.split('.')[0] === tab);
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
    category = categories[next];
    document.getElementById(`tab-${category}`)?.focus();
  }

  async function save() {
    if (!state || busy || !dirty || needsReload) return;
    busy = true;
    errors = [];
    problem = '';
    message = 'Validating settings…';
    const draft = { revision: state.revision, edits: Object.values(edits) };
    let submitted = false;
    let saved = false;
    try {
      await settingsRequest('/validate', draft);
      message = 'Applying settings. Service may be briefly interrupted…';
      submitted = true;
      await settingsRequest('/apply', draft);
      saved = true;
      message = 'Reconnecting and reading active settings…';
      await readActive();
      edits = {};
      message = 'Settings saved and applied.';
    } catch (error) {
      handleError(error);
      if (submitted && !(error instanceof SettingsError && error.status < 500)) {
        needsReload = true;
        if (saved)
          problem =
            'Settings were saved, but active settings could not be refreshed. Reload settings before making another change.';
        else if (!(error instanceof SettingsError))
          problem =
            'The save response was lost. The change may have been applied. Reload settings to check the active values before saving again.';
        else problem += ' Reload settings before saving again.';
        // Show the active values alongside the retained draft, without accepting
        // a new revision for another save until the owner explicitly reloads.
        if (!saved) {
          try {
            await readActive();
          } catch (readError) {
            if (readError instanceof SettingsError && [401, 403].includes(readError.status))
              handleError(readError);
            else problem += ' The server could not be reached to read active settings.';
          }
        }
      }
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head><title>Settings | Hister</title></svelte:head>
<svelte:window onbeforeunload={beforeUnload} />

<div class="bg-page-bg text-text-brand flex min-h-0 flex-1 flex-col">
  <div class="flex min-h-0 w-full flex-1 flex-col pt-5">
    <div class="flex shrink-0 flex-col px-4 md:px-6">
      <h2 class="font-space text-2xl font-bold tracking-tight">Settings</h2>
      <p class="text-text-brand-muted my-2 text-sm">
        Configure appearance, search behavior, logging and indexing. Other settings are shown for
        reference.
      </p>
      {#if problem}<div role="alert" class="text-hister-rose my-2 text-sm">
          <p>{problem}{needsReload && dirty ? ' Your draft is still here.' : ''}</p>
          {#each errors.filter((error) => !error.path || !categories.includes(error.path.split('.')[0])) as error}<p
            >
              {error.message}
            </p>{/each}
        </div>{/if}
      {#if !state || denied}
        <p role="status" class="text-text-brand-muted my-2 text-sm">{message}</p>
      {/if}
      {#if needsReload || (!state && !denied && !busy)}<Button
          type="button"
          variant="outline"
          disabled={busy}
          onclick={reload}
          class="mb-3 self-start">Reload settings</Button
        >{/if}
    </div>
    {#if state && !denied}
      <div class="border-border-brand-muted flex min-h-0 flex-1 flex-col lg:flex-row lg:border-t">
        <div
          role="tablist"
          aria-label="Configuration categories"
          aria-orientation={sidebarLayout.current ? 'vertical' : 'horizontal'}
          class="border-border-brand-muted flex shrink-0 gap-1 overflow-x-auto border-b px-4 py-2 md:px-6 lg:w-56 lg:flex-col lg:overflow-x-hidden lg:overflow-y-auto lg:border-r lg:border-b-0 lg:p-3"
        >
          {#each categories as item, index}
            {@const count = tabErrors(item).length}
            <button
              type="button"
              role="tab"
              id={`tab-${item}`}
              aria-controls={`panel-${item}`}
              aria-selected={category === item}
              tabindex={category === item ? 0 : -1}
              onclick={() => (category = item)}
              onkeydown={(event) => tabKeydown(event, index)}
              class="font-inter hover:bg-muted-surface shrink-0 cursor-pointer border-b-2 px-3 py-2 text-sm whitespace-nowrap transition-colors lg:border-b-0 lg:border-l-2 lg:py-3 lg:text-left lg:whitespace-normal {category ===
              item
                ? 'border-hister-indigo text-text-brand lg:bg-hister-indigo/10 font-semibold'
                : 'text-text-brand-muted border-transparent font-medium'}"
              >{categoryLabels[item] ?? item}{count
                ? ` (${count} ${count === 1 ? 'error' : 'errors'})`
                : ''}</button
            >
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
              {#each tabErrors(category).filter((error) => !state?.fields.some((field) => field.path === error.path)) as error}<p
                  class="text-hister-rose my-2 text-sm"
                >
                  {error.path}: {error.message}
                </p>{/each}
              <div class="space-y-6">
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
                        {#if active}<SettingsField
                            {field}
                            {active}
                            edit={edits[field.path]}
                            errors={errors
                              .filter((error) => error.path === field.path)
                              .map((error) => error.message)}
                            disabled={busy}
                            onEdit={(edit) => update(field.path, edit)}
                          />{/if}
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
              ? 'border-hister-indigo/50 bg-hister-indigo/5'
              : 'border-border-brand-muted bg-page-bg'}"
          >
            <div
              class="flex w-full max-w-5xl flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
            >
              <div class="min-w-0 sm:flex-1">
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
                <Button type="submit" disabled={busy || !dirty || needsReload} class="px-2 sm:px-4"
                  >Save and apply</Button
                >
              </div>
            </div>
          </div>
        </form>
      </div>
    {/if}
  </div>
</div>
