<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, fetchConfig } from '$lib/api';
  import { base } from '$app/paths';
  import { Button } from '@hister/components/ui/button';
  import * as Card from '@hister/components/ui/card';
  import * as Dialog from '@hister/components/ui/dialog';
  import { PageHeader } from '@hister/components';
  import { StatusMessage } from '$lib/components';
  import { Copy, Eye, EyeOff, RefreshCw, User, Info } from '@lucide/svelte';

  let username = $state('');
  let token = $state('');
  let tokenVisible = $state(false);
  let message = $state('');
  let messageType = $state<'success' | 'error'>('success');
  let generating = $state(false);
  let copying = $state(false);
  let resetConfirmOpen = $state(false);
  let isAdmin = $state(false);
  let version = $state('');

  onMount(async () => {
    const cfg = await fetchConfig();
    if (cfg.authMode !== 'user' || !cfg.authenticated) {
      window.location.href = base + '/auth';
      return;
    }
    apiFetch('/profile')
      .then((r) => r.json())
      .then((data) => {
        username = data.username;
        isAdmin = data.is_admin ?? false;
        version = data.version ?? '';
      })
      .catch(() => {
        message = 'Failed to load profile';
        messageType = 'error';
      });
  });

  async function generateToken() {
    generating = true;
    message = '';
    try {
      const r = await apiFetch('/profile/token', { method: 'POST' });
      if (!r.ok) {
        message = 'Failed to generate token';
        messageType = 'error';
        return;
      }
      const data = await r.json();
      token = data.token;
      tokenVisible = true;
      message = 'New token generated. Store it securely. It will not be shown again.';
      messageType = 'success';
    } catch {
      message = 'Failed to generate token';
      messageType = 'error';
    } finally {
      generating = false;
    }
  }

  function confirmTokenReset() {
    resetConfirmOpen = false;
    void generateToken();
  }

  async function copyToken() {
    if (!token || copying || generating) return;
    copying = true;
    try {
      await navigator.clipboard.writeText(token);
      message = 'Token copied to clipboard. Store it securely. It will not be shown again.';
      messageType = 'success';
    } catch {
      message = 'Unable to copy token. Show it and copy it manually.';
      messageType = 'error';
    } finally {
      copying = false;
    }
  }
</script>

<svelte:head>
  <title>Profile - Hister</title>
</svelte:head>

