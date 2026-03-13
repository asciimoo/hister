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
    openURL,
  } from '$lib/search';
  import { fetchConfig, apiFetch } from '$lib/api';
  import type { SearchResults } from '$lib/search';
  import { animate } from 'animejs';
  import { Input } from '@hister/components/ui/input';
  import { Button } from '@hister/components/ui/button';
  import { Badge } from '@hister/components/ui/badge';
  import { Separator } from '@hister/components/ui/separator';
  import * as Dialog from '@hister/components/ui/dialog';
  import * as Card from '@hister/components/ui/card';
  import * as DropdownMenu from '@hister/components/ui/dropdown-menu';
  import { ScrollArea } from '@hister/components/ui/scroll-area';
  import { Kbd } from '@hister/components/ui/kbd';
  import {
    Search,
    Star,
    Globe,
    MoreVertical,
    Eye,
    Trash2,
    Pin,
    PinOff,
    Download,
    ExternalLink,
    History,
    Shield,
    Link2,
    Keyboard,
    HelpCircle,
    X,
    ChevronDown,
    Calendar,
    Filter,
  } from 'lucide-svelte';
  import type { HistoryItem } from '$lib/types';

  interface Config {
    wsUrl: string;
    searchUrl: string;
    openResultsOnNewTab: boolean;
    hotkeys: Record<string, string>;
  }

  let config: Config = $state({
    wsUrl: '',
    searchUrl: '',
    openResultsOnNewTab: false,
    hotkeys: {},
  });

  let wsManager: WebSocketManager | undefined;
  let keyHandler: KeyHandler | undefined;
  let inputEl: HTMLInputElement | null = $state(null);

  let query = $state('');
  let autocomplete = $state('');
  let connected = $state(false);
  let lastResults = $state<SearchResults | null>(null);
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
  let resultsShown = $state(false);

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
  let kbdEl: HTMLElement | null = $state(null);
  let underlineEl: HTMLElement | undefined = $state();

  let animationHandles: any[] = [];

  const chipColors = [
    { border: 'border-hister-indigo', bg: 'bg-hister-indigo/10', text: 'text-hister-indigo' },
    { border: 'border-hister-teal', bg: 'bg-hister-teal/10', text: 'text-hister-teal' },
    { border: 'border-hister-coral', bg: 'bg-hister-coral/10', text: 'text-hister-coral' },
    { border: 'border-hister-amber', bg: 'bg-hister-amber/10', text: 'text-hister-amber' },
  ];

  const hotkeyActions: Record<
    string,
    (e?: KeyboardEvent, isInputFocus?: boolean) => void | boolean
  > = {
    open_result: openSelectedResult,
    open_result_in_new_tab: (e?: KeyboardEvent, i?: boolean) => openSelectedResult(e, i, true),
    select_next_result: selectNextResult,
    select_previous_result: selectPreviousResult,
    open_query_in_search_engine: openQueryInSearchEngine,
    focus_search_input: focusSearchInput,
    view_result_popup: viewResultPopup,
    autocomplete: autocompleteQuery,
    show_hotkeys: showHotkeys,
  };

  const isSearching = $derived(query.length > 0 || resultsShown);

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
      onClose: () => {
        connected = false;
      },
      onError: () => {
        connected = false;
      },
    });
    wsManager.connect();
  }

  function sendQuery(q: string) {
    const message = buildSearchQuery(q, currentSort, dateFrom, dateTo);
    wsManager?.send(JSON.stringify(message));
  }

  let skipUrlUpdate = false;
  let lastPushedEmpty = true;

  function updateURL() {
    if (skipUrlUpdate) return;
    const isEmpty = !query;
    const url = query
      ? `${window.location.pathname}?q=${encodeURIComponent(query)}${dateFrom ? '&date_from=' + encodeURIComponent(dateFrom) : ''}${dateTo ? '&date_to=' + encodeURIComponent(dateTo) : ''}`
      : window.location.pathname;

    if (isEmpty !== lastPushedEmpty) {
      history.pushState({ query, dateFrom, dateTo }, '', url);
      lastPushedEmpty = isEmpty;
    } else {
      history.replaceState({ query, dateFrom, dateTo }, '', url);
    }
  }

  function handlePopState() {
    skipUrlUpdate = true;
    const params = new URLSearchParams(window.location.search);
    query = params.get('q') || '';
    dateFrom = params.get('date_from') || '';
    dateTo = params.get('date_to') || '';
    lastPushedEmpty = !query;
    if (query && connected) sendQuery(query);
    if (!query) {
      autocomplete = '';
      lastResults = null;
    }
    tick().then(() => {
      skipUrlUpdate = false;
    });
  }

  function renderResults(event: MessageEvent) {
    const res = parseSearchResults(event.data);
    lastResults = res;
    autocomplete = (query && res.query_suggestion) || '';
    highlightIdx = 0;
    resultsShown = true;
  }

  function stripHtml(s: string): string {
    return s.replace(/<[^>]*>/g, '');
  }

  function openResult(url: string, title: string, newWindow = false) {
    if (config.openResultsOnNewTab) newWindow = true;
    saveHistoryItem(url, stripHtml(title), query, false, () => openURL(url, newWindow));
  }

  async function saveHistoryItem(
    url: string,
    title: string,
    queryStr: string,
    remove: boolean,
    callback?: () => void,
  ) {
    try {
      const res = await apiFetch('/history', {
        method: 'POST',
        headers: { 'Content-type': 'application/json; charset=UTF-8' },
        body: JSON.stringify({ url, title, query: queryStr, delete: remove }),
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
        documents: lastResults.documents.filter((d) => d.url !== url),
      };
    }
  }

  function updatePriorityResult(url: string, title: string, remove: boolean) {
    const q = actionsQuery || query;
    if (!q) return;
    saveHistoryItem(url, stripHtml(title), q, remove, () => {
      actionsMessage = `Priority result ${remove ? 'deleted' : 'added'}.`;
      actionsError = false;
    });
  }

  async function openReadable(e: Event, url: string, title: string) {
    e.preventDefault();
    if (e.stopPropagation) e.stopPropagation();
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

  function selectNextResult(e?: KeyboardEvent) {
    if (e) e.preventDefault();
    selectNthResult(1);
  }
  function selectPreviousResult(e?: KeyboardEvent) {
    if (e) e.preventDefault();
    selectNthResult(-1);
  }

  function openSelectedResult(e?: KeyboardEvent, isInputFocus?: boolean, newWindow = false) {
    if (e) e.preventDefault();
    if (query.startsWith('!!')) {
      openURL(getSearchUrl(config.searchUrl, query.substring(2)), newWindow);
      return;
    }
    const res = document.querySelectorAll<HTMLAnchorElement>('[data-result] [data-result-link]')[
      highlightIdx
    ];
    if (res) {
      openResult(res.getAttribute('href')!, res.innerText, newWindow);
    }
  }

  function viewResultPopup(e?: KeyboardEvent) {
    if (e) e.preventDefault();
    if (showPopup) {
      closePopup();
      return;
    }
    const readables = document.querySelectorAll('[data-result] [data-readable]');
    if (highlightIdx >= 0 && highlightIdx < readables.length) {
      const el = readables[highlightIdx] as HTMLElement;
      const result = el.closest('[data-result]')!;
      const link = result.querySelector<HTMLAnchorElement>('[data-result-link]')!;
      openReadable({ preventDefault: () => {} } as Event, link.href, link.innerText);
    }
  }

  function autocompleteQuery(e?: KeyboardEvent, isInputFocus?: boolean) {
    if (e) e.preventDefault();
    if (isInputFocus && autocomplete && query !== autocomplete) {
      query = autocomplete;
      sendQuery(query);
    } else {
      return true;
    }
  }

  function openQueryInSearchEngine(e?: KeyboardEvent) {
    if (e) e.preventDefault();
    openURL(getSearchUrl(config.searchUrl, query));
  }
  function focusSearchInput(e?: KeyboardEvent, isInputFocus?: boolean) {
    if (!isInputFocus) {
      if (e) e.preventDefault();
      inputEl?.focus();
    }
  }

  function closePopup(): boolean {
    if (showPopup) {
      showPopup = false;
      return true;
    }
    return false;
  }

  const hotkeyDescriptions: Record<string, string> = {
    open_result: 'Open result',
    open_result_in_new_tab: 'Open result in new tab',
    select_next_result: 'Select next result',
    select_previous_result: 'Select previous result',
    open_query_in_search_engine: 'Open in search engine',
    focus_search_input: 'Focus search input',
    view_result_popup: 'View result content',
    autocomplete: 'Autocomplete query',
    show_hotkeys: 'Show help',
  };

  function showHotkeys(e?: KeyboardEvent, isInputFocus?: boolean) {
    if (showHelp) {
      showHelp = false;
      return;
    }
    if (!isInputFocus) {
      showHelp = true;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    const isInputFocus =
      document.activeElement instanceof HTMLInputElement ||
      document.activeElement instanceof HTMLTextAreaElement;
    keyHandler?.handle(e, isInputFocus);
    if (e.key === 'Escape') {
      if (showHelp) {
        showHelp = false;
        e.preventDefault();
        return;
      }
      if (contextMenuSearch) {
        contextMenuSearch = null;
        e.preventDefault();
        return;
      }
      if (closePopup()) {
        e.preventDefault();
        return;
      }
      if (isSearching) {
        query = '';
        resultsShown = false;
        return;
      }
    }
    showActionsForResult = null;
    contextMenuSearch = null;
  }

  function clickChip(q: string) {
    query = q;
    inputEl?.focus();
  }

  function deleteRecentSearch(q: string) {
    recentSearches = recentSearches.filter((s) => s !== q);
    localStorage.setItem(
      'deletedSearches',
      JSON.stringify([...JSON.parse(localStorage.getItem('deletedSearches') || '[]'), q]),
    );
    contextMenuSearch = null;
  }

  function deleteAllRecentSearches() {
    localStorage.setItem(
      'deletedSearches',
      JSON.stringify([
        ...JSON.parse(localStorage.getItem('deletedSearches') || '[]'),
        ...recentSearches,
      ]),
    );
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
      const statsRes = await apiFetch('/stats', { headers: { Accept: 'application/json' } });

      if (statsRes.ok) {
        const stats = await statsRes.json();
        rulesCount = stats.rule_count;
        aliasesCount = stats.alias_count;
        historyCount = stats.doc_count;
        if (stats.recent_searches) {
          const deletedSearches: string[] = JSON.parse(
            localStorage.getItem('deletedSearches') || '[]',
          );
          recentSearches = stats.recent_searches
            .map((s: { query: string }) => s.query)
            .filter((q: string) => !deletedSearches.includes(q));
        }
      }
    } catch (e) {
      console.log('Failed to retreive stats', e);
    }
    statsLoaded = true;
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
          alternate: true,
        }),
      );
    }

    if (kbdEl) {
      animationHandles.push(
        animate(kbdEl, {
          translateY: [0, 3, 0],
          duration: 400,
          ease: 'inOutSine',
          loop: true,
          loopDelay: 10000,
        }),
      );
    }

    if (underlineEl) {
      animationHandles.push(
        animate(underlineEl, {
          scaleX: [0, 1],
          duration: 800,
          ease: 'outCubic',
          delay: 300,
        }),
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
        },
      }),
    );
  }

  function cleanupAnimations() {
    for (const h of animationHandles) {
      try {
        h.revert();
      } catch {}
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
    (async () => {
      await tick();
      inputEl?.focus();
    })();
  });
  $effect(() => {
    if (query && connected) {
      sendQuery(query);
      localStorage.setItem('lastQuery', query);
    }
  });
  $effect(() => {
    if (!query) {
      autocomplete = '';
      lastResults = null;
    }
  });
  $effect(() => {
    if (dateFrom || dateTo) sendQuery(query);
  });
  $effect(() => {
    updateURL();
  });
  $effect.pre(() => {
    const urlParams = new URLSearchParams(window.location.search);
    const q = urlParams.get('q');
    const df = urlParams.get('date_from');
    const dt = urlParams.get('date_to');
    if (q) query = q;
    if (df) dateFrom = df;
    if (dt) dateTo = dt;
    lastPushedEmpty = !q;
  });

  onMount(() => {
    (async () => {
      const appConfig = await fetchConfig();
      const wsProto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const wsUrl = new URL(appConfig.wsUrl);
      config = {
        wsUrl: `${wsProto}//${location.host}${wsUrl.pathname}`,
        searchUrl: appConfig.searchUrl,
        openResultsOnNewTab: appConfig.openResultsOnNewTab,
        hotkeys: appConfig.hotkeys,
      };
      inputEl?.focus();
      connect();
      keyHandler = new KeyHandler(config.hotkeys, hotkeyActions);
      loadHomeStats();
    })();
    return () => {
      wsManager?.close();
      cleanupAnimations();
    };
  });
