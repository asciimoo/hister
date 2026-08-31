<script lang="ts">
  import { page } from '$app/stores';
  import { base } from '$app/paths';
  import { Button } from '@hister/components/ui/button';
  import * as DropdownMenu from '@hister/components/ui/dropdown-menu';
  import { userPrefersMode } from 'mode-watcher';
  import {
    ExternalLink,
    Keyboard,
    LogIn,
    LogOut,
    Menu,
    Monitor,
    Moon,
    Sun,
    UserRound,
  } from '@lucide/svelte';
  import type { AppConfig } from '$lib/api';
  import { showHelp } from '$lib/stores';
  import { setThemePreference, type ThemePreference } from '$lib/theme';

  let { config, onLogout }: { config: AppConfig | null; onLogout: () => void } = $props();

  const navItems = [
    { label: 'History', href: 'history', color: 'var(--hister-indigo)' },
    { label: 'Rules', href: 'rules', color: 'var(--hister-teal)' },
  ];
  const addMenuItem = { label: 'Add', href: 'add', color: 'var(--hister-coral)' };

  const secondaryItems = [
    { label: 'Help', href: 'help', color: 'var(--hister-indigo)' },
    { label: 'Extractors', href: 'extractors', color: 'var(--hister-cyan)' },
    { label: 'Storage', href: 'storage', color: 'var(--hister-amber)' },
    { label: 'About', href: 'about', color: 'var(--hister-teal)' },
    { label: 'API', href: 'api-docs', color: 'var(--hister-coral)' },
    {
      label: 'GitHub',
      href: 'https://github.com/asciimoo/hister/',
      color: 'var(--hister-amber)',
      external: true,
    },
  ];

  const menuItem =
    'font-space text-text-brand-muted data-[highlighted]:bg-muted-surface data-[highlighted]:text-text-brand cursor-pointer rounded-none px-3 py-2 text-xs font-semibold tracking-wider uppercase';
  const themeMenuItem =
    'font-space text-text-brand-muted focus:bg-muted-surface focus:text-text-brand cursor-pointer rounded-none py-2 text-xs font-semibold tracking-wider uppercase';
  const navLink =
    'primary-link font-space relative p-3 text-[11px] font-semibold tracking-[1px] uppercase no-underline transition-colors hover:no-underline md:p-6 md:text-[13px] md:tracking-[1.5px]';

  const showWriteNav = $derived(!config?.public || !!config?.canWrite);
  const showLogin = $derived(
    !!config &&
      !config.authenticated &&
      (config.authMode === 'user' || config.authMode === 'token'),
  );
  const showLogout = $derived(
    !!config && config.authenticated && (config.authMode === 'user' || config.authMode === 'token'),
  );
  const showProfile = $derived(
    config?.authMode === 'user' && !!config.authenticated && !!config.username,
  );
  const appTitle = $derived(config?.title ?? 'Hister');
</script>

<header
  class="site-header bg-brutal-bg border-brutal-border sticky top-0 z-50 flex h-12 shrink-0 items-center justify-between gap-2 overflow-hidden border-b-[3px] px-3 md:grid md:h-16 md:grid-cols-[12rem_auto_12rem] md:justify-stretch md:gap-4 md:px-6"
