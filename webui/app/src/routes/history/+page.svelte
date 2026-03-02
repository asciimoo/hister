<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchConfig, apiFetch } from '$lib/api';
  import type { HistoryItem } from '$lib/types';
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import { Badge } from '@hister/components/ui/badge';
  import { Separator } from '@hister/components/ui/separator';
  import { ScrollArea } from '@hister/components/ui/scroll-area';
  import * as AlertDialog from '@hister/components/ui/alert-dialog';
  import StatusMessage from '$lib/components/StatusMessage.svelte';
  import { Search, Clock, Trash2 } from 'lucide-svelte';

  let items: HistoryItem[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let filter = $state('');
  let activeGroup = $state('');
  let filterByDate = $state('');
  let showDeleteAllDialog = $state(false);

  const groupColors = [
    'hister-indigo', 'hister-coral', 'hister-teal', 'hister-amber',
    'hister-rose', 'hister-cyan', 'hister-lime'
  ];

  function getColorVar(color: string): string {
    return `var(--${color})`;
  }

  function formatDateLabel(dateStr: string): string {
    const date = new Date(dateStr);
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const yesterday = new Date(today);
    yesterday.setDate(yesterday.getDate() - 1);
    const itemDate = new Date(date.getFullYear(), date.getMonth(), date.getDate());

    if (itemDate.getTime() === today.getTime()) return 'Today';
    if (itemDate.getTime() === yesterday.getTime()) return 'Yesterday';
    return itemDate.toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' });
  }

  function getDateKey(dateStr: string): string {
    const date = new Date(dateStr);
    return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
  }

  const filteredItems = $derived.by(() => {
    let result = items;
    if (filter) {
      const f = filter.toLowerCase();
      result = result.filter(item =>
        item.query.toLowerCase().includes(f) ||
        item.title.toLowerCase().includes(f) ||
        item.url.toLowerCase().includes(f)
      );
    }
    if (filterByDate) {
      result = result.filter(item => item.updated_at && getDateKey(item.updated_at) === filterByDate);
    }
    return result;
  });

  const allGroups = $derived.by(() => {
    const g: { key: string; label: string; items: HistoryItem[] }[] = [];
    const seen = new Map<string, number>();
    let baseItems = items;
    if (filter) {
      const f = filter.toLowerCase();
      baseItems = baseItems.filter(item =>
        item.query.toLowerCase().includes(f) ||
        item.title.toLowerCase().includes(f) ||
        item.url.toLowerCase().includes(f)
      );
    }
    for (const item of baseItems) {
      const key = item.updated_at ? getDateKey(item.updated_at) : 'unknown';
      const label = item.updated_at ? formatDateLabel(item.updated_at) : 'Unknown';
      if (seen.has(key)) {
        g[seen.get(key)!].items.push(item);
      } else {
        seen.set(key, g.length);
        g.push({ key, label, items: [item] });
      }
    }
    return g;
  });

  const groups = $derived.by(() => {
    const g: { key: string; label: string; items: HistoryItem[] }[] = [];
    const seen = new Map<string, number>();
    for (const item of filteredItems) {
      const key = item.updated_at ? getDateKey(item.updated_at) : 'unknown';
      const label = item.updated_at ? formatDateLabel(item.updated_at) : 'Unknown';
      if (seen.has(key)) {
        g[seen.get(key)!].items.push(item);
      } else {
        seen.set(key, g.length);
        g.push({ key, label, items: [item] });
      }
    }
    return g;
  });

  function getGroupColor(idx: number): string {
    return groupColors[idx % groupColors.length];
  }

  function scrollToGroup(key: string) {
    activeGroup = key;
    filterByDate = key;
  }

  function showAll() {
    filterByDate = '';
    activeGroup = groups.length > 0 ? groups[0].key : '';
  }

  async function deleteHistoryItem(item: HistoryItem) {
    try {
      await apiFetch('/history', {
        method: 'POST',
        headers: { 'Content-type': 'application/json; charset=UTF-8' },
        body: JSON.stringify({ url: item.url, title: item.title, query: item.query, delete: true })
      });
      items = items.filter(i => i.url !== item.url || i.query !== item.query);
    } catch (e) {
      error = String(e);
    }
  }

  async function deleteAllHistory() {
    try {
      for (const item of items) {
        await apiFetch('/history', {
          method: 'POST',
          headers: { 'Content-type': 'application/json; charset=UTF-8' },
          body: JSON.stringify({ url: item.url, title: item.title, query: item.query, delete: true })
        });
      }
      items = [];
    } catch (e) {
      error = String(e);
    }
  }

  onMount(async () => {
    try {
      await fetchConfig();
      const res = await apiFetch('/history', {
        headers: { 'Accept': 'application/json' }
      });
      if (!res.ok) throw new Error('Failed to load history');
      items = await res.json();
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<svelte:head>
  <title>Hister - History</title>
</svelte:head>

<header class="flex items-center justify-between px-3 md:px-6 py-3 bg-card-surface border-b-[3px] border-brutal-border shrink-0 gap-2 overflow-hidden">
  <h1 class="flex items-center gap-2 shrink-0 min-w-0"><span class="w-1 h-6 bg-hister-indigo shrink-0"></span><span class="font-space text-sm md:text-lg tracking-[1px] font-extrabold text-text-brand truncate uppercase">Search History</span></h1>
  <nav class="flex items-center gap-2 md:gap-3 min-w-0 shrink-0">
    <div class="flex items-center gap-2 h-8 px-2 md:px-3 border-[3px] border-brutal-border bg-page-bg min-w-0">
      <Search class="size-3.5 text-text-brand-muted shrink-0" />
      <Input
        bind:value={filter}
        placeholder="Filter..."
        class="w-20 md:w-40 h-full bg-transparent font-inter text-xs font-medium text-text-brand placeholder:text-text-brand-muted border-0 shadow-none focus-visible:ring-0 p-0"
      />
    </div>
    {#if items.length > 0}
      <AlertDialog.Root bind:open={showDeleteAllDialog}>
        <AlertDialog.Trigger asChild>
          {#snippet children({ props })}
            <Button
              {...props}
              variant="outline"
              size="sm"
              class="border-[3px] border-hister-rose text-hister-rose hover:bg-hister-rose/10 font-inter text-xs font-semibold h-8 gap-1.5 shrink-0 shadow-[3px_3px_0_var(--brutal-shadow)] hover:shadow-[1px_1px_0_var(--brutal-shadow)] hover:translate-x-[2px] hover:translate-y-[2px] transition-all"
            >
              <Trash2 class="size-3.5" />
              <span class="hidden md:inline">Delete All</span>
            </Button>
          {/snippet}
        </AlertDialog.Trigger>
        <AlertDialog.Content class="border-[3px] border-brutal-border bg-card-surface rounded-none shadow-[6px_6px_0_var(--brutal-shadow)]">
          <AlertDialog.Header>
            <AlertDialog.Title class="font-space text-lg font-extrabold tracking-[1px] text-text-brand">DELETE ALL HISTORY</AlertDialog.Title>
            <AlertDialog.Description class="font-inter text-sm text-text-brand-secondary">
              This will permanently delete all search history entries. This action cannot be undone.
            </AlertDialog.Description>
          </AlertDialog.Header>
          <AlertDialog.Footer>
            <AlertDialog.Cancel class="border-[3px] border-brutal-border font-space text-xs font-bold tracking-[0.5px] rounded-none">CANCEL</AlertDialog.Cancel>
            <AlertDialog.Action
              class="bg-hister-rose text-white border-[3px] border-brutal-border font-space text-xs font-bold tracking-[0.5px] rounded-none shadow-[3px_3px_0_var(--brutal-shadow)] hover:shadow-[1px_1px_0_var(--brutal-shadow)] hover:translate-x-[2px] hover:translate-y-[2px] transition-all"
              onclick={deleteAllHistory}
            >
              DELETE ALL
            </AlertDialog.Action>
          </AlertDialog.Footer>
        </AlertDialog.Content>
      </AlertDialog.Root>
    {/if}
  </nav>
</header>

{#if loading}
  <StatusMessage message="Loading history..." type="loading" />
{:else if error}
  <StatusMessage message={error} type="error" className="mx-3 md:mx-6 mt-4" />
{:else if filteredItems.length === 0}
  <StatusMessage message={filter ? 'No matching entries' : 'No history yet'} type="empty" />
{:else}
  <div class="flex flex-col md:flex-row flex-1 min-h-0 overflow-hidden">
    <!-- Timeline sidebar: hidden on mobile, shown on md+ -->
    <ScrollArea class="hidden md:block w-[280px] shrink-0 border-r-[3px] border-brutal-border pt-5 pr-3">
      <div class="space-y-1">
        <span class="font-space text-xs font-bold tracking-[2px] text-text-brand-muted px-2.5 flex items-center gap-1.5 uppercase">
          <Clock class="size-3" />
          Timeline
        </span>
        <Separator class="bg-border-brand-muted" />

        <Button
          variant="ghost"
          class="flex items-center gap-2 w-full py-1.5 px-2.5 justify-start h-auto rounded-none {!filterByDate ? 'bg-[var(--hister-indigo)] text-white hover:bg-[var(--hister-indigo)]/90 hover:text-white' : 'hover:bg-muted-surface'}"
          onclick={showAll}
        >
          <span class="font-inter text-sm font-semibold" class:text-text-brand-secondary={!!filterByDate}>
            Show All
          </span>
          <Badge
            variant="secondary"
            class="ml-auto shrink-0 text-xs px-1.5 py-0 h-4 border-0 {filterByDate ? 'bg-muted-surface text-text-brand-muted' : 'bg-white/20 text-white'}"
          >
            {filteredItems.length}
          </Badge>
        </Button>

        <Separator class="bg-border-brand-muted" />

        {#each allGroups as group, i}
          {@const color = getGroupColor(i)}
          {@const isActive = filterByDate === group.key}
          <Button
            variant="ghost"
            class="flex items-center gap-2 w-full py-1.5 px-2.5 justify-start h-auto rounded-none {isActive ? 'text-white hover:text-white' : 'hover:bg-muted-surface'}"
            style={isActive ? `background-color: ${getColorVar(color)};` : ''}
            onclick={() => scrollToGroup(group.key)}
          >
            <span
              class="w-2 h-2 shrink-0 rounded-full"
              style={isActive ? 'background-color: white;' : `background-color: ${getColorVar(color)};`}
            ></span>
            <span
              class="font-inter text-sm truncate"
              class:font-semibold={isActive}
              class:font-medium={!isActive}
              class:text-text-brand-secondary={!isActive}
            >
              {group.label}
            </span>
            <Badge
              variant="secondary"
              class="ml-auto shrink-0 text-xs px-1.5 py-0 h-4 border-0 {isActive ? 'bg-white/20 text-white' : 'bg-muted-surface text-text-brand-muted'}"
            >
              {group.items.length}
            </Badge>
          </Button>
        {/each}
      </div>
    </ScrollArea>

    <!-- Mobile timeline: horizontal scrollable filter chips -->
    <div class="flex md:hidden items-center gap-2 px-4 py-2 overflow-x-auto border-b-[3px] border-brutal-border bg-card-surface shrink-0">
      <Button
        variant="ghost"
        size="sm"
        class="shrink-0 text-xs font-inter font-semibold h-7 px-2.5 rounded-none {!filterByDate ? 'bg-hister-indigo text-white hover:bg-hister-indigo/90 hover:text-white' : 'text-text-brand-secondary hover:bg-muted-surface'}"
        onclick={showAll}
      >
        All ({filteredItems.length})
      </Button>
      {#each allGroups as group, i}
        {@const color = getGroupColor(i)}
        {@const isActive = filterByDate === group.key}
        <Button
          variant="ghost"
          size="sm"
          class="shrink-0 text-xs font-inter font-medium h-7 px-2.5 rounded-none {isActive ? 'text-white hover:text-white' : 'text-text-brand-secondary hover:bg-muted-surface'}"
          style={isActive ? `background-color: ${getColorVar(color)};` : ''}
          onclick={() => scrollToGroup(group.key)}
        >
          {group.label} ({group.items.length})
        </Button>
      {/each}
    </div>

    <ScrollArea orientation="vertical" class="flex-1 min-w-0 min-h-0 max-w-full overflow-x-hidden">
      <div class="w-full overflow-hidden px-3 md:px-6 py-3 md:py-5 space-y-4 md:space-y-6">
      {#each groups as group, gi}
        {@const color = getGroupColor(gi)}
        <div id="group-{encodeURIComponent(group.key)}" class="space-y-2">
          <span class="font-outfit text-sm font-bold" style="color: {getColorVar(color)};">{group.label}</span>
          <Separator class="h-0.5" style="background-color: {getColorVar(color)};" />

          <div class="space-y-0">
            {#each group.items as item, ii}
              {@const itemColor = getGroupColor(gi + ii)}
              <article
                class="flex items-start md:items-center gap-2 md:gap-3 py-2 md:py-2.5 px-2.5 md:px-3.5 bg-card-surface border-b-[3px] border-b-brutal-border overflow-hidden"
                style="border-left: 3px solid {getColorVar(itemColor)};"
              >
                <div class="flex-1 min-w-0 w-0 space-y-0.5">
                  <a
                    href={item.url}
                    class="font-outfit text-sm md:text-base font-bold hover:underline block truncate no-underline"
                    style="color: {getColorVar(itemColor)};"
                    target="_blank"
                    rel="noopener"
                  >
                    {(item.title || item.url).replace(/<[^>]*>/g, '')}
                  </a>
                  <span class="font-fira text-xs md:text-sm text-text-brand-muted block truncate" title={item.url}>{item.url}</span>
                </div>
                <nav class="flex items-center gap-1 shrink-0">
                  <Button
                    variant="ghost"
                    size="sm"
                    class="text-xs font-inter text-text-brand-muted shrink-0 hover:text-hister-indigo gap-1 h-7 px-1.5 md:px-2 no-underline"
                    href="/?q={encodeURIComponent(item.query)}"
                  >
                    <Search class="size-3" />
                    <span class="hidden md:inline">Search</span>
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    class="text-text-brand-muted hover:text-hister-rose shrink-0 size-7"
                    onclick={() => deleteHistoryItem(item)}
                  >
                    <Trash2 class="size-3.5" />
                  </Button>
                </nav>
              </article>
            {/each}
          </div>
        </div>
      {/each}
      </div>
    </ScrollArea>
  </div>
{/if}
