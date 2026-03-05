<script lang="ts">
  import { page } from '$app/state';
  import Github from '@lucide/svelte/icons/github';
  import Menu from '@lucide/svelte/icons/menu';
  import X from '@lucide/svelte/icons/x';
  import { Button } from '@hister/components';

  let menuOpen = $state(false);

  const links = [
    { href: '/', label: 'HOME' },
    { href: '/docs', label: 'DOCS' },
    { href: '/posts', label: 'POSTS' },
  ];

  function isActive(href: string): boolean {
    if (href === '/') return page.url.pathname === '/';
    return page.url.pathname.startsWith(href);
  }
</script>

<header class="sticky top-0 z-50 w-full bg-brutal-bg border-b-[3px] border-brutal-border">
  <nav class="flex items-center justify-between px-6 md:px-12 py-4">
    <a
      href="/"
      class="font-space text-[28px] font-extrabold tracking-[2px] text-[var(--text-primary)] no-underline"
    >
      HISTER
    </a>

    <ul class="hidden md:flex items-center gap-8 list-none m-0 p-0">
      {#each links as link}
        <li>
          <a
            href={link.href}
            class="font-space text-[13px] font-semibold tracking-[1.5px] no-underline transition-colors {isActive(
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

    <div class="hidden md:flex items-center gap-4">
      <Button
        href="https://github.com/asciimoo/hister"
        target="_blank"
        rel="noopener noreferrer"
        class="bg-hister-indigo text-white font-space text-[13px] font-semibold tracking-[1px] px-5 py-2.5 h-auto border-[3px] border-brutal-border shadow-[3px_3px_0_rgba(0,0,0,0.25)] no-underline hover:shadow-[1px_1px_0_rgba(0,0,0,0.25)] hover:translate-x-[2px] hover:translate-y-[2px] transition-all rounded-none"
      >
        <Github size={18} />
        GITHUB
      </Button>
    </div>

    <button class="md:hidden p-2" onclick={() => (menuOpen = !menuOpen)} aria-label="Toggle menu">
      {#if menuOpen}
        <X size={24} />
      {:else}
        <Menu size={24} />
      {/if}
    </button>
  </nav>

  {#if menuOpen}
    <ul
      class="md:hidden border-t-[2px] border-brutal-border bg-brutal-bg px-6 py-4 flex flex-col gap-4 list-none m-0"
    >
      {#each links as link}
        <li>
          <a
            href={link.href}
            class="font-space text-[15px] font-semibold tracking-[1.5px] no-underline {isActive(
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
          href="https://github.com/asciimoo/hister"
          target="_blank"
          rel="noopener noreferrer"
          class="bg-hister-indigo text-white font-space text-[13px] font-semibold tracking-[1px] px-5 py-2.5 h-auto border-[3px] border-brutal-border shadow-[3px_3px_0_rgba(0,0,0,0.25)] no-underline w-fit rounded-none"
        >
          <Github size={18} />
          GITHUB
        </Button>
      </li>
    </ul>
  {/if}
</header>