>
  <h1 class="flex min-w-0 items-center gap-1.5 md:gap-2">
    <a
      data-sveltekit-reload
      href="./"
      class="group flex min-w-0 items-center gap-1.5 no-underline md:gap-2"
    >
      <img src="static/logo.png" alt={`${appTitle} logo`} class="h-6 w-6 md:h-8 md:w-8" />
      <span
        class="font-space text-text-brand truncate text-lg font-extrabold tracking-[1px] uppercase group-hover:underline md:text-[28px] md:tracking-[2px]"
      >
        {appTitle}
      </span>
    </a>
  </h1>

  <nav class="flex items-center justify-self-center" aria-label="Primary">
    {#if showWriteNav}
      {#each navItems as item (item.href)}
        {@const active = $page.url.pathname === new URL(item.href, $page.url).pathname}
        <a
          class="{navLink} {active
            ? 'is-active text-text-brand font-bold'
            : 'text-text-brand-muted hover:bg-muted-surface hover:text-text-brand'}"
          style="--nav-color: {item.color};"
          aria-current={active ? 'page' : undefined}
          href={item.href}>{item.label}</a
        >
      {/each}
    {/if}
  </nav>

  <div class="flex items-center justify-self-end">
    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        {#snippet child({ props })}
          <Button
            {...props}
            variant="ghost"
            size="icon"
            class="text-text-brand-muted hover:text-hister-indigo hover:bg-muted-surface size-8 shrink-0 transition-all md:size-10"
            title="Open menu"
            aria-label="Open navigation menu"
          >
            <Menu class="size-5 md:size-6" />
          </Button>
        {/snippet}
      </DropdownMenu.Trigger>

      <DropdownMenu.Content
        align="end"
        sideOffset={8}
        class="border-brutal-border bg-card-surface w-64 rounded-none border-[3px] p-2 shadow-[4px_4px_0_var(--brutal-shadow)]"
      >
        {#if showWriteNav}
          {@const active = $page.route.id === `/${addMenuItem.href}`}
          <DropdownMenu.Item
            class="primary-menu-item font-space cursor-pointer rounded-none border-l-4 px-3 py-2.5 text-sm font-extrabold tracking-widest uppercase {active
              ? 'is-active text-text-brand'
              : 'text-text-brand-muted'}"
            style="--menu-color: {addMenuItem.color};"
          >
            {#snippet child({ props })}
              <a {...props} aria-current={active ? 'page' : undefined} href={addMenuItem.href}>
                {addMenuItem.label}
              </a>
            {/snippet}
          </DropdownMenu.Item>
          <DropdownMenu.Separator class="bg-border-brand-muted mx-0 my-2 h-[2px]" />
        {/if}

        {#each secondaryItems as item (item.href)}
          {@const active = !item.external && $page.route.id === `/${item.href}`}
          <DropdownMenu.Item
            class="secondary-menu-item {menuItem} {active ? 'is-active text-text-brand' : ''}"
            style="--menu-color: {item.color};"
          >
            {#snippet child({ props })}
              <a
                {...props}
                aria-current={active ? 'page' : undefined}
                href={item.href}
                target={item.external ? '_blank' : undefined}
                rel={item.external ? 'noopener' : undefined}
              >
                {item.label}
                {#if item.external}<ExternalLink class="ml-auto size-3.5" />{/if}
              </a>
            {/snippet}
          </DropdownMenu.Item>
        {/each}

        <DropdownMenu.Separator class="bg-border-brand-muted mx-0 my-2 h-[2px]" />

        {#if $page.route.id === '/'}
          <DropdownMenu.Item
            class={menuItem}
            onSelect={() => ($showHelp = !$showHelp)}
            textValue="Keyboard shortcuts"
          >
            <Keyboard class="size-4" />
            Keyboard shortcuts
          </DropdownMenu.Item>
        {/if}
        <DropdownMenu.Label
          class="font-space text-text-brand-muted px-3 pt-2 pb-1 text-[10px] font-bold tracking-widest uppercase"
        >
          Appearance
        </DropdownMenu.Label>
        <DropdownMenu.RadioGroup
          aria-label="Appearance"
          value={userPrefersMode.current}
          onValueChange={(value) => setThemePreference(value as ThemePreference)}
        >
          <DropdownMenu.RadioItem class={themeMenuItem} value="system">
            <Monitor class="size-4" />
            Automatic
          </DropdownMenu.RadioItem>
          <DropdownMenu.RadioItem class={themeMenuItem} value="light">
            <Sun class="size-4" />
            Light
          </DropdownMenu.RadioItem>
          <DropdownMenu.RadioItem class={themeMenuItem} value="dark">
            <Moon class="size-4" />
            Dark
          </DropdownMenu.RadioItem>
        </DropdownMenu.RadioGroup>

        {#if showProfile || showLogin || showLogout}
          <DropdownMenu.Separator class="bg-border-brand-muted mx-0 my-2 h-[2px]" />
        {/if}
        {#if showProfile}
          <DropdownMenu.Item
            class={menuItem}
            onSelect={() => (window.location.href = base + '/profile')}
          >
            <UserRound class="size-4" />
            Profile
          </DropdownMenu.Item>
        {:else if showLogin}
          <DropdownMenu.Item
            class={menuItem}
            onSelect={() => (window.location.href = base + '/auth')}
          >
            <LogIn class="size-4" />
            Login
          </DropdownMenu.Item>
        {/if}
        {#if showLogout}
          <DropdownMenu.Item class={menuItem} onSelect={() => onLogout()}>
            <LogOut class="size-4" />
            {config?.username ? `Logout ${config.username}` : 'Logout'}
          </DropdownMenu.Item>
        {/if}
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  </div>
</header>

<style>
  .site-header {
    box-shadow: 0 1px 0 color-mix(in srgb, white 6%, transparent) inset;
  }

  .primary-link:hover {
    color: color-mix(in srgb, var(--nav-color) 76%, var(--text-primary-brand));
  }

  .primary-link.is-active {
    background: color-mix(in srgb, var(--nav-color) 9%, transparent);
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--nav-color) 10%, transparent) inset;
  }

  .primary-menu-item {
    border-left-color: var(--menu-color);
  }

  .primary-menu-item[data-highlighted],
  .primary-menu-item.is-active {
    color: color-mix(in srgb, var(--menu-color) 76%, var(--text-primary-brand));
    background: color-mix(in srgb, var(--menu-color) 9%, transparent);
  }

  .secondary-menu-item[data-highlighted],
  .secondary-menu-item.is-active {
    color: color-mix(in srgb, var(--menu-color) 78%, var(--text-primary-brand));
  }

  :global(.dark) .site-header {
    box-shadow: 0 1px 0 color-mix(in srgb, white 8%, transparent) inset;
  }

  :global(.dark) .primary-link:hover {
    color: color-mix(in srgb, var(--nav-color) 84%, white);
  }

  :global(.dark) .primary-link.is-active {
    background: color-mix(in srgb, var(--nav-color) 13%, transparent);
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--nav-color) 16%, transparent) inset;
  }

  :global(.dark) .primary-menu-item[data-highlighted],
  :global(.dark) .primary-menu-item.is-active {
    color: color-mix(in srgb, var(--menu-color) 84%, white);
    background: color-mix(in srgb, var(--menu-color) 13%, transparent);
  }

  :global(.dark) .secondary-menu-item[data-highlighted],
  :global(.dark) .secondary-menu-item.is-active {
    color: color-mix(in srgb, var(--menu-color) 86%, white);
  }
</style>
