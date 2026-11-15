<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { resolve } from '$app/paths';
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import { fetchConfig, resetConfig } from '#lib/api.js';
  import {
    settingsRequest,
    SettingsError,
    type PendingAccess,
    type SettingsState,
  } from '#lib/settings.js';

  let {
    initial,
    destination,
    warning = '',
    onFinished,
  }: {
    initial?: PendingAccess;
    destination: string;
    warning?: string;
    onFinished: (outcome: 'confirmed' | 'cancelled' | 'ended') => Promise<void>;
  } = $props();
  let pending = $state<PendingAccess | null>(null);
  let confirmationToken = $state('');
  let receivedCode = $state(false);
  let busy = $state(false);
  let problem = $state('');
  let needsLogin = $state(false);
  let uncertain = $state(false);
  let noAuthentication = $state(false);
  let now = $state(Date.now());
  let closed = false;
  const target = $derived.by(() => {
    try {
      const url = new URL(pending?.destination_url ?? destination);
      return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password
        ? url
        : null;
    } catch {
      return null;
    }
  });
  const atDestination = $derived(
    !!target && target.origin === page.url.origin && target.pathname === page.url.pathname,
  );
  const remaining = $derived(
    pending ? Math.max(0, Math.ceil((Date.parse(pending.deadline) - now) / 1000)) : null,
  );
  const expired = $derived(remaining === 0);
  const authURL = $derived(
    `${resolve('/')}auth?${new URLSearchParams({
      reauth: '1',
      return_to: page.url.pathname + page.url.search,
      ...(['password', 'oauth'].includes(pending?.authentication ?? '')
        ? { method: pending!.authentication! }
        : {}),
    })}`,
  );
  const oauthURL = $derived(
    `${resolve('/')}api/oauth?${new URLSearchParams({
      provider: pending?.oauth_provider ?? '',
      return_to: page.url.pathname + page.url.search,
    })}`,
  );

  onMount(() => {
    pending = initial ?? null;
    confirmationToken = initial?.confirmation_token ?? '';
    receivedCode = !!confirmationToken;
    problem = warning;
    void refresh();
    const clock = setInterval(() => {
      now = Date.now();
    }, 1000);
    // Only status reads are repeated. Apply, confirm and cancel are never retried.
    const poll = setInterval(() => {
      if (!busy && !needsLogin && atDestination) void refresh();
    }, 3000);
    return () => {
      closed = true;
      clearInterval(clock);
      clearInterval(poll);
      confirmationToken = '';
    };
  });

  async function refresh() {
    if (busy || !atDestination) return;
    busy = true;
    try {
      resetConfig();
      const config = await fetchConfig();
      if (closed) return;
      noAuthentication = config.authMode === 'none';
      if (!config.canConfigure) {
        needsLogin = true;
        problem = 'Sign in as an administrator using the temporary configuration to continue.';
        return;
      }
      const current = await settingsRequest<SettingsState>();
      if (closed) return;
      if (!current.pending_access) {
        await onFinished('ended');
        return;
      }
      if (pending && pending.revision !== current.pending_access.revision) {
        confirmationToken = '';
        receivedCode = false;
      }
      pending = current.pending_access;
      needsLogin = false;
      problem = '';
      uncertain = false;
    } catch (error) {
      if (closed) return;
      if (error instanceof SettingsError && [401, 403].includes(error.status)) needsLogin = true;
      problem =
        'Could not read the pending change. Check the connection or sign in again. Unconfirmed changes automatically roll back after two minutes.';
    } finally {
      busy = false;
    }
  }

  async function complete(cancel: boolean) {
    if (busy || !pending || uncertain || expired || !atDestination) return;
    busy = true;
    problem = '';
    try {
      await settingsRequest(cancel ? '/cancel' : '/confirm', {
        revision: pending.revision,
        ...(confirmationToken ? { confirmation_token: confirmationToken } : {}),
      });
      confirmationToken = '';
      await onFinished(cancel ? 'cancelled' : 'confirmed');
    } catch (error) {
      if (closed) return;
      if (error instanceof SettingsError) {
        problem = error.message;
        needsLogin =
          error.status === 401 ||
          (error.status === 403 && error.code !== 'destination_required' && !noAuthentication);
        uncertain = error.status === 409 || error.status >= 500;
      } else {
        uncertain = true;
        problem =
          'The response was lost. Check status before taking another action; the change may already have completed.';
      }
    } finally {
      busy = false;
    }
  }
