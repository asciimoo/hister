<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { page } from '$app/stores';
  import {
    WebSocketManager,
    KeyHandler,
    getSearchUrl,
    exportJSON,
    exportCSV,
    exportRSS,
    formatTimestamp,
    formatRelativeTime,
    scrollTo,
    escapeHTML,
    buildSearchQuery,
    parseSearchResults,
    updateSearchURL,
    openURL
  } from '$lib/search';
  import { fetchConfig, apiFetch } from '$lib/api';
  import type { SearchResults } from '$lib/search';
  import { animate, stagger } from 'animejs';
  import { Input } from '@hister/components/ui/input';
  import { Button } from '@hister/components/ui/button';
  import { Badge } from '@hister/components/ui/badge';
  import { Separator } from '@hister/components/ui/separator';
  import {
    Search, Star, Globe, MoreVertical, Eye, Trash2,
    Pin, PinOff, Download, ExternalLink, History, Shield, Link2,
    Keyboard, HelpCircle, X
  } from 'lucide-svelte';

  interface Config {
    wsUrl: string;
    searchUrl: string;
    openResultsOnNewTab: boolean;
    hotkeys: Record<string, string>;
  }

  interface HistoryItem {
    query: string;
    url: string;
    title: string;
    favicon?: string;
  }

  let config: Config = $state({
    wsUrl: '',
    searchUrl: '',
    openResultsOnNewTab: false,
    hotkeys: {},
  });

  let wsManager: WebSocketManager | undefined;
  let keyHandler: KeyHandler | undefined;
  let inputEl: HTMLInputElement | undefined = $state();

  let query = $state('');
  let autocomplete = $state('');
  let connected = $state(false);
  let lastResults: SearchResults | null = $state(null);
  let highlightIdx = $state(0);
  let currentSort = $state('');
  let dateFrom = $state('');
  let dateTo = $state('');
  let showPopup = $state(false);
  let popupTitle = $state('');
  let popupContent = $state('');
  let actionsQuery = $state('');
  let actionsMessage: string | null = $state(null);
  let actionsError = $state(false);
  let showActionsForResult: string | null = $state(null);

  let showHelp = $state(false);

  let contextMenuSearch: string | null = $state(null);
  let contextMenuPos = $state({ x: 0, y: 0 });

  let recentSearches: string[] = $state([]);
  let rulesCount = $state(0);
  let aliasesCount = $state(0);
  let historyCount = $state(0);

  let displayHistoryCount = $state(0);
  let displayRulesCount = $state(0);
  let displayAliasesCount = $state(0);

  let heroTitleEl: HTMLElement | undefined = $state();
  let searchBoxEl: HTMLElement | undefined = $state();
  let hintEl: HTMLElement | undefined = $state();
  let chipsContainerEl: HTMLElement | undefined = $state();
  let statsRowEl: HTMLElement | undefined = $state();
  let kbdEl: HTMLElement | undefined = $state();
  let underlineEl: HTMLElement | undefined = $state();

  let animationHandles: any[] = [];

  const resultColors = [
    'hister-indigo', 'hister-teal', 'hister-coral', 'hister-amber',
    'hister-rose', 'hister-cyan', 'hister-lime'
  ];

  const chipColors = [
    { border: 'border-hister-indigo', bg: 'bg-hister-indigo/10', text: 'text-hister-indigo' },
    { border: 'border-hister-teal', bg: 'bg-hister-teal/10', text: 'text-hister-teal' },
    { border: 'border-hister-coral', bg: 'bg-hister-coral/10', text: 'text-hister-coral' },
    { border: 'border-hister-amber', bg: 'bg-hister-amber/10', text: 'text-hister-amber' },
  ];

  const hotkeyActions: Record<string, (e?: KeyboardEvent) => void> = {
    'open_result': openSelectedResult,
    'open_result_in_new_tab': (e) => openSelectedResult(e, true),
    'select_next_result': selectNextResult,
    'select_previous_result': selectPreviousResult,
    'open_query_in_search_engine': openQueryInSearchEngine,
    'focus_search_input': focusSearchInput,
    'view_result_popup': viewResultPopup,
    'autocomplete': autocompleteQuery,
    'show_hotkeys': showHotkeys
  };

  const isSearching = $derived(query.length > 0);

  const historyLen = $derived((lastResults?.history as any)?.length || 0);
  const docsLen = $derived((lastResults?.documents as any)?.length || 0);
  const totalResults = $derived(historyLen + docsLen);
  const hasResults = $derived(totalResults > 0);

  function connect() {
    wsManager = new WebSocketManager(config.wsUrl, {
      onOpen: () => {
        connected = true;
        if (query) sendQuery(query);
      },
      onMessage: renderResults,
      onClose: () => { connected = false; },
      onError: () => { connected = false; }
    });
    wsManager.connect();
  }

  function sendQuery(q: string) {
    const message = buildSearchQuery(q, currentSort, dateFrom, dateTo);
    wsManager?.send(JSON.stringify(message));
  }

  function updateURL() {
    updateSearchURL(window.location.pathname, query, dateFrom, dateTo);
  }

  function renderResults(event: MessageEvent) {
    const res = parseSearchResults(event.data);
    lastResults = res;
    autocomplete = (query && res.query_suggestion) || '';
    highlightIdx = 0;
  }

  function openResult(url: string, title: string, newWindow = false) {
    if (config.openResultsOnNewTab) newWindow = true;
    saveHistoryItem(url, title, query, false, () => openURL(url, newWindow));
  }

  async function saveHistoryItem(url: string, title: string, queryStr: string, remove: boolean, callback?: () => void) {
    try {
      const res = await apiFetch('/history', {
        method: 'POST',
        headers: { 'Content-type': 'application/json; charset=UTF-8' },
        body: JSON.stringify({ url, title, query: queryStr, delete: remove })
      });
      callback?.();
    } catch {}
  }

  function setSort(sortId: string) {
    if (currentSort === sortId) return;
    currentSort = sortId;
    if (query) sendQuery(query);
  }

  async function deleteResult(url: string) {
    const data = new URLSearchParams({ url });
    await apiFetch('/delete', { method: 'POST', body: data });
    if (lastResults?.documents) {
      lastResults = {
        ...lastResults,
        documents: lastResults.documents.filter((d) => d.url !== url)
      };
    }
  }

  function updatePriorityResult(url: string, title: string, remove: boolean) {
    const q = actionsQuery || query;
    if (!q) return;
    saveHistoryItem(url, title, q, remove, () => {
      actionsMessage = `Priority result ${remove ? 'deleted' : 'added'}.`;
      actionsError = false;
    });
  }

  async function openReadable(e: Event, url: string, title: string) {
    e.preventDefault();
    e.stopPropagation();
    try {
      const resp = await apiFetch(`/readable?url=${encodeURIComponent(url)}`);
      if (!resp.ok) {
        popupTitle = 'Error';
        popupContent = `<p class="text-hister-rose">Failed to load readable content. Status: ${resp.status}</p>`;
        showPopup = true;
        return;
      }
      const data = await resp.json();
      popupTitle = data.title || title;
      popupContent = data.content || '<p>No content available</p>';
      showPopup = true;
    } catch (err) {
      popupTitle = 'Error';
      popupContent = `<p class="text-hister-rose">Failed to parse response: ${err}</p>`;
      showPopup = true;
    }
  }

  function selectNthResult(n: number) {
    if (!totalResults) return;
    highlightIdx = (highlightIdx + n + totalResults) % totalResults;
    const results = document.querySelectorAll('[data-result]');
    scrollTo(results[highlightIdx]);
  }

  function selectNextResult(e?: KeyboardEvent) { if (e) e.preventDefault(); selectNthResult(1); }
  function selectPreviousResult(e?: KeyboardEvent) { if (e) e.preventDefault(); selectNthResult(-1); }

  function openSelectedResult(e?: KeyboardEvent, newWindow = false) {
    if (e) e.preventDefault();
    if (query.startsWith('!!')) {
      openURL(getSearchUrl(config.searchUrl, query.substring(2)), newWindow);
      return;
    }
    const res = document.querySelectorAll<HTMLAnchorElement>('[data-result] [data-result-link]')[highlightIdx];
    if (res) {
      openResult(res.getAttribute('href')!, res.innerText, newWindow);
    }
  }

  function viewResultPopup(e?: KeyboardEvent) {
    if (e) e.preventDefault();
    closePopup();
    const readables = document.querySelectorAll('[data-result] [data-readable]');
    if (highlightIdx >= 0 && highlightIdx < readables.length) {
      const el = readables[highlightIdx] as HTMLElement;
      const result = el.closest('[data-result]')!;
      const link = result.querySelector<HTMLAnchorElement>('[data-result-link]')!;
      openReadable({ preventDefault: () => {} } as Event, link.href, link.innerText);
    }
  }

  function autocompleteQuery(e?: KeyboardEvent) {
    if (e) e.preventDefault();
    if (document.activeElement === inputEl && autocomplete && query !== autocomplete) {
      query = autocomplete;
      sendQuery(query);
    }
  }

  function openQueryInSearchEngine(e?: KeyboardEvent) { if (e) e.preventDefault(); openURL(getSearchUrl(config.searchUrl, query)); }
  function focusSearchInput(e?: KeyboardEvent) { if (document.activeElement !== inputEl) { if (e) e.preventDefault(); inputEl?.focus(); } }

  function closePopup(): boolean { if (showPopup) { showPopup = false; return true; } return false; }

  const hotkeyDescriptions: Record<string, string> = {
    'open_result': 'Open result',
    'open_result_in_new_tab': 'Open result in new tab',
    'select_next_result': 'Select next result',
    'select_previous_result': 'Select previous result',
    'open_query_in_search_engine': 'Open in search engine',
    'focus_search_input': 'Focus search input',
    'view_result_popup': 'View result content',
    'autocomplete': 'Autocomplete query',
    'show_hotkeys': 'Show help'
  };

  function showHotkeys(e?: KeyboardEvent) {
    if (document.activeElement === inputEl) return;
    if (showHelp) { showHelp = false; return; }
    showHelp = true;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (keyHandler?.handle(e)) { e.preventDefault(); return; }
    if (e.key === 'Escape') {
      if (showHelp) { showHelp = false; e.preventDefault(); return; }
      if (contextMenuSearch) { contextMenuSearch = null; e.preventDefault(); return; }
      if (closePopup()) { e.preventDefault(); return; }
    }
    showActionsForResult = null;
    contextMenuSearch = null;
  }

  function getResultColor(idx: number): string {
    return resultColors[idx % resultColors.length];
  }

  function clickChip(q: string) {
    query = q;
    inputEl?.focus();
  }

  function deleteRecentSearch(q: string) {
    recentSearches = recentSearches.filter(s => s !== q);
    localStorage.setItem('deletedSearches', JSON.stringify(
      [...JSON.parse(localStorage.getItem('deletedSearches') || '[]'), q]
    ));
    contextMenuSearch = null;
  }

  function deleteAllRecentSearches() {
    localStorage.setItem('deletedSearches', JSON.stringify(
      [...JSON.parse(localStorage.getItem('deletedSearches') || '[]'), ...recentSearches]
    ));
    recentSearches = [];
  }

  function showChipContextMenu(e: MouseEvent, q: string) {
    e.preventDefault();
    contextMenuSearch = q;
    contextMenuPos = { x: e.clientX, y: e.clientY };
  }

  function getFaviconSrc(favicon: string | undefined, url: string): string | null {
    if (favicon) return favicon;
    return null;
  }

  async function loadHomeStats() {
    try {
      const [historyRes, rulesRes] = await Promise.all([
        apiFetch('/history', { headers: { 'Accept': 'application/json' } }),
        apiFetch('/rules', { headers: { 'Accept': 'application/json' } })
      ]);

      if (historyRes.ok) {
        const items: HistoryItem[] = await historyRes.json();
        historyCount = items.length;
        const seen = new Set<string>();
        for (const item of items) {
          if (item.query && !seen.has(item.query)) {
            seen.add(item.query);
            if (seen.size >= 4) break;
          }
        }
        const deletedSearches: string[] = JSON.parse(localStorage.getItem('deletedSearches') || '[]');
        recentSearches = [...seen].filter(q => !deletedSearches.includes(q));
      }

      if (rulesRes.ok) {
        const rules = await rulesRes.json();
        rulesCount = (rules.skip?.length || 0) + (rules.priority?.length || 0);
        aliasesCount = Object.keys(rules.aliases || {}).length;
      }

      statsLoaded = true;
    } catch {
      statsLoaded = true;
    }
  }

  let statsLoaded = $state(false);

  function startHeroAnimations() {
    cleanupAnimations();

    if (heroTitleEl) {
      animationHandles.push(
        animate(heroTitleEl, {
          backgroundPosition: ['0% 50%', '100% 50%'],
          ease: 'inOutSine',
          duration: 6000,
          loop: true,
          alternate: true
        })
      );
    }

    if (kbdEl) {
      animationHandles.push(
        animate(kbdEl, {
          translateY: [0, 3, 0],
          duration: 400,
          ease: 'inOutSine',
          loop: true,
          loopDelay: 10000
        })
      );
    }

    if (underlineEl) {
      animationHandles.push(
        animate(underlineEl, {
          scaleX: [0, 1],
          duration: 800,
          ease: 'outCubic',
          delay: 300
        })
      );
    }
  }

  function animateCounters() {
    const counterObj = { h: displayHistoryCount, r: displayRulesCount, a: displayAliasesCount };
    animationHandles.push(
      animate(counterObj, {
        h: historyCount,
        r: rulesCount,
        a: aliasesCount,
        duration: 800,
        ease: 'outCubic',
        onRender: () => {
          displayHistoryCount = Math.round(counterObj.h);
          displayRulesCount = Math.round(counterObj.r);
          displayAliasesCount = Math.round(counterObj.a);
        }
      })
    );
  }

  function cleanupAnimations() {
    for (const h of animationHandles) {
      try { h.revert(); } catch {}
    }
    animationHandles = [];
  }

  $effect(() => {
    if (!isSearching) {
      tick().then(() => startHeroAnimations());
    }
    return () => cleanupAnimations();
  });

  $effect(() => {
    if (statsLoaded && !isSearching) {
      tick().then(() => animateCounters());
    }
  });

  $effect(() => {
    isSearching;
    (async () => { await tick(); inputEl?.focus(); })();
  });
  $effect(() => { if (query && connected) { sendQuery(query); localStorage.setItem('lastQuery', query); } });
  $effect(() => { if (!query) { autocomplete = ''; lastResults = null; } });
  $effect(() => { if (dateFrom || dateTo) sendQuery(query); });
  $effect(() => { updateURL(); });
  $effect.pre(() => {
    const urlParams = new URLSearchParams(window.location.search);
    const q = urlParams.get('q');
    const df = urlParams.get('date_from');
    const dt = urlParams.get('date_to');
    if (q) query = q;
    if (df) dateFrom = df;
    if (dt) dateTo = dt;
  });

  onMount(() => {
    (async () => {
      const appConfig = await fetchConfig();
      config = {
        wsUrl: appConfig.wsUrl,
        searchUrl: appConfig.searchUrl,
        openResultsOnNewTab: appConfig.openResultsOnNewTab,
        hotkeys: appConfig.hotkeys,
      };
      inputEl?.focus();
      connect();
      keyHandler = new KeyHandler(config.hotkeys, hotkeyActions);
      loadHomeStats();
    })();
    return () => { wsManager?.close(); cleanupAnimations(); };
  });
