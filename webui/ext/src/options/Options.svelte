<script lang="ts">
  import { Button } from '@nogon/components/ui/button';
  import { Input } from '@nogon/components/ui/input';
  import { Label } from '@nogon/components/ui/label';
  import { Switch } from '@nogon/components/ui/switch';
  import * as Card from '@nogon/components/ui/card';
  import SettingsInput from './SettingsInput.svelte';
  import { Plus, Trash2, Sun, Moon } from '@lucide/svelte';
  import { ModeWatcher, toggleMode, mode } from 'mode-watcher';
  import { fetchAPI, syncServerCookies } from '../modules/network';
  import { DEFAULT_SERVER_URL } from '../modules/settings';

  let url = $state(DEFAULT_SERVER_URL);
  let accessToken = $state('');
  let customHeaders: { name: string; value: string }[] = $state([]);
  let submitPublicDocuments = $state(false);
  let profileUserID = $state(0);
  let message = $state('');
  let messageType: 'success' | 'error' = $state('success');

  function isAuthenticated(userID: number): boolean {
    return userID > 0;
  }

  chrome.storage.local.get(
    [
      'histerURL',
      'histerToken',
      'histerCustomHeaders',
      'submitPublicDocuments',
      'histerProfileUserID',
    ],
    (data) => {
      if (!data['histerURL']) {
        chrome.storage.local.set({ histerURL: DEFAULT_SERVER_URL });
      }
      url = data['histerURL'] || DEFAULT_SERVER_URL;
      accessToken = data['histerToken'] || '';
      customHeaders = Array.isArray(data['histerCustomHeaders']) ? data['histerCustomHeaders'] : [];
      submitPublicDocuments = data['submitPublicDocuments'] === true;
      profileUserID = Number(data['histerProfileUserID'] ?? 0);
      refreshProfileUserID(url, customHeaders);
    },
  );

  function setProfileUserID(userID: number) {
    profileUserID = userID;
    chrome.storage.local.set({ histerProfileUserID: userID });
    if (!isAuthenticated(userID) && submitPublicDocuments) {
      submitPublicDocuments = false;
      chrome.storage.local.set({ submitPublicDocuments: false });
    }
  }

  async function refreshProfileUserID(
    serverURL: string,
    headersList = customHeaders,
    token = accessToken,
  ): Promise<boolean> {
    let profileURL = serverURL;
    if (!profileURL.endsWith('/')) {
      profileURL += '/';
    }
    try {
      const response = await fetchAPI(profileURL + 'api/profile', {
        customHeaders: headersList,
        accessToken: token,
      });
      if (!response.ok) {
        setProfileUserID(0);
        return false;
      }
      let userID = 0;
      try {
        const profile = await response.json();
        userID = Number(profile?.user_id ?? 0);
      } catch (_) {}
      setProfileUserID(userID);
      return true;
    } catch (_) {
      setProfileUserID(0);
      return false;
    }
  }

  function addHeader() {
    customHeaders.push({ name: '', value: '' });
  }

  function removeHeader(index: number) {
    customHeaders.splice(index, 1);
  }

  async function save(e: Event) {
    e.preventDefault();
    const headersToSave = customHeaders.filter((h) => h.name.trim() !== '');
    const tokenToSave = accessToken.trim();
    await chrome.storage.local.set({
      histerURL: url,
      histerToken: tokenToSave,
      histerCustomHeaders: $state.snapshot(headersToSave),
      submitPublicDocuments: isAuthenticated(profileUserID) && submitPublicDocuments,
    });
    customHeaders = headersToSave;
    accessToken = tokenToSave;
    const authenticated = await refreshProfileUserID(url, headersToSave, tokenToSave);
    if (tokenToSave && !authenticated) {
      message = 'Settings saved, but the access token was rejected.';
      messageType = 'error';
    } else {
      message = 'Settings saved';
      messageType = 'success';
    }
  }

  function toggleSubmitPublicDocuments() {
    chrome.storage.local.set({
      submitPublicDocuments: isAuthenticated(profileUserID) && submitPublicDocuments,
    });
  }

  async function authenticate() {
    let authURL = url;
    if (!authURL.endsWith('/')) {
      authURL += '/';
    }
    try {
      const cookieHeader = await syncServerCookies(authURL);
      if (!cookieHeader) {
        message =
          'No cookies found for server URL. Make sure you are logged in to the Nogon web app.';
        messageType = 'error';
        return;
      }
      if (await refreshProfileUserID(url)) {
        message = 'Authentication successful';
        messageType = 'success';
      } else {
        message = 'Authentication failed. Make sure you are logged in to the Nogon web app.';
        messageType = 'error';
      }
    } catch (error) {
      message = (error as Error).message ?? 'Failed to read browser cookies.';
      messageType = 'error';
    }
  }
</script>

<ModeWatcher />

