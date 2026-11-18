<script lang="ts">
  import { Separator } from '@nogon/components/ui/separator';
  import Heart from '@lucide/svelte/icons/heart';

  const linkCols = [
    {
      title: 'Product',
      links: [
        { label: 'Features', href: '/#features' },
        { label: 'Documentation', href: '/docs' },
        { label: 'Demo', href: 'https://demo.nogon.eu/' },
      ],
    },
    {
      title: 'Resources',
      links: [
        { label: 'GitHub', href: 'https://github.com/asciimoo/nogon' },
        { label: 'Codeberg', href: 'https://codeberg.org/asciimoo/nogon' },
        { label: 'Posts', href: '/posts' },
        { label: 'Datasets', href: '/datasets' },
      ],
    },
    {
      title: 'Extensions',
      links: [
        {
          label: 'Chrome',
          href: 'https://chromewebstore.google.com/detail/nogon/cciilamhchpmbdnniabclekddabkifhb',
        },
        { label: 'Firefox', href: 'https://addons.mozilla.org/en-US/firefox/addon/nogon/' },
      ],
    },
    {
      title: 'Community',
      links: [
        { label: 'Mastodon', href: 'https://chaos.social/@hister' },
        { label: 'Discord', href: 'https://discord.gg/beEyuHxRSs' },
        { label: 'IRCNet: #hister', href: '' },
      ],
    },
  ];
</script>

<footer class="flex w-full flex-col gap-10 bg-[var(--text-primary)] px-6 py-12 md:px-12">
  <div class="flex flex-col justify-between gap-10 md:flex-row">
    <hgroup class="flex flex-col gap-4">
      <span class="font-space text-[22px] font-extrabold tracking-[2px] text-white uppercase"
        >Nogon</span
      >
      <span class="font-inter text-sm text-white/70">Your Own Search Engine</span>
    </hgroup>

    <nav aria-label="Footer" class="flex flex-wrap gap-16">
      {#each linkCols as col}
        <ul class="m-0 flex list-none flex-col gap-3 p-0">
          <li class="font-space text-[11px] font-bold tracking-[2px] text-[#e0e0e0] uppercase">
            {col.title}
          </li>
          {#each col.links as link}
            <li>
              {#if link.href}
                <a
                  href={link.href}
                  class="font-inter text-sm text-white/65 no-underline transition-colors hover:text-white"
                  target={link.href.startsWith('http') ? '_blank' : undefined}
                  rel={link.href.startsWith('http') ? 'noopener noreferrer' : undefined}
                >
                  {link.label}
                </a>
              {:else}
                <span
                  class="font-inter text-sm text-white/65 no-underline transition-colors hover:text-white"
                  >{link.label}</span
                >
              {/if}
            </li>
          {/each}
        </ul>
      {/each}
    </nav>
  </div>

  <Separator class="bg-white/15" />

  <div class="flex flex-col items-start justify-between gap-2 md:flex-row md:items-center">
    <span class="font-inter text-[13px] text-white/65"
      >2026 Nogon. Open-source under <a href="https://www.gnu.org/licenses/#AGPL">AGPLv3 License</a
      >.</span
    >
    <span class="font-space flex text-[13px] font-semibold tracking-[1px] text-white/65"
      >Built with <Heart size={16} class="ml-1 fill-red-300 text-red-300" /></span
    >
  </div>
</footer>