</script>

<svelte:head>
  <title>Hister</title>
</svelte:head>

<svelte:window onkeydown={handleKeydown} />

{#if showPopup}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
    role="presentation"
    onclick={(e) => { if (e.target === e.currentTarget) closePopup(); }}
    onkeydown={(e) => { if (e.key === 'Escape') closePopup(); }}
  >
    <div class="relative w-full max-w-2xl max-h-[80vh] overflow-auto mx-4 border-[3px] border-border-brand bg-card-surface shadow-[6px_6px_0px_var(--hister-indigo)] p-6">
      <button
        class="absolute top-3 right-3 text-text-brand-muted hover:text-text-brand bg-transparent border-0 cursor-pointer text-lg font-bold leading-none p-1"
        onclick={closePopup}
        aria-label="Close"
      >&times;</button>
      <div class="border-b-[3px] border-border-brand-muted pb-4 mb-4">
        <h2 class="font-outfit font-bold text-lg text-text-brand pr-6">{popupTitle}</h2>
      </div>
      <div class="font-inter text-sm text-text-brand-secondary prose max-w-none">{@html popupContent}</div>
    </div>
  </div>
{/if}

{#if showHelp}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
    role="presentation"
    onclick={(e) => { if (e.target === e.currentTarget) showHelp = false; }}
  >
    <div class="relative w-full max-w-md mx-4 border-[3px] border-border-brand bg-card-surface shadow-[6px_6px_0px_var(--hister-indigo)] overflow-hidden">
      <div class="flex items-center justify-between px-5 py-4 bg-hister-indigo">
        <div class="flex items-center gap-2">
          <Keyboard class="size-5 text-white" />
          <span class="font-outfit text-lg font-extrabold text-white">Keyboard Shortcuts</span>
        </div>
        <button
          class="text-white/70 hover:text-white bg-transparent border-0 cursor-pointer p-0.5"
          onclick={() => { showHelp = false; }}
          aria-label="Close"
        >
          <X class="size-5" />
        </button>
      </div>
      <div class="p-4 space-y-0">
        {#each Object.entries(config.hotkeys) as [key, action]}
          <div class="flex items-center justify-between py-2.5 border-b-[1px] border-border-brand-muted">
            <span class="font-inter text-sm text-text-brand-secondary">{hotkeyDescriptions[action] || action}</span>
            <kbd class="bg-muted-surface border-[2px] border-border-brand-muted px-2.5 py-0.5 font-fira text-xs font-semibold text-text-brand">{key}</kbd>
          </div>
        {/each}
      </div>
      <div class="px-5 py-3 bg-muted-surface border-t-[2px] border-border-brand-muted">
        <p class="font-inter text-xs text-text-brand-muted">
          Press <kbd class="bg-card-surface border border-border-brand-muted px-1.5 py-0.5 font-fira text-xs">?</kbd> to toggle this dialog
        </p>
      </div>
    </div>
  </div>
{/if}

<button
  class="fixed bottom-14 right-6 z-30 w-10 h-10 flex items-center justify-center bg-card-surface border-[2px] border-border-brand-muted text-text-brand-muted hover:border-hister-indigo hover:text-hister-indigo cursor-pointer shadow-[3px_3px_0px_var(--border-brand)] hover:shadow-[3px_3px_0px_var(--hister-indigo)] transition-all"
  onclick={() => { showHelp = !showHelp; }}
  title="Keyboard shortcuts (?)"
  aria-label="Show keyboard shortcuts"
>
  <Keyboard class="size-4" />
</button>

{#if isSearching}
  <div class="flex-1 flex flex-col min-h-0">
    <div class="flex items-center gap-3 h-11 px-4 bg-card-surface border-b-[2px] border-border-brand-muted">
      <Search class="size-4 text-text-brand-muted shrink-0" />
      <input
        type="text"
        bind:this={inputEl}
        bind:value={query}
        placeholder="Search..."
        class="flex-1 h-full bg-transparent font-inter text-sm font-medium text-text-brand placeholder:text-text-brand-muted outline-none border-0"
      />
      {#if autocomplete && autocomplete !== query}
        <span class="font-fira text-xs text-text-brand-muted">
          Tab: <span class="text-hister-indigo">{autocomplete}</span>
        </span>
      {/if}
      <div class="w-2 h-2 shrink-0 pulse-dot {connected ? 'bg-hister-teal' : 'bg-hister-rose'}"></div>
    </div>

    <div class="flex-1 overflow-y-auto px-12 py-6 space-y-3 overflow-x-hidden">
      {#if hasResults}
        <div class="flex items-center justify-between">
          <span class="font-outfit text-base font-bold text-hister-indigo">
            {lastResults?.total || totalResults} results{query ? ` for "${query}"` : ''}
          </span>
          <div class="flex items-center gap-2">
            <Button
              variant="ghost"
              size="sm"
              class="font-inter text-xs text-text-brand-muted hover:text-hister-coral gap-1 no-underline"
              href={getSearchUrl(config.searchUrl, query)}
            >
              <ExternalLink class="size-3" />
              Web
            </Button>
            <Button
              variant="ghost"
              size="sm"
              class="font-inter text-xs text-hister-indigo hover:text-hister-coral"
              onclick={() => setSort(currentSort === '' ? 'domain' : '')}
            >
              Sort: {currentSort === 'domain' ? 'Domain' : 'Relevance'}
            </Button>
          </div>
        </div>

        {#if lastResults?.query && lastResults.query.text !== query}
          <p class="font-inter text-sm text-text-brand-muted">
            Expanded query: <code class="font-fira bg-muted-surface px-1.5 py-0.5 text-xs">{lastResults.query.text}</code>
          </p>
        {/if}

        <div class="flex items-center gap-3 font-inter text-sm text-text-brand-secondary">
          <label class="flex items-center gap-1.5">
            From:
            <Input type="date" bind:value={dateFrom} class="h-7 px-2 text-xs border-[2px] border-border-brand-muted bg-card-surface text-text-brand font-fira shadow-none focus-visible:ring-0 focus-visible:border-hister-indigo" />
          </label>
          <label class="flex items-center gap-1.5">
            To:
            <Input type="date" bind:value={dateTo} class="h-7 px-2 text-xs border-[2px] border-border-brand-muted bg-card-surface text-text-brand font-fira shadow-none focus-visible:ring-0 focus-visible:border-hister-indigo" />
          </label>
        </div>

        {#if lastResults?.history?.length}
          {#each lastResults.history as r, i}
            {@const favSrc = getFaviconSrc(r.favicon, r.url)}
            <div data-result class="flex gap-3 py-3.5 border-b-[2px] border-border-brand-muted w-full overflow-hidden transition-all duration-150"
              style={i === highlightIdx ? 'background: linear-gradient(90deg, transparent, rgba(90, 138, 138, 0.12), transparent); border-left: 3px solid var(--hister-teal); padding-left: 0.75rem;' : ''}>
              <div class="w-5 h-5 shrink-0 flex items-center justify-center mt-0.5 overflow-hidden bg-hister-teal">
                {#if favSrc}
                  <img src={favSrc} alt="" class="w-full h-full object-cover" onload={(e) => { (e.target as HTMLImageElement).parentElement!.style.backgroundColor = 'transparent'; }} onerror={(e) => { (e.target as HTMLImageElement).style.display = 'none'; (e.target as HTMLImageElement).nextElementSibling?.classList.remove('hidden'); }} />
                  <Star class="size-3 text-white hidden" />
                {:else}
                  <Star class="size-3 text-white" />
                {/if}
              </div>
              <div class="flex-1 min-w-0 w-0 space-y-0.5">
                <a data-result-link href={r.url} class="font-outfit text-base font-semibold text-hister-teal hover:underline block overflow-hidden text-ellipsis whitespace-nowrap w-full" onclick={(e) => { e.preventDefault(); openResult(r.url, r.title || '*title*'); }}>
                  {@html r.title || '*title*'}
                </a>
                <div class="flex items-center gap-2">
                  <span class="font-fira text-xs text-hister-teal truncate overflow-hidden text-ellipsis whitespace-nowrap">{r.url}</span>
                  <Badge variant="secondary" class="text-xs px-1.5 py-0 h-4 bg-hister-teal/10 text-hister-teal border-0">pinned</Badge>
                  <button data-readable class="flex items-center gap-0.5 font-inter text-xs font-medium text-hister-indigo hover:underline border-0 bg-transparent cursor-pointer p-0 shrink-0" onclick={(e) => openReadable(e, r.url, r.title || '*title*')}>
                    <Eye class="size-3" /><span>view</span>
                  </button>
                </div>
              </div>
              <Button
                variant="ghost"
                size="icon-sm"
                class="shrink-0 text-text-brand-muted hover:text-text-brand cursor-pointer"
                onclick={() => { showActionsForResult = showActionsForResult === 'history:' + r.url ? null : 'history:' + r.url; }}
              >
                <MoreVertical class="size-4" />
              </Button>
            </div>
            {#if showActionsForResult === 'history:' + r.url}
              <div class="ml-8 p-3 border-[2px] border-border-brand-muted bg-card-surface space-y-2">
                <Button variant="outline" size="sm" class="text-xs border-[2px] border-hister-rose text-hister-rose hover:bg-hister-rose/10" onclick={() => updatePriorityResult(r.url, r.title || '*title*', true)}>
                  <PinOff class="size-3.5" />
                  Remove priority
                </Button>
                {#if actionsMessage}
                  <p class="text-xs font-inter {actionsError ? 'text-hister-rose' : 'text-hister-teal'}">{actionsMessage}</p>
                {/if}
              </div>
            {/if}
          {/each}
        {/if}

        {#if lastResults?.documents}
          {#each lastResults.documents as r, i}
            {@const idx = historyLen + i}
            {@const color = getResultColor(i)}
            {@const favSrc = getFaviconSrc(r.favicon, r.url)}
            <div data-result class="flex gap-3 py-3.5 border-b-[2px] border-border-brand-muted w-full overflow-hidden transition-all duration-150"
              style={idx === highlightIdx ? `background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--${color}) 12%, transparent), transparent); border-left: 3px solid var(--${color}); padding-left: 0.75rem;` : ''}>
              <div class="w-5 h-5 shrink-0 flex items-center justify-center mt-0.5 overflow-hidden" style="background-color: var(--{color});">
                {#if favSrc}
                  <img src={favSrc} alt="" class="w-full h-full object-cover" onload={(e) => { (e.target as HTMLImageElement).parentElement!.style.backgroundColor = 'transparent'; }} onerror={(e) => { (e.target as HTMLImageElement).style.display = 'none'; (e.target as HTMLImageElement).nextElementSibling?.classList.remove('hidden'); }} />
                  <Globe class="size-3 text-white hidden" />
                {:else}
                  <Globe class="size-3 text-white" />
                {/if}
              </div>
              <div class="flex-1 min-w-0 w-0 space-y-0.5">
                <a data-result-link href={r.url} class="font-outfit text-base font-semibold hover:underline block overflow-hidden text-ellipsis whitespace-nowrap w-full" style="color: var(--{color});" onclick={(e) => { e.preventDefault(); openResult(r.url, r.title || '*title*'); }}>
                  {@html r.title || '*title*'}
                </a>
                {#if r.text}
                  <p class="font-inter text-sm text-text-brand-secondary leading-[1.4] line-clamp-1 overflow-hidden text-ellipsis whitespace-nowrap">{@html r.text}</p>
                {/if}
                <div class="flex items-center gap-2">
                  <span class="font-fira text-xs text-hister-teal truncate overflow-hidden text-ellipsis whitespace-nowrap">{r.url}</span>
                  {#if r.added}
                    <span class="font-inter text-xs text-text-brand-muted" title={formatTimestamp(r.added)}>· {formatRelativeTime(r.added)}</span>
                  {/if}
                  <button data-readable class="flex items-center gap-0.5 font-inter text-xs font-medium text-hister-indigo hover:underline border-0 bg-transparent cursor-pointer p-0 shrink-0" onclick={(e) => openReadable(e, r.url, r.title || '*title*')}>
                    <Eye class="size-3" /><span>view</span>
                  </button>
                </div>
              </div>
              <Button
                variant="ghost"
                size="icon-sm"
                class="shrink-0 text-text-brand-muted hover:text-text-brand cursor-pointer"
                onclick={() => { showActionsForResult = showActionsForResult === 'doc:' + r.url ? null : 'doc:' + r.url; }}
              >
                <MoreVertical class="size-4" />
              </Button>
            </div>
            {#if showActionsForResult === 'doc:' + r.url}
              <div class="ml-8 p-3 border-[2px] border-border-brand-muted bg-card-surface space-y-2">
                <div class="flex items-center gap-2">
                  <Input bind:value={actionsQuery} placeholder="Query for priority..." class="flex-1 h-7 text-sm font-inter border-[2px] border-border-brand-muted shadow-none focus-visible:ring-0 focus-visible:border-hister-indigo" />
                  <Button variant="outline" size="sm" class="text-xs border-[2px] border-hister-indigo text-hister-indigo" onclick={() => updatePriorityResult(r.url, r.title || '*title*', false)}>
                    <Pin class="size-3.5" />
                    Pin
                  </Button>
                </div>
                <Button variant="outline" size="sm" class="text-xs border-[2px] border-hister-rose text-hister-rose hover:bg-hister-rose/10" onclick={() => deleteResult(r.url)}>
                  <Trash2 class="size-3.5" />
                  Delete
                </Button>
                {#if actionsMessage}
                  <p class="text-xs font-inter {actionsError ? 'text-hister-rose' : 'text-hister-teal'}">{actionsMessage}</p>
                {/if}
              </div>
            {/if}
          {/each}
        {/if}

        <Separator class="bg-border-brand-muted" />
        <div class="flex items-center gap-4 font-inter text-xs text-text-brand-muted">
          <Download class="size-3.5" />
          <span>Export:</span>
          <Button variant="link" size="sm" class="text-xs text-hister-indigo p-0 h-auto" onclick={() => exportJSON(lastResults!)}>JSON</Button>
          <Button variant="link" size="sm" class="text-xs text-hister-indigo p-0 h-auto" onclick={() => exportCSV(lastResults!, query)}>CSV</Button>
          <Button variant="link" size="sm" class="text-xs text-hister-indigo p-0 h-auto" onclick={() => exportRSS(lastResults!, query)}>RSS</Button>
        </div>
      {:else if query && lastResults}
        <div class="text-center py-12">
          <p class="font-inter text-text-brand-secondary mb-4">No results found for "<span class="font-semibold">{query}</span>"</p>
          <Button variant="outline" class="border-[3px] border-hister-coral text-hister-coral hover:bg-hister-coral/10 font-inter font-semibold shadow-[3px_3px_0px_var(--hister-coral)]" href={getSearchUrl(config.searchUrl, query)}>
            <ExternalLink class="size-4" />
            Search
          </Button>
        </div>
      {:else if query}
        <div class="flex items-center justify-center py-16">
          <span class="font-inter text-text-brand-muted">Searching...</span>
        </div>
      {/if}
    </div>
  </div>
{:else}
  <div class="flex-1 flex flex-col items-center justify-center gap-10 py-12 px-12 overflow-y-auto relative">

    <h1
      bind:this={heroTitleEl}
      class="font-outfit font-black text-8xl leading-none tracking-[8px] bg-clip-text text-transparent select-none"
      style="background-image: linear-gradient(90deg, var(--hister-indigo), var(--hister-coral), var(--hister-teal), var(--hister-indigo)); background-size: 300% 100%; background-position: 0% 50%;"
    >
      HISTER
    </h1>

    <p class="font-inter text-lg text-text-brand-secondary">
      Your personal search engine
    </p>
    <div
      bind:this={underlineEl}
      class="h-[2px] w-48"
      style="background: linear-gradient(90deg, var(--hister-indigo), var(--hister-coral), var(--hister-teal)); transform: scaleX(0); transform-origin: left;"
    ></div>

    <div bind:this={searchBoxEl} class="search-box-gradient w-full max-w-[680px] p-[3px] shadow-[4px_4px_0px_var(--hister-coral)]">
      <div class="h-14 flex items-center gap-3 pl-4 bg-card-surface">
        <Search class="size-5 text-text-brand-muted shrink-0" />
        <input
          type="text"
          bind:this={inputEl}
          bind:value={query}
          placeholder="Search ..."
          class="flex-1 h-full bg-transparent font-inter text-base text-text-brand placeholder:text-text-brand-muted outline-none border-0"
        />
        <div class="w-2.5 h-2.5 mr-4 shrink-0 pulse-dot {connected ? 'bg-hister-teal' : 'bg-hister-rose'}" title={connected ? 'Connected' : 'Disconnected'}></div>
      </div>
    </div>

    <div bind:this={hintEl} class="flex items-center gap-2 font-inter text-xs text-text-brand-muted">
      <span>Pro tip: Press</span>
      <kbd bind:this={kbdEl} class="inline-block bg-muted-surface border-[2px] border-border-brand-muted px-2 py-0.5 font-fira text-xs font-semibold text-text-brand-secondary">/</kbd>
      <span>to focus search anywhere</span>
    </div>

    {#if recentSearches.length > 0}
      <div bind:this={chipsContainerEl} class="flex flex-wrap gap-3 items-center justify-center relative">
        {#each recentSearches as search, i}
          {@const chip = chipColors[i % chipColors.length]}
          <button
            class="border-[2px] {chip.border} {chip.bg} px-3.5 py-1.5 font-inter text-sm font-semibold {chip.text} cursor-pointer hover:opacity-90 hover:scale-105 hover:-translate-y-0.5 transition-all duration-200 bg-transparent"
            onclick={() => clickChip(search)}
            oncontextmenu={(e) => showChipContextMenu(e, search)}
          >
            {search}
          </button>
        {/each}
        <button
          class="border-[2px] border-hister-rose/40 px-2.5 py-1.5 font-inter text-xs font-semibold text-hister-rose/60 cursor-pointer hover:text-hister-rose hover:border-hister-rose hover:bg-hister-rose/10 transition-all duration-200 bg-transparent"
          onclick={deleteAllRecentSearches}
          title="Clear all recent searches"
        >
          &times; clear
        </button>
      </div>
    {/if}

    {#if contextMenuSearch}
      <div
        class="fixed inset-0 z-40"
        role="presentation"
        onclick={() => { contextMenuSearch = null; }}
        oncontextmenu={(e) => { e.preventDefault(); contextMenuSearch = null; }}
      ></div>
      <div
        class="fixed z-50 border-[2px] border-border-brand bg-card-surface shadow-[4px_4px_0px_var(--hister-indigo)] py-1 min-w-[160px]"
        style="left: {contextMenuPos.x}px; top: {contextMenuPos.y}px;"
      >
        <button
          class="w-full flex items-center gap-2 px-3 py-2 text-left font-inter text-sm text-text-brand hover:bg-muted-surface bg-transparent border-0 cursor-pointer"
          onclick={() => { clickChip(contextMenuSearch!); contextMenuSearch = null; }}
        >
          <Search class="size-3.5" /> Search "{contextMenuSearch}"
        </button>
        <div class="h-px bg-border-brand-muted mx-2"></div>
        <button
          class="w-full flex items-center gap-2 px-3 py-2 text-left font-inter text-sm text-hister-rose hover:bg-hister-rose/10 bg-transparent border-0 cursor-pointer"
          onclick={() => deleteRecentSearch(contextMenuSearch!)}
        >
          <Trash2 class="size-3.5" /> Remove
        </button>
      </div>
    {/if}

    <div bind:this={statsRowEl} class="flex items-center gap-8">
      <div class="flex items-center gap-2 text-hister-indigo">
        <History class="size-[18px]" />
        <span class="font-outfit text-xl font-extrabold">{displayHistoryCount}</span>
        <span class="font-inter text-sm">indexed pages</span>
      </div>
      <div class="flex items-center gap-2 text-hister-teal">
        <Shield class="size-[18px]" />
        <span class="font-outfit text-xl font-extrabold">{displayRulesCount}</span>
        <span class="font-inter text-sm">active rules</span>
      </div>
      <div class="flex items-center gap-2 text-hister-coral">
        <Link2 class="size-[18px]" />
        <span class="font-outfit text-xl font-extrabold">{displayAliasesCount}</span>
        <span class="font-inter text-sm">aliases</span>
      </div>
    </div>

  </div>
{/if}

<style>
  :global(.pulse-dot) {
    animation: pulse-throb 6s ease-in-out infinite;
  }
  @keyframes pulse-throb {
    0%, 100% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.5; transform: scale(1.6); }
  }
  .search-box-gradient {
    background: linear-gradient(90deg, var(--hister-indigo), var(--hister-coral), var(--hister-teal), var(--hister-indigo));
    background-size: 300% 100%;
    animation: gradient-slide 6s ease-in-out infinite alternate;
  }
  @keyframes gradient-slide {
    0% { background-position: 0% 50%; }
    100% { background-position: 100% 50%; }
  }
</style>