</script>

<svelte:head>
  <title>Hister</title>
</svelte:head>

<svelte:window onkeydown={handleKeydown} onpopstate={handlePopState} />

<Dialog.Root bind:open={showPopup}>
  <Dialog.Content escapeKeydownBehavior="ignore"
    class="border-border-brand bg-card-surface max-h-[80vh] max-w-2xl overflow-auto rounded-none border-[3px] p-6 shadow-[6px_6px_0px_var(--hister-indigo)]"
  >
    <Dialog.Header class="border-border-brand-muted border-b-[3px] pb-4">
      <Dialog.Title class="font-outfit text-text-brand text-lg font-bold">{popupTitle}</Dialog.Title
      >
    </Dialog.Header>
    <div class="font-inter text-text-brand-secondary prose max-w-none text-sm">
      {@html popupContent}
    </div>
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={showHelp}>
  <Dialog.Content
    showCloseButton={false}
    class="border-border-brand bg-card-surface max-w-md gap-0 overflow-hidden rounded-none border-[3px] p-0 shadow-[6px_6px_0px_var(--hister-indigo)]"
  >
    <Dialog.Header class="bg-hister-indigo flex-row items-center justify-between gap-2 px-5 py-4">
      <Dialog.Title class="flex items-center gap-2">
        <Keyboard class="size-5 text-white" />
        <span class="font-outfit text-lg font-extrabold text-white">Keyboard Shortcuts</span>
      </Dialog.Title>
      <Dialog.Close class="p-0.5 text-white/70 hover:text-white">
        <X class="size-5" />
      </Dialog.Close>
    </Dialog.Header>
    <Card.Content class="space-y-0 p-4">
      {#each Object.entries(config.hotkeys) as [key, action]}
        <div
          class="border-border-brand-muted flex items-center justify-between border-b-[1px] py-2.5"
        >
          <span class="font-inter text-text-brand-secondary"
            >{hotkeyDescriptions[action] || action}</span
          >
          <Kbd
            class="bg-muted-surface border-border-brand-muted font-fira text-text-brand h-auto rounded-none border-[2px] px-2.5 py-0.5 text-xs font-semibold"
            >{key}</Kbd
          >
        </div>
      {/each}
    </Card.Content>
    <Card.Footer class="bg-muted-surface border-border-brand-muted border-t-[2px] px-5 py-3">
      <p class="font-inter text-text-brand-muted text-xs">
        Press <Kbd
          class="bg-card-surface border-border-brand-muted font-fira h-auto rounded-none border px-1.5 py-0.5 text-[10px]"
          >?</Kbd
        > to toggle this dialog
      </p>
    </Card.Footer>
  </Dialog.Content>
</Dialog.Root>

<Button
  variant="outline"
  size="icon"
  class="bg-card-surface border-brutal-border text-text-brand-muted hover:border-hister-indigo hover:text-hister-indigo shadow-brutal hover:shadow-brutal-sm fixed right-6 bottom-14 z-30 hidden rounded-none border-[3px] transition-all hover:translate-x-[2px] hover:translate-y-[2px] md:inline-flex"
  onclick={() => {
    showHelp = !showHelp;
  }}
  title="Keyboard shortcuts (?)"
  aria-label="Show keyboard shortcuts"
>
  <Keyboard class="size-4" />
</Button>

{#if isSearching}
  <div class="flex min-h-0 flex-1 flex-col">
    <div
      class="search bg-card-surface border-brutal-border flex h-10 shrink-0 items-center gap-3 border-b-[3px] px-4 md:h-14"
    >
      <Search class="text-text-brand-muted size-4 md:size-6" />
      <Input
        bind:ref={inputEl}
        bind:value={query}
        placeholder="Search..."
        class="font-inter text-text-brand placeholder:text-text-brand-muted h-full flex-1 border-0 bg-transparent p-0 text-lg font-medium shadow-none focus-visible:ring-0 md:text-2xl"
      />
      <div
        class="pulse-dot h-2 w-2 shrink-0 {connected ? 'bg-hister-teal' : 'bg-hister-rose'}"
        title={connected ? 'Connected' : 'Disconnected'}
      ></div>
    </div>
    {#if autocomplete && autocomplete !== query}
      <span class="font-fira text-text-brand-muted mx-8 text-sm">
        Tab: <span class="text-hister-indigo">{autocomplete}</span>
      </span>
    {/if}

    <ScrollArea class="min-h-0 flex-1">
      <div class="w-full max-w-[70em] space-y-3 overflow-x-hidden px-3 py-2 md:px-12">
        {#if hasResults}
          <div class="flex flex-wrap items-center justify-between gap-2">
            <span class="font-outfit text-hister-indigo text-sm font-bold md:text-base">
              {lastResults?.total || totalResults} results{query ? ` for "${query}"` : ''}
            </span>
            <div class="flex items-center gap-2">
              <DropdownMenu.Root>
                <DropdownMenu.Trigger>
                  {#snippet child({ props })}
                  <Button
                    {...props}
                    variant="ghost"
                    size="sm"
                    class="font-inter text-text-brand-muted hover:text-hister-indigo gap-1 text-xs"
                  >
                    <Filter class="size-3" />
                    Search Actions
                    <ChevronDown class="size-3" />
                  </Button>
                  {/snippet}
                </DropdownMenu.Trigger>
                <DropdownMenu.Content
                  class="border-brutal-border bg-card-surface w-80 rounded-none border-[3px] p-3 shadow-[4px_4px_0_var(--brutal-shadow)]"
                >
                  <div class="space-y-3">
                    <div class="space-y-2">
                      <p class="font-inter text-text-brand-muted flex items-center gap-1.5 text-xs font-semibold">
                        <Calendar class="size-3" />
                        Date Filter
                      </p>
                      <div class="flex flex-col gap-2">
                        <label class="font-inter text-text-brand-secondary flex items-center gap-1.5 text-xs">
                          From:
                          <Input
                            type="date"
                            bind:value={dateFrom}
                            class="border-border-brand-muted bg-card-surface text-text-brand font-fira focus-visible:border-hister-indigo h-7 flex-1 border-[2px] px-2 text-xs shadow-none focus-visible:ring-0"
                          />
                        </label>
                        <label class="font-inter text-text-brand-secondary flex items-center gap-1.5 text-xs">
                          To:
                          <Input
                            type="date"
                            bind:value={dateTo}
                            class="border-border-brand-muted bg-card-surface text-text-brand font-fira focus-visible:border-hister-indigo h-7 flex-1 border-[2px] px-2 text-xs shadow-none focus-visible:ring-0"
                          />
                        </label>
                      </div>
                    </div>
                    <Separator class="bg-border-brand-muted" />
                    <div class="space-y-2">
                      <p class="font-inter text-text-brand-muted flex items-center gap-1.5 text-xs font-semibold">
                        <Download class="size-3" />
                        Export Results
                      </p>
                      <div class="flex flex-wrap gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          class="border-hister-indigo text-hister-indigo hover:bg-hister-indigo/10 h-7 border-[2px] text-xs"
                          onclick={() => exportJSON(lastResults!)}
                        >
                          JSON
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          class="border-hister-indigo text-hister-indigo hover:bg-hister-indigo/10 h-7 border-[2px] text-xs"
                          onclick={() => exportCSV(lastResults!, query)}
                        >
                          CSV
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          class="border-hister-indigo text-hister-indigo hover:bg-hister-indigo/10 h-7 border-[2px] text-xs"
                          onclick={() => exportRSS(lastResults!, query)}
                        >
                          RSS
                        </Button>
                      </div>
                    </div>
                  </div>
                </DropdownMenu.Content>
              </DropdownMenu.Root>
              <Button
                variant="ghost"
                size="sm"
                class="font-inter text-text-brand-muted hover:text-hister-coral gap-1 text-xs no-underline"
                href={getSearchUrl(config.searchUrl, query)}
              >
                <ExternalLink class="size-3" />
                Web
              </Button>
              <Button
                variant="ghost"
                size="sm"
                class="font-inter text-hister-indigo hover:text-hister-coral text-xs"
                onclick={() => setSort(currentSort === '' ? 'domain' : '')}
              >
                Sort: {currentSort === 'domain' ? 'Domain' : 'Relevance'}
              </Button>
            </div>
          </div>

          {#if lastResults?.query && lastResults.query.text !== query}
            <p class="font-inter text-text-brand-muted text-sm">
              Expanded query: <code
                class="font-fira bg-muted-surface text-primary px-1.5 py-0.5 text-xs"
                >{lastResults.query.text}</code
              >
            </p>
          {/if}

          {#if lastResults?.history?.length}
            {#each lastResults.history as r, i}
              {@const favSrc = getFaviconSrc(r.favicon, r.url)}
              <article
                data-result
                class="flex w-full gap-3 overflow-hidden py-3.5 transition-all duration-150 scroll-my-[6em]"
                style={i === highlightIdx
                  ? 'background: linear-gradient(90deg, transparent, rgba(90, 138, 138, 0.12), transparent); border-left: 3px solid var(--hister-teal); padding-left: 0.75rem;'
                  : ''}
              >
                <div
                  class="bg-hister-teal mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center overflow-hidden"
                >
                  {#if favSrc}
                    <img
                      src={favSrc}
                      alt=""
                      class="h-full w-full object-cover"
                      onload={(e) => {
                        (e.target as HTMLImageElement).parentElement!.style.backgroundColor =
                          'transparent';
                      }}
                      onerror={(e) => {
                        (e.target as HTMLImageElement).style.display = 'none';
                        (e.target as HTMLImageElement).nextElementSibling?.classList.remove(
                          'hidden',
                        );
                      }}
                    />
                    <Star class="hidden size-3 text-white" />
                  {:else}
                    <Star class="size-3 text-white" />
                  {/if}
                </div>
                <div class="w-0 min-w-0 flex-1 space-y-0.5">
                  <a
                    data-result-link
                    href={r.url}
                    class="font-outfit text-md text-hister-teal block w-full overflow-hidden font-semibold text-ellipsis whitespace-nowrap hover:underline md:text-xl"
                    onclick={(e) => {
                      e.preventDefault();
                      openResult(r.url, r.title || '*title*', e.ctrlKey || e.metaKey);
                    }}
                  >
                    {@html r.title || '*title*'}
                  </a>
                  <div class="flex items-center gap-2">
                    <span
                      class="font-fira text-hister-teal truncate overflow-hidden text-ellipsis whitespace-nowrap"
                      >{r.url}</span
                    >
                    <Badge
                      variant="secondary"
                      class="bg-hister-teal/10 text-hister-teal h-4 border-0 px-1.5 py-0"
                      >pinned</Badge
                    >
                    <Button
                      data-readable
                      variant="link"
                      size="sm"
                      class="text-hister-indigo h-auto shrink-0 gap-0.5 p-0 text-xs font-medium md:text-sm"
                      onclick={(e) => openReadable(e, r.url, r.title || '*title*')}
                    >
                      <Eye class="size-3" /><span>view</span>
                    </Button>
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="text-text-brand-muted hover:text-text-brand shrink-0 cursor-pointer"
                  onclick={() => {
                    showActionsForResult =
                      showActionsForResult === 'history:' + r.url ? null : 'history:' + r.url;
                  }}
                >
                  <MoreVertical class="size-4" />
                </Button>
              </article>
              {#if showActionsForResult === 'history:' + r.url}
                {(actionsMessage = '')}
                <Card.Root
                  class="border-brutal-border bg-card-surface ml-8 gap-2 rounded-none border-[3px] py-3 shadow-[3px_3px_0_var(--brutal-shadow)]"
                >
                  <Card.Content class="space-y-2">
                    <Button
                      variant="outline"
                      size="sm"
                      class="border-hister-rose text-hister-rose hover:bg-hister-rose/10 border-[2px] text-xs"
                      onclick={() => updatePriorityResult(r.url, r.title || '*title*', true)}
                    >
                      <PinOff class="size-3.5" />
                      Unpin
                    </Button>
                    {#if actionsMessage}
                      <p
                        class="font-inter text-xs {actionsError
                          ? 'text-hister-rose'
                          : 'text-hister-teal'}"
                      >
                        {actionsMessage}
                      </p>
                    {/if}
                  </Card.Content>
                </Card.Root>
              {/if}
            {/each}
          {/if}

          {#if lastResults?.documents}
            {#each lastResults.documents as r, i}
              {@const idx = historyLen + i}
              {@const color = 'hister-cyan'}
              {@const favSrc = getFaviconSrc(r.favicon, r.url)}
              <article
                data-result
                class="flex w-full gap-3 overflow-hidden py-3.5 transition-all duration-150 scroll-my-[6em]"
                style={idx === highlightIdx
                  ? `background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--${color}) 12%, transparent), transparent); border-left: 3px solid var(--${color}); padding-left: 0.75rem;`
                  : ''}
              >
                <div
                  class="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center overflow-hidden"
                  style="background-color: var(--{color});"
                >
                  {#if favSrc}
                    <img
                      src={favSrc}
                      alt=""
                      class="h-full w-full object-cover"
                      onload={(e) => {
                        (e.target as HTMLImageElement).parentElement!.style.backgroundColor =
                          'transparent';
                      }}
                      onerror={(e) => {
                        (e.target as HTMLImageElement).style.display = 'none';
                        (e.target as HTMLImageElement).nextElementSibling?.classList.remove(
                          'hidden',
                        );
                      }}
                    />
                    <Globe class="hidden size-3 text-white" />
                  {:else}
                    <Globe class="size-3 text-white" />
                  {/if}
                </div>
                <div class="w-0 min-w-0 flex-1 space-y-0.5">
                  <a
                    data-result-link
                    href={r.url}
                    class="font-outfit text-md block w-full font-semibold hover:underline md:text-xl"
                    style="color: var(--{color});"
                    onclick={(e) => {
                      e.preventDefault();
                      openResult(r.url, r.title || '*title*', e.ctrlKey || e.metaKey);
                    }}
                  >
                    {@html r.title || '*title*'}
                  </a>
                  <div class="items-left flex flex-col gap-0 md:flex-row md:items-center md:gap-2">
                    <span
                      class="font-fira text-hister-teal truncate overflow-hidden text-xs text-ellipsis whitespace-nowrap md:text-sm"
                      >{r.url}</span
                    >
                    {#if r.added}
                      <span
                        class="font-inter text-text-brand-muted whitespace-nowrap text-xs md:text-sm"
                        title={formatTimestamp(r.added)}>· {formatRelativeTime(r.added)}</span
                      >
                    {/if}
                    <Button
                      data-readable
                      variant="link"
                      size="sm"
                      class="text-hister-indigo h-auto shrink-0 gap-0.5 p-0 text-xs font-medium md:text-sm"
                      onclick={(e) => openReadable(e, r.url, r.title || '*title*')}
                    >
                      <Eye class="size-3" /><span>view</span>
                    </Button>
                  </div>
                  {#if r.text}
                    <p
                      class="font-inter text-text-brand-secondary text-sm leading-[1.4] md:text-base"
                    >
                      {@html r.text}
                    </p>
                  {/if}
                </div>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="text-text-brand-muted hover:text-text-brand shrink-0 cursor-pointer"
                  onclick={() => {
                    showActionsForResult =
                      showActionsForResult === 'doc:' + r.url ? null : 'doc:' + r.url;
                  }}
                >
                  <MoreVertical class="size-4" />
                </Button>
              </article>
              {#if showActionsForResult === 'doc:' + r.url}
                {(actionsMessage = '')}
                <Card.Root
                  class="border-brutal-border bg-card-surface ml-8 gap-2 rounded-none border-[3px] py-3 shadow-[3px_3px_0_var(--brutal-shadow)]"
                >
                  <Card.Content class="space-y-2">
                    <div class="flex items-center gap-2">
                      <Input
                        bind:value={actionsQuery}
                        placeholder="Query string where this result should appear pinned..."
                        class="font-inter border-border-brand-muted focus-visible:border-hister-indigo h-7 flex-1 border-[2px] text-sm shadow-none focus-visible:ring-0"
                      />
                      <Button
                        variant="outline"
                        size="sm"
                        class="border-hister-indigo text-hister-indigo border-[2px] text-xs"
                        onclick={() => updatePriorityResult(r.url, r.title || '*title*', false)}
                      >
                        <Pin class="size-3.5" />
                        Pin
                      </Button>
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      class="border-hister-rose text-hister-rose hover:bg-hister-rose/10 border-[2px] text-xs"
                      onclick={() => deleteResult(r.url)}
                    >
                      <Trash2 class="size-3.5" />
                      Delete result
                    </Button>
                    {#if actionsMessage}
                      <p
                        class="font-inter text-xs {actionsError
                          ? 'text-hister-rose'
                          : 'text-hister-teal'}"
                      >
                        {actionsMessage}
                      </p>
                    {/if}
                  </Card.Content>
                </Card.Root>
              {/if}
            {/each}
          {/if}
        {:else if query && lastResults}
          <section class="pmd:px-12 y-12 text-center">
            <p class="font-inter text-text-brand-secondary mb-4">
              No results found for "<span class="font-semibold">{query}</span>"
            </p>
            <Button
              variant="outline"
              class="border-hister-coral text-hister-coral hover:bg-hister-coral/10 font-inter border-[3px] font-semibold shadow-[3px_3px_0px_var(--hister-coral)]"
              href={getSearchUrl(config.searchUrl, query)}
            >
              <ExternalLink class="size-4" />
              Search
            </Button>
          </section>
        {:else if query}
          <div class="flex items-center justify-center py-16">
            <span class="font-inter text-text-brand-muted">Searching...</span>
          </div>
        {/if}
      </div>
    </ScrollArea>
  </div>
{:else}
  <div
    class="relative flex flex-1 flex-col items-center gap-5 overflow-y-auto px-4 py-4 md:gap-10 md:px-12 md:py-12"
  >
    <h1
      bind:this={heroTitleEl}
      class="font-outfit bg-clip-text text-5xl leading-none font-black tracking-[8px] text-transparent select-none md:text-9xl"
      style="background-image: linear-gradient(90deg, var(--hister-indigo), var(--hister-coral), var(--hister-teal), var(--hister-indigo)); background-size: 300% 100%; background-position: 0% 50%;"
    >
      HISTER
    </h1>

    <p class="font-inter text-md text-text-brand-secondary md:text-lg">Your own search engine</p>
    <div
      bind:this={underlineEl}
      class="h-[3px] w-48"
      style="background: linear-gradient(90deg, var(--hister-indigo), var(--hister-coral), var(--hister-teal)); transform: scaleX(0); transform-origin: left;"
    ></div>

    <div
      bind:this={searchBoxEl}
      class="search-box-gradient w-full max-w-[1200px] p-[3px] shadow-[4px_4px_0px_var(--hister-coral)]"
    >
      <div class="bg-card-surface flex h-10 items-center gap-3 pl-4 md:h-14">
        <Search class="text-text-brand-muted size-6" />
        <Input
          bind:ref={inputEl}
          bind:value={query}
          placeholder="Search ..."
          class="font-inter text-text-brand placeholder:text-text-brand-muted h-full min-w-0 flex-1 border-0 bg-transparent p-0 shadow-none focus-visible:ring-0 md:text-lg"
        />
        <div
          class="pulse-dot mr-4 h-2.5 w-2.5 shrink-0 {connected
            ? 'bg-hister-teal'
            : 'bg-hister-rose'}"
          title={connected ? 'Connected' : 'Disconnected'}
        ></div>
      </div>
    </div>

    <div
      bind:this={hintEl}
      class="font-inter text-text-brand-muted hidden items-center gap-1 text-xs md:flex md:gap-2"
    >
      <span>Pro tip: Press</span>
      <Kbd
        bind:ref={kbdEl}
        class="bg-muted-surface border-border-brand-muted font-fira text-text-brand-secondary rounded-none border-[2px] px-2 py-0.5 text-xs font-semibold"
        >/</Kbd
      >
      <span>to focus search anywhere</span>
    </div>

    {#if recentSearches.length > 0}
      <div
        bind:this={chipsContainerEl}
        class="relative flex flex-wrap items-center justify-center gap-3"
      >
        {#each recentSearches as search, i}
          {@const chip = chipColors[i % chipColors.length]}
          <Button
            variant="outline"
            class="border-[3px] {chip.border} {chip.bg} font-inter px-3.5 py-1.5 text-sm font-semibold {chip.text} brutal-press h-auto rounded-none"
            onclick={() => clickChip(search)}
            oncontextmenu={(e) => showChipContextMenu(e, search)}
          >
            {search}
          </Button>
        {/each}
        <Button
          variant="ghost"
          size="sm"
          class="border-hister-rose/40 font-inter text-hister-rose/60 hover:text-hister-rose hover:border-hister-rose hover:bg-hister-rose/10 h-auto rounded-none border-[2px] px-2.5 py-1.5 text-xs font-semibold transition-all duration-200"
          onclick={deleteAllRecentSearches}
          title="Clear all recent searches"
        >
          &times; clear
        </Button>
      </div>
    {/if}

    {#if contextMenuSearch}
      <div
        class="fixed inset-0 z-40"
        role="presentation"
        onclick={() => {
          contextMenuSearch = null;
        }}
        oncontextmenu={(e) => {
          e.preventDefault();
          contextMenuSearch = null;
        }}
      ></div>
      <div
        class="border-brutal-border bg-card-surface fixed z-50 min-w-[160px] border-[3px] py-1 shadow-[4px_4px_0_var(--brutal-shadow)]"
        style="left: {contextMenuPos.x}px; top: {contextMenuPos.y}px;"
      >
        <Button
          variant="ghost"
          class="font-inter text-text-brand hover:bg-muted-surface h-auto w-full justify-start gap-2 rounded-none px-3 py-2 text-sm"
          onclick={() => {
            clickChip(contextMenuSearch!);
            contextMenuSearch = null;
          }}
        >
          <Search class="size-3.5" /> Search "{contextMenuSearch}"
        </Button>
        <Separator class="bg-border-brand-muted mx-2" />
        <Button
          variant="ghost"
          class="font-inter text-hister-rose hover:bg-hister-rose/10 h-auto w-full justify-start gap-2 rounded-none px-3 py-2 text-sm"
          onclick={() => deleteRecentSearch(contextMenuSearch!)}
        >
          <Trash2 class="size-3.5" /> Remove
        </Button>
      </div>
    {/if}

    <div bind:this={statsRowEl} class="flex flex-col items-center gap-3 md:flex-row md:gap-8">
      <div
        class="border-brutal-border shadow-brutal-sm flex items-center gap-2 border-[3px] px-4 py-2"
        style="color: var(--hister-indigo);"
      >
        <History class="size-3 md:size-4.5" />
        <span class="font-outfit text-xl font-extrabold">{displayHistoryCount}</span>
        <span class="font-inter text-sm">indexed pages</span>
      </div>
      <div
        class="border-brutal-border shadow-brutal-sm flex items-center gap-2 border-[3px] px-4 py-2"
        style="color: var(--hister-teal);"
      >
        <Shield class="size-3 md:size-4.5" />
        <span class="font-outfit text-xl font-extrabold">{displayRulesCount}</span>
        <span class="font-inter text-sm">active rules</span>
      </div>
      <div
        class="border-brutal-border shadow-brutal-sm flex items-center gap-2 border-[3px] px-4 py-2"
        style="color: var(--hister-coral);"
      >
        <Link2 class="size-3 md:size-4.5" />
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
    0%,
    100% {
      opacity: 1;
      transform: scale(1);
    }
    50% {
      opacity: 0.5;
      transform: scale(1.6);
    }
  }
  .search-box-gradient {
    background: linear-gradient(
      90deg,
      var(--hister-indigo),
      var(--hister-coral),
      var(--hister-teal),
      var(--hister-indigo)
    );
    background-size: 300% 100%;
    animation: gradient-slide 6s ease-in-out infinite alternate;
  }
  @keyframes gradient-slide {
    0% {
      background-position: 0% 50%;
    }
    100% {
      background-position: 100% 50%;
    }
  }
</style>
