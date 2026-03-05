<script lang="ts">
  import { page } from '$app/stores';
  import { onMount } from 'svelte';
  import { Button } from '@hister/components/ui/button';
  import { Sun, Moon } from 'lucide-svelte';
  import '../style.css';

  let { children } = $props();
  let theme = $state('');

  const navItems = [
    { label: 'History', href: 'history' },
    { label: 'Rules', href: 'rules' },
    { label: 'Add', href: 'add' },
  ];

  function applyTheme() {
    document.documentElement.setAttribute('data-theme', theme);
    if (theme === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }

  onMount(() => {
    theme =
      localStorage.getItem('theme') ||
      (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
    applyTheme();
  });

  function toggleTheme() {
    const current = document.documentElement.getAttribute('data-theme');
    theme = current === 'dark' ? 'light' : 'dark';
    applyTheme();
    localStorage.setItem('theme', theme);
  }
</script>

<div class="flex flex-col h-dvh overflow-hidden">
  <header
    class="h-12 md:h-16 px-3 md:px-6 bg-brutal-bg border-b-[3px] border-brutal-border md:grid md:grid-cols-[4rem_auto_4rem] md:justify-stretch justify-between flex items-center sticky top-0 z-50 gap-2 md:gap-4 shrink-0 overflow-hidden"
  >
    <h1 class="flex items-center gap-1.5 md:gap-2 shrink-0">
      <img src="static/logo.png" alt="Hister logo" class="h-6 w-6 md:h-8 md:w-8" />
      <a
        data-sveltekit-reload
        href="./"
        class="font-space text-lg md:text-[28px] tracking-[1px] md:tracking-[2px] font-extrabold text-text-brand no-underline hover:underline uppercase"
      >
        Hister
      </a>
    </h1>
    <nav class="justify-self-center flex items-center">
      {#each navItems as item (item.href)}
        <a
          class="font-space p-3 md:p-6 text-[11px] md:text-[13px] tracking-[1px] md:tracking-[1.5px] font-semibold no-underline hover:underline uppercase {$page
            .url.pathname === new URL(item.href, $page.url).pathname
            ? 'text-text-brand font-bold'
            : 'text-text-brand-secondary hover:text-text-brand'}"
          href={item.href}
        >
          {item.label}
        </a>
      {/each}
    </nav>
    <Button
      variant="ghost"
      size="icon"
      class="text-text-brand-muted hover:text-hister-indigo transition-all hover:scale-110 shrink-0 size-8 md:size-10"
      title="Toggle theme"
      onclick={toggleTheme}
    >
      {#if theme === 'dark'}<Sun class="size-6" />{:else}<Moon class="size-6" />{/if}
    </Button>
  </header>

  <main class="flex flex-col overflow-clip flex-1 min-h-0">
    {@render children()}
  </main>

  <footer
    class="h-12 px-6 bg-brutal-bg border-t-[3px] border-brutal-border flex items-center justify-center gap-6 text-sm"
  >
    <a
      href="help"
      class="font-space text-[13px] tracking-[1px] text-text-brand-secondary hover:text-hister-indigo no-underline hover:underline uppercase"
      >Help</a
    >
    <a
      href="about"
      class="font-space text-[13px] tracking-[1px] text-text-brand-secondary hover:text-hister-indigo no-underline hover:underline uppercase"
      >About</a
    >
    <a
      href="api-docs"
      class="font-space text-[11px] md:text-[13px] tracking-[1px] text-text-brand-secondary hover:text-hister-indigo no-underline hover:underline uppercase"
      >API</a
    >
    <a
      href="https://github.com/asciimoo/hister/"
      class="font-space text-[13px] tracking-[1px] text-text-brand-secondary hover:text-hister-indigo no-underline hover:underline uppercase"
      target="_blank"
      rel="noopener">GitHub</a
    >
  </footer>
</div>
