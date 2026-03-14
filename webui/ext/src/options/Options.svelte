<script lang="ts">
  import { Button } from '@hister/components/ui/button';
  import * as Card from '@hister/components/ui/card';
  import SettingsInput from './SettingsInput.svelte';
  import { queueStore } from '../modules/queue-store.svelte';
  import QueuePanel from '../modules/QueuePanel.svelte';
  import { STORAGE_KEYS } from '../modules/constants';

  const defaultURL = 'http://127.0.0.1:4433/';

  let url = $state(defaultURL);
  let token = $state('');
  let message = $state('');
  let messageType: 'success' | 'error' = $state('success');

  const queue = queueStore();

  chrome.storage.local.get([STORAGE_KEYS.url, STORAGE_KEYS.token], (data) => {
    if (!data[STORAGE_KEYS.url]) {
      chrome.storage.local.set({ [STORAGE_KEYS.url]: defaultURL });
    }
    url = data[STORAGE_KEYS.url] || defaultURL;
    token = data[STORAGE_KEYS.token] || '';
  });

  queue.refresh();

  async function retry() {
    const r = await queue.retry();
    message = r.message;
    messageType = r.type;
  }

  async function clear() {
    const r = await queue.clear();
    message = r.message;
    messageType = r.type;
  }

  function save(e: Event) {
    e.preventDefault();
    chrome.storage.local.set({ [STORAGE_KEYS.url]: url, [STORAGE_KEYS.token]: token }).then(() => {
      message = 'Settings saved';
      messageType = 'success';
    });
  }
</script>

<div class="bg-page-bg min-h-screen">
  <!-- Page header -->
  <div class="bg-brutal-bg border-brutal-border border-b-[3px] px-8 py-5">
    <span class="font-outfit text-text-brand-muted text-sm font-bold tracking-widest uppercase">
      Hister <span class="mx-1">/</span> Options
    </span>
  </div>

  <div class="mx-auto max-w-2xl space-y-8 px-8 py-10">
    <QueuePanel
      status={queue.status}
      count={queue.count}
      expanded={queue.expanded}
      items={queue.items}
      onretry={retry}
      onclear={clear}
      ontoggle={queue.toggle}
      onremove={queue.remove}
    />

    <!-- Connection settings card -->
    <Card.Root
      class="bg-card-surface border-hister-indigo gap-0 overflow-hidden rounded-none border-[3px] py-0 shadow-[6px_6px_0_var(--hister-indigo)]"
    >
      <Card.Header class="bg-hister-indigo px-7 py-5">
        <Card.Title class="font-outfit text-xl font-black tracking-wide text-white"
          >Connection Settings</Card.Title
        >
        <Card.Description class="font-inter text-sm text-white/70"
          >Configure how the extension connects to your Hister server.</Card.Description
        >
      </Card.Header>

      <Card.Content class="space-y-6 p-7">
        {#if message}
          <div
            class="font-inter border-l-[4px] px-4 py-3 text-sm {messageType === 'success'
              ? 'border-l-hister-teal bg-hister-teal/10 text-hister-teal'
              : 'border-l-hister-rose bg-hister-rose/10 text-hister-rose'}"
          >
            {message}
          </div>
        {/if}

        <form onsubmit={save} class="space-y-6">
          <SettingsInput
            label="Server URL"
            bind:value={url}
            placeholder="Server URL..."
            description="The full URL of your Hister server, including the port number."
          />

          <SettingsInput
            label="Access Token"
            bind:value={token}
            placeholder="Token..."
            description="If your server requires authentication, enter your access token here."
          />

          <Button
            type="submit"
            size="lg"
            class="bg-hister-coral border-brutal-border font-outfit h-12 w-full border-[3px] text-base font-bold tracking-wide text-white shadow-[4px_4px_0_var(--brutal-shadow)] transition-all hover:translate-x-px hover:translate-y-px hover:shadow-[2px_2px_0_var(--brutal-shadow)]"
          >
            Save Settings
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
