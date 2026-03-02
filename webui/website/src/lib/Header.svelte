<script lang="ts">
  import { page } from '$app/state';
  import Github from '@lucide/svelte/icons/github';
  import Menu from '@lucide/svelte/icons/menu';
  import { Button } from '@hister/components';
  import * as Sheet from '@hister/components/ui/sheet';

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
    <a href="/" class="font-space text-[28px] font-extrabold tracking-[2px] text-[var(--text-primary)] no-underline">
      HISTER
    </a>

    <ul class="hidden md:flex items-center gap-8 list-none m-0 p-0">
      {#each links as link}
        <li>
          <a
            href={link.href}
            class="font-space text-[13px] font-semibold tracking-[1.5px] no-underline transition-colors {isActive(link.href)
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

    <Sheet.Root bind:open={menuOpen}>
      <Sheet.Trigger
        class="md:hidden inline-flex items-center justify-center size-9 rounded-none border-none bg-transparent cursor-pointer hover:bg-accent hover:text-accent-foreground"
        aria-label="Toggle menu"
      >
        <Menu size={24} />
      </Sheet.Trigger>
      <Sheet.Content side="right" class="border-l-[3px] border-brutal-border bg-brutal-bg rounded-none w-[280px] p-0">
        <Sheet.Header class="px-6 py-5 border-b-[3px] border-brutal-border">
          <Sheet.Title class="font-space text-xl font-extrabold tracking-[2px] text-[var(--text-primary)]">HISTER</Sheet.Title>
        </Sheet.Header>
        <nav class="flex flex-col gap-1 px-4 py-4">
          {#each links as link}
            <a
              href={link.href}
              class="font-space text-[15px] font-semibold tracking-[1.5px] no-underline px-3 py-3 border-l-[3px] transition-colors {isActive(link.href)
                ? 'text-[var(--text-primary)] border-hister-indigo bg-hister-indigo/5'
                : 'text-[var(--text-secondary)] border-transparent hover:border-brutal-border'}"
              onclick={() => menuOpen = false}
            >
              {link.label}
            </a>
          {/each}
          <div class="mt-4 px-3">
            <Button
              href="https://github.com/asciimoo/hister"
              target="_blank"
              rel="noopener noreferrer"
              class="bg-hister-indigo text-white font-space text-[13px] font-semibold tracking-[1px] px-5 py-2.5 h-auto border-[3px] border-brutal-border shadow-[3px_3px_0_rgba(0,0,0,0.25)] no-underline w-fit rounded-none"
            >
              <Github size={18} />
              GITHUB
            </Button>
          </div>
        </nav>
      </Sheet.Content>
    </Sheet.Root>
  </nav>
</header>