<div class="bg-page-bg min-h-screen">
  <!-- Page header -->
  <div
    class="bg-brutal-bg border-brutal-border flex items-center justify-between border-b-[3px] px-8 py-5"
  >
    <span class="font-outfit text-text-brand-muted text-sm font-bold tracking-widest uppercase">
      Nogon <span class="mx-1">/</span> Options
    </span>
    <button
      onclick={toggleMode}
      class="text-text-brand-muted hover:text-nogon-coral cursor-pointer border-0 bg-transparent p-0 transition-colors"
      aria-label="Toggle theme"
    >
      {#if mode.current === 'light'}
        <Moon size={18} />
      {:else}
        <Sun size={18} />
      {/if}
    </button>
  </div>

  <div class="mx-auto max-w-2xl space-y-8 px-8 py-10">
    <!-- Connection settings card -->
    <Card.Root
      class="bg-card-surface border-nogon-indigo gap-0 overflow-hidden rounded-none border-[3px] py-0 shadow-[6px_6px_0_var(--nogon-indigo)]"
    >
      <Card.Header class="bg-nogon-indigo px-7 py-5">
        <Card.Title class="font-outfit text-xl font-black tracking-wide text-white"
          >Connection Settings</Card.Title
        >
        <Card.Description class="font-inter text-sm text-white/70"
          >Configure how the extension connects to your Nogon server.</Card.Description
        >
      </Card.Header>

      <Card.Content class="space-y-6 p-7">
        {#if message}
          <div
            class="font-inter border-l-[4px] px-4 py-3 text-sm {messageType === 'success'
              ? 'border-l-nogon-teal bg-nogon-teal/10 text-nogon-teal'
              : 'border-l-nogon-rose bg-nogon-rose/10 text-nogon-rose'}"
          >
            {message}
          </div>
        {/if}

        <form onsubmit={save} class="space-y-6">
          <SettingsInput
            label="Server URL"
            bind:value={url}
            placeholder="Server URL..."
            description="The full URL of your Nogon server, including the port number."
          />

          <SettingsInput
            label="Access Token"
            bind:value={accessToken}
            type="password"
            placeholder="Optional access token..."
            description="Authenticate with a global or personal access token. Leave blank to use browser cookies."
          />

          <!-- Custom Headers -->
          <div class="space-y-3">
            <div>
              <Label class="font-outfit text-text-brand text-sm font-bold">Custom Headers</Label>
              <p class="text-text-brand-muted font-inter mt-1 text-xs">
                Add custom HTTP headers sent with every request. Useful for reverse proxy
                authentication.
              </p>
            </div>
            {#each customHeaders as header, i}
              <div class="flex items-center gap-2">
                <Input
                  bind:value={header.name}
                  placeholder="Header name"
                  class="bg-page-bg border-nogon-indigo font-fira text-text-brand placeholder:text-text-brand-muted focus-visible:border-nogon-coral h-10 border-[3px] px-3 text-sm shadow-none transition-colors focus-visible:ring-0"
                />
                <Input
                  bind:value={header.value}
                  placeholder="Header value"
                  class="bg-page-bg border-nogon-indigo font-fira text-text-brand placeholder:text-text-brand-muted focus-visible:border-nogon-coral h-10 border-[3px] px-3 text-sm shadow-none transition-colors focus-visible:ring-0"
                />
                <button
                  type="button"
                  onclick={() => removeHeader(i)}
                  class="text-text-brand-muted hover:text-nogon-rose h-10 shrink-0 cursor-pointer border-0 bg-transparent p-2 transition-colors"
                  aria-label="Remove header"
                >
                  <Trash2 size={16} />
                </button>
              </div>
            {/each}
            <Button
              type="button"
              variant="outline"
              onclick={addHeader}
              class="border-brutal-border font-outfit hover:border-nogon-indigo h-9 border-[3px] text-sm font-bold tracking-wide transition-all"
            >
              <Plus size={16} class="mr-1" />
              Add Header
            </Button>
          </div>

          {#if isAuthenticated(profileUserID)}
            <div class="flex items-center justify-between">
              <div>
                <Label
                  for="submit-public-documents"
                  class="font-outfit text-text-brand cursor-pointer text-sm font-bold"
                >
                  Submit as public documents
                </Label>
                <p class="text-text-brand-muted font-inter mt-1 text-xs">
                  Store newly indexed documents as public results with user id 0.
                </p>
              </div>
              <Switch
                id="submit-public-documents"
                bind:checked={submitPublicDocuments}
                onCheckedChange={toggleSubmitPublicDocuments}
              />
            </div>
          {/if}

          <Button
            type="submit"
            size="lg"
            class="bg-nogon-coral border-brutal-border font-outfit h-12 w-full border-[3px] text-base font-bold tracking-wide text-white shadow-[4px_4px_0_var(--brutal-shadow)] transition-all hover:translate-x-px hover:translate-y-px hover:shadow-[2px_2px_0_var(--brutal-shadow)]"
          >
            Save Settings
          </Button>

          <Button
            type="button"
            variant="outline"
            size="lg"
            onclick={authenticate}
            class="border-brutal-border font-outfit hover:border-nogon-indigo h-12 w-full border-[3px] text-base font-bold tracking-wide transition-all"
          >
            Authenticate with Browser Session
          </Button>
        </form>
      </Card.Content>
    </Card.Root>

    <!-- Indexing rules placeholder -->
    <Card.Root
      class="bg-card-surface border-brutal-border gap-0 overflow-hidden rounded-none border-[3px] py-0 opacity-50"
    >
      <Card.Header class="px-7 py-5">
        <Card.Title class="font-outfit text-text-brand text-lg font-bold">Indexing Rules</Card.Title
        >
        <Card.Description class="font-inter text-text-brand-muted text-sm"
          >Coming soon — configure which pages to index or skip.</Card.Description
        >
      </Card.Header>
    </Card.Root>
  </div>
</div>
