<script lang="ts">
  import { page } from '$app/stores';
  import { onMount } from 'svelte';
  import { Button } from '@hister/components/ui/button';
  import { Switch } from '@hister/components/ui/switch';
  import { Sun, Moon } from 'lucide-svelte';
  import "../style.css";

  let { children } = $props();
  let theme = $state("");

  const navItems = [
    { label: 'History', href: 'history' },
    { label: 'Rules', href: 'rules' },
    { label: 'Add', href: 'add' }
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
    theme = localStorage.getItem('theme') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
    applyTheme();
    isDark = theme === 'dark';
  });

  let isDark = $state(false);

  function toggleTheme() {
    const current = document.documentElement.getAttribute('data-theme');
    theme = current === 'dark' ? 'light' : 'dark';
    applyTheme();
    isDark = theme === 'dark';
    localStorage.setItem('theme', theme);
  }
</script>

<div class="flex flex-col h-dvh overflow-hidden">
<header class="h-12 md:h-16 px-3 md:px-6 bg-brutal-bg border-b-[3px] border-brutal-border flex items-center justify-between sticky top-0 z-50 gap-2 md:gap-4 shrink-0 overflow-hidden">
  <h1 class="flex items-center gap-1.5 md:gap-2 shrink-0">
    <img src="static/logo.png" alt="Hister logo" class="h-6 w-6 md:h-8 md:w-8" />
    <a data-sveltekit-reload href="./" class="font-space text-lg md:text-[28px] tracking-[1px] md:tracking-[2px] font-extrabold text-text-brand no-underline hover:underline uppercase">
      Hister
    </a>
  </h1>
  <nav class="flex items-center gap-1 md:gap-2">
    {#each navItems as item (item.href)}
      <Button
        variant="ghost"
        href={item.href}
        class="font-space text-[11px] md:text-[13px] tracking-[1px] md:tracking-[1.5px] font-semibold no-underline hover:underline uppercase px-2 md:px-3 h-8 md:h-9 rounded-none {$page.url.pathname === new URL(item.href, $page.url).pathname ? 'text-text-brand font-bold' : 'text-text-brand-secondary hover:text-text-brand'}"
      >
        {item.label}
      </Button>
    {/each}
  </nav>
  <div class="flex items-center gap-1.5 shrink-0" title="Toggle theme">
    <Sun class="size-3.5 md:size-4 text-text-brand-muted" />
    <Switch
      checked={isDark}
      onCheckedChange={toggleTheme}
      class="data-[state=checked]:bg-hister-indigo data-[state=unchecked]:bg-border-brand-muted border-[2px] border-brutal-border h-5 w-9 rounded-none [&_span[data-slot=switch-thumb]]:rounded-none"
    />
    <Moon class="size-3.5 md:size-4 text-text-brand-muted" />
  </div>
</header>

<main class="flex flex-col overflow-clip flex-1 min-h-0">
  {@render children()}
</main>

<footer class="h-10 md:h-12 px-3 md:px-6 bg-brutal-bg border-t-[3px] border-brutal-border flex items-center justify-center gap-1 md:gap-2 text-sm shrink-0">
  <Button variant="ghost" href="help" class="font-space text-[11px] md:text-[13px] tracking-[1px] text-text-brand-secondary hover:text-hister-indigo no-underline hover:underline uppercase px-2 md:px-3 h-8 rounded-none">Help</Button>
  <Button variant="ghost" href="about" class="font-space text-[11px] md:text-[13px] tracking-[1px] text-text-brand-secondary hover:text-hister-indigo no-underline hover:underline uppercase px-2 md:px-3 h-8 rounded-none">About</Button>
  <Button variant="ghost" href="api-docs" class="font-space text-[11px] md:text-[13px] tracking-[1px] text-text-brand-secondary hover:text-hister-indigo no-underline hover:underline uppercase px-2 md:px-3 h-8 rounded-none">API</Button>
  <Button variant="ghost" href="https://github.com/asciimoo/hister/" target="_blank" rel="noopener" class="font-space text-[11px] md:text-[13px] tracking-[1px] text-text-brand-secondary hover:text-hister-indigo no-underline hover:underline uppercase px-2 md:px-3 h-8 rounded-none">GitHub</Button>
</footer>
</div>
