<script lang="ts">
  import Shield from '@lucide/svelte/icons/shield';
  import FileText from '@lucide/svelte/icons/file-text';
  import Zap from '@lucide/svelte/icons/zap';
  import Search from '@lucide/svelte/icons/search';
  import Settings from '@lucide/svelte/icons/settings';
  import Puzzle from '@lucide/svelte/icons/puzzle';
  import WifiOff from '@lucide/svelte/icons/wifi-off';
  import Download from '@lucide/svelte/icons/download';
  import Terminal from '@lucide/svelte/icons/terminal';
  import * as Card from '@hister/components/ui/card';

  const rows = [
    [
      { icon: Shield, title: 'PRIVACY-FOCUSED', desc: 'Keep your browsing history indexed locally. No remote servers, no tracking, no data leaving your machine.', bg: 'bg-brutal-card', textColor: 'text-[var(--text-primary)]', descColor: 'text-[var(--text-secondary)]', iconBg: 'bg-hister-indigo' },
      { icon: FileText, title: 'FULL-TEXT INDEXING', desc: "Search through the actual content of web pages you've visited, not just URLs and titles.", bg: 'bg-brutal-card', textColor: 'text-[var(--text-primary)]', descColor: 'text-[var(--text-secondary)]', iconBg: 'bg-hister-coral' },
    ],
    [
      { icon: Zap, title: 'BLAZING FAST', desc: 'Instant results as you type. Full keyboard-driven workflow for maximum efficiency.', bg: 'bg-hister-indigo', textColor: 'text-white', descColor: 'text-white/80', iconBg: 'bg-white/20' },
      { icon: Search, title: 'ADVANCED SEARCH', desc: 'Powerful query language for precise results.', bg: 'bg-[var(--text-primary)]', textColor: 'text-white', descColor: 'text-white/80', iconBg: 'bg-white/20' },
      { icon: Settings, title: 'FLEXIBLE MANAGEMENT', desc: 'Configure blacklist and priority rules to control your indexed content.', bg: 'bg-hister-teal', textColor: 'text-white', descColor: 'text-white/80', iconBg: 'bg-white/20' },
    ],
    [
      { icon: Puzzle, title: 'BROWSER EXTENSIONS', desc: 'Available for Chrome and Firefox.', bg: 'bg-hister-lime', textColor: 'text-white', descColor: 'text-white/80', iconBg: 'bg-white/20' },
      { icon: WifiOff, title: 'OFFLINE PREVIEW', desc: 'Readable offline previews of any result. Access your saved pages without internet.', bg: 'bg-hister-coral', textColor: 'text-white', descColor: 'text-white/80', iconBg: 'bg-white/20' },
      { icon: Download, title: 'BROWSER IMPORT', desc: 'Import existing Firefox or Chrome history with a single command.', bg: 'bg-hister-rose', textColor: 'text-white', descColor: 'text-white/80', iconBg: 'bg-white/20' },
    ],
  ];

  const aliasCard = {
    icon: Terminal, title: 'SEARCH ALIASES', desc: 'Create custom keyword shortcuts to simplify complex or frequently used queries.', bg: 'bg-hister-cyan', textColor: 'text-white', descColor: 'text-white/80', iconBg: 'bg-[#f5f5f5]', iconColor: 'text-hister-teal',
  };
</script>

<section id="features" class="max-w-[2000px] mx-auto bg-brutal-bg px-6 md:px-12 py-12 md:py-16 flex flex-col gap-10">
  <div class="flex items-center gap-6">
    <div class="w-1.5 h-10 bg-hister-coral"></div>
    <h2 class="font-space text-3xl md:text-5xl font-black tracking-[3px] text-[var(--text-primary)]">FEATURES</h2>
  </div>

  <div class="flex flex-col gap-6">
    {#each rows as row}
      <div class="grid grid-cols-1 gap-4 {row.length === 2 ? 'md:grid-cols-2' : 'md:grid-cols-3'}">
        {#each row as card}
          <Card.Root class="{card.bg} p-0 border-[3px] border-brutal-border shadow-[6px_6px_0_var(--brutal-shadow)] rounded-none gap-0">
            <Card.Content class="p-7 flex flex-col gap-4">
              <div class="{card.iconBg} w-12 h-12 flex items-center justify-center">
                <card.icon size={24} class="{card.iconColor ?? 'text-white'}" />
              </div>
              <Card.Title class="{card.textColor} font-space text-xl md:text-3xl font-extrabold tracking-[1px]">{card.title}</Card.Title>
              <Card.Description class="{card.descColor} font-inter text-sm md:text-lg leading-relaxed">{card.desc}</Card.Description>
            </Card.Content>
          </Card.Root>
        {/each}
      </div>
    {/each}

    <!-- Row 4: Search Aliases (horizontal) -->
    <Card.Root class="{aliasCard.bg} p-0 border-[3px] border-brutal-border shadow-[6px_6px_0_var(--brutal-shadow)] rounded-none gap-0">
      <Card.Content class="p-7 flex items-center gap-5">
        <div class="{aliasCard.iconBg} w-12 h-12 flex items-center justify-center shrink-0">
          <aliasCard.icon size={24} class="{aliasCard.iconColor}" />
        </div>
        <div class="flex flex-col gap-2">
          <Card.Title class="{aliasCard.textColor} font-space text-xl md:text-3xl font-extrabold tracking-[1px]">{aliasCard.title}</Card.Title>
          <Card.Description class="{aliasCard.descColor} font-inter text-sm md:text-lg leading-relaxed">{aliasCard.desc}</Card.Description>
        </div>
      </Card.Content>
    </Card.Root>
  </div>
</section>
