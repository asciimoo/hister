<script lang="ts">
  import { onMount } from 'svelte';
  import { apiFetch, fetchConfig } from '#lib/api.js';
  import { resolve } from '$app/paths';
  import { redirectToAuth } from '#lib/auth-navigation.js';
  import { Button } from '@nogon/components/ui/button';
  import * as Card from '@nogon/components/ui/card';
  import * as Dialog from '@nogon/components/ui/dialog';
  import { PageHeader } from '@nogon/components';
  import { StatusMessage } from '#lib/components/index.js';
  import { Copy, Eye, EyeOff, RefreshCw, User, Info } from '@lucide/svelte';

  let username = $state('');
  let userId = $state(0);
  let revision = $state(0);
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
    if (cfg.authMode !== 'user') {
      window.location.href = resolve('/');
      return;
    }
    if (!cfg.authenticated) {
      redirectToAuth();
      return;
    }
    apiFetch('/profile')
      .then((r) => {
        if (!r.ok) throw new Error('Failed to load profile');
        return r.json();
      })
      .then((data) => {
        if (
          !Number.isSafeInteger(data.user_id) ||
          data.user_id <= 0 ||
          !Number.isSafeInteger(data.revision) ||
          data.revision <= 0
        ) {
          throw new Error('Invalid profile response');
        }
        username = data.username;
        userId = data.user_id;
        revision = data.revision;
        isAdmin = data.is_admin ?? false;
        version = data.version ?? '';
      })
      .catch(() => {
        message = 'Failed to load profile';
        messageType = 'error';
      });
  });

  async function generateToken() {
    if (generating || copying || !userId || !revision) return;
    generating = true;
    message = '';
    token = '';
    tokenVisible = false;
    try {
      const r = await apiFetch(`/users/${userId}/token`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ revision }),
      });
      if (!r.ok) {
        revision = 0;
        message =
          r.status === 409
            ? 'Your account changed. Reload this page before resetting the token again.'
            : 'Failed to generate token. Reload this page before trying again.';
        messageType = 'error';
        return;
      }
      const data = await r.json();
      if (
        typeof data.token !== 'string' ||
        !data.token ||
        !Number.isSafeInteger(data.revision) ||
        data.revision !== revision + 1
      ) {
        throw new Error('Invalid token response');
      }
      token = data.token;
      revision = data.revision;
      tokenVisible = true;
      message = 'New token generated. Store it securely. It will not be shown again.';
      messageType = 'success';
    } catch {
      revision = 0;
      message = 'Failed to generate token. Reload this page before trying again.';
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
  <title>Profile - Nogon</title>
</svelte:head>

<div class="flex-1 overflow-y-auto px-4 py-6 md:px-12 md:py-10">
  <PageHeader color="nogon-indigo" class="mx-auto mb-8 max-w-2xl">Profile</PageHeader>

  <div class="mx-auto max-w-2xl space-y-6">
    <!-- User info card -->
    <Card.Root
      class="bg-card-surface border-nogon-indigo gap-0 overflow-hidden rounded-none border-[3px] py-0 shadow-[6px_6px_0_var(--nogon-indigo)]"
    >
      <Card.Header class="bg-nogon-indigo px-7 py-5">
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
              class="text-text-brand-muted hover:text-nogon-indigo shrink-0 cursor-pointer border-0 bg-transparent p-1 transition-colors"
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
              class="text-text-brand-muted hover:text-nogon-indigo"
            >
              <Copy size={16} />
            </Button>
          </div>
        {/if}

        <Button
          onclick={() => (resetConfirmOpen = true)}
          disabled={generating || copying || !userId || !revision}
          variant="outline"
          class="border-brutal-border font-outfit hover:border-nogon-indigo h-11 w-full border-[3px] text-sm font-bold tracking-wide transition-all hover:shadow-[4px_4px_0_var(--brutal-shadow)] disabled:opacity-50"
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
    <Dialog.Header class="bg-nogon-rose flex-row items-center gap-2 px-5 py-4">
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
        class="bg-nogon-rose font-space border-brutal-border rounded-none border-[3px] text-xs font-bold text-white uppercase"
      >
        Reset Token
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
