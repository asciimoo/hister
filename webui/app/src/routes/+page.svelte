<script lang="ts">
  import { onMount, tick, untrack, type Component } from 'svelte';
  import {
    buildPreviewUrl,
    pushPreviewHistory,
    replacePreviewHistory,
    withSkipUrl,
    createResizeHandler,
  } from '$lib/preview';
  import { page } from '$app/stores';
  import { base } from '$app/paths';
  import { beforeNavigate } from '$app/navigation';
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
    isIMEKeyboardEvent,
  } from '$lib/search';
  import { fetchConfig, apiFetch, getUserId } from '$lib/api';
  import { ResultState } from '$lib/result-state.svelte';
  import { showHelp } from '$lib/stores';
  import type {
    SearchResults,
    SemanticHit,
    SearchResult,
    SearchQueryOptions,
    FacetsResult,
  } from '$lib/search';
  import { RESULTS_PER_PAGE } from '$lib/search';
  import {
    customDatesFromQuery,
    removeTimeFilters,
    replaceTimeFilters,
    shiftISODate,
    timeFilters,
  } from '$lib/time-filters';
  import { emptySearchCapabilities, queryFilterValues, valuesForFacet } from '$lib/search-schema';
  import type { SearchCapabilities, SearchFacetDefinition } from '$lib/search-schema';
  import {
    removeSortDirectives,
    replaceSortDirective,
    sortDirectiveFromQuery,
    sortValueFromQuery,
  } from '$lib/sort-directive';
  import {
    applyQuerySuggestion,
    buildQuerySuggestions,
    facetSuggestionContext,
  } from '$lib/query-suggestions';
  import type { QuerySuggestion } from '$lib/query-suggestions';
  import { animate } from 'animejs';
  import { Input } from '@hister/components/ui/input';
  import { Button } from '@hister/components/ui/button';
  import { Badge } from '@hister/components/ui/badge';
  import { Separator } from '@hister/components/ui/separator';
  import * as Dialog from '@hister/components/ui/dialog';
  import * as Card from '@hister/components/ui/card';
  import * as DropdownMenu from '@hister/components/ui/dropdown-menu';
  import * as Tooltip from '@hister/components/ui/tooltip';
  import { ScrollArea } from '@hister/components/ui/scroll-area';
  import {
    PreviewPanel,
    QuerySuggestions,
    ResultActionsMenu,
    ResultFavicon,
  } from '$lib/components';
  import { Kbd } from '@hister/components/ui/kbd';
  import {
    Search,
    Star,
    Globe,
    Eye,
    Trash2,
    Tag,
    Download,
    ExternalLink,
    History,
    FileText,
    Shield,
    Link2,
    Keyboard,
    HelpCircle,
    X,
    ChevronDown,
    Calendar,
    Filter,
    Sparkles,
    SlidersHorizontal,
    Copy,
    Check,
    ArrowUpDown,
    RefreshCw,
    WifiOff,
  } from '@lucide/svelte';
  import type { HistoryItem } from '$lib/types';

  interface Config {
    wsUrl: string;
    title: string;
    subtitle: string;
    searchUrl: string;
    openResultsOnNewTab: boolean;
    hotkeys: Record<string, string>;
    semanticEnabled: boolean;
    similarityThreshold: number;
    semanticWeight: number;
    authenticated: boolean;
    public: boolean;
    canWrite: boolean;
    historyEnabled: boolean;
    search: SearchCapabilities;
  }

  interface DisplayResult {
    id?: string;
    url: string;
    title: string;
    domain: string;
    score?: number;
    text?: string;
    favicon?: string;
    favicon_key?: string;
    added?: number;
    updated?: number;
    add_count?: number;
    label?: string;
    semanticScore?: number;
    finalScore?: number;
    sourceType?: 'keyword' | 'semantic' | 'both';
    isPinned: boolean;
    isHistoryResult: boolean;
  }

  let config: Config = $state({
    wsUrl: '',
    title: 'Hister',
    subtitle: 'Your own search engine',
    searchUrl: '',
    openResultsOnNewTab: false,
    hotkeys: {},
    semanticEnabled: false,
    similarityThreshold: 0.5,
    semanticWeight: 0.4,
    authenticated: true,
    public: false,
    canWrite: true,
    historyEnabled: true,
    search: emptySearchCapabilities(),
  });

  const sortOptions = $derived(
    config.search.sort.options
      .filter((option) => option.visible)
      .map((option) => ({ value: option.default ? '' : option.value, label: option.label })),
  );
  const sortValues = $derived(new Set(config.search.sort.options.map((option) => option.value)));

  let wsManager: WebSocketManager | undefined;
  let keyHandler: KeyHandler | undefined;
  let inputEl: HTMLInputElement | null = $state(null);

  let query = $state('');
  let isComposing = $state(false);
  let compositionEndPending = $state(false);
  let compositionEndFrame: number | undefined;
  let compositionStartedOnHome = $state(false);
  let autocomplete = $state('');
  let queryCursor = $state(0);
  let querySuggestionOpen = $state(false);
  let querySuggestionIndex = $state(0);
  let querySuggestionKeyboardActive = $state(false);
  let querySuggestionFacets = $state<FacetsResult | null>(null);
  let querySuggestionFacetKey = $state('');
  let querySuggestionFacetsLoading = $state(false);
  let searchAliases = $state<Record<string, string>>({});
  let querySuggestionFacetRequest = 0;
  type ConnectionState = 'connecting' | 'connected' | 'disconnected';
  let connectionState = $state<ConnectionState>('connecting');
  let connected = $state(false);
  let unloading = false;
  let lastResults = $state<SearchResults | null>(null);
  let accumulatedDocs = $state<SearchResult[]>([]);
  let pageKey = $state('');
  let hasMore = $state(false);
  let loadingMoreForQuery = $state('');
  let sentinelEl = $state<HTMLElement | undefined>();
  let highlightIdx = $state(0);
  const currentSort = $derived(sortValueFromQuery(query, config.search.sort));
  let dateFrom = $state('');
  let dateTo = $state('');
  let showPopup = $state(false);
  let popupUrl = $state('');
  let popupHintTitle = $state('');
  let previewFullscreen = $state(false);
  let disablePreviews = $state(false);

  // Desktop split-pane readability panel state
  let panelUrl = $state('');
  let panelDocumentId = $state('');
  let panelHintTitle = $state('');
  let panelViewingVersion = $state<number | null>(null);
  let isDesktop = $state(false);
  let panelOpen = $state(true);
  let panelWidthPct = $state(parseFloat(localStorage.getItem('hister-panel-width') ?? '') || 50);
  let splitContainerEl: HTMLDivElement | undefined = $state();
  const startPanelResize = createResizeHandler({
    getContainer: () => splitContainerEl,
    onWidth: (pct) => {
      panelWidthPct = pct;
    },
    onDone: (pct) => {
      localStorage.setItem('hister-panel-width', String(pct));
    },
  });

  let resultsShown = $state(false);

  // Semantic search per-session state — read from localStorage immediately so
  // the first $effect run doesn't overwrite the saved value with the default.
  let semanticOn = $state(localStorage.getItem('hister-semantic-on') === 'true');
  let similarityThreshold = $state(
    parseFloat(localStorage.getItem('hister-semantic-threshold') ?? 'NaN') || 0.5,
  );
  let semanticWeight = $state(
    parseFloat(localStorage.getItem('hister-semantic-weight') ?? 'NaN') || 0.4,
  );

  let showDeleteConfirm = $state(false);
  let deleteConfirmUrl = $state('');
  let deleteConfirmSkip = $state(false);

  let showDeleteAllConfirm = $state(false);
  let deleteError: string | null = $state(null);
  let deleteErrorTimer: any = null;
  let copiedUrl: string | null = $state(null);

  let recentSearches: string[] = $state([]);
  let rulesCount = $state(0);
  let aliasesCount = $state(0);
  let historyCount = $state(0);
  let fileCount = $state(0);
  let statsLoaded = $state(false);
  let statsAvailable = $state(false);

  let displayHistoryCount = $state(0);
  let displayFileCount = $state(0);
  let displayRulesCount = $state(0);
  let displayAliasesCount = $state(0);

  let heroTitleEl: HTMLElement | undefined = $state();
  let searchBoxEl: HTMLElement | undefined = $state();
  let statsRowEl: HTMLElement | undefined = $state();
  let kbdEl: HTMLElement | null = $state(null);
  let underlineEl: HTMLElement | undefined = $state();

  const querySuggestionListId = 'query-suggestions';
  const queryFacetContext = $derived(facetSuggestionContext(query, queryCursor, config.search));
  const querySuggestionFacetData = $derived(
    queryFacetContext?.key === querySuggestionFacetKey ? querySuggestionFacets : null,
  );
  const querySuggestions = $derived(
    buildQuerySuggestions({
      query,
      cursor: queryCursor,
      aliases: searchAliases,
      recentSearches,
      capabilities: config.search,
      facets: querySuggestionFacetData,
      serverSuggestion: autocomplete,
    }),
  );
  const querySuggestionsVisible = $derived(
    query.trim().length > 0 &&
      querySuggestionOpen &&
      (querySuggestions.length > 0 || querySuggestionFacetsLoading),
  );
  const activeQuerySuggestionId = $derived(
    querySuggestionsVisible && querySuggestionKeyboardActive && querySuggestions.length > 0
      ? `${querySuggestionListId}-option-${querySuggestionIndex}`
      : undefined,
  );
  const connectionStatus = $derived.by(() => {
    if (connectionState === 'connected') {
      return { label: 'Online', description: 'Server connected', dotClass: 'bg-hister-lime' };
    }
    if (connectionState === 'connecting') {
      return {
        label: 'Connecting',
        description: 'Connecting to server',
        dotClass: 'bg-hister-amber',
      };
    }
    return { label: 'Offline', description: 'Server disconnected', dotClass: 'bg-hister-rose' };
  });

  let animationHandles: any[] = [];

  type TipPart =
    | { type: 'text'; value: string }
    | { type: 'kbd'; value: string }
    | { type: 'code'; value: string }
    | { type: 'link'; value: string; href: string }
    | { type: 'hotkey'; action: string };

  const tips: TipPart[][] = [
    [
      { type: 'text', value: 'Press' },
      { type: 'hotkey', action: 'focus_search_input' },
      { type: 'text', value: 'to focus search anywhere' },
    ],
    [
      { type: 'text', value: 'Use' },
      { type: 'code', value: '"quotes"' },
      { type: 'text', value: 'to search for an exact phrase' },
    ],
    [
      { type: 'text', value: 'Prefix a term with' },
      { type: 'code', value: '-' },
      { type: 'text', value: 'to exclude it from results' },
    ],
    [
      { type: 'text', value: 'Use' },
      { type: 'code', value: 'domain:example.com' },
      { type: 'text', value: 'to search within a specific domain' },
    ],
    [
      { type: 'text', value: 'Use' },
      { type: 'code', value: 'title:keyword' },
      { type: 'text', value: 'to search only in page titles' },
    ],
    [
      { type: 'text', value: 'Use' },
      { type: 'code', value: 'url:*pattern*' },
      { type: 'text', value: 'to match URL patterns with wildcards' },
    ],
    [
      { type: 'text', value: 'Use' },
      { type: 'code', value: '(a|b|c)' },
      { type: 'text', value: 'to match any of the listed terms' },
    ],
    [
      { type: 'text', value: 'Use' },
      { type: 'code', value: 'type:web' },
      { type: 'text', value: 'or' },
      { type: 'code', value: 'type:file' },
      { type: 'text', value: 'to filter by document type' },
    ],
    [
      { type: 'text', value: 'Use' },
      { type: 'code', value: 'language:en' },
      { type: 'text', value: 'to filter results by language' },
    ],
    [
      { type: 'text', value: 'Press' },
      { type: 'hotkey', action: 'open_result' },
      { type: 'text', value: 'to open the first result directly' },
    ],
    [
      { type: 'text', value: 'Press' },
      { type: 'hotkey', action: 'select_next_result' },
      { type: 'text', value: '/' },
      { type: 'hotkey', action: 'select_previous_result' },
      { type: 'text', value: 'to navigate between results' },
    ],
    [
      { type: 'text', value: 'Press' },
      { type: 'hotkey', action: 'open_result_in_new_tab' },
      { type: 'text', value: 'to open a result in a new tab' },
    ],
    [
      { type: 'text', value: 'Press' },
      { type: 'hotkey', action: 'open_query_in_search_engine' },
      { type: 'text', value: 'to open the query in your configured search engine' },
    ],
    [
      { type: 'text', value: 'Define aliases in the' },
      { type: 'link', value: 'Rules page', href: base + '/rules' },
      { type: 'text', value: 'to shorten common queries' },
    ],
    [
      { type: 'text', value: 'Use' },
      { type: 'code', value: 'word*' },
      { type: 'text', value: 'for wildcard searches' },
    ],
  ];

  function visibleTips(): TipPart[][] {
    if (config.canWrite) return tips;
    return tips.filter(
      (tip) => !tip.some((part) => part.type === 'link' && part.href.endsWith('/rules')),
    );
  }

  let currentTip = $state(tips[Math.floor(Math.random() * tips.length)]);

  const hotkeyByAction = $derived(
    Object.fromEntries(Object.entries(config.hotkeys).map(([key, action]) => [action, key])),
  );

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
    delete_result: deleteSelectedResult,
  };

  const isSearching = $derived(
    (query.length > 0 && (!isComposing || !compositionStartedOnHome)) || resultsShown,
  );

  let tipWasSearching = false;
  $effect(() => {
    if (tipWasSearching && !isSearching) {
      const choices = visibleTips();
      currentTip = choices[Math.floor(Math.random() * choices.length)];
    }
    tipWasSearching = isSearching;
  });

  interface MergedResult {
    id?: string;
    url: string;
    title: string;
    domain: string;
    score?: number;
    text?: string;
    favicon?: string;
    favicon_key?: string;
    added?: number;
    updated?: number;
    add_count?: number;
    label?: string;
    semanticScore?: number;
    finalScore: number;
    sourceType: 'keyword' | 'semantic' | 'both';
  }

  function mergeResults(
    docs: SearchResults['documents'],
    hits: SemanticHit[] | undefined,
    alpha: number,
  ): MergedResult[] {
    const kwDocs = docs ?? [];
    if (!semanticOn || !config.semanticEnabled || !hits?.length) {
      return kwDocs.map((d) => ({ ...d, finalScore: d.score ?? 0, sourceType: 'keyword' }));
    }

    const maxBleve = Math.max(...kwDocs.map((d) => d.score ?? 0), 1);
    const semByDocId = new Map<string, number>(hits.map((h) => [h.doc_id, h.similarity]));

    // Helper: the doc_id is either a bare URL or "{uid}:{url}".
    function urlFromDocId(docId: string): string {
      const userId = getUserId();
      if (userId) {
        const prefix = `${userId}:`;
        if (docId.startsWith(prefix)) return docId.slice(prefix.length);
      }
      return docId;
    }

    const merged = new Map<string, MergedResult>();

    for (const d of kwDocs) {
      // Find whether this keyword doc also appears in semantic hits.
      // The semantic doc_id for this user+URL:
      const userId = getUserId();
      const expectedDocId = userId ? `${userId}:${d.url}` : d.url;
      const semScore = semByDocId.get(expectedDocId) ?? semByDocId.get(d.url);
      const norm = (d.score ?? 0) / maxBleve;
      const finalScore =
        semScore !== undefined ? (1 - alpha) * norm + alpha * semScore : (1 - alpha) * norm;
      merged.set(d.url, {
        ...d,
        semanticScore: semScore,
        finalScore,
        sourceType: semScore !== undefined ? 'both' : 'keyword',
      });
    }

    // Add semantic-only hits (server sets `document` only for non-keyword hits).
    for (const hit of hits) {
      if (!hit.document) continue;
      const url = hit.document.url;
      if (!merged.has(url)) {
        merged.set(url, {
          url,
          title: hit.document.title ?? '',
          domain: hit.document.domain ?? '',
          favicon: hit.document.favicon,
          favicon_key: hit.document.favicon_key,
          added: hit.document.added,
          updated: hit.document.updated,
          add_count: hit.document.add_count,
          text: hit.document.text,
          semanticScore: hit.similarity,
          finalScore: alpha * hit.similarity,
          sourceType: 'semantic',
        });
      }
    }

    return Array.from(merged.values()).sort((a, b) => b.finalScore - a.finalScore);
  }

  const mergedResults = $derived(
    mergeResults(accumulatedDocs, lastResults?.semantic_hits, semanticWeight),
  );

  const historyLen = $derived((lastResults?.history as any)?.length || 0);
  const docsLen = $derived(mergedResults.length);
  const totalResults = $derived(historyLen + docsLen);
  const hasResults = $derived(totalResults > 0);
  const displayResults = $derived<DisplayResult[]>([
    ...(lastResults?.history ?? []).map((r): DisplayResult => ({
      ...r,
      isPinned: r.pinned ?? false,
      isHistoryResult: true,
    })),
    ...mergedResults.map((r): DisplayResult => ({ ...r, isPinned: false, isHistoryResult: false })),
  ]);

  // Faceted navigation — lazy fetch on dropdown open
  const termFacetDefinitions = $derived(
    config.search.facets.filter((facet) => facet.kind !== 'date_ranges'),
  );
  const dateFacetDefinition = $derived(
    config.search.facets.find((facet) => facet.kind === 'date_ranges'),
  );
  const dateFacetValues = $derived(
    dateFacetDefinition ? valuesForFacet(config.search, dateFacetDefinition) : [],
  );
  const facetIcons: Record<string, typeof Globe> = {
    calendar: Calendar,
    eye: Eye,
    globe: Globe,
    tag: Tag,
  };
  // facetsCache maps a canonical query key to the fetched FacetsResult.
  let facetsCache = $state(new Map<string, FacetsResult>());
  let facetsLoading = $state(false);
  let filtersDropdownOpen = $state(false);
  let actionsDropdownOpen = $state(false);
  // Auto-open the Actions dropdown when a search returns no results so the
  // similarity threshold slider (and other actions) are reachable to recover
  // or refine the query. We latch the user's manual close so we don't fight
  // them, and reset the latch on each fresh search (see renderResults).
  let userClosedActions = $state(false);
  function handleActionsOpenChange(open: boolean) {
    if (!open) {
      userClosedActions = true;
    }
  }
  let sortDropdownOpen = $state(false);
  // Maps facet name (e.g. "domains", "languages") to the requested top-N size.
  let facetSizes = $state(new Map<string, number>());

  function facetSize(name: string): number {
    return (
      facetSizes.get(name) ??
      config.search.facets.find((definition) => definition.name === name)?.defaultSize ??
      0
    );
  }

  function facetsCacheKey(): string {
    const sizes = [...facetSizes.entries()].sort(([a], [b]) => a.localeCompare(b));
    return `${query}|${JSON.stringify(sizes)}`;
  }

  const currentFacets = $derived(facetsCache.get(facetsCacheKey()));

  async function fetchFacets() {
    if (!query || facetsLoading) return;
    const key = facetsCacheKey();
    if (facetsCache.has(key)) return;
    facetsLoading = true;
    try {
      const params = new URLSearchParams({ q: query });
      for (const [name, size] of facetSizes) {
        const defaultSize =
          config.search.facets.find((definition) => definition.name === name)?.defaultSize ?? 0;
        if (size !== defaultSize) params.set(`size_${name}`, String(size));
      }
      const res = await fetch(`api/facets?${params}`);
      if (res.ok) {
        const data: FacetsResult = await res.json();
        facetsCache = new Map(facetsCache).set(key, data);
      }
    } finally {
      facetsLoading = false;
    }
  }

  function loadMoreFacet(name: string) {
    const increment =
      config.search.facets.find((definition) => definition.name === name)?.defaultSize ?? 0;
    if (increment === 0) return;
    facetSizes = new Map(facetSizes).set(name, facetSize(name) + increment);
    fetchFacets();
  }

  $effect(() => {
    if (filtersDropdownOpen) fetchFacets();
  });

  // Invalidate cache and reset sizes when the canonical query changes.
  $effect(() => {
    const _q = query;
    facetsCache = new Map();
    facetSizes = new Map();
  });

  const activeFacetFilters = $derived(
    new Map(
      termFacetDefinitions.map((facet) => [facet.name, queryFilterValues(query, facet.queryField)]),
    ),
  );
  const activeTimeFilters = $derived(timeFilters(query, dateFacetDefinition?.queryField ?? ''));
  const activeDateBucket = $derived(
    activeTimeFilters.length === 1
      ? (dateFacetValues.find(
          (value) =>
            value.value ===
            `${activeTimeFilters[0].comparison}${activeTimeFilters[0].value.toLowerCase()}`,
        )?.facetBucket ?? null)
      : null,
  );
  const activeFilterCount = $derived(
    [...activeFacetFilters.values()].reduce((total, filters) => total + filters.size, 0) +
      (activeTimeFilters.length > 0 ? 1 : 0),
  );

  function showFacetCategory(facet: SearchFacetDefinition) {
    return (
      (currentFacets?.terms?.[facet.name]?.terms?.length ?? 0) > 1 ||
      (activeFacetFilters.get(facet.name)?.size ?? 0) > 0
    );
  }
  function activeFiltersForFacet(name: string): Set<string> {
    return activeFacetFilters.get(name) ?? new Set<string>();
  }
  const visibleTermFacets = $derived(termFacetDefinitions.filter(showFacetCategory));
  const showFiltersButton = $derived(hasResults || activeFilterCount > 0);

  function toggleQueryToken(prefix: string, value: string) {
    const token = `${prefix}:${value}`;
    if (query.includes(token)) {
      query = query.replace(token, '').replace(/\s+/g, ' ').trim();
    } else {
      query = query.trim() ? `${query.trim()} ${token}` : token;
    }
  }

  function toggleDateBucket(name: string) {
    const field = dateFacetDefinition?.queryField ?? '';
    if (activeDateBucket === name) {
      query = removeTimeFilters(query, field);
    } else {
      const filter = dateFacetValues.find((value) => value.facetBucket === name)?.value;
      if (filter) query = replaceTimeFilters(query, field, [filter]);
    }
  }

  function updateCustomDateFilter(field: 'from' | 'to', value: string) {
    const from = field === 'from' ? value : dateFrom;
    const to = field === 'to' ? value : dateTo;
    dateFrom = from;
    dateTo = to;
    const filters: string[] = [];
    if (shiftISODate(from, 0)) filters.push(`>=${from}`);
    const exclusiveTo = shiftISODate(to, 1);
    if (exclusiveTo) filters.push(`<${exclusiveTo}`);
    query = replaceTimeFilters(query, dateFacetDefinition?.queryField ?? '', filters);
  }

  $effect(() => {
    const dates = customDatesFromQuery(query, dateFacetDefinition?.queryField ?? '');
    untrack(() => {
      dateFrom = dates.from;
      dateTo = dates.to;
    });
  });

  function connect() {
    connectionState = 'connecting';
    wsManager = new WebSocketManager(config.wsUrl, {
      onOpen: () => {
        connected = true;
        connectionState = 'connected';
        if (query) sendQuery(query);
      },
      onMessage: renderResults,
      onClose: () => {
        if (unloading) return;
        connected = false;
        connectionState = 'disconnected';
      },
      onError: () => {
        if (unloading) return;
        connected = false;
        connectionState = 'disconnected';
      },
    });
    wsManager.connect();
  }

  beforeNavigate(({ willUnload, complete }) => {
    if (!willUnload) return;
    unloading = true;
    complete.catch(() => {
      unloading = false;
    });
  });

  function retryConnection() {
    connected = false;
    connectionState = 'connecting';
    wsManager?.reconnect();
  }

  function searchQueryOpts(pageKey = ''): SearchQueryOptions {
    return {
      semantic: { enabled: semanticOn && config.semanticEnabled, threshold: similarityThreshold },
      pageKey,
      limit: RESULTS_PER_PAGE,
    };
  }

  function sendQuery(q: string) {
    loadingMoreForQuery = '';
    pageKey = '';
    hasMore = false;
    wsManager?.send(JSON.stringify(buildSearchQuery(q, searchQueryOpts())));
  }

  function loadMoreResults() {
    if (!pageKey || !hasMore || loadingMoreForQuery) return;
    loadingMoreForQuery = query;
    wsManager?.sendImmediate(JSON.stringify(buildSearchQuery(query, searchQueryOpts(pageKey))));
  }

  const skipUrl = { value: false };
  let lastPushedEmpty = true;

  // --- URL builders ---

  function queryFromSearchParams(params: URLSearchParams): string {
    let value = params.get('q') || '';
    const legacySort = params.get('sort');
    if (
      legacySort &&
      sortValues.has(legacySort) &&
      sortDirectiveFromQuery(value, config.search.sort) === null
    ) {
      value = replaceSortDirective(value, legacySort, config.search.sort);
    }
    return value;
  }

  function buildSearchUrl(): string {
    const params = new URLSearchParams();
    if (query) params.set('q', query);
    const search = params.toString();
    return `${base}/${search ? `?${search}` : ''}`;
  }

  // --- History state helpers ---

  function pushSearchHistory() {
    const url = buildSearchUrl();
    history.pushState({ type: 'search', query }, '', url);
    lastPushedEmpty = !query;
  }

  function replaceSearchHistory() {
    const url = buildSearchUrl();
    history.replaceState({ type: 'search', query }, '', url);
    lastPushedEmpty = !query;
  }

  function updateURL() {
    if (skipUrl.value) return;
    if (previewFullscreen) return;
    const isEmpty = !query;
    if (isEmpty !== lastPushedEmpty) {
      pushSearchHistory();
    } else {
      replaceSearchHistory();
    }
  }

  function handlePopState(event: PopStateEvent) {
    const state = event.state as {
      type?: string;
      id?: string;
      documentId?: string;
      title?: string;
      versionId?: number | null;
    } | null;
    if (state?.type === 'preview') {
      panelUrl = state.id || '';
      panelDocumentId = state.documentId || '';
      panelHintTitle = state.title || '';
      panelOpen = true;
      previewFullscreen = true;
      panelViewingVersion = state.versionId ?? null;
      return;
    }
    previewFullscreen = false;
    skipUrl.value = true;
    const params = new URLSearchParams(window.location.search);
    query = queryFromSearchParams(params);
    lastPushedEmpty = !query;
    if (query && connected) sendQuery(query);
    if (!query) {
      autocomplete = '';
      lastResults = null;
    }
    tick().then(() => {
      skipUrl.value = false;
    });
  }

  function renderResults(event: MessageEvent) {
    const res = parseSearchResults(event.data);
    const isLoadMore = loadingMoreForQuery !== '' && loadingMoreForQuery === query;
    loadingMoreForQuery = '';
    if (isLoadMore) {
      accumulatedDocs = [...accumulatedDocs, ...(res.documents ?? [])];
      lastResults = { ...lastResults!, ...res, documents: accumulatedDocs };
    } else {
      accumulatedDocs = res.documents ?? [];
      lastResults = res;
      autocomplete = (query && res.query_suggestion) || '';
      highlightIdx = 0;
      if (!isComposing) resultsShown = true;
      userClosedActions = false; // reset latch on each fresh search
      // Guard against out-of-order WebSocket responses: only auto-open if this
      // response is for the query that's currently in the input. Stale responses
      // (for a query the user has already moved past) must not open Actions.
      // SearchResults.query is echoed back as { text }, so compare against
      // query?.text (a fresh-search response always sets lastResults to itself).
      if (query && !hasResults && !userClosedActions && lastResults?.query?.text === query) {
        actionsDropdownOpen = true;
      }
    }
    hasMore = !!res.page_key;
    pageKey = res.page_key ?? '';
  }

  function stripHtml(s: string): string {
    return s.replace(/<[^>]*>/g, '');
  }

  function openResult(url: string, title: string, newWindow = false, sourceUrl = url) {
    if (config.openResultsOnNewTab) newWindow = true;
    saveHistoryItem(sourceUrl, stripHtml(title), query, false, () => openURL(url, newWindow));
  }

  function sendHistoryBeacon(url: string, title: string, queryStr: string) {
    if (!config.historyEnabled) return;
    const payload = JSON.stringify({
      url,
      title: stripHtml(title),
      query: queryStr,
      delete: false,
    });
    navigator.sendBeacon('api/history', new Blob([payload], { type: 'application/json' }));
  }

  async function saveHistoryItem(
    url: string,
    title: string,
    queryStr: string,
    remove: boolean,
    callback?: () => void,
  ) {
    if (!config.historyEnabled) {
      callback?.();
      return;
    }
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
    query = replaceSortDirective(query, sortId, config.search.sort);
  }

  function setDeleteError(msg: string) {
    deleteError = msg;
    if (deleteErrorTimer) clearTimeout(deleteErrorTimer);
    deleteErrorTimer = setTimeout(() => {
      deleteError = null;
    }, 6000);
  }

  async function deleteResult(url: string) {
    if (!config.canWrite) return;
    const res = await apiFetch('/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        query: 'url:"' + url.replaceAll('"', '\\"') + '"',
      }),
    });
    if (!res.ok) {
      const text = await res.text();
      setDeleteError(text || 'Delete failed.');
      return;
    }
    const data = await res.json();
    if (!data.deleted) {
      setDeleteError('Document not found in index. Run "hister reindex" to fix stale entries.');
      return;
    }
    removeResult(url);
  }

  function deleteSelectedResult(e?: KeyboardEvent) {
    if (e) e.preventDefault();
    if (!config.canWrite) return;
    const links = document.querySelectorAll<HTMLAnchorElement>('[data-result] [data-result-link]');
    const link = links[highlightIdx];
    if (!link) return;
    const url = link.getAttribute('data-result-link') ?? link.getAttribute('href');
    if (!url) return;
    if (localStorage.getItem('hister-skip-delete-confirm') === 'true') {
      deleteResult(url);
      return;
    }
    deleteConfirmUrl = url;
    deleteConfirmSkip = false;
    showDeleteConfirm = true;
  }

  function confirmDelete() {
    if (deleteConfirmSkip) {
      localStorage.setItem('hister-skip-delete-confirm', 'true');
    }
    showDeleteConfirm = false;
    deleteResult(deleteConfirmUrl);
    deleteConfirmUrl = '';
  }

  function cancelDelete() {
    showDeleteConfirm = false;
    deleteConfirmUrl = '';
  }

  async function deleteAllResults() {
    if (!config.canWrite) return;
    const searchText = removeSortDirectives(query, config.search.sort) || '*';
    const q = searchText + (getUserId() !== undefined ? ' user_id:' + getUserId() : '');
    const res = await apiFetch('/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query: q }),
    });
    if (!res.ok) {
      const text = await res.text();
      setDeleteError(text || 'Delete all failed.');
      return;
    }
    const data = await res.json();
    if (!data.deleted) {
      setDeleteError(
        'No matching documents found in index. Run "hister reindex" if results appear stale.',
      );
      return;
    }
    accumulatedDocs = [];
    if (lastResults) {
      lastResults = { ...lastResults, documents: [], total: 0 };
    }
    resultsShown = false;
  }

  function confirmDeleteAll() {
    showDeleteAllConfirm = false;
    deleteAllResults();
  }

  function cancelDeleteAll() {
    showDeleteAllConfirm = false;
  }

  const resultStates = new Map<string, ResultState>();

  function getResultState(url: string, initialLabel?: string): ResultState {
    if (!resultStates.has(url)) {
      resultStates.set(url, new ResultState(initialLabel));
    }
    return resultStates.get(url)!;
  }

  function removeResult(url: string) {
    accumulatedDocs = accumulatedDocs.filter((d) => d.url !== url);
    if (lastResults) lastResults = { ...lastResults, documents: accumulatedDocs };
  }

  function removeResultsByDomain(domain: string) {
    accumulatedDocs = accumulatedDocs.filter((d) => d.domain !== domain);
    if (lastResults) lastResults = { ...lastResults, documents: accumulatedDocs };
  }

  // Convert file document URLs to browser viewable Hister URLs.
  function fileResultUrl(url: string, documentID?: string, title = ''): string {
    if (url.startsWith('remote-file://')) return buildPreviewUrl(url, title, undefined, documentID);
    if (url.startsWith('file://') && documentID)
      return 'api/file?id=' + encodeURIComponent(documentID);
    return url;
  }

  function displayResultPath(url: string, domain: string): string {
    const withoutProtocol = url.replace(/^[a-z][a-z0-9+.-]*:\/\//i, '');
    const withoutDomain =
      domain && withoutProtocol.startsWith(domain)
        ? withoutProtocol.slice(domain.length)
        : withoutProtocol;
    return withoutDomain || '/';
  }

  function openReadable(e: Event, url: string, title: string, documentId = '') {
    e.preventDefault();
    if (e.stopPropagation) e.stopPropagation();
    if (isDesktop) {
      if (!panelOpen) {
        panelOpen = true;
        localStorage.setItem('hister-panel-open', 'true');
      }
      panelViewingVersion = null;
      panelDocumentId = documentId;
      panelHintTitle = title;
      panelUrl = url;
      return;
    }
    panelViewingVersion = null;
    panelDocumentId = documentId;
    panelUrl = url;
    panelHintTitle = title;
    previewFullscreen = true;
    withSkipUrl(skipUrl, () => pushPreviewHistory(url, title, null, documentId));
  }

  function enterFullscreen() {
    previewFullscreen = true;
    withSkipUrl(skipUrl, () =>
      pushPreviewHistory(panelUrl, panelHintTitle, panelViewingVersion, panelDocumentId),
    );
  }

  function exitFullscreen() {
    previewFullscreen = false;
    withSkipUrl(skipUrl, () => pushSearchHistory());
  }

  function closePanelAndFullscreen() {
    previewFullscreen = false;
    panelOpen = false;
    localStorage.setItem('hister-panel-open', 'false');
    withSkipUrl(skipUrl, () => pushSearchHistory());
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
      const href = res.getAttribute('href')!;
      openResult(href, res.innerText, newWindow, res.dataset.resultLink || href);
    }
  }

  function viewResultPopup(e?: KeyboardEvent) {
    if (e) e.preventDefault();
    if (isDesktop) {
      if (previewFullscreen) {
        // Fullscreen → back to split-screen
        exitFullscreen();
      } else if (panelOpen) {
        // Split-screen → fullscreen
        enterFullscreen();
      } else {
        // No preview → open split-screen
        openHighlightedReadable();
      }
    } else {
      // Mobile: toggle fullscreen
      if (previewFullscreen) {
        closePanelAndFullscreen();
      } else {
        openHighlightedReadable();
      }
    }
  }

  function openHighlightedReadable() {
    const readables = document.querySelectorAll('[data-result] [data-readable]');
    if (highlightIdx >= 0 && highlightIdx < readables.length) {
      const el = readables[highlightIdx] as HTMLElement;
      const result = el.closest('[data-result]')!;
      const link = result.querySelector<HTMLAnchorElement>('[data-result-link]')!;
      openReadable(
        { preventDefault: () => {}, stopPropagation: () => {} } as unknown as Event,
        link.getAttribute('data-result-link')!,
        link.innerText,
        el.getAttribute('data-document-id') || '',
      );
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

  function handleQueryInput(event: Event) {
    const target = event.currentTarget as HTMLInputElement;
    query = target.value;
    queryCursor = target.selectionStart ?? target.value.length;
    if (compositionEndPending && !(event as InputEvent).isComposing) {
      if (compositionEndFrame !== undefined) cancelAnimationFrame(compositionEndFrame);
      isComposing = false;
      compositionEndPending = false;
      compositionEndFrame = undefined;
    }
    querySuggestionIndex = 0;
    querySuggestionKeyboardActive = false;
    querySuggestionOpen = true;
  }

  function handleCompositionStart() {
    if (compositionEndFrame !== undefined) cancelAnimationFrame(compositionEndFrame);
    compositionEndFrame = undefined;

    compositionStartedOnHome = !isSearching;
    isComposing = true;
    compositionEndPending = false;
  }

  function handleCompositionEnd(event: CompositionEvent) {
    compositionEndPending = true;
    const input = event.currentTarget as HTMLInputElement;
    compositionEndFrame = requestAnimationFrame(() => {
      compositionEndFrame = undefined;
      if (!compositionEndPending) return;
      query = input.value;
      queryCursor = input.selectionStart ?? input.value.length;
      isComposing = false;
      compositionEndPending = false;
    });
  }

  function handleQuerySelection(event: Event) {
    const target = event.currentTarget as HTMLInputElement;
    queryCursor = target.selectionStart ?? target.value.length;
    querySuggestionIndex = 0;
    querySuggestionKeyboardActive = false;
    querySuggestionOpen = true;
  }

  function handleQueryFocus(event: FocusEvent) {
    handleQuerySelection(event);
  }

  function selectQuerySuggestion(suggestion: QuerySuggestion) {
    const applied = applyQuerySuggestion(query, queryCursor, suggestion);
    query = applied.query;
    queryCursor = applied.cursor;
    querySuggestionIndex = 0;
    querySuggestionKeyboardActive = false;
    querySuggestionOpen = suggestion.keepOpen ?? false;
    tick().then(() => {
      inputEl?.focus();
      inputEl?.setSelectionRange(applied.cursor, applied.cursor);
      if (suggestion.keepOpen) querySuggestionOpen = true;
    });
  }

  function setActiveQuerySuggestion(index: number) {
    querySuggestionIndex = index;
    querySuggestionKeyboardActive = true;
  }

  function handleQueryInputKeydown(event: KeyboardEvent) {
    if (isIMEKeyboardEvent(event)) return;
    if (!querySuggestionsVisible || querySuggestions.length === 0) return;

    if (event.key === 'Tab' && event.shiftKey) {
      event.stopPropagation();
      return;
    }

    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      event.stopPropagation();
      if (!querySuggestionKeyboardActive) {
        querySuggestionIndex = event.key === 'ArrowDown' ? 0 : querySuggestions.length - 1;
      } else {
        const direction = event.key === 'ArrowDown' ? 1 : -1;
        querySuggestionIndex =
          (querySuggestionIndex + direction + querySuggestions.length) % querySuggestions.length;
      }
      querySuggestionKeyboardActive = true;
      return;
    }

    if (event.key === 'Tab' || (event.key === 'Enter' && querySuggestionKeyboardActive)) {
      event.preventDefault();
      event.stopPropagation();
      selectQuerySuggestion(querySuggestions[querySuggestionIndex] ?? querySuggestions[0]);
      return;
    }

    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      querySuggestionOpen = false;
      querySuggestionKeyboardActive = false;
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
    if (previewFullscreen) {
      closePanelAndFullscreen();
      return true;
    }
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
    delete_result: 'Delete focused result',
  };

  function showHotkeys(e?: KeyboardEvent, isInputFocus?: boolean) {
    if ($showHelp) {
      $showHelp = false;
      return;
    }
    if (!isInputFocus) {
      $showHelp = true;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (isIMEKeyboardEvent(e)) return;
    if (showDeleteConfirm) {
      if (e.key === 'Enter') {
        e.preventDefault();
        confirmDelete();
      } else if (e.key === 'Escape') {
        e.preventDefault();
        cancelDelete();
      }
      return;
    }
    const isInputFocus =
      document.activeElement instanceof HTMLInputElement ||
      document.activeElement instanceof HTMLTextAreaElement;
    keyHandler?.handle(e, isInputFocus);
    if (e.key === 'Escape') {
      if ($showHelp) {
        $showHelp = false;
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

  async function loadQueryAliases() {
    if (!config.canWrite) return;
    try {
      const res = await apiFetch('/rules', {
        headers: { Accept: 'application/json' },
        redirectOnForbidden: false,
      });
      if (!res.ok) return;
      const data = await res.json();
      searchAliases = data.aliases ?? {};
    } catch {}
  }

  async function loadHomeStats() {
    statsAvailable = false;
    try {
      const statsRes = await apiFetch('/stats', { headers: { Accept: 'application/json' } });

      if (statsRes.ok) {
        const stats = await statsRes.json();
        rulesCount = stats.rule_count ?? 0;
        aliasesCount = stats.alias_count ?? 0;
        historyCount = stats.doc_count ?? 0;
        fileCount = stats.file_count ?? 0;
        statsAvailable = true;
        recentSearches = [];
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
      console.log('Failed to retrieve stats', e);
    }
    statsLoaded = true;
  }

  function addAnimation(target: HTMLElement | null | undefined, options: any) {
    if (!target) {
      return;
    }
    animationHandles.push(animate(target, options));
  }

  function startHeroAnimations() {
    cleanupAnimations();

    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      underlineEl?.style.setProperty('transform', 'scaleX(1)');
      return;
    }

    addAnimation(heroTitleEl, {
      backgroundPosition: ['0% 50%', '100% 50%'],
      ease: 'inOutSine',
      duration: 6000,
      loop: true,
      alternate: true,
    });

    addAnimation(kbdEl, {
      translateY: [0, 3, 0],
      duration: 400,
      ease: 'inOutSine',
      loop: true,
      loopDelay: 10000,
    });

    addAnimation(underlineEl, {
      scaleX: [0, 1],
      duration: 800,
      ease: 'outCubic',
      delay: 300,
    });

    addAnimation(searchBoxEl, {
      translateY: [8, 0],
      opacity: [0, 1],
      duration: 420,
      ease: 'outCubic',
    });
  }

  function animateCounters() {
    const counterObj = {
      h: displayHistoryCount,
      f: displayFileCount,
      r: displayRulesCount,
      a: displayAliasesCount,
    };
    animationHandles.push(
      animate(counterObj, {
        h: historyCount,
        f: fileCount,
        r: rulesCount,
        a: aliasesCount,
        duration: 800,
        ease: 'outCubic',
        onRender: () => {
          displayHistoryCount = Math.round(counterObj.h);
          displayFileCount = Math.round(counterObj.f);
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
    if (statsLoaded && statsAvailable && !isSearching) {
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
    const count = querySuggestions.length;
    if (count === 0) {
      querySuggestionIndex = 0;
      querySuggestionKeyboardActive = false;
    } else if (querySuggestionIndex >= count) {
      querySuggestionIndex = count - 1;
    }
  });
  $effect(() => {
    const context = queryFacetContext;
    const isOpen = querySuggestionOpen;
    const request = ++querySuggestionFacetRequest;
    if (!context || !isOpen) {
      querySuggestionFacetsLoading = false;
      return;
    }

    querySuggestionFacetsLoading = true;
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      try {
        const params = new URLSearchParams({ q: context.baseQuery });
        params.set(`size_${context.facetName}`, '20');
        const res = await apiFetch(`/facets?${params}`, { signal: controller.signal });
        if (!res.ok || request !== querySuggestionFacetRequest) return;
        querySuggestionFacets = await res.json();
        querySuggestionFacetKey = context.key;
      } catch (error) {
        if (!(error instanceof DOMException && error.name === 'AbortError')) {
          querySuggestionFacets = null;
        }
      } finally {
        if (request === querySuggestionFacetRequest) querySuggestionFacetsLoading = false;
      }
    }, 120);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
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
      accumulatedDocs = [];
      pageKey = '';
      hasMore = false;
      loadingMoreForQuery = '';
    }
  });
  // Persist and react to semantic setting changes.
  $effect(() => {
    localStorage.setItem('hister-semantic-on', String(semanticOn));
    if (query && connected) sendQuery(query);
  });
  $effect(() => {
    localStorage.setItem('hister-semantic-threshold', String(similarityThreshold));
    if (query && connected) sendQuery(query);
  });
  $effect(() => {
    localStorage.setItem('hister-semantic-weight', String(semanticWeight));
  });

  // Auto-load the readability panel for the focused result on desktop.
  // Tracks mergedResults (not just lastResults) so that reordering caused by
  // the semantic weight slider also refreshes the panel.
  // Uses data instead of DOM queries so it works when results are hidden (fullscreen mode).
  $effect(() => {
    const idx = highlightIdx;
    const result = displayResults[idx]; // reactive: covers both pinned and regular results
    const isFullscreen = previewFullscreen;
    if (!isDesktop || (!panelOpen && !isFullscreen)) return;
    if (!result) {
      panelUrl = '';
      panelDocumentId = '';
      panelHintTitle = '';
      return;
    }
    const url = result.url;
    const documentId = result.id || '';
    if (url === untrack(() => panelUrl) && documentId === untrack(() => panelDocumentId)) return;
    panelViewingVersion = null;
    panelDocumentId = documentId;
    panelHintTitle = result.title || '';
    panelUrl = url;
    if (isFullscreen) {
      withSkipUrl(skipUrl, () => replacePreviewHistory(url, result.title || '', null, documentId));
    }
  });
  $effect(() => {
    updateURL();
  });
  $effect.pre(() => {
    const urlParams = new URLSearchParams(window.location.search);
    const q = queryFromSearchParams(urlParams);
    if (q) query = q;
    lastPushedEmpty = !q;
  });

  // IntersectionObserver: load more results when the sentinel comes into view.
  $effect(() => {
    const el = sentinelEl;
    if (!el) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasMore && !loadingMoreForQuery) {
          loadMoreResults();
        }
      },
      { rootMargin: '200px' },
    );
    observer.observe(el);
    return () => observer.disconnect();
  });

  onMount(() => {
    (async () => {
      const appConfig = await fetchConfig();
      const wsProto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const wsUrl = new URL(appConfig.wsUrl);
      config = {
        wsUrl: `${wsProto}//${location.host}${wsUrl.pathname}`,
        title: appConfig.title ?? 'Hister',
        subtitle: appConfig.subtitle ?? 'Your own search engine',
        searchUrl: appConfig.searchUrl,
        openResultsOnNewTab: appConfig.openResultsOnNewTab,
        hotkeys: appConfig.hotkeys,
        semanticEnabled: appConfig.semanticEnabled ?? false,
        similarityThreshold: appConfig.similarityThreshold ?? 0.1,
        semanticWeight: appConfig.semanticWeight ?? 0.4,
        authenticated: appConfig.authenticated,
        public: appConfig.public,
        canWrite: appConfig.canWrite,
        historyEnabled: appConfig.historyEnabled,
        search: appConfig.search,
      };
      disablePreviews = (appConfig as any).disablePreviews ?? false;
      if (config.semanticEnabled) {
        // Apply server defaults only when the user has not yet customised these.
        if (localStorage.getItem('hister-semantic-threshold') === null)
          similarityThreshold = config.similarityThreshold;
        if (localStorage.getItem('hister-semantic-weight') === null)
          semanticWeight = config.semanticWeight;
      }
      if (
        !config.canWrite &&
        currentTip.some((part) => part.type === 'link' && part.href.endsWith('/rules'))
      ) {
        const choices = visibleTips();
        currentTip = choices[Math.floor(Math.random() * choices.length)];
      }
      inputEl?.focus();
      connect();
      keyHandler = new KeyHandler(config.hotkeys, hotkeyActions);
      loadHomeStats();
      loadQueryAliases();
    })();
    const mq = window.matchMedia('(min-width: 1280px)');
    isDesktop = mq.matches;
    const stored = localStorage.getItem('hister-panel-open');
    if (stored !== null) panelOpen = stored !== 'false';
    const mqHandler = (e: MediaQueryListEvent) => {
      isDesktop = e.matches;
    };
    mq.addEventListener('change', mqHandler);
    return () => {
      wsManager?.close();
      cleanupAnimations();
      mq.removeEventListener('change', mqHandler);
    };
  });
</script>

<svelte:head>
  <title>{query ? `${query} - ${config.title} search` : config.title}</title>
</svelte:head>

<svelte:window onkeydown={handleKeydown} onpopstate={handlePopState} />

<Dialog.Root bind:open={$showHelp}>
  <Dialog.Content
    showCloseButton={false}
    class="border-border-brand bg-card-surface max-w-md gap-0 overflow-hidden rounded-none border-[3px] p-0 shadow-[6px_6px_0px_var(--hister-indigo)]"
  >
    <Dialog.Header class="bg-hister-indigo flex-row items-center justify-between gap-2 px-5 py-4">
      <Dialog.Title class="flex items-center gap-2">
        <Keyboard class="text-primary-foreground size-5" />
        <span class="font-outfit text-primary-foreground text-lg font-extrabold"
          >Keyboard Shortcuts</span
        >
      </Dialog.Title>
      <Dialog.Close class="text-primary-foreground/70 hover:text-primary-foreground p-0.5">
        <X class="size-5" />
      </Dialog.Close>
    </Dialog.Header>
    <Card.Content class="space-y-0 p-4">
      {#each Object.entries(config.hotkeys).filter(([, action]) => config.canWrite || action !== 'delete_result') as [key, action]}
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

<Dialog.Root bind:open={showDeleteConfirm}>
  <Dialog.Content
    escapeKeydownBehavior="ignore"
    showCloseButton={false}
    class="border-border-brand bg-card-surface flex max-h-[80vh] max-w-md flex-col gap-0 overflow-hidden rounded-none border-[3px] p-0 shadow-[6px_6px_0px_var(--brutal-shadow)]"
  >
    <Dialog.Header class="bg-hister-rose flex-row items-center justify-between gap-2 px-5 py-4">
      <Dialog.Title class="flex items-center gap-2">
        <Trash2 class="text-primary-foreground size-5" />
        <span class="font-outfit text-primary-foreground text-lg font-extrabold">Delete result</span
        >
      </Dialog.Title>
    </Dialog.Header>
    <div class="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
      <p class="font-inter text-text-brand-secondary text-sm">
        Are you sure you want to delete this result?
      </p>
      <code
        class="font-fira bg-muted-surface text-text-brand-muted block px-2 py-1 text-xs break-all"
        title={deleteConfirmUrl}>{deleteConfirmUrl}</code
      >
      <label class="flex cursor-pointer items-center gap-2">
        <input type="checkbox" bind:checked={deleteConfirmSkip} class="accent-hister-rose" />
        <span class="font-inter text-text-brand-secondary text-sm">Don't ask again</span>
      </label>
    </div>
    <div class="border-border-brand-muted flex shrink-0 justify-end gap-2 border-t-[3px] px-5 py-3">
      <Button
        variant="outline"
        size="sm"
        class="border-border-brand-muted text-text-brand-secondary rounded-none"
        onclick={cancelDelete}
      >
        No
      </Button>
      <Button
        size="sm"
        class="bg-hister-rose text-primary-foreground hover:bg-hister-rose/90 rounded-none border-0"
        onclick={confirmDelete}
      >
        Yes, delete
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={showDeleteAllConfirm}>
  <Dialog.Content
    escapeKeydownBehavior="ignore"
    showCloseButton={false}
    class="border-border-brand bg-card-surface flex max-h-[80vh] max-w-md flex-col gap-0 overflow-hidden rounded-none border-[3px] p-0 shadow-[6px_6px_0px_var(--brutal-shadow)]"
  >
    <Dialog.Header class="bg-hister-rose flex-row items-center justify-between gap-2 px-5 py-4">
      <Dialog.Title class="flex items-center gap-2">
        <Trash2 class="text-primary-foreground size-5" />
        <span class="font-outfit text-primary-foreground text-lg font-extrabold"
          >Delete all matching results</span
        >
      </Dialog.Title>
    </Dialog.Header>
    <div class="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
      <p class="font-inter text-text-brand-secondary text-sm">
        Are you sure you want to delete <strong
          >all {lastResults?.total || totalResults} result(s)</strong
        > matching:
      </p>
      <code
        class="font-fira bg-muted-surface text-text-brand-muted block px-2 py-1 text-xs break-all"
        >{query}</code
      >
      <p class="font-inter text-hister-rose text-xs font-semibold">This action cannot be undone.</p>
    </div>
    <div class="border-border-brand-muted flex shrink-0 justify-end gap-2 border-t-[3px] px-5 py-3">
      <Button
        variant="outline"
        size="sm"
        class="border-border-brand-muted text-text-brand-secondary rounded-none"
        onclick={cancelDeleteAll}
      >
        No
      </Button>
      <Button
        size="sm"
        class="bg-hister-rose text-primary-foreground hover:bg-hister-rose/90 rounded-none border-0"
        onclick={confirmDeleteAll}
      >
        Yes, delete all
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>

{#snippet connectionNotice(className: string)}
  {#if connectionState === 'disconnected'}
    <div
      class="border-hister-rose bg-hister-rose/10 flex flex-col gap-3 border-[3px] px-4 py-3 sm:flex-row sm:items-center {className}"
      role="alert"
    >
      <WifiOff class="text-hister-rose size-5 shrink-0" />
      <div class="min-w-0 flex-1">
        <p class="font-outfit text-hister-rose font-bold">Cannot connect to Hister</p>
        <p class="font-inter text-text-brand-secondary text-sm">
          {lastResults
            ? 'Showing the last loaded results while automatic reconnection continues.'
            : 'Search is unavailable while automatic reconnection continues.'}
        </p>
      </div>
      <div class="flex shrink-0 flex-wrap gap-2">
        <Button
          variant="outline"
          size="sm"
          class="border-hister-rose text-hister-rose hover:bg-hister-rose/10 border-[2px]"
          onclick={retryConnection}
        >
          <RefreshCw class="size-3.5" />
          Retry now
        </Button>
        <Button
          variant="ghost"
          size="sm"
          href="https://hister.org/docs/troubleshooting"
          target="_blank"
          rel="noopener noreferrer"
          class="text-text-brand-secondary hover:text-hister-indigo"
        >
          Troubleshooting
          <ExternalLink class="size-3.5" />
        </Button>
      </div>
    </div>
  {/if}
{/snippet}

{#snippet homeStatChip(
  href: string,
  colorClass: string,
  ariaLabel: string,
  Icon: Component<{ class?: string }>,
  count: number,
  label: string,
  searchQuery: string | null,
)}
  <a
    {href}
    class="home-stat-pill home-stat-link {colorClass}"
    aria-label={ariaLabel}
    onclick={(event) => {
      if (searchQuery === null) return;
      event.preventDefault();
      clickChip(searchQuery);
    }}
  >
    <Icon class="size-3.5 md:size-4" />
    <span class="font-outfit text-xl font-extrabold">{count}</span>
    <span class="font-inter text-text-brand-secondary text-sm">{label}</span>
  </a>
{/snippet}

{#if isSearching}
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="relative z-40 shrink-0">
      <div
        class="search bg-page-bg border-brutal-border flex h-10 items-center gap-3 border-b-[3px] px-4 md:h-14"
      >
        <Search class="text-text-brand-muted size-4 md:size-6" />
        <Input
          bind:ref={inputEl}
          bind:value={query}
          type="search"
          placeholder="Search..."
          aria-autocomplete="list"
          aria-controls={querySuggestionsVisible ? querySuggestionListId : undefined}
          aria-expanded={querySuggestionsVisible}
          aria-haspopup="listbox"
          aria-activedescendant={activeQuerySuggestionId}
          oninput={handleQueryInput}
          oncompositionstart={handleCompositionStart}
          oncompositionend={handleCompositionEnd}
          onfocus={handleQueryFocus}
          onselect={handleQuerySelection}
          onclick={handleQuerySelection}
          onkeydown={handleQueryInputKeydown}
          class="font-inter text-text-brand placeholder:text-text-brand-muted h-full flex-1 border-0 bg-transparent p-0 text-lg font-medium shadow-none focus-visible:ring-0 md:text-2xl"
        />
        {#if config.semanticEnabled}
          <Tooltip.Provider delayDuration={0}>
            <Tooltip.Root>
              <Tooltip.Trigger>
                <button
                  type="button"
                  onclick={() => (semanticOn = !semanticOn)}
                  class="flex shrink-0 items-center gap-1 px-1.5 py-0.5 text-xs font-semibold transition-colors {semanticOn
                    ? 'text-hister-indigo'
                    : 'text-text-brand-muted hover:text-hister-indigo'}"
                  aria-pressed={semanticOn}
                  aria-label="Toggle semantic search"
                >
                  <Sparkles class="size-3.5" />
                  <span class="hidden md:inline">Semantic</span>
                </button>
              </Tooltip.Trigger>
              <Tooltip.Portal>
                <Tooltip.Content>
                  {semanticOn ? 'Semantic search on' : 'Semantic search off'} — click to toggle
                </Tooltip.Content>
              </Tooltip.Portal>
            </Tooltip.Root>
          </Tooltip.Provider>
        {/if}
        <div class="flex shrink-0 items-center gap-1">
          {#if query}
            <button
              type="button"
              class="text-text-brand-muted hover:bg-muted-surface hover:text-text-brand flex h-8 w-8 items-center justify-center transition-colors md:h-9 md:w-9"
              aria-label="Clear search"
              title="Clear search"
              onclick={() => {
                query = '';
                queryCursor = 0;
                querySuggestionOpen = true;
                resultsShown = false;
                inputEl?.focus();
              }}
            >
              <X class="size-4" />
            </button>
          {/if}
          <Tooltip.Provider delayDuration={0}>
            <Tooltip.Root>
              <Tooltip.Trigger>
                <button
                  type="button"
                  class="text-text-brand-muted hover:bg-muted-surface flex h-8 items-center gap-2 px-2 text-xs font-semibold transition-colors md:h-9 md:px-3"
                  aria-label={connectionStatus.description}
                  onclick={() => {
                    if (connectionState === 'disconnected') retryConnection();
                  }}
                >
                  <span class="h-2 w-2 shrink-0 {connectionStatus.dotClass}"></span>
                  <span class="hidden md:inline">{connectionStatus.label}</span>
                </button>
              </Tooltip.Trigger>
              <Tooltip.Portal>
                <Tooltip.Content>
                  {connectionStatus.description}{connectionState === 'disconnected'
                    ? '. Click to retry.'
                    : ''}
                </Tooltip.Content>
              </Tooltip.Portal>
            </Tooltip.Root>
          </Tooltip.Provider>
        </div>
      </div>
      <QuerySuggestions
        id={querySuggestionListId}
        floating={false}
        open={querySuggestionOpen}
        suggestions={querySuggestions}
        activeIndex={querySuggestionKeyboardActive ? querySuggestionIndex : -1}
        loading={querySuggestionFacetsLoading}
        onactivechange={setActiveQuerySuggestion}
        onselect={selectQuerySuggestion}
      />
    </div>
    {@render connectionNotice('mx-3 my-3 md:mx-6')}

    <div class="flex min-h-0 flex-1 overflow-hidden" bind:this={splitContainerEl}>
      {#if !previewFullscreen}
        <ScrollArea class="results-scroll min-h-0 min-w-0 flex-1 overflow-hidden">
          <div
            class="results-list w-full max-w-[70em] space-y-3 overflow-x-hidden px-3 py-2 md:px-6"
            class:results-list-panel={lastResults && panelOpen && isDesktop && !disablePreviews}
          >
            {#if deleteError}
              <div
                class="border-hister-rose bg-hister-rose/10 text-hister-rose flex items-center justify-between gap-2 border-[2px] px-3 py-2 text-sm"
              >
                <span class="font-inter">{deleteError}</span>
                <button
                  class="shrink-0 cursor-pointer opacity-60 hover:opacity-100"
                  onclick={() => (deleteError = null)}
                  aria-label="Dismiss">✕</button
                >
              </div>
            {/if}
            <div class="flex min-w-0 flex-wrap items-center justify-end gap-2 px-1 py-2">
              <DropdownMenu.Root bind:open={actionsDropdownOpen} onOpenChange={handleActionsOpenChange}>
                <DropdownMenu.Trigger>
                  {#snippet child({ props })}
                    <Button
                      {...props}
                      variant="ghost"
                      size="sm"
                      class="font-inter gap-1 text-xs {actionsDropdownOpen
                        ? 'text-hister-indigo'
                        : 'text-text-brand-muted hover:text-hister-indigo'}"
                    >
                      <SlidersHorizontal class="size-3" />
                      Actions
                      <ChevronDown
                        class="size-3 transition-transform duration-200 {actionsDropdownOpen
                          ? 'rotate-180'
                          : ''}"
                      />
                    </Button>
                  {/snippet}
                </DropdownMenu.Trigger>
                <DropdownMenu.Content
                  class="border-brutal-border bg-card-surface w-80 rounded-none border-[3px] p-3 shadow-[4px_4px_0_var(--brutal-shadow)]"
                >
                  <div class="space-y-3">
                    {#if config.semanticEnabled && semanticOn}
                      <div class="space-y-2">
                        <p
                          class="font-inter text-text-brand-muted flex items-center gap-1.5 text-xs font-semibold"
                        >
                          <Sparkles class="size-3" />
                          Semantic Search
                        </p>
                        <label
                          class="font-inter text-text-brand-secondary flex flex-col gap-1 text-xs"
                        >
                          <span
                            >Similarity threshold: <span class="font-fira text-hister-indigo"
                              >{similarityThreshold.toFixed(2)}</span
                            ></span
                          >
                          <input
                            type="range"
                            min="0"
                            max="1"
                            step="0.002"
                            bind:value={similarityThreshold}
                            class="accent-hister-indigo w-full cursor-pointer"
                          />
                        </label>
                        <label
                          class="font-inter text-text-brand-secondary flex flex-col gap-1 text-xs"
                        >
                          <span
                            >Semantic weight: <span class="font-fira text-hister-indigo"
                              >{semanticWeight.toFixed(2)}</span
                            ></span
                          >
                          <input
                            type="range"
                            min="0"
                            max="1"
                            step="0.05"
                            bind:value={semanticWeight}
                            class="accent-hister-indigo w-full cursor-pointer"
                          />
                        </label>
                      </div>
                      <Separator class="bg-border-brand-muted" />
                    {/if}
                    <div class="space-y-2">
                      <p
                        class="font-inter text-text-brand-muted flex items-center gap-1.5 text-xs font-semibold"
                      >
                        <Download class="size-3" />
                        Export Results
                      </p>
                      <div class="flex flex-wrap gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          class="border-hister-indigo text-hister-indigo hover:bg-hister-indigo/10 h-7 border-[2px] text-xs"
                          onclick={() =>
                            exportJSON({ ...lastResults!, documents: accumulatedDocs })}
                        >
                          JSON
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          class="border-hister-indigo text-hister-indigo hover:bg-hister-indigo/10 h-7 border-[2px] text-xs"
                          onclick={() =>
                            exportCSV({ ...lastResults!, documents: accumulatedDocs }, query)}
                        >
                          CSV
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          class="border-hister-indigo text-hister-indigo hover:bg-hister-indigo/10 h-7 border-[2px] text-xs"
                          onclick={() =>
                            exportRSS({ ...lastResults!, documents: accumulatedDocs }, query)}
                        >
                          RSS
                        </Button>
                      </div>
                    </div>
                    {#if config.canWrite}
                      <Separator class="bg-border-brand-muted" />
                      <div class="space-y-2">
                        <p
                          class="font-inter text-hister-rose flex items-center gap-1.5 text-xs font-semibold"
                        >
                          <Trash2 class="size-3" />
                          Danger Zone
                        </p>
                        <Button
                          variant="outline"
                          size="sm"
                          class="border-hister-rose text-hister-rose hover:bg-hister-rose/10 h-7 w-full border-[2px] text-xs"
                          onclick={() => {
                            showDeleteAllConfirm = true;
                          }}
                        >
                          <Trash2 class="size-3" />
                          Delete all matching results
                        </Button>
                      </div>
                    {/if}
                  </div>
                </DropdownMenu.Content>
              </DropdownMenu.Root>
            </div>
            {#if hasResults}
              <div
                class="results-toolbar flex min-w-0 flex-wrap items-center justify-between gap-2 px-1 py-2"
              >
                <span class="font-outfit text-text-brand text-sm font-bold md:text-base">
                  {lastResults?.total && lastResults.total > totalResults
                    ? lastResults.total
                    : totalResults} results{#if lastResults?.search_duration}{' '}<span
                      class="text-xs font-normal md:text-sm"
                    >
                      ({lastResults.search_duration})</span
                    >{/if}
                </span>
                <div class="flex min-w-0 flex-wrap items-center justify-end gap-2 overflow-hidden">
                  {#if isDesktop && !panelOpen && !disablePreviews}
                    <Button
                      variant="ghost"
                      size="sm"
                      class="font-inter text-text-brand-muted hover:text-hister-indigo gap-1 text-xs"
                      onclick={() => {
                        panelOpen = true;
                        localStorage.setItem('hister-panel-open', 'true');
                      }}
                    >
                      <Eye class="size-3" />
                      Preview
                    </Button>
                  {/if}
                  {#if showFiltersButton}
                    <DropdownMenu.Root bind:open={filtersDropdownOpen}>
                      <DropdownMenu.Trigger>
                        {#snippet child({ props })}
                          <Button
                            {...props}
                            variant="ghost"
                            size="sm"
                            class="font-inter gap-1 text-xs {filtersDropdownOpen
                              ? 'text-hister-indigo'
                              : 'text-text-brand-muted hover:text-hister-indigo'}"
                          >
                            <Filter class="size-3" />
                            Filters
                            {#if activeFilterCount > 0}
                              <span
                                class="bg-hister-indigo text-background flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-none font-bold"
                                >{activeFilterCount}</span
                              >
                            {/if}
                            <ChevronDown
                              class="size-3 transition-transform duration-200 {filtersDropdownOpen
                                ? 'rotate-180'
                                : ''}"
                            />
                          </Button>
                        {/snippet}
                      </DropdownMenu.Trigger>
                      <DropdownMenu.Content
                        class="border-brutal-border bg-card-surface w-80 rounded-none border-[3px] p-3 shadow-[4px_4px_0_var(--brutal-shadow)]"
                      >
                        <div class="space-y-3">
                          {#snippet facetSection(
                            facet: SearchFacetDefinition,
                            activeFilters: Set<string>,
                            showSeparator: boolean,
                          )}
                            {@const Icon = facetIcons[facet.icon ?? ''] ?? Filter}
                            {#if showSeparator}
                              <Separator class="bg-border-brand-muted" />
                            {/if}
                            <div class="space-y-1.5">
                              <p
                                class="font-inter text-text-brand-muted flex items-center gap-1.5 text-xs font-semibold"
                              >
                                <Icon class="size-3" />
                                {facet.label}
                              </p>
                              <div class="flex flex-wrap gap-1">
                                {#each currentFacets?.terms?.[facet.name]?.terms ?? [] as { term, count, label } (term)}
                                  <button
                                    class="font-inter cursor-pointer rounded-none border-[2px] px-2 py-0.5 text-xs transition-colors {activeFilters.has(
                                      term,
                                    )
                                      ? 'border-hister-indigo bg-hister-indigo text-background'
                                      : 'border-border-brand-muted text-text-brand-secondary hover:border-hister-indigo hover:text-hister-indigo'}"
                                    onclick={() => toggleQueryToken(facet.queryField, term)}
                                  >
                                    {label ?? term}
                                    <span class="opacity-60">({count})</span>
                                  </button>
                                {/each}
                              </div>
                              {#if currentFacets?.terms?.[facet.name]?.other}
                                <button
                                  class="font-inter text-text-brand-muted hover:text-hister-indigo mt-1 cursor-pointer text-xs underline-offset-2 hover:underline"
                                  onclick={() => loadMoreFacet(facet.name)}>Load more</button
                                >
                              {/if}
                            </div>
                          {/snippet}
                          {#if facetsLoading}
                            <p class="font-inter text-text-brand-muted text-xs">Loading filters…</p>
                          {:else}
                            {#each visibleTermFacets as facet, index (facet.name)}
                              {@render facetSection(
                                facet,
                                activeFiltersForFacet(facet.name),
                                index > 0,
                              )}
                            {/each}
                            {#snippet customDateInputs()}
                              <details class="group/custom w-full">
                                <summary
                                  class="font-inter text-text-brand-muted hover:text-hister-indigo cursor-pointer list-none text-xs underline-offset-2 hover:underline"
                                  >Custom</summary
                                >
                                <div
                                  class="mt-1.5 grid grid-cols-[auto_1fr] items-center gap-x-1.5 gap-y-2"
                                >
                                  <span class="font-inter text-text-brand-secondary text-xs"
                                    >From:</span
                                  >
                                  <Input
                                    type="date"
                                    value={dateFrom}
                                    oninput={(event) =>
                                      updateCustomDateFilter('from', event.currentTarget.value)}
                                    class="border-border-brand-muted bg-card-surface text-text-brand font-fira focus-visible:border-hister-indigo h-7 w-auto min-w-0 border-[2px] px-2 text-xs shadow-none focus-visible:ring-0"
                                  />
                                  <span class="font-inter text-text-brand-secondary text-xs"
                                    >To:</span
                                  >
                                  <Input
                                    type="date"
                                    value={dateTo}
                                    oninput={(event) =>
                                      updateCustomDateFilter('to', event.currentTarget.value)}
                                    class="border-border-brand-muted bg-card-surface text-text-brand font-fira focus-visible:border-hister-indigo h-7 w-auto min-w-0 border-[2px] px-2 text-xs shadow-none focus-visible:ring-0"
                                  />
                                </div>
                              </details>
                            {/snippet}
                            {#if dateFacetDefinition && currentFacets?.date_histogram?.some((b) => b.count > 0)}
                              {@const DateIcon =
                                facetIcons[dateFacetDefinition.icon ?? ''] ?? Filter}
                              {#if visibleTermFacets.length > 0}
                                <Separator class="bg-border-brand-muted" />
                              {/if}
                              <div class="space-y-1.5">
                                <p
                                  class="font-inter text-text-brand-muted flex items-center gap-1.5 text-xs font-semibold"
                                >
                                  <DateIcon class="size-3" />
                                  {dateFacetDefinition.label}
                                </p>
                                <div class="flex flex-col gap-1">
                                  {#each currentFacets.date_histogram as { name, count } (name)}
                                    {#if count > 0}
                                      <button
                                        class="font-inter flex cursor-pointer items-center justify-between rounded-none border-[2px] px-2 py-1 text-xs transition-colors {activeDateBucket ===
                                        name
                                          ? 'border-hister-indigo bg-hister-indigo text-background'
                                          : 'border-border-brand-muted text-text-brand-secondary hover:border-hister-indigo hover:text-hister-indigo'}"
                                        onclick={() => toggleDateBucket(name)}
                                      >
                                        <span
                                          >{dateFacetValues.find(
                                            (value) => value.facetBucket === name,
                                          )?.label ?? name}</span
                                        >
                                        <span class="opacity-60">{count}</span>
                                      </button>
                                    {/if}
                                  {/each}
                                </div>
                                {@render customDateInputs()}
                              </div>
                            {:else if dateFacetDefinition}
                              {@const DateIcon =
                                facetIcons[dateFacetDefinition.icon ?? ''] ?? Filter}
                              <div class="space-y-1.5">
                                <p
                                  class="font-inter text-text-brand-muted flex items-center gap-1.5 text-xs font-semibold"
                                >
                                  <DateIcon class="size-3" />
                                  {dateFacetDefinition.label}
                                </p>
                                {@render customDateInputs()}
                              </div>
                            {/if}
                            {#if !termFacetDefinitions.some((facet) => currentFacets?.terms?.[facet.name]?.terms?.length) && !currentFacets?.date_histogram?.some((bucket) => bucket.count > 0)}
                              <p class="font-inter text-text-brand-muted text-xs">
                                No filters available for this query.
                              </p>
                            {/if}
                          {/if}
                        </div>
                      </DropdownMenu.Content>
                    </DropdownMenu.Root>
                  {/if}
                  <DropdownMenu.Root bind:open={sortDropdownOpen}>
                    <DropdownMenu.Trigger>
                      {#snippet child({ props })}
                        <Button
                          {...props}
                          variant="ghost"
                          size="sm"
                          class="font-inter gap-1 text-xs {sortDropdownOpen || currentSort
                            ? 'text-hister-indigo'
                            : 'text-text-brand-muted hover:text-hister-indigo'}"
                        >
                          <ArrowUpDown class="size-3" />
                          Sort
                          {#if currentSort}
                            <span
                              class="bg-hister-indigo text-background flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-none font-bold"
                              >1</span
                            >
                          {/if}
                          <ChevronDown
                            class="size-3 transition-transform duration-200 {sortDropdownOpen
                              ? 'rotate-180'
                              : ''}"
                          />
                        </Button>
                      {/snippet}
                    </DropdownMenu.Trigger>
                    <DropdownMenu.Content
                      class="border-brutal-border bg-card-surface w-52 rounded-none border-[3px] p-3 shadow-[4px_4px_0_var(--brutal-shadow)]"
                    >
                      <div class="space-y-1.5">
                        <p
                          class="font-inter text-text-brand-muted flex items-center gap-1.5 text-xs font-semibold"
                        >
                          <ArrowUpDown class="size-3" />
                          Sort by
                        </p>
                        <div class="flex flex-col gap-1">
                          {#each sortOptions as { value, label } (value)}
                            <button
                              class="font-inter flex cursor-pointer items-center gap-2 rounded-none border-[2px] px-2 py-1 text-xs transition-colors {currentSort ===
                              value
                                ? 'border-hister-indigo bg-hister-indigo text-background'
                                : 'border-border-brand-muted text-text-brand-secondary hover:border-hister-indigo hover:text-hister-indigo'}"
                              onclick={() => {
                                setSort(value);
                                sortDropdownOpen = false;
                              }}
                            >
                              {label}
                              {#if currentSort === value}
                                <Check class="ml-auto size-3" />
                              {/if}
                            </button>
                          {/each}
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
                </div>
              </div>

              {#if lastResults?.query && lastResults.query.text.length > query.length}
                <p class="font-inter text-text-brand-muted text-sm">
                  Expanded query: <code
                    class="font-fira bg-muted-surface text-primary px-1.5 py-0.5 text-xs"
                    >{lastResults.query.text}</code
                  >
                </p>
              {/if}

              {#if displayResults.length > 0}
                {#each displayResults as r, i}
                  {@const color = r.isPinned
                    ? 'hister-teal'
                    : r.isHistoryResult
                      ? 'hister-amber'
                      : 'hister-cyan'}
                  {@const state = getResultState(r.url, r.label)}
                  <article
                    data-result
                    class="result-card flex w-full scroll-my-[6em] gap-3 transition-all duration-150"
                    class:result-card-active={i === highlightIdx}
                    class:mt-2.5={i !== 0}
                    style="--result-color: var(--{color});"
                  >
                    <div class="w-0 min-w-0 flex-1 space-y-0.5">
                      <div class="flex min-w-0 items-start gap-1.5">
                        <ResultFavicon
                          favicon={r.favicon}
                          faviconKey={r.favicon_key}
                          pinned={r.isPinned}
                          class="mt-0.5"
                        />
                        <a
                          data-result-link={r.url}
                          href={fileResultUrl(r.url, r.id, r.title || '*title*')}
                          class="result-title font-outfit text-md line-clamp-3 min-w-0 flex-1 font-semibold hover:underline md:text-xl"
                          title={r.title || '*title*'}
                          target={config.openResultsOnNewTab ? '_blank' : undefined}
                          onclick={() => {
                            sendHistoryBeacon(r.url, r.title || '*title*', query);
                          }}
                          onauxclick={(e) => {
                            if (e.button === 1)
                              sendHistoryBeacon(r.url, r.title || '*title*', query);
                          }}
                        >
                          {r.title || '*title*'}
                        </a>
                        <ResultActionsMenu
                          url={r.url}
                          title={r.title || '*title*'}
                          domain={r.domain}
                          resultState={state}
                          {query}
                          pinned={r.isPinned}
                          historyResult={r.isHistoryResult}
                          canWrite={config.canWrite}
                          onForget={() => sendQuery(query)}
                          onDelete={r.isPinned || !config.canWrite
                            ? undefined
                            : () => deleteResult(r.url)}
                          {removeResult}
                          {removeResultsByDomain}
                        />
                      </div>
                      <div
                        class="result-meta flex max-w-full min-w-0 items-center gap-x-3 gap-y-1 overflow-hidden"
                      >
                        <div
                          class="result-url-line flex max-w-full min-w-0 shrink items-center gap-1.5"
                        >
                          {#if r.add_count && r.add_count > 1}
                            <span
                              class="border-border-brand-muted text-text-brand-muted inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-none border-[2px] px-1.5 text-[11px] leading-none font-semibold"
                              title="index count"
                            >
                              {r.add_count}
                            </span>
                          {/if}
                          <span
                            class="result-url-text min-w-0 overflow-hidden text-xs text-ellipsis whitespace-nowrap md:text-sm"
                            title={r.url}
                          >
                            {#if r.domain}<span class="result-domain font-inter font-semibold"
                                >{r.domain}</span
                              >{/if}<span class="result-path font-fira text-text-brand-muted"
                              >{displayResultPath(r.url, r.domain)}</span
                            >
                          </span>
                        </div>
                        <div
                          class="result-secondary-meta flex shrink-0 items-center gap-x-3 gap-y-1"
                        >
                          <button
                            class="text-text-brand-muted hover:text-text-brand shrink-0 cursor-pointer"
                            title="Copy URL"
                            onclick={async () => {
                              await navigator.clipboard.writeText(r.url);
                              copiedUrl = r.url;
                              setTimeout(() => {
                                copiedUrl = null;
                              }, 1500);
                            }}
                          >
                            {#if copiedUrl === r.url}
                              <Check class="text-hister-teal size-3" />
                            {:else}
                              <Copy class="size-3" />
                            {/if}
                          </button>
                          {#if r.isPinned}
                            <Badge
                              variant="secondary"
                              class="bg-hister-teal/10 text-hister-teal h-4 border-0 px-1.5 py-0"
                              >pinned</Badge
                            >
                          {:else if r.isHistoryResult}
                            <Badge
                              variant="secondary"
                              class="bg-hister-amber/10 text-hister-amber h-4 border-0 px-1.5 py-0"
                              title="Prioritized because you opened it for this query. Use the result menu to forget it for this query."
                              >prioritized</Badge
                            >
                          {:else if r.updated}
                            <span
                              class="font-inter text-text-brand-muted text-xs whitespace-nowrap md:text-sm"
                              title={formatTimestamp(r.updated)}
                              >{formatRelativeTime(r.updated)}</span
                            >
                          {/if}
                          {#if state.displayLabel}
                            <Badge
                              variant="secondary"
                              class="result-label bg-hister-teal/20 min-h-4 shrink-0 border-0 px-1.5 py-0"
                              title={state.displayLabel}
                            >
                              <Tag class="mr-0.5 size-2.5 shrink-0" />{state.displayLabel}
                            </Badge>
                          {/if}
                          {#if !disablePreviews}
                            <Button
                              data-readable
                              variant="link"
                              size="sm"
                              href={buildPreviewUrl(r.url, r.title || '*title*', undefined, r.id)}
                              data-document-id={r.id || ''}
                              class="text-text-brand-muted hover:text-hister-indigo h-auto shrink-0 cursor-pointer gap-0.5 p-0 text-xs font-medium md:text-sm"
                              onclick={(e) => {
                                highlightIdx = i;
                                openReadable(e, r.url, r.title || '*title*', r.id || '');
                              }}
                            >
                              <Eye class="size-3" /><span>view</span>
                            </Button>
                          {/if}
                          {#if !r.isPinned && r.finalScore && config.semanticEnabled && semanticOn}
                            <Tooltip.Provider delayDuration={0}>
                              <Tooltip.Root>
                                <Tooltip.Trigger>
                                  <Badge
                                    variant="secondary"
                                    class="bg-hister-indigo/10 text-hister-indigo shrink-0 border-0 px-1.5 py-0 align-middle font-mono text-[10px]"
                                    >{r.finalScore?.toFixed(2)}</Badge
                                  >
                                </Tooltip.Trigger>
                                <Tooltip.Portal>
                                  <Tooltip.Content>
                                    Result score: {r.finalScore?.toFixed(2)}
                                  </Tooltip.Content>
                                </Tooltip.Portal>
                              </Tooltip.Root>
                            </Tooltip.Provider>
                          {/if}
                        </div>
                      </div>
                      {#if r.text}
                        <p
                          class="result-snippet font-inter text-text-brand-secondary line-clamp-3 text-sm leading-[1.45] md:text-base"
                        >
                          {@html r.text}
                        </p>
                      {/if}
                    </div>
                  </article>
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
            {:else if query && connectionState !== 'disconnected'}
              <div class="flex items-center justify-center py-16">
                <span class="font-inter text-text-brand-muted">Searching...</span>
              </div>
            {/if}
            {#if hasMore || loadingMoreForQuery}
              <div bind:this={sentinelEl} class="flex items-center justify-center py-4">
                <span class="font-inter text-text-brand-muted text-sm">Loading more…</span>
              </div>
            {:else if hasResults}
              <div bind:this={sentinelEl}></div>
            {/if}
          </div>
        </ScrollArea>
      {/if}

      <!-- Preview panel: fullscreen (both mobile and desktop) or split-pane (desktop only) -->
      {#if !disablePreviews}
        {#if previewFullscreen}
          <PreviewPanel
            url={panelUrl}
            documentId={panelDocumentId}
            hintTitle={panelHintTitle}
            fullscreen={true}
            onclose={closePanelAndFullscreen}
            onfullscreentoggle={isDesktop ? exitFullscreen : undefined}
            initialViewingVersionId={panelViewingVersion}
            onviewingversionchange={(id) => {
              panelViewingVersion = id;
              withSkipUrl(skipUrl, () =>
                replacePreviewHistory(panelUrl, panelHintTitle, id, panelDocumentId),
              );
            }}
          />
        {:else if lastResults && panelOpen && isDesktop}
          <!-- Drag handle to resize the split-screen panel -->
          <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
          <div
            class="split-resize-handle shrink-0 cursor-col-resize"
            onmousedown={startPanelResize}
            role="separator"
            aria-label="Resize preview panel"
          ></div>
          <div
            style="width: {panelWidthPct}%; flex: none;"
            class="preview-shell bg-card-surface relative z-10 flex min-h-0 overflow-hidden"
          >
            <PreviewPanel
              url={panelUrl}
              documentId={panelDocumentId}
              hintTitle={panelHintTitle}
              fullscreen={false}
              onclose={() => {
                panelOpen = false;
                localStorage.setItem('hister-panel-open', 'false');
              }}
              onfullscreentoggle={enterFullscreen}
              connected={true}
              initialViewingVersionId={panelViewingVersion}
              onviewingversionchange={(id) => {
                panelViewingVersion = id;
              }}
            />
          </div>
        {/if}
      {/if}
    </div>
  </div>
{:else}
  <div
    class="home-shell relative flex flex-1 flex-col items-center gap-5 overflow-y-auto px-4 py-8 md:gap-8 md:px-12 md:py-12"
  >
    <h1
      bind:this={heroTitleEl}
      class="home-title font-outfit bg-clip-text text-5xl leading-none font-black tracking-[8px] text-transparent uppercase select-none md:text-9xl"
      style="background-image: linear-gradient(90deg, var(--hister-indigo), var(--hister-coral), var(--hister-teal), var(--hister-indigo)); background-size: 300% 100%; background-position: 0% 50%;"
    >
      {config.title}
    </h1>

    {#if config.subtitle}
      <p class="home-subtitle font-inter text-text-brand-secondary text-center text-sm md:text-lg">
        {@html config.subtitle}
      </p>
    {/if}
    <div
      bind:this={underlineEl}
      class="home-underline h-[3px] w-48"
      style="background: linear-gradient(90deg, var(--hister-indigo), var(--hister-coral), var(--hister-teal)); transform: scaleX(0); transform-origin: left;"
    ></div>

    <div class="relative z-40 w-full max-w-[1100px] shrink-0">
      <div
        bind:this={searchBoxEl}
        class="home-search border-brutal-border bg-card-surface flex h-12 w-full items-center gap-3 border-[3px] px-4 md:h-15 md:px-5"
      >
        <Search aria-hidden="true" class="text-hister-indigo size-5 shrink-0 md:size-6" />
        <Input
          bind:ref={inputEl}
          bind:value={query}
          type="search"
          aria-label="Search your history"
          aria-autocomplete="list"
          aria-controls={querySuggestionsVisible ? querySuggestionListId : undefined}
          aria-expanded={querySuggestionsVisible}
          aria-haspopup="listbox"
          aria-activedescendant={activeQuerySuggestionId}
          placeholder="Search..."
          oninput={handleQueryInput}
          oncompositionstart={handleCompositionStart}
          oncompositionend={handleCompositionEnd}
          onfocus={handleQueryFocus}
          onselect={handleQuerySelection}
          onclick={handleQuerySelection}
          onkeydown={handleQueryInputKeydown}
          class="font-inter text-text-brand placeholder:text-text-brand-muted h-full min-w-0 flex-1 border-0 bg-transparent p-0 text-lg font-medium shadow-none focus-visible:ring-0 md:text-2xl"
        />
        <Tooltip.Provider delayDuration={0}>
          <Tooltip.Root>
            <Tooltip.Trigger>
              <button
                type="button"
                class="flex h-8 w-8 items-center justify-center"
                aria-label={connectionStatus.description}
                onclick={() => {
                  if (connectionState === 'disconnected') retryConnection();
                }}
              >
                <span class="h-2.5 w-2.5 shrink-0 {connectionStatus.dotClass}"></span>
              </button>
            </Tooltip.Trigger>
            <Tooltip.Portal>
              <Tooltip.Content>
                {connectionStatus.description}{connectionState === 'disconnected'
                  ? '. Click to retry.'
                  : ''}
              </Tooltip.Content>
            </Tooltip.Portal>
          </Tooltip.Root>
        </Tooltip.Provider>
      </div>
    </div>
    {@render connectionNotice('w-full max-w-[1100px]')}

    <div
      class="home-tip font-inter text-text-brand-muted hidden shrink-0 items-center gap-1 text-xs md:flex md:gap-2"
    >
      <span>Tip:</span>
      {#each currentTip as part}
        {#if part.type === 'text'}
          <span>{part.value}</span>
        {:else if part.type === 'kbd'}
          <Kbd
            bind:ref={kbdEl}
            class="bg-muted-surface border-border-brand-muted font-fira text-text-brand-secondary rounded-none border-[2px] px-2 py-0.5 text-xs font-semibold"
            >{part.value}</Kbd
          >
        {:else if part.type === 'hotkey'}
          {#if hotkeyByAction[part.action]}
            <Kbd
              bind:ref={kbdEl}
              class="bg-muted-surface border-border-brand-muted font-fira text-text-brand-secondary rounded-none border-[2px] px-2 py-0.5 text-xs font-semibold"
              >{hotkeyByAction[part.action]}</Kbd
            >
          {/if}
        {:else if part.type === 'code'}
          <code
            class="bg-muted-surface border-border-brand-muted font-fira text-text-brand-secondary rounded-none border-[2px] px-2 py-0.5 font-semibold"
            >{part.value}</code
          >
        {:else if part.type === 'link'}
          <a href={part.href} class="text-hister-indigo hover:underline">{part.value}</a>
        {/if}
      {/each}
    </div>

    {#if recentSearches.length > 0}
      <section
        class="home-recents flex w-full max-w-[900px] shrink-0 flex-col gap-2"
        aria-labelledby="recent-searches-title"
      >
        <div class="flex items-center justify-between gap-4">
          <h2
            id="recent-searches-title"
            class="font-inter text-text-brand-secondary flex items-center gap-2 text-sm font-semibold"
          >
            <History class="text-hister-indigo size-4" />
            Recent searches
          </h2>
          <Button
            variant="ghost"
            size="sm"
            class="font-inter text-text-brand-muted hover:text-hister-rose h-auto cursor-pointer px-2 py-1 text-xs"
            onclick={deleteAllRecentSearches}
            title="Clear all recent searches"
          >
            Clear all
          </Button>
        </div>
        <div class="flex flex-wrap items-center justify-center gap-2">
          {#each recentSearches.slice(0, 8) as search, i}
            {@const chip = chipColors[i % chipColors.length]}
            <div
              class="flex max-w-full min-w-0 items-stretch border-[2px] shadow-[2px_2px_0_var(--brutal-shadow)] {chip.border} {chip.bg}"
            >
              <button
                type="button"
                class="font-inter max-w-80 min-w-0 cursor-pointer truncate px-3 py-1.5 text-left text-sm font-semibold transition-colors hover:bg-black/5 dark:hover:bg-white/5 {chip.text}"
                title={search}
                onclick={() => clickChip(search)}
              >
                {search}
              </button>
              <button
                type="button"
                class="hover:bg-hister-rose/10 hover:text-hister-rose flex w-8 shrink-0 cursor-pointer items-center justify-center border-l transition-colors {chip.border} {chip.text}"
                aria-label="Remove recent search: {search}"
                title="Remove recent search"
                onclick={() => deleteRecentSearch(search)}
              >
                <X class="size-3.5" />
              </button>
            </div>
          {/each}
        </div>
      </section>
    {/if}

    {#if statsLoaded && statsAvailable && historyCount === 0 && config.canWrite}
      <section
        class="home-empty-index border-brutal-border bg-card-surface w-full max-w-[900px] shrink-0 border-[3px] shadow-[4px_4px_0_var(--brutal-shadow)]"
        aria-labelledby="empty-index-title"
      >
        <div
          class="border-brutal-border flex flex-col gap-2 border-b-[3px] px-4 py-3 sm:flex-row sm:items-center sm:justify-between md:px-5"
        >
          <div>
            <h2 id="empty-index-title" class="font-outfit text-text-brand text-lg font-extrabold">
              Start building your index
            </h2>
            <p class="font-inter text-text-brand-secondary text-sm">
              Choose any option to add your first searchable page.
            </p>
          </div>
          <a
            href="https://hister.org/docs/quickstart"
            target="_blank"
            rel="noopener noreferrer"
            class="font-inter text-hister-indigo flex shrink-0 items-center gap-1 text-sm font-semibold hover:underline"
          >
            <HelpCircle class="size-4" />
            Quickstart
            <ExternalLink class="size-3" />
          </a>
        </div>

        <div class="grid gap-3 p-4 md:grid-cols-3 md:p-5">
          <a
            href="https://hister.org/docs/browser-extension"
            target="_blank"
            rel="noopener noreferrer"
            class="empty-index-action border-hister-indigo bg-hister-indigo/5 hover:bg-hister-indigo/10"
          >
            <Globe class="text-hister-indigo size-5 shrink-0" />
            <span class="min-w-0">
              <span class="font-outfit text-text-brand block font-bold">Install the extension</span>
              <span class="font-inter text-text-brand-muted block text-xs"
                >Capture pages as you browse</span
              >
            </span>
            <ExternalLink class="text-text-brand-muted ml-auto size-3.5 shrink-0" />
          </a>

          <a
            href="{base}/add"
            class="empty-index-action border-hister-coral bg-hister-coral/5 hover:bg-hister-coral/10"
          >
            <Link2 class="text-hister-coral size-5 shrink-0" />
            <span class="min-w-0">
              <span class="font-outfit text-text-brand block font-bold">Add one page</span>
              <span class="font-inter text-text-brand-muted block text-xs"
                >Paste a URL to index it now</span
              >
            </span>
          </a>

          <a
            href="https://hister.org/docs/import#importing-browser-history"
            target="_blank"
            rel="noopener noreferrer"
            class="empty-index-action border-hister-teal bg-hister-teal/5 hover:bg-hister-teal/10"
          >
            <History class="text-hister-teal size-5 shrink-0" />
            <span class="min-w-0">
              <span class="font-outfit text-text-brand block font-bold">Import browser history</span
              >
              <span class="font-inter text-text-brand-muted block text-xs"
                >Bring your previously visited pages</span
              >
            </span>
            <ExternalLink class="text-text-brand-muted ml-auto size-3.5 shrink-0" />
          </a>
        </div>
      </section>
    {/if}
    <div
      bind:this={statsRowEl}
      class="home-stats flex shrink-0 flex-col items-center gap-3 md:flex-row md:gap-4"
      class:hidden={statsLoaded && statsAvailable && historyCount === 0 && config.canWrite}
      aria-live="polite"
      aria-busy={!statsLoaded}
    >
      {#if !statsLoaded}
        {#each Array(!config.public || config.canWrite ? 3 : 1) as _}
          <div class="home-stat-pill" aria-hidden="true">
            <span class="bg-muted-surface size-4"></span>
            <span class="bg-muted-surface h-5 w-10"></span>
            <span class="bg-muted-surface h-3 w-12"></span>
          </div>
        {/each}
      {:else if statsAvailable}
        {@render homeStatChip(
          `${base}/?q=*`,
          'text-hister-indigo',
          'Browse all indexed pages',
          History,
          displayHistoryCount,
          'pages',
          '*',
        )}
        {#if fileCount > 0}
          {@render homeStatChip(
            `${base}/?q=type:file`,
            'text-hister-amber',
            'Browse all indexed files',
            FileText,
            displayFileCount,
            'files',
            'type:file',
          )}
        {/if}
        {#if !config.public || config.canWrite}
          {@render homeStatChip(
            `${base}/rules#indexing-rules`,
            'text-hister-teal',
            'Open indexing rules',
            Shield,
            displayRulesCount,
            'rules',
            null,
          )}
          {@render homeStatChip(
            `${base}/rules#search-aliases`,
            'text-hister-coral',
            'Open search aliases',
            Link2,
            displayAliasesCount,
            'aliases',
            null,
          )}
        {/if}
      {:else}
        <p class="font-inter text-text-brand-muted text-sm" role="status">Statistics unavailable</p>
      {/if}
    </div>
  </div>
{/if}

<style>
  .home-search {
    position: relative;
    min-height: 3rem;
    overflow: hidden;
    background: var(--card-surface);
    box-shadow:
      0 1px 0 color-mix(in srgb, white 7%, transparent) inset,
      4px 4px 0 var(--brutal-shadow);
  }

  .home-search:focus-within {
    border-color: var(--hister-indigo);
    box-shadow:
      0 1px 0 color-mix(in srgb, white 7%, transparent) inset,
      4px 4px 0 var(--hister-indigo);
  }

  .home-title,
  .home-subtitle,
  .home-underline {
    flex-shrink: 0;
  }

  @media (min-width: 48rem) and (max-height: 48rem) {
    .home-shell {
      gap: clamp(0.75rem, 2vh, 1.25rem);
      padding-top: clamp(1rem, 3vh, 2rem);
      padding-bottom: clamp(1rem, 3vh, 2rem);
    }

    .home-title {
      font-size: clamp(3.5rem, 14vh, 6rem);
    }
  }

  @media (min-width: 48rem) and (max-height: 36rem) {
    .home-shell {
      gap: 0.5rem;
      padding-top: 0.75rem;
      padding-bottom: 0.75rem;
    }

    .home-title {
      font-size: 3rem;
    }

    .home-tip {
      display: none;
    }
  }

  .home-stat-pill {
    position: relative;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    overflow: hidden;
    border: 2px solid var(--border-muted-brand);
    background:
      linear-gradient(90deg, color-mix(in srgb, currentColor 8%, transparent), transparent 52%),
      var(--card-surface);
    padding: 0.5rem 0.875rem;
    box-shadow: 2px 2px 0 var(--brutal-shadow);
  }

  .home-stat-link {
    cursor: pointer;
    text-decoration: none;
    transition:
      box-shadow 150ms ease,
      transform 150ms ease;
  }

  .home-stat-link:hover {
    text-decoration: none;
    transform: translate(1px, 1px);
    box-shadow: 1px 1px 0 var(--brutal-shadow);
  }

  .empty-index-action {
    display: flex;
    min-width: 0;
    align-items: center;
    gap: 0.75rem;
    border-width: 2px;
    padding: 0.75rem;
    text-decoration: none;
    transition:
      background-color 150ms ease,
      box-shadow 150ms ease,
      transform 150ms ease;
  }

  .empty-index-action:hover {
    text-decoration: none;
    transform: translate(1px, 1px);
    box-shadow: 2px 2px 0 var(--brutal-shadow);
  }

  .home-stat-pill::before {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 3px;
    content: '';
    background: currentColor;
    opacity: 0.65;
  }

  .split-resize-handle {
    position: relative;
    z-index: 2;
    width: 0.375rem;
    border-left: 2px solid var(--border-brand);
    background: var(--card-surface);
    transition: background-color 160ms ease;
  }

  .split-resize-handle::before {
    position: absolute;
    top: 0;
    bottom: 0;
    left: -4px;
    width: 10px;
    content: '';
    background: transparent;
  }

  .split-resize-handle:hover {
    background: color-mix(in srgb, var(--hister-indigo) 28%, transparent);
  }

  :global(.results-list-panel) {
    box-sizing: border-box;
    max-width: min(70em, 100%);
    padding-right: 0;
    overflow: clip;
    contain: paint;
  }

  :global(.results-scroll),
  :global(.results-scroll [data-slot='scroll-area-viewport']) {
    min-width: 0;
    overflow-x: clip;
    contain: paint;
    background: var(--card-surface);
  }

  :global(.results-toolbar) {
    position: sticky;
    top: 0;
    z-index: 12;
    max-width: 100%;
    overflow: hidden;
    border-bottom: 1px solid var(--border-muted-brand);
    background: color-mix(in srgb, var(--card-surface) 92%, transparent);
    backdrop-filter: blur(8px);
  }

  .result-card {
    position: relative;
    container-type: inline-size;
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    border: 0;
    background: transparent;
    padding: 0.2rem 0;
    box-shadow: none;
  }

  .result-card:hover {
    background: linear-gradient(
      90deg,
      color-mix(in srgb, var(--result-color) 4%, transparent),
      transparent 44%
    );
  }

  .result-domain {
    color: color-mix(in srgb, var(--result-color) 70%, var(--text-primary-brand));
  }

  .result-meta {
    color: var(--text-muted-brand);
  }

  .result-card-active {
    border-left: 3px solid var(--result-color);
    background: linear-gradient(
      90deg,
      color-mix(in srgb, var(--result-color) 8%, transparent),
      color-mix(in srgb, var(--result-color) 2%, transparent) 62%,
      transparent
    );
    padding-left: 0.75rem;
    box-shadow: none;
  }

  :global(.results-list-panel) .result-card:not(.result-card-active),
  :global(.results-list-panel) .result-card-active {
    width: calc(100% - 1.5rem);
    max-width: 100%;
    overflow: hidden;
  }

  :global(.results-list-panel) .result-card-active {
    box-shadow: none;
  }

  .result-title {
    color: color-mix(in srgb, var(--result-color) 82%, var(--text-primary-brand));
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .result-title:hover {
    color: var(--result-color);
  }

  .result-snippet {
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  :global(.result-label) {
    overflow-wrap: anywhere;
    word-break: break-word;
    white-space: normal;
  }

  .result-url-text,
  .result-url-text .result-domain,
  .result-url-text .result-path {
    overflow-wrap: normal;
    white-space: nowrap;
    word-break: normal;
  }

  @container (max-width: 360px) {
    .result-meta {
      flex-wrap: wrap;
      overflow: visible;
    }

    .result-url-line {
      flex-basis: 100%;
      width: 100%;
    }

    .result-secondary-meta {
      flex-basis: 100%;
    }
  }

  :global(.result-snippet mark) {
    color: var(--text-primary-brand);
    background: color-mix(in srgb, var(--hister-amber) 18%, transparent);
    padding: 0 0.12em;
  }

  :global(.dark) .home-search {
    background: var(--card-surface);
    box-shadow:
      0 1px 0 color-mix(in srgb, white 10%, transparent) inset,
      0 0 0 1px color-mix(in srgb, white 3%, transparent),
      3px 3px 0 color-mix(in srgb, var(--hister-indigo) 16%, var(--brutal-shadow));
  }

  :global(.dark) .home-stat-pill {
    background:
      linear-gradient(90deg, color-mix(in srgb, currentColor 11%, transparent), transparent 54%),
      var(--card-surface);
    box-shadow:
      0 1px 0 color-mix(in srgb, white 8%, transparent) inset,
      2px 2px 0 color-mix(in srgb, white 4%, var(--brutal-shadow));
  }

  :global(.dark) .results-toolbar {
    background: color-mix(in srgb, var(--card-surface) 88%, transparent);
  }

  :global(.dark) .result-card {
    box-shadow: none;
  }

  :global(.dark) .result-card:hover {
    background: linear-gradient(
      90deg,
      color-mix(in srgb, var(--result-color) 7%, transparent),
      transparent 46%
    );
  }

  :global(.dark) .result-title {
    color: color-mix(in srgb, var(--result-color) 88%, white);
  }

  :global(.dark) .result-card-active {
    border-left-color: var(--result-color);
    background: linear-gradient(
      90deg,
      color-mix(in srgb, var(--result-color) 10%, transparent),
      color-mix(in srgb, var(--result-color) 4%, transparent) 58%,
      transparent
    );
    box-shadow: none;
  }

  :global(.dark .results-list-panel .result-card-active) {
    box-shadow: none;
  }
</style>
