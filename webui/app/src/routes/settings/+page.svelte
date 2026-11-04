<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { beforeNavigate } from '$app/navigation';
  import { Button } from '@hister/components/ui/button';
  import { fetchConfig, resetConfig } from '#lib/api.js';
  import { redirectToAuth } from '#lib/auth-navigation.js';
  import SettingsField from '#lib/components/SettingsField.svelte';
  import {
    categoryLabels,
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
  const categories = $derived([...new Set(state?.fields.map((field) => field.category) ?? [])]);
  const dirty = $derived(Object.keys(edits).length > 0);
  const values = $derived(new Map(state?.values.map((value) => [value.path, value]) ?? []));

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
            ? 'Configuration sources changed or cannot be read. Reload the server configuration, then reload settings. Your draft is still here.'
            : 'Settings changed since you opened this page. Reload settings before saving again. Your draft is still here.';
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

  function tabErrors(tab: string) {
    return errors.filter((error) => error.path.split('.')[0] === tab);
  }

  function tabKeydown(event: KeyboardEvent, index: number) {
    let next = index;
    switch (event.key) {
      case 'ArrowRight':
        next = (index + 1) % categories.length;
        break;
      case 'ArrowLeft':
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
            'The save response was lost. The change may have been applied. Reload settings to check the active values before saving again. Your draft is still here.';
        else problem += ' Reload settings before saving again. Your draft is still here.';
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
  <div class="mx-auto flex min-h-0 w-full max-w-5xl flex-1 flex-col px-4 pt-5 md:px-8">
    <h2 class="font-space text-2xl font-extrabold">Settings</h2>
    <p class="text-text-brand-muted my-2 text-sm">
      Configure appearance, search behavior and logging. Other settings are shown for reference.
    </p>
    {#if problem}<div role="alert" class="text-hister-rose my-2 text-sm">
        <p>{problem}</p>
        {#each errors.filter((error) => !error.path || !categories.includes(error.path.split('.')[0])) as error}<p
          >
            {error.message}
          </p>{/each}
      </div>{/if}
    <p role="status" class="text-text-brand-muted my-2 text-sm">{message}</p>
    {#if needsReload || (!state && !denied && !busy)}<Button
        type="button"
        variant="outline"
        disabled={busy}
        onclick={reload}
        class="mb-3 self-start">Reload settings</Button
      >{/if}
    {#if state && !denied}
      <div
        role="tablist"
        aria-label="Configuration categories"
        class="border-border-brand-muted flex shrink-0 gap-1 overflow-x-auto border-b pb-2"
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
            class="font-space shrink-0 cursor-pointer border-b-2 px-3 py-2 text-sm font-semibold {category ===
            item
              ? 'border-hister-indigo text-text-brand'
              : 'text-text-brand-muted border-transparent'}"
            >{categoryLabels[item] ?? item}{count
              ? ` (${count} ${count === 1 ? 'error' : 'errors'})`
              : ''}</button
          >
        {/each}
      </div>
      <form
        class="flex min-h-0 flex-1 flex-col"
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
          class="min-h-0 flex-1 overflow-y-auto px-1"
        >
          {#each tabErrors(category).filter((error) => !state?.fields.some((field) => field.path === error.path)) as error}<p
              class="text-hister-rose my-2 text-sm"
            >
              {error.path}: {error.message}
            </p>{/each}
          {#each state.fields.filter((field) => field.category === category) as field (field.path)}
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
        <div
          class="border-border-brand-muted flex shrink-0 flex-wrap items-center justify-between gap-3 border-t py-3"
        >
          <p class="text-text-brand-muted text-sm">
            {dirty ? 'Unsaved changes' : 'No unsaved changes'}
          </p>
          <Button type="submit" disabled={busy || !dirty || needsReload}>Save and apply</Button>
        </div>
      </form>
    {/if}
  </div>
</div>
