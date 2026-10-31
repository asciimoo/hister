<script lang="ts">
  import { page } from '$app/state';
  import Menu from '@lucide/svelte/icons/menu';
  import X from '@lucide/svelte/icons/x';
  import Heart from '@lucide/svelte/icons/heart';
  import { Button } from '@hister/components';

  let menuOpen = $state(false);
  let menuButton: HTMLButtonElement;

  const links = [
    { href: '/datasets', label: 'Datasets' },
    { href: '/docs', label: 'Docs' },
    { href: '/posts', label: 'Posts' },
  ];

  function isActive(href: string): boolean {
    if (href === '/') return page.url.pathname === '/';
    return page.url.pathname.startsWith(href);
  }

  function ariaCurrent(href: string): 'page' | 'location' | undefined {
    if (page.url.pathname === href || page.url.pathname === `${href}/`) return 'page';
    return isActive(href) ? 'location' : undefined;
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && menuOpen) {
      menuOpen = false;
      menuButton.focus();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<header class="bg-brutal-bg border-brutal-border w-full border-b-[3px]">
  <nav aria-label="Primary" class="grid grid-cols-[1fr_auto_1fr] items-center px-6 py-4 md:px-12">
    <a
      href="/"
      aria-current={page.url.pathname === '/' ? 'page' : undefined}
      class="group font-space flex items-center gap-1.5 justify-self-start text-[28px] font-extrabold tracking-[2px] text-[var(--text-primary)] uppercase no-underline md:gap-2"
    >
      <img src="/logo.png" alt="" width="32" height="32" class="h-6 w-6 md:h-8 md:w-8" />
      <span class="group-hover:underline">Hister</span>
    </a>

    <ul class="m-0 hidden list-none items-center p-0 md:flex">
      {#each links as link}
        <li>
          <a
            href={link.href}
            aria-current={ariaCurrent(link.href)}
            class="font-space gap-4 px-4 py-8 font-semibold tracking-[1.5px] uppercase no-underline transition-colors hover:underline md:text-sm lg:px-8 {isActive(
              link.href,
            )
              ? 'text-[var(--text-primary)]'
              : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)]'}"
          >
            {link.label}
          </a>
        </li>
      {/each}
    </ul>

    <div class="hidden items-center gap-4 justify-self-end md:flex">
      <Button
        href="/support"
        aria-current={page.url.pathname === '/support' ? 'page' : undefined}
        class="bg-hister-rose font-space border-brutal-border h-auto rounded-none border-[3px] px-5 py-2.5 text-[13px] font-semibold tracking-[1px] text-white uppercase no-underline shadow-[3px_3px_0_rgba(0,0,0,0.25)] transition-all hover:translate-x-[2px] hover:translate-y-[2px] hover:shadow-[1px_1px_0_rgba(0,0,0,0.25)]"
      >
        <Heart size={14} class="shrink-0 fill-white text-white" />
        Support
      </Button>
      <Button
        href="https://github.com/asciimoo/hister"
        target="_blank"
        rel="noopener noreferrer"
        class="bg-hister-indigo font-space border-brutal-border h-auto rounded-none border-[3px] px-5 py-2.5 text-[13px] font-semibold tracking-[1px] text-white uppercase no-underline shadow-[3px_3px_0_rgba(0,0,0,0.25)] transition-all hover:translate-x-[2px] hover:translate-y-[2px] hover:shadow-[1px_1px_0_rgba(0,0,0,0.25)]"
      >
        GitHub
      </Button>
    </div>

    <button
      bind:this={menuButton}
      class="cursor-pointer justify-self-end p-2 md:hidden"
      onclick={() => (menuOpen = !menuOpen)}
      aria-label={menuOpen ? 'Close menu' : 'Open menu'}
      aria-expanded={menuOpen}
      aria-controls="mobile-navigation"
    >
      {#if menuOpen}
        <X size={24} />
      {:else}
        <Menu size={24} />
      {/if}
    </button>
  </nav>

  {#if menuOpen}
    <ul
      id="mobile-navigation"
      class="border-brutal-border bg-brutal-bg m-0 flex list-none flex-col gap-4 border-t-[2px] md:hidden"
    >
      {#each links as link}
        <li>
          <a
            href={link.href}
            aria-current={ariaCurrent(link.href)}
            class="font-space px-6 py-4 text-[15px] font-semibold tracking-[1.5px] uppercase no-underline {isActive(
              link.href,
            )
              ? 'text-[var(--text-primary)]'
              : 'text-[var(--text-secondary)]'}"
            onclick={() => (menuOpen = false)}
          >
            {link.label}
          </a>
        </li>
      {/each}
      <li>
        <Button
          href="/support"
          aria-current={page.url.pathname === '/support' ? 'page' : undefined}
          onclick={() => (menuOpen = false)}
          class="bg-hister-rose font-space border-brutal-border h-auto w-fit rounded-none border-[3px] px-5 py-2.5 text-[13px] font-semibold tracking-[1px] text-white uppercase no-underline shadow-[3px_3px_0_rgba(0,0,0,0.25)]"
        >
          <Heart size={14} class="shrink-0 fill-white text-white" />
          Support
        </Button>
      </li>
      <li>
        <Button
          href="https://github.com/asciimoo/hister"
          target="_blank"
          rel="noopener noreferrer"
          class="bg-hister-indigo font-space border-brutal-border h-auto w-fit rounded-none border-[3px] px-5 py-2.5 text-[13px] font-semibold tracking-[1px] text-white uppercase no-underline shadow-[3px_3px_0_rgba(0,0,0,0.25)]"
        >
          GitHub
        </Button>
      </li>
    </ul>
  {/if}
</header>