<div class="flex-1 overflow-y-auto px-4 py-6 md:px-12 md:py-10">
  <PageHeader color="hister-indigo" class="mx-auto mb-8 max-w-2xl">Profile</PageHeader>

  <div class="mx-auto max-w-2xl space-y-6">
    <!-- User info card -->
    <Card.Root
      class="bg-card-surface border-hister-indigo gap-0 overflow-hidden rounded-none border-[3px] py-0 shadow-[6px_6px_0_var(--hister-indigo)]"
    >
      <Card.Header class="bg-hister-indigo px-7 py-5">
        <Card.Title
          class="font-outfit flex items-center gap-2 text-xl font-black tracking-wide text-white"
        >
          <User size={20} />
          Account
        </Card.Title>
      </Card.Header>
      <Card.Content class="px-7 py-6">
        <div class="flex items-center gap-3">
          <span
            class="font-outfit text-text-brand-muted text-sm font-bold tracking-widest uppercase"
            >Username</span
          >
          <span class="font-fira text-text-brand text-sm">{username}</span>
        </div>
      </Card.Content>
    </Card.Root>

    <!-- Access token card -->
    <Card.Root
      class="bg-card-surface border-brutal-border gap-0 overflow-hidden rounded-none border-[3px] py-0 shadow-[6px_6px_0_var(--brutal-shadow)]"
    >
      <Card.Header class="border-brutal-border border-b-[3px] px-7 py-5">
        <Card.Title class="font-outfit text-text-brand text-xl font-black tracking-wide"
          >Access Token</Card.Title
        >
        <Card.Description class="font-inter text-text-brand-muted text-sm">
          Use this token to authenticate CLI and API access. Generating a new token will invalidate
          the previous one.
        </Card.Description>
      </Card.Header>
      <Card.Content class="space-y-4 px-7 py-6">
        <div role="status">
          {#if message}
            <StatusMessage {message} type={messageType} />
          {/if}
        </div>

        {#if token}
          <div class="border-brutal-border flex items-center gap-2 border-[3px] px-4 py-3">
            {#if tokenVisible}
              <code class="font-fira text-text-brand min-w-0 flex-1 text-sm break-all">{token}</code
              >
            {:else}
              <code class="font-fira text-text-brand-muted min-w-0 flex-1 text-sm break-all"
                >{'•'.repeat(40)}</code
              >
            {/if}
            <button
              onclick={() => (tokenVisible = !tokenVisible)}
              class="text-text-brand-muted hover:text-hister-indigo shrink-0 cursor-pointer border-0 bg-transparent p-1 transition-colors"
              aria-label={tokenVisible ? 'Hide token' : 'Show token'}
            >
              {#if tokenVisible}
                <EyeOff size={16} />
              {:else}
                <Eye size={16} />
              {/if}
            </button>
            <Button
              variant="ghost"
              size="icon-sm"
              onclick={copyToken}
              disabled={copying || generating}
              title="Copy token"
              aria-label="Copy token"
              class="text-text-brand-muted hover:text-hister-indigo"
            >
              <Copy size={16} />
            </Button>
          </div>
        {/if}

        <Button
          onclick={() => (resetConfirmOpen = true)}
          disabled={generating || copying}
          variant="outline"
          class="border-brutal-border font-outfit hover:border-hister-indigo h-11 w-full border-[3px] text-sm font-bold tracking-wide transition-all hover:shadow-[4px_4px_0_var(--brutal-shadow)] disabled:opacity-50"
        >
          <RefreshCw size={16} class="mr-2 {generating ? 'animate-spin' : ''}" />
          {generating ? 'Resetting Token...' : 'Reset Token'}
        </Button>
      </Card.Content>
    </Card.Root>

    <!-- Instance info card — admin only -->
    {#if isAdmin && version}
      <Card.Root
        class="bg-card-surface border-brutal-border gap-0 overflow-hidden rounded-none border-[3px] py-0 shadow-[6px_6px_0_var(--brutal-shadow)]"
      >
        <Card.Header class="border-brutal-border border-b-[3px] px-7 py-5">
          <Card.Title
            class="font-outfit text-text-brand flex items-center gap-2 text-xl font-black tracking-wide"
          >
            <Info size={20} />
            Instance Info
          </Card.Title>
        </Card.Header>
        <Card.Content class="px-7 py-6">
          <div class="flex items-center gap-3">
            <span
              class="font-outfit text-text-brand-muted w-32 shrink-0 text-sm font-bold tracking-widest uppercase"
              >Version</span
            >
            <span class="font-fira text-text-brand text-sm">{version}</span>
          </div>
        </Card.Content>
      </Card.Root>
    {/if}
  </div>
</div>

<Dialog.Root bind:open={resetConfirmOpen}>
  <Dialog.Content
    showCloseButton={false}
    class="border-border-brand bg-card-surface max-w-md gap-0 overflow-hidden rounded-none border-[3px] p-0 shadow-[6px_6px_0px_var(--brutal-shadow)]"
  >
    <Dialog.Header class="bg-hister-rose flex-row items-center gap-2 px-5 py-4">
      <Dialog.Title class="flex items-center gap-2">
        <RefreshCw class="size-5 text-white" />
        <span class="font-outfit text-lg font-extrabold text-white">Reset access token?</span>
      </Dialog.Title>
    </Dialog.Header>
    <div class="space-y-3 px-5 py-5">
      <Dialog.Description class="font-inter text-text-brand-secondary text-sm">
        Your current access token will stop working immediately. Any CLI, browser extension, or API
        client using it must be updated with the new token.
      </Dialog.Description>
      <p class="font-inter text-text-brand-muted text-xs">The new token will be shown only once.</p>
    </div>
    <Dialog.Footer class="border-border-brand-muted bg-muted-surface border-t-[3px] px-5 py-3">
      <Button
        type="button"
        variant="outline"
        onclick={() => (resetConfirmOpen = false)}
        class="border-border-brand-muted text-text-brand-secondary rounded-none"
      >
        Cancel
      </Button>
      <Button
        type="button"
        onclick={confirmTokenReset}
        class="bg-hister-rose font-space border-brutal-border rounded-none border-[3px] text-xs font-bold text-white uppercase"
      >
        Reset Token
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
