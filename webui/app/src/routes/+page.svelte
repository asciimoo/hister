<script lang="ts">
  import { onMount, tick, untrack, type Component } from 'svelte';
  import { buildPreviewUrl, PreviewState } from '#lib/preview.svelte.js';
  import { resolve } from '$app/paths';
  import { beforeNavigate } from '$app/navigation';
  import {
    WebSocketManager,
    KeyHandler,
    getSearchUrl,
    exportJSON,
    exportCSV,
    exportRSS,
    scrollTo,
    escapeHTML,
    buildSearchQuery,
    parseSearchResults,
    openURL,
    isIMEKeyboardEvent,
  } from '#lib/search.js';
  import { fetchConfig, apiFetch, getUserId } from '#lib/api.js';
  import { createDebouncer } from '#lib/debounce.js';
  import { ResultState } from '#lib/result-state.svelte.js';
  import { mergeSearchResults, removeSearchResults } from '#lib/search-results.js';
  import { showHelp } from '#lib/stores.js';
  import { hotkeyDescriptions } from '#lib/hotkeys.js';
  import type {
    SearchResults,
    SearchResult,
    SearchQueryOptions,
    FacetsResult,
    WebSocketRequest,
  } from '#lib/search.js';
  import { RESULTS_PER_PAGE } from '#lib/search.js';
  import {
    customDatesFromQuery,
    removeTimeFilters,
    replaceTimeFilters,
    shiftISODate,
    timeFilters,
  } from '#lib/time-filters.js';
  import { emptySearchCapabilities, valuesForFacet } from '#lib/search-schema.js';
  import { queryFilters, removeQueryFilters, toggleQueryFilter } from '#lib/query-filters.js';
  import type { QueryFilter } from '#lib/query-filters.js';
  import type { SearchCapabilities, SearchFacetDefinition } from '#lib/search-schema.js';
  import {
    removeSortDirectives,
    replaceSortDirective,
    sortDirectiveFromQuery,
    sortValueFromQuery,
  } from '#lib/sort-directive.js';
  import {
    applyQuerySuggestion,
    buildQuerySuggestions,
    facetSuggestionContext,
  } from '#lib/query-suggestions.js';
  import type { QuerySuggestion } from '#lib/query-suggestions.js';
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
    SearchLoading,
    ResultActionsMenu,
    ResultFavicon,
  } from '#lib/components/index.js';
  import { Kbd } from '@hister/components/ui/kbd';
  import {
    Search,
    Star,
    Pin,
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
    Calendar,
    Filter,
    Sparkles,
    Ellipsis,
    Check,
    ArrowUpDown,
    RefreshCw,
    WifiOff,
  } from '@lucide/svelte';
  import type { HistoryItem } from '#lib/types.js';

  interface Config {
    wsUrl: string;
    title: string;
    subtitle: string;
    searchUrl: string;
    openResultsOnNewTab: boolean;
    showSuggestions: boolean;
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
    showSuggestions: false,
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
  let searchInProgress = $state(false);
  let completedSearchMessage = $state('');
  let latestSearchRequest = 0;
  let latestPageRequest = 0;
  let accumulatedDocs = $state<SearchResult[]>([]);
  let pageKey = $state('');
  let hasMore = $state(false);
  let loadingMoreForQuery = $state('');
  let sentinelEl = $state<HTMLElement | undefined>();
  let highlightIdx = $state(0);
  const currentSort = $derived(sortValueFromQuery(query, config.search));
  const currentSortLabel = $derived(
    config.search.sort.options.find((option) =>
      currentSort ? option.value === currentSort : option.default,
    )?.label ?? 'Relevance',
  );
  let dateFrom = $state('');
  let dateTo = $state('');
  let showPopup = $state(false);
  let popupUrl = $state('');
  let popupHintTitle = $state('');
  let disablePreviews = $state(false);
  let expandSnippets = $state(localStorage.getItem('hister-expanded-snippets') === 'true');
  const savedShowSuggestions = localStorage.getItem('hister-show-suggestions');
  let showSuggestionsOverride = $state<boolean | undefined>(
    savedShowSuggestions === 'true' ? true : savedShowSuggestions === 'false' ? false : undefined,
  );
  const showSuggestions = $derived(showSuggestionsOverride ?? config.showSuggestions);

  const previewState = new PreviewState({
    storageKey: 'hister-panel-open',
    onReturn: pushSearchHistory,
    beforeOpen: () => debouncedURLUpdate.flush(),
  });

  let resultsShown = $state(false);

  function storedSemanticValue(key: string): number | undefined {
    const stored = localStorage.getItem(key);
    if (!stored?.trim()) return undefined;
    const value = Number(stored);
    return Number.isFinite(value) && value >= 0 && value <= 1 ? value : undefined;
  }

  const savedSimilarityThreshold = storedSemanticValue('hister-semantic-threshold');
  const savedSemanticWeight = storedSemanticValue('hister-semantic-weight');
  // Wait for server defaults before persisting settings that have not been saved yet.
  let semanticSettingsReady = $state(false);
  let semanticOn = $state(localStorage.getItem('hister-semantic-on') === 'true');
  let similarityThreshold = $state(savedSimilarityThreshold ?? 0.5);
  let semanticWeight = $state(savedSemanticWeight ?? 0.4);
  const currentSearchMessage = $derived(JSON.stringify(buildSearchQuery(query, searchQueryOpts())));
  const searchPending = $derived(
    !!query && (searchInProgress || currentSearchMessage !== completedSearchMessage),
  );
  const isSearchLoading = $derived(searchPending && connectionState !== 'disconnected');

  let showDeleteConfirm = $state(false);
  let deleteConfirmUrl = $state('');
  let deleteConfirmSkip = $state(false);

  let showDeleteAllConfirm = $state(false);
  let deleteError: string | null = $state(null);
  let deleteErrorTimer: any = null;

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
    showSuggestions &&
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
      { type: 'link', value: 'Rules page', href: resolve('/rules') },
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

  const mergedResults = $derived(
    mergeSearchResults(accumulatedDocs, lastResults?.semantic_hits, {
      semanticEnabled: semanticOn && config.semanticEnabled,
      weight: semanticWeight,
      sort: currentSort,
      userId: getUserId(),
    }),
  );

  const historyLen = $derived((lastResults?.history as any)?.length || 0);
  const docsLen = $derived(mergedResults.length);
  const totalResults = $derived(historyLen + docsLen);
  const resultCount = $derived(Math.max(lastResults?.total ?? 0, totalResults));
  const hasResults = $derived(!searchPending && totalResults > 0);
  const showPreviewPanel = $derived(
    hasResults &&
      !disablePreviews &&
      (previewState.fullscreen || (previewState.panelOpen && previewState.isDesktop)),
  );
  const displayResults = $derived<DisplayResult[]>([
    ...(lastResults?.history ?? []).map((r): DisplayResult => ({
      ...r,
      isPinned: r.pinned ?? false,
      isHistoryResult: true,
    })),
    ...mergedResults.map((r): DisplayResult => ({ ...r, isPinned: false, isHistoryResult: false })),
  ]);
  const panelResult = $derived(
    displayResults.find(
      (result) =>
        result.url === previewState.url &&
        (!previewState.documentId || result.id === previewState.documentId),
    ),
  );
  const panelSearchScore = $derived(
    config.semanticEnabled && semanticOn && !panelResult?.isPinned
      ? panelResult?.finalScore
      : undefined,
  );

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

  const filterFields = $derived([
    ...new Set([
      ...config.search.facets.map((facet) => facet.queryField),
      ...config.search.fields.filter((field) => field.kind === 'time').map((field) => field.name),
    ]),
  ]);
  const activeQueryFilters = $derived(queryFilters(query, filterFields, config.search));
  const activeFilterChips = $derived(
    activeQueryFilters.map((filter) => {
      const facet = config.search.facets.find((facet) => facet.queryField === filter.field);
      const field = config.search.fields.find((field) => field.name === filter.field);
      const values = facet
        ? valuesForFacet(config.search, facet)
        : (config.search.valueSets[field?.valueSet ?? ''] ?? []);
      const valueLabel =
        values.find((value) => value.value === filter.value)?.label ??
        currentFacets?.terms?.[facet?.name ?? '']?.terms?.find((term) => term.term === filter.value)
          ?.label ??
        filter.value;
      return {
        filter,
        label: `${field?.label ?? facet?.label ?? filter.field}: ${filter.negated ? 'Not ' : ''}${valueLabel}`,
      };
    }),
  );
  const activeFacetFilters = $derived(
    new Map(
      termFacetDefinitions.map((facet) => [
        facet.name,
        new Set(
          activeQueryFilters
            .filter((filter) => filter.field === facet.queryField && !filter.negated)
            .map((filter) => filter.value),
        ),
      ]),
    ),
  );
  const activeTimeFilters = $derived(
    timeFilters(query, dateFacetDefinition?.queryField ?? '', config.search),
  );
  const activeDateBucket = $derived(
    activeTimeFilters.length === 1
      ? (dateFacetValues.find(
          (value) =>
            value.value ===
            `${activeTimeFilters[0].comparison}${activeTimeFilters[0].value.toLowerCase()}`,
        )?.facetBucket ?? null)
      : null,
  );
  const activeFilterCount = $derived(activeQueryFilters.length);

  function removeFilters(filters: QueryFilter[]) {
    query = removeQueryFilters(query, filters, config.search) || '*';
    querySuggestionOpen = false;
  }

  function clearFilters() {
    removeFilters(activeQueryFilters);
  }

  function facetTerms(facet: SearchFacetDefinition) {
    const terms = new Map(
      (currentFacets?.terms?.[facet.name]?.terms ?? []).map((term) => [term.term, term]),
    );
    for (const value of activeFiltersForFacet(facet.name)) {
      if (!terms.has(value)) {
        terms.set(value, {
          term: value,
          count: 0,
          label: valuesForFacet(config.search, facet).find((option) => option.value === value)
            ?.label,
        });
      }
    }
    return [...terms.values()];
  }

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
  const showFiltersButton = $derived(config.search.facets.length > 0 || activeFilterCount > 0);

  function toggleQueryToken(prefix: string, value: string) {
    query = toggleQueryFilter(query, prefix, value, config.search);
    querySuggestionOpen = false;
  }

  function toggleDateBucket(name: string) {
    const field = dateFacetDefinition?.queryField ?? '';
    if (activeDateBucket === name) {
      query = removeTimeFilters(query, field, config.search);
    } else {
      const filter = dateFacetValues.find((value) => value.facetBucket === name)?.value;
      if (filter) query = replaceTimeFilters(query, field, [filter], config.search);
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
    query = replaceTimeFilters(
      query,
      dateFacetDefinition?.queryField ?? '',
      filters,
      config.search,
    );
  }

  $effect(() => {
    const dates = customDatesFromQuery(query, dateFacetDefinition?.queryField ?? '', config.search);
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

  beforeNavigate(({ willUnload, complete, shallow }) => {
    if (shallow) return;
    debouncedURLUpdate.cancel();
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
    searchInProgress = true;
    latestPageRequest = 0;
    loadingMoreForQuery = '';
    pageKey = '';
    hasMore = false;
    latestSearchRequest =
      wsManager?.send(JSON.stringify(buildSearchQuery(q, searchQueryOpts()))) ?? 0;
  }

  function loadMoreResults() {
    if (searchPending || !pageKey || !hasMore || loadingMoreForQuery) return;
    loadingMoreForQuery = query;
    latestPageRequest =
      wsManager?.sendImmediate(JSON.stringify(buildSearchQuery(query, searchQueryOpts(pageKey)))) ??
      0;
  }

  let lastPushedEmpty = true;
  const debouncedURLUpdate = createDebouncer(updateURL, 1000);

  // --- URL builders ---

  function queryFromSearchParams(params: URLSearchParams): string {
    let value = params.get('q') || '';
    const legacySort = params.get('sort');
    if (
      legacySort &&
      sortValues.has(legacySort) &&
      sortDirectiveFromQuery(value, config.search) === null
    ) {
      value = replaceSortDirective(value, legacySort, config.search);
    }
    return value;
  }

  function buildSearchUrl(): string {
    const params = new URLSearchParams();
    if (query) params.set('q', query);
    const search = params.toString();
    return `${resolve('/')}${search ? `?${search}` : ''}${window.location.hash}`;
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
    if (previewState.skipUrl) return;
    if (previewState.fullscreen) return;
    const isEmpty = !query;
    if (isEmpty !== lastPushedEmpty) {
      pushSearchHistory();
    } else {
      replaceSearchHistory();
    }
  }

  function handlePopState(event: PopStateEvent) {
    debouncedURLUpdate.cancel();
    if (previewState.restore(event)) return;
    previewState.withSkipUrl(() => {
      const params = new URLSearchParams(window.location.search);
      query = queryFromSearchParams(params);
      lastPushedEmpty = !query;
      if (query && connected) sendQuery(query);
      if (!query) {
        autocomplete = '';
        lastResults = null;
      }
    });
  }

  function renderResults(event: MessageEvent, request: WebSocketRequest) {
    if (!query) return;
    const isLoadMore =
      request.id === latestPageRequest && !searchPending && loadingMoreForQuery === query;
    if (
      !isLoadMore &&
      (request.id !== latestSearchRequest || request.message !== currentSearchMessage)
    )
      return;
    const res = parseSearchResults(event.data);
    loadingMoreForQuery = '';
    if (isLoadMore) {
      accumulatedDocs = [...accumulatedDocs, ...(res.documents ?? [])];
      lastResults = { ...lastResults!, ...res, documents: accumulatedDocs };
    } else {
      searchInProgress = false;
      completedSearchMessage = request.message;
      accumulatedDocs = res.documents ?? [];
      lastResults = res;
      autocomplete = (query && res.query_suggestion) || '';
      highlightIdx = 0;
      if (!isComposing) resultsShown = true;
    }
    hasMore = !!res.page_key;
    pageKey = res.page_key ?? '';
  }

  function stripHtml(s: string): string {
    return s.replace(/<[^>]*>/g, '');
  }

  function openResult(url: string, title: string, newWindow = false, sourceUrl = url) {
    debouncedURLUpdate.flush();
    if (config.openResultsOnNewTab) newWindow = true;
    sendHistoryBeacon(sourceUrl, title, query);
    openURL(url, newWindow);
  }

  function sendHistoryBeacon(url: string, title: string, queryStr: string) {
    if (!config.historyEnabled) return;
    const payload = JSON.stringify({
      url,
      title: stripHtml(title),
      query: queryStr,
      delete: false,
    });
    try {
      navigator.sendBeacon('api/history', new Blob([payload], { type: 'application/json' }));
    } catch {
      // History is best effort and must not prevent opening a result.
    }
  }

  function setSort(sortId: string) {
    if (currentSort === sortId) return;
    query = replaceSortDirective(query, sortId, config.search);
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
    // Refresh because semantic and history matches may fall outside the deleted query.
    if (connected) sendQuery(query);
    else removeMatchingResults(() => true);
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

  function setExpandSnippets(expanded: boolean) {
    expandSnippets = expanded;
    for (const state of resultStates.values()) state.snippetExpanded = undefined;
    localStorage.setItem('hister-expanded-snippets', String(expanded));
  }

  function setShowSuggestions(enabled: boolean) {
    showSuggestionsOverride = enabled;
    querySuggestionOpen = enabled;
    querySuggestionIndex = 0;
    querySuggestionKeyboardActive = false;
    localStorage.setItem('hister-show-suggestions', String(enabled));
  }

  function measureSnippet(node: HTMLElement, result: { state: ResultState; text: string }) {
    let active = true;
    let frame = 0;
    function measure() {
      if (!active) return;
      const lineHeight = parseFloat(getComputedStyle(node).lineHeight);
      result.state.snippetOverflows = node.scrollHeight > lineHeight * 3 + 1;
    }
    function scheduleMeasure() {
      if (!active) return;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    }
    const observer = new ResizeObserver(scheduleMeasure);
    observer.observe(node);
    document.fonts.ready.then(scheduleMeasure);
    return {
      update(next: typeof result) {
        result = next;
        tick().then(scheduleMeasure);
      },
      destroy() {
        active = false;
        cancelAnimationFrame(frame);
        observer.disconnect();
      },
    };
  }

  function removeMatchingResults(matches: (doc: SearchResult) => boolean) {
    if (!lastResults) return;
    lastResults = removeSearchResults({ ...lastResults, documents: accumulatedDocs }, matches);
    accumulatedDocs = lastResults.documents ?? [];
    highlightIdx = Math.min(highlightIdx, Math.max(0, totalResults - 1));
    facetsCache = new Map();
    // Discard a page fetched before deletion so it cannot restore stale results.
    latestPageRequest = 0;
    loadingMoreForQuery = '';
    if (lastResults.total === 0 && totalResults === 0) {
      hasMore = false;
      pageKey = '';
    }
  }

  function removeResult(url: string) {
    removeMatchingResults((doc) => doc.url === url);
  }

  function removeResultsByDomain(domain: string) {
    removeMatchingResults((doc) => doc.domain === domain);
  }

  // Convert file document URLs to browser viewable Hister URLs.
  function fileResultUrl(url: string, documentID?: string, title = ''): string {
    if (url.startsWith('file://') || url.startsWith('remote-file://'))
      return buildPreviewUrl(url, title, undefined, documentID);
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
    previewState.open(url, title, documentId);
  }

  function selectNthResult(n: number) {
    if (!hasResults) return;
    highlightIdx = (highlightIdx + n + totalResults) % totalResults;
    const results = document.querySelectorAll('[data-result]');
    if (results[highlightIdx]) scrollTo(results[highlightIdx]);
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
      debouncedURLUpdate.flush();
      openURL(getSearchUrl(config.searchUrl, query.substring(2)), newWindow);
      return;
    }
    if (searchPending) return;
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
    if (searchPending) return;
    previewState.toggle(openHighlightedReadable);
  }

  function openHighlightedReadable() {
    const el = document.querySelectorAll<HTMLElement>('[data-result] [data-readable]')[
      highlightIdx
    ];
    if (!el) return;
    const link = el
      .closest('[data-result]')!
      .querySelector<HTMLAnchorElement>('[data-result-link]')!;
    previewState.open(
      link.getAttribute('data-result-link')!,
      link.innerText,
      el.getAttribute('data-document-id') || '',
    );
  }

  function autocompleteQuery(e?: KeyboardEvent, isInputFocus?: boolean) {
    if (!showSuggestions) return true;
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
    debouncedURLUpdate.flush();
    openURL(getSearchUrl(config.searchUrl, query));
  }
  function focusSearchInput(e?: KeyboardEvent, isInputFocus?: boolean) {
    if (!isInputFocus) {
      if (e) e.preventDefault();
      inputEl?.focus();
    }
  }

  function closePopup(): boolean {
    if (previewState.fullscreen) {
      previewState.close();
      return true;
    }
    if (showPopup) {
      showPopup = false;
      return true;
    }
    return false;
  }

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
    if (e.defaultPrevented || isIMEKeyboardEvent(e)) return;
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

  function hideRecentSearch(q: string) {
    recentSearches = recentSearches.filter((s) => s !== q);
    localStorage.setItem(
      'deletedSearches',
      JSON.stringify([...JSON.parse(localStorage.getItem('deletedSearches') || '[]'), q]),
    );
  }

  function hideAllRecentSearches() {
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
    if (!showSuggestions || !context || !isOpen) {
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
      searchInProgress = false;
      completedSearchMessage = '';
      latestSearchRequest = 0;
      latestPageRequest = 0;
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
    if (!semanticSettingsReady) return;
    localStorage.setItem('hister-semantic-threshold', String(similarityThreshold));
    if (query && connected) sendQuery(query);
  });
  $effect(() => {
    if (!semanticSettingsReady) return;
    localStorage.setItem('hister-semantic-weight', String(semanticWeight));
  });

  $effect(() => {
    if (lastResults && !searchPending && !hasResults && previewState.fullscreen)
      previewState.exitFullscreen();
  });

  // Auto-load the readability panel for the focused result on desktop.
  // Tracks mergedResults (not just lastResults) so that reordering caused by
  // the semantic weight slider also refreshes the panel.
  // Uses data instead of DOM queries so it works when results are hidden (fullscreen mode).
  $effect(() => {
    if (searchPending) return;
    const idx = highlightIdx;
    const result = displayResults[idx]; // reactive: covers both pinned and regular results
    previewState.syncSelection(result?.url || '', result?.title || '', result?.id || '');
  });
  $effect(() => {
    // Read the query synchronously so each edit restarts the URL debounce.
    const url = buildSearchUrl();
    if (previewState.skipUrl || previewState.fullscreen) return;
    if (url === window.location.pathname + window.location.search + window.location.hash) return;
    debouncedURLUpdate.schedule();
    return debouncedURLUpdate.cancel;
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
        showSuggestions: appConfig.showSuggestions ?? false,
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
        similarityThreshold = savedSimilarityThreshold ?? config.similarityThreshold;
        semanticWeight = savedSemanticWeight ?? config.semanticWeight;
        semanticSettingsReady = true;
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
    return () => {
      wsManager?.close();
      cleanupAnimations();
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
          {hasResults
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
          aria-autocomplete={showSuggestions ? 'list' : 'none'}
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
        </div>
      </div>
      <QuerySuggestions
        id={querySuggestionListId}
        floating={false}
        open={querySuggestionsVisible}
        suggestions={querySuggestions}
        activeIndex={querySuggestionKeyboardActive ? querySuggestionIndex : -1}
        loading={querySuggestionFacetsLoading}
        onactivechange={setActiveQuerySuggestion}
        onselect={selectQuerySuggestion}
      />
      {#if activeFilterCount > 0}
        <div
          class="border-border-brand-muted flex flex-wrap items-center gap-2 border-b px-3 py-2 md:px-6"
          role="group"
          aria-label="Active filters"
        >
          {#each activeFilterChips as chip (chip.filter.start)}
            <Button
              variant="outline"
              size="sm"
              class="border-hister-indigo/40 bg-hister-indigo/5 text-hister-indigo font-inter h-auto min-h-7 max-w-full gap-1 border-2 px-2 py-1 text-xs"
              aria-label={`Remove ${chip.label} filter`}
              title={`Remove ${chip.label} filter`}
              onclick={() => removeFilters([chip.filter])}
              onkeydown={(event) => {
                if (event.key !== 'Escape') event.stopPropagation();
              }}
            >
              <span class="min-w-0 truncate">{chip.label}</span>
              <X class="size-3 shrink-0" />
            </Button>
          {/each}
          <Button
            variant="ghost"
            size="sm"
            class="font-inter text-text-brand-muted hover:text-text-brand h-7 px-2 text-xs"
            onclick={clearFilters}
            onkeydown={(event) => {
              if (event.key !== 'Escape') event.stopPropagation();
            }}
          >
            Clear filters
          </Button>
        </div>
      {/if}
    </div>
    {@render connectionNotice('mx-3 my-3 md:mx-6')}

    <div class="flex min-h-0 flex-1 overflow-hidden" bind:this={previewState.container}>
      {#if !previewState.fullscreen || !showPreviewPanel}
        <ScrollArea class="results-scroll min-h-0 min-w-0 flex-1 overflow-hidden">
          <div
            class="results-list w-full max-w-[70em] space-y-3 overflow-x-hidden px-3 py-2 md:px-6"
            class:results-list-panel={showPreviewPanel}
            aria-busy={isSearchLoading}
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
            <div
              class="results-toolbar flex min-w-0 flex-wrap items-center justify-between gap-2 px-1 py-2"
            >
              <span class="font-inter text-text-brand-muted min-w-0 text-sm font-medium">
                {#if isSearchLoading}
                  Searching…
                {:else if !searchPending && lastResults?.error}
                  Invalid query
                {:else if !searchPending && lastResults}
                  {resultCount} {resultCount === 1 ? 'result' : 'results'}
                {/if}
              </span>
              <div class="flex min-w-0 flex-wrap items-center justify-end gap-1">
                {#if showFiltersButton}
                  <DropdownMenu.Root bind:open={filtersDropdownOpen}>
                    <DropdownMenu.Trigger>
                      {#snippet child({ props })}
                        <Button
                          {...props}
                          variant="ghost"
                          size="sm"
                          class="font-inter min-h-10 gap-1 px-2 text-xs has-[>svg]:px-2 {filtersDropdownOpen ||
                          activeFilterCount > 0
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
                              {#each facetTerms(facet) as { term, count, label } (term)}
                                <button
                                  class="font-inter cursor-pointer rounded-none border-[2px] px-2 py-0.5 text-xs transition-colors {activeFilters.has(
                                    term,
                                  )
                                    ? 'border-hister-indigo bg-hister-indigo text-background'
                                    : 'border-border-brand-muted text-text-brand-secondary hover:border-hister-indigo hover:text-hister-indigo'}"
                                  onclick={() => toggleQueryToken(facet.queryField, term)}
                                  aria-pressed={activeFilters.has(term)}
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
                                <span class="font-inter text-text-brand-secondary text-xs">To:</span
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
                            {@const DateIcon = facetIcons[dateFacetDefinition.icon ?? ''] ?? Filter}
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
                            {@const DateIcon = facetIcons[dateFacetDefinition.icon ?? ''] ?? Filter}
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
                              No matching filter values. Remove an active filter to broaden your
                              search.
                            </p>
                          {/if}
                        {/if}
                      </div>
                    </DropdownMenu.Content>
                  </DropdownMenu.Root>
                {/if}
                {#if sortOptions.length > 1}
                  <DropdownMenu.Root bind:open={sortDropdownOpen}>
                    <DropdownMenu.Trigger>
                      {#snippet child({ props })}
                        <Button
                          {...props}
                          variant="ghost"
                          size="sm"
                          aria-label={`Sort: ${currentSortLabel}`}
                          title={`Sort: ${currentSortLabel}`}
                          class="font-inter min-h-10 gap-1 px-2 text-xs has-[>svg]:px-2 {sortDropdownOpen ||
                          currentSort
                            ? 'text-hister-indigo'
                            : 'text-text-brand-muted hover:text-hister-indigo'}"
                        >
                          <ArrowUpDown class="size-3" />
                          <span class="max-w-28 truncate"
                            >{currentSort ? currentSortLabel : 'Sort'}</span
                          >
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
                              onkeydown={(event) => {
                                if (event.key !== 'Escape') event.stopPropagation();
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
                {/if}
                {#if config.searchUrl}
                  <Button
                    variant="ghost"
                    size="sm"
                    class="font-inter text-text-brand-muted hover:text-hister-coral min-h-10 gap-1 px-2 text-xs has-[>svg]:px-2"
                    href={getSearchUrl(config.searchUrl, query)}
                    aria-label="Search the web"
                    title="Search the web"
                    onkeydown={(event) => {
                      if (event.key !== 'Escape') event.stopPropagation();
                    }}
                  >
                    <ExternalLink class="size-3" />
                    Web
                  </Button>
                {/if}
                <DropdownMenu.Root bind:open={actionsDropdownOpen}>
                  <DropdownMenu.Trigger>
                    {#snippet child({ props })}
                      <Button
                        {...props}
                        variant="ghost"
                        size="icon"
                        aria-label="Actions"
                        title="Actions"
                        class="font-inter {actionsDropdownOpen
                          ? 'text-hister-indigo'
                          : 'text-text-brand-muted hover:text-hister-indigo'}"
                      >
                        <Ellipsis class="size-4" />
                      </Button>
                    {/snippet}
                  </DropdownMenu.Trigger>
                  <DropdownMenu.Content
                    align="end"
                    class="border-brutal-border bg-card-surface w-80 max-w-[calc(100vw-1rem)] rounded-none border-[3px] p-3 shadow-[4px_4px_0_var(--brutal-shadow)]"
                  >
                    <div class="space-y-3">
                      <label
                        class="font-inter text-text-brand-secondary flex min-h-8 cursor-pointer items-center gap-2 text-xs"
                      >
                        <input
                          type="checkbox"
                          checked={showSuggestions}
                          onchange={(event) => setShowSuggestions(event.currentTarget.checked)}
                          onkeydown={(event) => {
                            if (event.key !== 'Escape') event.stopPropagation();
                          }}
                          class="accent-hister-indigo size-4 cursor-pointer"
                        />
                        Show suggestions
                      </label>
                      <Separator class="bg-border-brand-muted" />
                      {#if hasResults && previewState.isDesktop && !previewState.panelOpen && !disablePreviews}
                        <Button
                          variant="ghost"
                          size="sm"
                          class="font-inter text-text-brand-secondary hover:text-hister-indigo w-full justify-start gap-2 text-xs"
                          onclick={() => {
                            previewState.setPanelOpen(true);
                            actionsDropdownOpen = false;
                          }}
                          onkeydown={(event) => {
                            if (event.key !== 'Escape') event.stopPropagation();
                          }}
                        >
                          <Eye class="size-3" />
                          Show preview
                        </Button>
                        <Separator class="bg-border-brand-muted" />
                      {/if}
                      {#if config.semanticEnabled}
                        <div class="space-y-2">
                          <p
                            class="font-inter text-text-brand-muted flex items-center gap-1.5 text-xs font-semibold"
                          >
                            <Sparkles class="size-3" />
                            Semantic Search
                          </p>
                          {#if !semanticOn}
                            <Button
                              variant="outline"
                              size="sm"
                              class="font-inter text-hister-indigo border-hister-indigo border-2 text-xs"
                              onclick={() => (semanticOn = true)}
                            >
                              Enable semantic search
                            </Button>
                          {/if}
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
                              disabled={!semanticOn}
                              onkeydown={(event) => {
                                if (event.key !== 'Escape') event.stopPropagation();
                              }}
                              class="accent-hister-indigo w-full cursor-pointer disabled:cursor-not-allowed disabled:opacity-50"
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
                              disabled={!semanticOn}
                              onkeydown={(event) => {
                                if (event.key !== 'Escape') event.stopPropagation();
                              }}
                              class="accent-hister-indigo w-full cursor-pointer disabled:cursor-not-allowed disabled:opacity-50"
                            />
                          </label>
                        </div>
                        {#if hasResults}<Separator class="bg-border-brand-muted" />{/if}
                      {/if}
                      {#if hasResults}
                        <label
                          class="font-inter text-text-brand-secondary flex min-h-8 cursor-pointer items-center gap-2 text-xs"
                        >
                          <input
                            type="checkbox"
                            checked={expandSnippets}
                            onchange={(event) => setExpandSnippets(event.currentTarget.checked)}
                            onkeydown={(event) => {
                              if (event.key !== 'Escape') event.stopPropagation();
                            }}
                            class="accent-hister-indigo size-4 cursor-pointer"
                          />
                          Always expand snippets
                        </label>
                        <Separator class="bg-border-brand-muted" />
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
                      {/if}
                      {#if !searchPending && lastResults?.search_duration}
                        <p class="font-inter text-text-brand-muted text-xs">
                          Search time: {lastResults.search_duration}
                        </p>
                      {/if}
                    </div>
                  </DropdownMenu.Content>
                </DropdownMenu.Root>
              </div>
            </div>

            {#if isSearchLoading}
              <SearchLoading />
            {:else if !searchPending && lastResults?.error}
              <section class="px-4 py-8 md:px-12">
                <p role="alert" class="font-inter text-hister-rose mb-4 break-words">
                  {lastResults.error}
                </p>
                {#if lastResults.query && lastResults.query.text !== query}
                  <p class="font-inter text-text-brand-muted mb-4 text-sm break-words">
                    Expanded query: <code class="font-fira">{lastResults.query.text}</code>
                  </p>
                {/if}
                <a href="help" class="font-inter text-hister-indigo text-sm underline">
                  Query syntax help
                </a>
              </section>
            {:else if hasResults}
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
                  {@const state = getResultState(r.url, r.label)}
                  {@const snippetExpanded = state.snippetExpanded ?? expandSnippets}
                  <article
                    data-result
                    class="result-card flex w-full scroll-my-[6em] gap-3 transition-colors duration-150"
                    class:result-card-active={i === highlightIdx}
                  >
                    <div class="w-0 min-w-0 flex-1 space-y-0.5">
                      <div class="flex min-w-0 items-start gap-1.5">
                        <a
                          data-result-link={r.url}
                          href={fileResultUrl(r.url, r.id, r.title || '*title*')}
                          class="result-title font-outfit line-clamp-3 min-w-0 flex-1 text-xl leading-tight font-semibold hover:underline md:text-2xl"
                          title={r.title || '*title*'}
                          target={config.openResultsOnNewTab ? '_blank' : undefined}
                          onclick={() => {
                            debouncedURLUpdate.flush();
                            sendHistoryBeacon(r.url, r.title || '*title*', query);
                          }}
                          onauxclick={(e) => {
                            if (e.button === 1) {
                              debouncedURLUpdate.flush();
                              sendHistoryBeacon(r.url, r.title || '*title*', query);
                            }
                          }}
                        >
                          <ResultFavicon
                            favicon={r.favicon}
                            faviconKey={r.favicon_key}
                            class="float-left mt-1 mr-1.5"
                          />
                          {r.title || '*title*'}
                        </a>
                        {#if r.isPinned || r.isHistoryResult}
                          <span
                            role="img"
                            aria-label={r.isPinned ? 'Pinned result' : 'Prioritized for this query'}
                            title={r.isPinned
                              ? 'Pinned result'
                              : 'Prioritized because you opened it for this query. Use the result menu to forget it for this query.'}
                            class="text-text-brand-muted mt-1 inline-flex h-5 shrink-0 items-center"
                          >
                            {#if r.isPinned}
                              <Pin class="size-3.5" />
                            {:else}
                              <History class="size-3.5" />
                            {/if}
                          </span>
                        {/if}
                      </div>
                      <div
                        class="result-meta flex max-w-full min-w-0 flex-wrap items-center gap-x-3 gap-y-1"
                      >
                        <div
                          class="result-url-line flex max-w-full min-w-0 shrink items-center gap-3"
                        >
                          <span
                            class="result-url-text font-inter min-w-0 overflow-hidden text-xs text-ellipsis whitespace-nowrap md:text-sm"
                            title={r.url}
                          >
                            {#if r.domain}<span class="result-domain font-bold">{r.domain}</span
                              >{/if}<span class="result-path"
                              >{displayResultPath(r.url, r.domain)}</span
                            >
                          </span>
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
                              <Eye class="size-3" /><span>Preview</span>
                            </Button>
                          {/if}
                        </div>
                        {#if state.displayLabel}
                          <div
                            class="result-secondary-meta flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1"
                          >
                            <Badge
                              variant="secondary"
                              class="result-label bg-muted-surface/60 text-text-brand-secondary min-h-4 max-w-full border-0 px-1.5 py-0"
                              title={state.displayLabel}
                            >
                              <Tag class="mr-0.5 size-2.5 shrink-0" />{state.displayLabel}
                            </Badge>
                          </div>
                        {/if}
                      </div>
                      {#if r.text}
                        <p
                          id={`result-snippet-${i}`}
                          use:measureSnippet={{ state, text: r.text }}
                          class="result-snippet font-inter text-text-brand-secondary text-sm leading-[1.45] md:text-base"
                          class:line-clamp-3={!snippetExpanded}
                        >
                          {@html r.text}
                        </p>
                        {#if state.snippetOverflows}
                          <Button
                            variant="link"
                            size="sm"
                            class="font-inter text-text-brand-muted hover:text-text-brand h-auto px-0 py-1 text-xs"
                            aria-expanded={snippetExpanded}
                            aria-controls={`result-snippet-${i}`}
                            onclick={() => (state.snippetExpanded = !snippetExpanded)}
                            onkeydown={(event) => {
                              if (event.key !== 'Escape') event.stopPropagation();
                            }}
                          >
                            {snippetExpanded ? 'Show less' : 'Show more'}
                          </Button>
                        {/if}
                      {/if}
                    </div>
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
                  </article>
                {/each}
              {/if}
            {:else if !searchPending && query && lastResults}
              <section class="px-4 py-12 text-center md:px-12">
                <p class="font-inter text-text-brand-secondary mb-4">
                  No results found for "<span class="font-semibold">{query}</span>"
                </p>
                <p class="font-inter text-text-brand-muted mb-4 text-sm">
                  {activeFilterCount > 0
                    ? 'Try removing filters or changing your search terms.'
                    : 'Try different search terms or search the web.'}
                </p>
                <div class="flex flex-wrap items-center justify-center gap-3">
                  {#if activeFilterCount > 0}
                    <Button
                      variant="outline"
                      class="border-hister-indigo text-hister-indigo font-inter border-2"
                      onclick={clearFilters}
                      onkeydown={(event) => {
                        if (event.key !== 'Escape') event.stopPropagation();
                      }}
                    >
                      Clear filters
                    </Button>
                  {/if}
                  <Button
                    variant="outline"
                    class="border-hister-coral text-hister-coral hover:bg-hister-coral/10 font-inter border-[3px] font-semibold shadow-[3px_3px_0px_var(--hister-coral)]"
                    href={getSearchUrl(config.searchUrl, query)}
                  >
                    <ExternalLink class="size-4" />
                    Search the web
                  </Button>
                </div>
              </section>
            {/if}
            {#if !searchPending && (hasMore || loadingMoreForQuery)}
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
      {#if showPreviewPanel}
        {#if previewState.fullscreen}
          <PreviewPanel
            url={previewState.url}
            documentId={previewState.documentId}
            hintTitle={previewState.title}
            searchScore={panelSearchScore}
            fullscreen={true}
            onclose={previewState.close}
            onfullscreentoggle={previewState.isDesktop ? previewState.exitFullscreen : undefined}
            initialViewingVersionId={previewState.versionId}
            onviewingversionchange={previewState.setVersion}
          />
        {:else}
          <!-- Drag handle to resize the split-screen panel -->
          <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
          <div
            class="split-resize-handle shrink-0 cursor-col-resize"
            onmousedown={previewState.startResize}
            role="separator"
            aria-label="Resize preview panel"
          ></div>
          <div
            style="width: {previewState.widthPct}%; flex: none;"
            class="preview-shell bg-card-surface relative z-10 flex min-h-0 overflow-hidden"
          >
            <PreviewPanel
              url={previewState.url}
              documentId={previewState.documentId}
              hintTitle={previewState.title}
              searchScore={panelSearchScore}
              fullscreen={false}
              onclose={previewState.close}
              onfullscreentoggle={previewState.enterFullscreen}
              connected={true}
              initialViewingVersionId={previewState.versionId}
              onviewingversionchange={previewState.setVersion}
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
          aria-autocomplete={showSuggestions ? 'list' : 'none'}
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
        <div class="flex flex-wrap items-center justify-between gap-2">
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
            onclick={hideAllRecentSearches}
            title="Hide all recent searches on this browser"
          >
            Hide all on this browser
          </Button>
        </div>
        <p class="font-inter text-text-brand-muted text-xs">
          Hiding searches only affects this browser. Your history remains on the server.
        </p>
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
                aria-label="Hide recent search on this browser: {search}"
                title="Hide on this browser"
                onclick={() => hideRecentSearch(search)}
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
              Choose how to add your first searchable document.
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
            href={resolve('/add')}
            class="empty-index-action border-hister-coral bg-hister-coral/5 hover:bg-hister-coral/10"
          >
            <Link2 class="text-hister-coral size-5 shrink-0" />
            <span class="min-w-0">
              <span class="font-outfit text-text-brand block font-bold">Add manually</span>
              <span class="font-inter text-text-brand-muted block text-xs"
                >Save a URL with optional title and content</span
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
          `${resolve('/')}?q=*`,
          'text-hister-indigo',
          'Browse all indexed pages',
          History,
          displayHistoryCount,
          'pages',
          '*',
        )}
        {#if fileCount > 0}
          {@render homeStatChip(
            `${resolve('/')}?q=type:file`,
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
            `${resolve('/rules')}#indexing-rules`,
            'text-hister-teal',
            'Open indexing rules',
            Shield,
            displayRulesCount,
            'rules',
            null,
          )}
          {@render homeStatChip(
            `${resolve('/rules')}#search-aliases`,
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
    border-left: 3px solid transparent;
    background: transparent;
    margin-block: 0;
    padding: 0.2rem 0 0.2rem 0.75rem;
    box-shadow: none;
  }

  .result-card + .result-card {
    margin-block-start: 1.5rem;
  }

  .result-card:hover {
    background: color-mix(in srgb, var(--text-primary-brand) 4%, transparent);
  }

  .result-meta {
    color: var(--text-muted-brand);
  }

  .result-card-active {
    border-left-color: var(--hister-cyan);
    background: color-mix(in srgb, var(--text-primary-brand) 8%, transparent);
  }

  :global(.results-list-panel) .result-card {
    width: calc(100% - 1.5rem);
    max-width: 100%;
    overflow: hidden;
  }

  .result-title {
    color: color-mix(in srgb, var(--hister-cyan) 82%, var(--text-primary-brand));
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .result-title:hover {
    color: var(--hister-cyan);
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
    background: color-mix(in srgb, var(--hister-amber) 8%, transparent);
    font-weight: 600;
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

  :global(.dark) .result-title {
    color: color-mix(in srgb, var(--hister-cyan) 88%, white);
  }
</style>
