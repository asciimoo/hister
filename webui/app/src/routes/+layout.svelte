<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { ModeWatcher, setMode } from 'mode-watcher';
  import SiteHeader from '#lib/components/SiteHeader.svelte';
  import { Toaster, toast } from '@hister/components/ui/sonner';
  import { fetchConfig, logout, resetConfig } from '#lib/api.js';
  import { publicConfig } from '#lib/stores.js';
  import { setFlashMessage, showFlashMessage } from '#lib/flash.js';
  import { getStoredThemePreference } from '#lib/theme.js';
  import { resolve } from '$app/paths';
  import '../style.css';

  let { children } = $props();

  const config = $derived($publicConfig);
  const storedThemePreference = getStoredThemePreference();

  onMount(() => {
    void showQueuedNotice();

    fetchConfig().catch(() => {});
  });

  $effect(() => {
    if (config) {
      const mode = config.colorScheme === 'automatic' ? 'system' : config.colorScheme;
      untrack(() => setMode(getStoredThemePreference() ?? mode));
    }
  });

  async function showQueuedNotice() {
    if (await showFlashMessage()) return;
    await tick();
    const params = new URLSearchParams(window.location.search);
    if (params.get('notice') === 'logged_out') {
      toast.success('You have been logged out.');
      params.delete('notice');
      const qs = params.toString();
      window.history.replaceState({}, '', `${window.location.pathname}${qs ? `?${qs}` : ''}`);
    }
  }

  async function handleLogout() {
    await logout();
    const isPublic = config?.public;
    resetConfig();
    publicConfig.set(null);
    setFlashMessage('You have been logged out.');
    window.location.href = isPublic ? resolve('/') : resolve('auth');
  }
</script>

{#if config}
  <ModeWatcher
    defaultMode={storedThemePreference ??
      (config.colorScheme === 'automatic' ? 'system' : config.colorScheme)}
  />
{/if}
<Toaster position="top-center" richColors offset="4.75rem" />

<div class="flex h-dvh flex-col overflow-hidden">
  <SiteHeader {config} onLogout={handleLogout} />

  <main class="flex min-h-0 flex-1 flex-col overflow-clip">
    {@render children()}
  </main>
</div>
