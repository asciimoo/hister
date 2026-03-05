<script lang="ts">
  import { Separator } from '@hister/components/ui/separator';

  const linkCols = [
    {
      title: 'Product',
      links: [
        { label: 'Features', href: '/#features' },
        { label: 'Documentation', href: '/docs' },
        { label: 'Demo', href: 'https://demo.hister.org/' },
      ],
    },
    {
      title: 'Resources',
      links: [
        { label: 'GitHub', href: 'https://github.com/asciimoo/hister' },
        { label: 'Posts', href: '/posts' },
      ],
    },
    {
      title: 'Extensions',
      links: [
        {
          label: 'Chrome',
          href: 'https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb',
        },
        { label: 'Firefox', href: 'https://addons.mozilla.org/en-US/firefox/addon/hister/' },
      ],
    },
    {
      title: 'Community',
      links: [
        { label: 'Discord', href: 'https://discord.gg/vAjtDtFp' },
        { label: 'IRCNet: #hister', href: '' },
      ],
    },
  ];
</script>

<footer class="w-full bg-[var(--text-primary)] px-6 md:px-12 py-12 flex flex-col gap-10">
  <div class="flex flex-col md:flex-row justify-between gap-10">
    <hgroup class="flex flex-col gap-4">
      <span class="font-space text-[22px] font-extrabold tracking-[2px] text-white uppercase"
        >Hister</span
      >
      <span class="text-white/40 font-inter text-sm">Your Own Web Search Engine</span>
    </hgroup>

    <nav class="flex flex-wrap gap-16">
      {#each linkCols as col}
        <ul class="flex flex-col gap-3 list-none m-0 p-0">
          <li class="font-space text-[11px] font-bold tracking-[2px] text-[#e0e0e0] uppercase">
            {col.title}
          </li>
          {#each col.links as link}
            <li>
              {#if link.href}
                <a
                  href={link.href}
                  class="text-white/65 font-inter text-sm no-underline hover:text-white transition-colors"
                  target={link.href.startsWith('http') ? '_blank' : undefined}
                  rel={link.href.startsWith('http') ? 'noopener noreferrer' : undefined}
                >
                  {link.label}
                </a>
              {:else}
                <span
                  class="text-white/65 font-inter text-sm no-underline hover:text-white transition-colors"
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

  <div class="flex flex-col md:flex-row justify-between items-start md:items-center gap-2">
    <span class="text-white/25 font-inter text-[13px]"
      >2026 Hister. Open source under AGPL-3 License.</span
    >
    <span class="text-white/25 font-space text-[13px] font-semibold tracking-[1px]"
      >Built with Go</span
    >
  </div>
</footer>