</script>

<section
  aria-labelledby="access-confirmation-title"
  class="mx-auto w-full max-w-2xl space-y-4 overflow-y-auto p-4 md:p-6"
>
  <h3 id="access-confirmation-title" class="font-space text-xl font-bold">
    Confirm working access
  </h3>
  <p class="text-sm">
    These settings are temporary until you confirm that you can still administer Hister.
  </p>
  {#if confirmationToken}
    <p class="text-sm">
      Keep this page open until you confirm or restore the previous settings. Leaving or reloading
      loses its private confirmation code. Without the code, you cannot confirm or restore settings
      immediately and must wait for automatic rollback.
    </p>
  {/if}
  {#if remaining !== null}
    <p role="timer" aria-label="Time remaining to confirm" class="font-mono text-lg">
      {Math.floor(remaining / 60)}:{String(remaining % 60).padStart(2, '0')} remaining
    </p>
  {/if}
  {#if expired}
    <p class="text-sm">
      The confirmation time has elapsed. Check status or return to the original address to verify
      that the previous settings were restored.
    </p>
  {:else}
    <p class="text-sm">Without confirmation, the previous settings return after two minutes.</p>
  {/if}
  {#if problem}<p role="alert" class="text-hister-rose text-sm">{problem}</p>{/if}
  {#if !atDestination}
    <p class="text-sm">
      Open the new address to verify access. Keep this page open until the change is confirmed.
    </p>
    {#if target}
      <a
        class="text-hister-indigo block [overflow-wrap:anywhere] underline"
        href={target.href}
        target="_blank"
        rel="noreferrer">Open new address: {target.href}</a
      >
    {:else}
      <p role="alert">
        The server did not supply a valid HTTP or HTTPS destination. Wait for automatic rollback.
      </p>
    {/if}
    {#if confirmationToken}
      <p class="text-sm">
        Copy this private confirmation code into the new page. It is lost when this page closes.
      </p>
      <label for="access-code" class="text-sm">Confirmation code</label>
      <Input
        id="access-code"
        type="password"
        value={confirmationToken}
        readonly
        autocomplete="off"
      />
      <Button
        variant="outline"
        onclick={() =>
          navigator.clipboard.writeText(confirmationToken).catch(() => {
            problem = 'Could not copy the code. Select and copy it from the field.';
          })}>Copy confirmation code</Button
      >
    {/if}
  {:else}
    {#if pending?.authentication || needsLogin}
      <p class="text-sm">
        Sign in again {pending?.authentication === 'password'
          ? 'with your password'
          : pending?.authentication === 'oauth'
            ? 'through the required OAuth provider'
            : 'using the new access settings'}, then confirm below.
      </p>
      <a
        class="text-hister-indigo inline-block underline"
        href={pending?.oauth_provider ? oauthURL : authURL}
      >
        {pending?.oauth_provider ? `Sign in with ${pending.oauth_provider}` : 'Sign in again'}
      </a>
    {/if}
    {#if noAuthentication && !receivedCode}
      <p class="text-sm">
        Enter the private confirmation code from the page that applied this change. If that code was
        lost, wait for automatic rollback.
      </p>
      <label for="access-code" class="text-sm">Confirmation code</label>
      <Input id="access-code" type="password" bind:value={confirmationToken} autocomplete="off" />
    {/if}
    <div class="flex flex-wrap gap-3">
      <Button variant="outline" disabled={busy} onclick={refresh}>Check status</Button>
      <Button
        variant="outline"
        disabled={busy ||
          !pending ||
          uncertain ||
          expired ||
          (noAuthentication && !confirmationToken)}
        onclick={() => complete(true)}>Restore previous settings</Button
      >
      <Button
        disabled={busy ||
          !pending ||
          uncertain ||
          expired ||
          (noAuthentication && !confirmationToken)}
        onclick={() => complete(false)}>Confirm working access</Button
      >
    </div>
  {/if}
</section>
