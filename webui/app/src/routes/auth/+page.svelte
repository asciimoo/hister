<script lang="ts">
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import * as Card from '@hister/components/ui/card';
  import { Lock } from 'lucide-svelte';

  let token = $state('');

  function handleSave() {
    localStorage.setItem('access-token', token);
    window.location.href = '/';
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      handleSave();
    }
  }
</script>

<svelte:head>
  <title>Authentication - Hister</title>
</svelte:head>

<div class="flex items-center justify-center min-h-screen p-4 bg-brutal-bg">
  <Card.Root
    class="w-full max-w-md border-[3px] border-brutal-border shadow-[8px_8px_0px_var(--hister-indigo)] rounded-none"
  >
    <Card.Header class="space-y-4 text-center border-b-[3px] border-border-brand-muted pb-6">
      <div class="flex justify-center">
        <div
          class="size-16 rounded-full bg-hister-indigo/10 flex items-center justify-center border-[3px] border-hister-indigo"
        >
          <Lock class="size-8 text-hister-indigo" />
        </div>
      </div>
      <Card.Title
        class="font-outfit text-2xl font-extrabold text-text-brand uppercase tracking-wide"
      >
        Authentication Required
      </Card.Title>
      <Card.Description class="font-inter text-text-brand-secondary">
        Please enter your access token.
      </Card.Description>
    </Card.Header>
    <Card.Content class="pt-6 space-y-6">
      <div class="space-y-2">
        <label
          for="token"
          class="font-space text-sm font-semibold text-text-brand uppercase tracking-wider"
        >
          Token
        </label>
        <Input
          id="token"
          type="password"
          bind:value={token}
          onkeydown={handleKeydown}
          placeholder="Enter your token"
          class="border-[2px] border-brutal-border focus:border-hister-indigo rounded-none h-12 font-mono"
          autofocus
        />
      </div>
      <Button
        onclick={handleSave}
        disabled={!token.trim()}
        class="w-full h-12 bg-hister-indigo hover:bg-hister-indigo/90 border-[3px] border-brutal-border shadow-[4px_4px_0px_var(--brutal-shadow)] hover:shadow-[2px_2px_0px_var(--brutal-shadow)] hover:translate-x-[2px] hover:translate-y-[2px] active:shadow-none active:translate-x-[4px] active:translate-y-[4px] transition-all rounded-none font-space font-bold uppercase tracking-wider disabled:opacity-50 disabled:cursor-not-allowed"
      >
        Save Token
      </Button>
    </Card.Content>
    <Card.Footer class="border-t-[3px] border-border-brand-muted bg-muted-surface/50 py-4">
      <p class="text-xs text-text-brand-muted text-center w-full font-inter">
        Your token will be stored locally and used for API requests.
      </p>
    </Card.Footer>
  </Card.Root>
</div>
