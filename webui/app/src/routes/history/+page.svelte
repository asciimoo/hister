<script lang="ts">
  import {
    buildPreviewUrl,
    pushPreviewHistory,
    replacePreviewHistory,
    withSkipUrl,
    createResizeHandler,
  } from '$lib/preview';
  import { onMount, untrack } from 'svelte';
  import { fetchConfig, apiFetch, getUserId } from '$lib/api';
  import { base } from '$app/paths';
  import { page } from '$app/state';
  import { formatTimestamp, formatRelativeTime, KeyHandler, scrollTo } from '$lib/search';
  import type { HistoryItem } from '$lib/types';
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import { Badge } from '@hister/components/ui/badge';
  import { Separator } from '@hister/components/ui/separator';
  import { ScrollArea } from '@hister/components/ui/scroll-area';
  import { PageHeader } from '@hister/components';
  import {
    StatusMessage,
    PreviewPanel,
    ResultFavicon,
    TimelinePeriodChips,
    TimelinePeriodRows,
  } from '$lib/components';
  import {
    formatDateLabel,
    getColorVar,
    getGroupColor,
    timelineBucketLabel,
    type HistoryTimeline,
    type TimelineBucket,
  } from '$lib/history-timeline';
  import {
    CalendarDays,
    ChevronDown,
    ChevronRight,
    Clock,
    Eye,
    ListFilter,
    Rss,
    Search,
    Trash2,
    X,
  } from '@lucide/svelte';

  const historyPageSize = 100;
  const historyFilterRequestDelay = 150;
  const historyDisplayTextLimit = 500;
  const initialFilter = page.url.searchParams.get('filter') ?? '';

  let items: HistoryItem[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let filter = $state(initialFilter);
  let serverFilter = $state(initialFilter.trim());
  let pageKey = $state('');
  let openedLastID = $state(0);
  let openedLastUpdatedAt = $state('');
  let selectedBucket: TimelineBucket | null = $state(null);
  let timelineData: HistoryTimeline = $state({
    days: [],
    older: { key: 'older', to: 0, count: 0 },
    months: [],
  });
  let timelineLoading = $state(false);
  let timelineError = $state('');
  let olderExpanded = $state(false);
  let expandedTimelinePeriods = $state(new Set<string>());
  let timelinePeriodDays = $state(new Map<string, TimelineBucket[]>());
  let timelinePeriodLoading = $state(new Set<string>());
  let timelinePeriodErrors = $state(new Map<string, string>());
  let openedOnly = $state(
    typeof localStorage !== 'undefined'
      ? localStorage.getItem('historyOpenedOnly') === 'true'
      : false,
  );
  let historyRequestController: AbortController | undefined;
  let historyRequestID = 0;
  let timelineRequestController: AbortController | undefined;
  let timelineRequestID = 0;
  let timelinePeriodGeneration = 0;

  interface HistoryGroup {
    key: string;
    label: string;
    items: HistoryItem[];
    offset: number;
  }

  // Keyboard navigation
  let keyHandler: KeyHandler | undefined;
  let openResultsOnNewTab = $state(false);
  let highlightIdx = $state(0);

  // Preview state
  let isDesktop = $state(false);
  let panelUrl = $state('');
  let panelDocumentId = $state('');
  let panelHintTitle = $state('');
  let panelViewingVersion = $state<number | null>(null);
  let panelOpen = $state(
    typeof localStorage !== 'undefined'
      ? localStorage.getItem('hister-history-panel-open') !== 'false'
      : true,
  );
  let previewFullscreen = $state(false);
  let disablePreviews = $state(false);
  const skipUrl = { value: false };
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

  // --- History state helpers ---

  function buildHistoryPageUrl(filterValue: string = filter): string {
    const params = new URLSearchParams();
    const normalizedFilter = filterValue.trim();
    if (normalizedFilter) params.set('filter', normalizedFilter);
    const query = params.toString();
    return `${base}/history${query ? `?${query}` : ''}`;
  }

  function pushHistoryPageHistory() {
    history.pushState({ type: 'history' }, '', buildHistoryPageUrl());
  }

  function replaceHistoryPageHistory(filterValue: string) {
    history.replaceState({ type: 'history' }, '', buildHistoryPageUrl(filterValue));
  }

  $effect(() => {
    const currentFilter = filter;
    if (skipUrl.value || previewFullscreen) return;
    replaceHistoryPageHistory(currentFilter);
  });

  $effect(() => {
    const mq = window.matchMedia('(min-width: 1280px)');
    isDesktop = mq.matches;
    const handler = (e: MediaQueryListEvent) => {
      isDesktop = e.matches;
    };
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  });

  function historyDocumentId(item: HistoryItem): string {
    return typeof item.id === 'string' ? item.id : '';
  }

  function openPreview(url: string, title: string, documentId = '') {
    if (isDesktop) {
      if (!panelOpen) {
        panelOpen = true;
        localStorage.setItem('hister-history-panel-open', 'true');
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

  // The preview panel followed a link to another archived page: highlight it in the list when
  // it is one of the loaded entries, and keep the panel state (and the URL) pointing at it.
  function handlePanelNavigate(url: string, title: string) {
    const idx = items.findIndex((i) => i.url === url);
    const item = items[idx];
    panelViewingVersion = null;
    panelDocumentId = item ? historyDocumentId(item) : '';
    panelHintTitle = item ? item.title || item.url : title;
    panelUrl = url;
    if (item) {
      highlightIdx = idx;
      const el = document.querySelectorAll('[data-result]')[idx];
      if (el) scrollTo(el);
    }
    if (previewFullscreen) {
      withSkipUrl(skipUrl, () =>
        replacePreviewHistory(panelUrl, panelHintTitle, null, panelDocumentId),
      );
    }
  }

  function enterFullscreen() {
    previewFullscreen = true;
    withSkipUrl(skipUrl, () =>
      pushPreviewHistory(panelUrl, panelHintTitle, panelViewingVersion, panelDocumentId),
    );
  }

  function exitFullscreen() {
    previewFullscreen = false;
    withSkipUrl(skipUrl, () => pushHistoryPageHistory());
  }

  function closePanelAndFullscreen() {
    previewFullscreen = false;
    panelOpen = false;
    localStorage.setItem('hister-history-panel-open', 'false');
    withSkipUrl(skipUrl, () => pushHistoryPageHistory());
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
    withSkipUrl(skipUrl, () => {
      filter = new URLSearchParams(window.location.search).get('filter') ?? '';
    });
  }

  $effect(() => {
    localStorage.setItem('historyOpenedOnly', String(openedOnly));
  });

  function getDateKey(timestamp: number): string {
    if (!timestamp) {
      return 'unknown';
    }
    const date = new Date(timestamp * 1000);
    return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
  }

  function itemTimestamp(item: HistoryItem): number {
    return item.updated ?? item.added ?? 0;
  }

  $effect(() => {
    const nextFilter = filter.trim();
    if (nextFilter === serverFilter) return;
    const timer = window.setTimeout(() => {
      serverFilter = nextFilter;
    }, historyFilterRequestDelay);
    return () => window.clearTimeout(timer);
  });

  function groupByDate(sourceItems: HistoryItem[]): HistoryGroup[] {
    const result: HistoryGroup[] = [];
    const seen = new Map<string, number>();
    for (const item of sourceItems) {
      const timestamp = itemTimestamp(item);
      const key = getDateKey(timestamp);
      if (seen.has(key)) {
        result[seen.get(key)!].items.push(item);
      } else {
        seen.set(key, result.length);
        result.push({ key, label: formatDateLabel(timestamp), items: [item], offset: 0 });
      }
    }
    let offset = 0;
    for (const group of result) {
      group.offset = offset;
      offset += group.items.length;
    }
    return result;
  }

  const groups = $derived.by(() => groupByDate(items));
  const activeTimelineKey = $derived(selectedBucket?.key ?? '');

  const globalGroupColors = $derived.by(
    () => new Map(groups.map((group, idx) => [group.key, getGroupColor(idx)])),
  );

  function getGlobalGroupColor(key: string): string {
    return globalGroupColors.get(key) ?? getGroupColor(0);
  }

  function selectTimelineBucket(bucket: TimelineBucket) {
    selectedBucket = bucket;
    if (sentinel) {
      getScrollParent(sentinel.parentElement)?.scrollTo({ top: 0, behavior: 'instant' });
    }
  }

  function showAll() {
    selectedBucket = null;
    if (sentinel) {
      getScrollParent(sentinel.parentElement)?.scrollTo({ top: 0, behavior: 'instant' });
    }
  }

  const populatedMonths = $derived(timelineData.months.filter((bucket) => bucket.count > 0));
  const timelineTotalCount = $derived(
    timelineData.days.reduce((sum, bucket) => sum + bucket.count, 0) + timelineData.older.count,
  );

  function clearFilter() {
    filter = '';
  }

  function historyQueryParams(
    requestedFilter: string,
    requestedOpenedOnly: boolean,
    requestedBucket: TimelineBucket | null = null,
    includeTimezone = false,
  ): URLSearchParams {
    const params = new URLSearchParams();
    if (requestedOpenedOnly) params.set('opened', 'true');
    if (requestedFilter) params.set('filter', requestedFilter);
    if (requestedBucket) {
      if (requestedBucket.from !== undefined) {
        params.set('date_from', String(requestedBucket.from));
      }
      params.set('date_to', String(requestedBucket.to));
    }
    if (includeTimezone) {
      const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
      if (timezone) params.set('timezone', timezone);
    }
    return params;
  }

  function displayText(value: string): string {
    if (value.length <= historyDisplayTextLimit) return value;
    return value.slice(0, historyDisplayTextLimit) + '\u2026';
  }

  function groupCount(group: HistoryGroup): number {
    return group.items.length;
  }

  async function loadItems(
    latest: string = '',
    requestedFilter: string = serverFilter,
    requestedOpenedOnly: boolean = openedOnly,
    requestedBucket: TimelineBucket | null = selectedBucket,
    requestedLastUpdatedAt: string = openedLastUpdatedAt,
  ) {
    historyRequestController?.abort();
    const controller = new AbortController();
    const requestID = ++historyRequestID;
    historyRequestController = controller;
    loading = true;
    error = '';
    if (!latest) items = [];
    try {
      const cfg = await fetchConfig();
      if (controller.signal.aborted) return;
      if (!cfg.historyEnabled) {
        window.location.href = base + '/';
        return;
      }
      const params = historyQueryParams(requestedFilter, requestedOpenedOnly, requestedBucket);
      if (requestedOpenedOnly) {
        if (latest) {
          params.set('last_id', latest);
          if (requestedLastUpdatedAt) params.set('last_updated_at', requestedLastUpdatedAt);
        }
      } else if (latest) {
        params.set('last', latest);
      }
      const url = `/history${params.size > 0 ? `?${params.toString()}` : ''}`;
      const res = await apiFetch(url, {
        headers: { Accept: 'application/json' },
        signal: controller.signal,
      });
      if (!res.ok) throw new Error('Failed to load history');
      const resJSON = await res.json();
      if (controller.signal.aborted || requestID !== historyRequestID) return;
      if (resJSON && Array.isArray(resJSON.documents)) {
        const loadedCount = resJSON.documents.length;
        if (!latest) {
          items = resJSON.documents;
        } else {
          items.push(...resJSON.documents);
        }
        if (requestedOpenedOnly) {
          openedLastID = loadedCount >= historyPageSize ? (resJSON.last_id ?? 0) : 0;
          openedLastUpdatedAt =
            loadedCount >= historyPageSize ? (resJSON.last_updated_at ?? '') : '';
          pageKey = '';
        } else {
          pageKey = loadedCount >= historyPageSize ? (resJSON.page_key ?? '') : '';
          openedLastID = 0;
          openedLastUpdatedAt = '';
        }
      } else {
        if (!latest) items = [];
        pageKey = '';
        openedLastID = 0;
        openedLastUpdatedAt = '';
      }
    } catch (e) {
      if (controller.signal.aborted) return;
      error = String(e);
    } finally {
      if (requestID === historyRequestID) {
        loading = false;
        historyRequestController = undefined;
      }
    }
  }

  $effect(() => {
    const requestedOpenedOnly = openedOnly;
    const requestedFilter = serverFilter;
    const requestedBucket = selectedBucket;
    openedLastID = 0;
    openedLastUpdatedAt = '';
    pageKey = '';
    loadItems('', requestedFilter, requestedOpenedOnly, requestedBucket, '');
  });

  async function loadMore() {
    if (loading || !hasMore) return;
    if (openedOnly) {
      await loadItems(
        String(openedLastID),
        serverFilter,
        true,
        selectedBucket,
        openedLastUpdatedAt,
      );
    } else {
      await loadItems(pageKey, serverFilter, false, selectedBucket);
    }
  }

  const hasMore = $derived((!openedOnly && pageKey !== '') || (openedOnly && openedLastID > 0));

  async function loadTimeline(
    requestedFilter: string = serverFilter,
    requestedOpenedOnly: boolean = openedOnly,
  ) {
    timelineRequestController?.abort();
    const controller = new AbortController();
    const requestID = ++timelineRequestID;
    timelineRequestController = controller;
    timelineLoading = true;
    timelineError = '';
    try {
      const params = historyQueryParams(requestedFilter, requestedOpenedOnly, null, true);
      const res = await apiFetch(`/history/timeline?${params.toString()}`, {
        headers: { Accept: 'application/json' },
        signal: controller.signal,
      });
      if (!res.ok) throw new Error('Failed to load timeline');
      const data: HistoryTimeline = await res.json();
      if (controller.signal.aborted || requestID !== timelineRequestID) return;
      timelineData = data;
    } catch (e) {
      if (controller.signal.aborted) return;
      timelineError = String(e);
    } finally {
      if (requestID === timelineRequestID) {
        timelineLoading = false;
        timelineRequestController = undefined;
      }
    }
  }

  function resetTimelinePeriodDays() {
    timelinePeriodGeneration += 1;
    expandedTimelinePeriods = new Set();
    timelinePeriodDays = new Map();
    timelinePeriodLoading = new Set();
    timelinePeriodErrors = new Map();
  }

  async function loadTimelinePeriodDays(
    bucket: TimelineBucket,
    requestedFilter: string = serverFilter,
    requestedOpenedOnly: boolean = openedOnly,
  ) {
    if (!bucket.from || timelinePeriodLoading.has(bucket.key)) return;
    const generation = timelinePeriodGeneration;
    timelinePeriodLoading = new Set(timelinePeriodLoading).add(bucket.key);
    const nextErrors = new Map(timelinePeriodErrors);
    nextErrors.delete(bucket.key);
    timelinePeriodErrors = nextErrors;
    try {
      const params = historyQueryParams(requestedFilter, requestedOpenedOnly, bucket, true);
      const res = await apiFetch(`/history/timeline?${params.toString()}`, {
        headers: { Accept: 'application/json' },
      });
      if (!res.ok) throw new Error('Failed to load daily counts');
      const data: Pick<HistoryTimeline, 'days'> = await res.json();
      if (
        generation !== timelinePeriodGeneration ||
        requestedFilter !== serverFilter ||
        requestedOpenedOnly !== openedOnly
      )
        return;
      const nextDays = new Map(timelinePeriodDays);
      nextDays.set(bucket.key, data.days ?? []);
      timelinePeriodDays = nextDays;
    } catch (e) {
      if (generation !== timelinePeriodGeneration) return;
      const errors = new Map(timelinePeriodErrors);
      errors.set(bucket.key, String(e));
      timelinePeriodErrors = errors;
    } finally {
      if (generation === timelinePeriodGeneration) {
        const loadingPeriods = new Set(timelinePeriodLoading);
        loadingPeriods.delete(bucket.key);
        timelinePeriodLoading = loadingPeriods;
      }
    }
  }

  async function toggleTimelinePeriod(bucket: TimelineBucket) {
    const expanded = new Set(expandedTimelinePeriods);
    if (expanded.has(bucket.key)) {
      expanded.delete(bucket.key);
      expandedTimelinePeriods = expanded;
      return;
    }
    expanded.add(bucket.key);
    expandedTimelinePeriods = expanded;
    if (!timelinePeriodDays.has(bucket.key)) {
      await loadTimelinePeriodDays(bucket);
    }
  }

  const timelinePeriodRendererProps = $derived.by(() => ({
    activeKey: activeTimelineKey,
    expandedPeriods: expandedTimelinePeriods,
    periodDays: timelinePeriodDays,
    loadingPeriods: timelinePeriodLoading,
    periodErrors: timelinePeriodErrors,
    onToggle: toggleTimelinePeriod,
    onSelect: selectTimelineBucket,
  }));

  $effect(() => {
    const requestedOpenedOnly = openedOnly;
    const requestedFilter = serverFilter;
    resetTimelinePeriodDays();
    loadTimeline(requestedFilter, requestedOpenedOnly);
  });

  function toggleOlder() {
    olderExpanded = !olderExpanded;
  }

  const rssUrl = $derived.by(() => {
    const params = historyQueryParams(serverFilter, openedOnly, selectedBucket);
    params.set('format', 'rss');
    return `api/history?${params.toString()}`;
  });

  let sentinel: HTMLDivElement | undefined = $state();
  let sentinelVisible = $state(false);

  function getScrollParent(el: HTMLElement | null): HTMLElement | null {
    while (el && el !== document.documentElement) {
      const style = getComputedStyle(el);
      if (/auto|scroll/.test(style.overflow + style.overflowY + style.overflowX)) return el;
      el = el.parentElement;
    }
    return null;
  }

  $effect(() => {
    if (!sentinel) {
      sentinelVisible = false;
      return;
    }
    const root = getScrollParent(sentinel.parentElement);
    const observer = new IntersectionObserver(
      (entries) => {
        sentinelVisible = entries[0].isIntersecting;
      },
      { root, threshold: 0 },
    );
    observer.observe(sentinel);
    return () => {
      observer.disconnect();
      sentinelVisible = false;
    };
  });

  $effect(() => {
    const inputFilter = filter.trim();
    if (inputFilter !== serverFilter || !sentinel || !sentinelVisible || loading || !hasMore)
      return;
    loadMore();
  });

  async function deleteItem(item: HistoryItem) {
    try {
      if (openedOnly) {
        await apiFetch('/history', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ query: item.query, url: item.url, delete: true }),
        });
      } else {
        await apiFetch('/delete', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            query: 'url:' + item.url + (getUserId() !== undefined ? ' user_id:' + getUserId() : ''),
          }),
        });
      }
      items = items.filter((i) => i.url !== item.url);
      resetTimelinePeriodDays();
      await loadTimeline();
    } catch (e) {
      error = String(e);
    }
  }

  function selectNthResult(n: number) {
    if (!items.length) return;
    highlightIdx = (highlightIdx + n + items.length) % items.length;
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

  function openSelectedResult(e?: KeyboardEvent, _isInputFocus?: boolean, newWindow = false) {
    if (e) e.preventDefault();
    const item = items[highlightIdx];
    if (!item) return;
    if (openResultsOnNewTab) newWindow = true;
    window.open(historyResultUrl(item), newWindow ? '_blank' : '_self');
  }

  function historyResultUrl(item: HistoryItem): string {
    if (item.url.startsWith('remote-file://')) {
      return buildPreviewUrl(item.url, item.title || item.url, undefined, historyDocumentId(item));
    }
    return item.url;
  }

  function viewResultPopup(e?: KeyboardEvent) {
    if (e) e.preventDefault();
    const item = items[highlightIdx];
    if (!item) return;
    if (isDesktop) {
      if (previewFullscreen) {
        exitFullscreen();
      } else if (panelOpen) {
        enterFullscreen();
      } else {
        openPreview(item.url, item.title || item.url, historyDocumentId(item));
      }
    } else {
      if (previewFullscreen) {
        closePanelAndFullscreen();
      } else {
        openPreview(item.url, item.title || item.url, historyDocumentId(item));
      }
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    const target = e.target as HTMLElement;
    const isInputFocus =
      ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable;
    keyHandler?.handle(e, isInputFocus);
  }

  const hotkeyActions: Record<
    string,
    (e?: KeyboardEvent, isInputFocus?: boolean) => void | boolean
  > = {
    open_result: openSelectedResult,
    open_result_in_new_tab: (e?: KeyboardEvent, i?: boolean) => openSelectedResult(e, i, true),
    select_next_result: selectNextResult,
    select_previous_result: selectPreviousResult,
    view_result_popup: viewResultPopup,
  };

  onMount(async () => {
    const cfg = await fetchConfig();
    if (!cfg.historyEnabled) {
      window.location.href = base + '/';
      return;
    }
    openResultsOnNewTab = (cfg as any).openResultsOnNewTab ?? false;
    keyHandler = new KeyHandler((cfg as any).hotkeys ?? {}, hotkeyActions);
    disablePreviews = (cfg as any).disablePreviews ?? false;
  });

  // Reset highlight when the list changes
  $effect(() => {
    items;
    highlightIdx = 0;
  });

  // Auto-update panel on desktop when focused item changes.
  // Uses data so it works even when results are hidden in fullscreen mode.
  $effect(() => {
    const idx = highlightIdx;
    items;
    const isFullscreen = previewFullscreen;
    if (!isDesktop || !items.length || (!panelOpen && !isFullscreen)) return;
    const item = items[idx];
    if (!item) return;
    const documentId = historyDocumentId(item);
    if (untrack(() => panelUrl) === item.url && untrack(() => panelDocumentId) === documentId)
      return;
    panelViewingVersion = null;
    panelDocumentId = documentId;
    panelHintTitle = item.title || item.url;
    panelUrl = item.url;
    if (isFullscreen) {
      withSkipUrl(skipUrl, () =>
        replacePreviewHistory(item.url, item.title || item.url, null, documentId),
      );
    }
  });
</script>

<svelte:window onkeydown={handleKeydown} onpopstate={handlePopState} />

<svelte:head>
  <title>Hister - History</title>
  <link rel="alternate" type="application/rss+xml" title="Hister History" href={rssUrl} />
</svelte:head>

<header class="border-brutal-border bg-card-surface shrink-0 border-b-[3px] px-3 py-3 md:px-6">
  <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
    <div class="flex min-w-0 items-center gap-4">
      <PageHeader color="hister-indigo" size="sm" class="min-w-0 shrink-0" truncate>
        History
      </PageHeader>
      <a
        href={rssUrl}
        title="RSS feed"
        aria-label="RSS feed"
        class="text-(--text-secondary) transition-colors hover:text-[#f26522]"
      >
        <Rss size={20} />
      </a>
    </div>

    <nav class="grid min-w-0 gap-2 md:grid-cols-[auto_minmax(16rem,28rem)] md:items-center">
      <label
        class="font-inter text-text-brand-secondary flex shrink-0 cursor-pointer items-center gap-1.5 text-xs font-semibold select-none"
      >
        <input
          type="checkbox"
          bind:checked={openedOnly}
          class="accent-hister-indigo size-3.5 cursor-pointer"
        />
        <span>Show only opened results</span>
      </label>

      <div
        class="border-brutal-border bg-page-bg flex h-11 min-w-0 items-center gap-2 border-[3px] px-3 shadow-[2px_2px_0_var(--brutal-shadow)]"
      >
        <Search class="text-text-brand-muted size-4 shrink-0" />
        <Input
          bind:value={filter}
          placeholder="Filter title or URL"
          aria-label="Filter history"
          class="font-inter text-text-brand placeholder:text-text-brand-muted h-full min-w-0 flex-1 border-0 bg-transparent p-0 text-sm font-medium shadow-none focus-visible:ring-0"
        />
        {#if filter}
          <Button
            variant="ghost"
            size="icon-sm"
            class="text-text-brand-muted hover:bg-muted-surface hover:text-text-brand size-7 shrink-0"
            aria-label="Clear history filter"
            onclick={clearFilter}
          >
            <X class="size-4" />
          </Button>
        {/if}
      </div>
    </nav>
  </div>
</header>

{#if loading && items.length === 0}
  <StatusMessage message="Loading history..." type="loading" />
{:else if error}
  <StatusMessage message={error} type="error" class="mx-3 mt-4 md:mx-6" />
{:else if items.length === 0 && !selectedBucket}
  <StatusMessage message={filter ? 'No matching entries' : 'No history yet'} type="empty" />
{:else}
  <div class="flex min-h-0 flex-1 flex-col overflow-hidden md:flex-row">
    <!-- Timeline sidebar: hidden on mobile, shown on md+ -->
    {#if !previewFullscreen}
      <ScrollArea
        class="border-brutal-border bg-page-bg hidden w-72 shrink-0 border-r-[3px] md:block"
      >
        <div class="space-y-2 p-4">
          <span
            class="font-space text-text-brand-muted flex items-center gap-2 px-1 text-xs font-bold uppercase"
          >
            <CalendarDays class="size-3.5" />
            Timeline
          </span>
          <Separator class="bg-border-brand-muted h-[2px]" />

          <Button
            variant="ghost"
            class="flex h-auto w-full cursor-pointer items-center justify-start gap-2 rounded-none border-[2px] px-3 py-2 shadow-[2px_2px_0_transparent] hover:shadow-[2px_2px_0_var(--brutal-shadow)] {!activeTimelineKey
              ? 'border-brutal-border bg-hister-indigo text-primary-foreground hover:bg-hister-indigo/90 hover:text-primary-foreground'
              : 'hover:border-border-brand hover:bg-muted-surface border-transparent'}"
            onclick={showAll}
          >
            <span
              class="font-inter text-sm font-semibold"
              class:text-text-brand-secondary={!!activeTimelineKey}
            >
              Show All
            </span>
            <Badge
              variant="secondary"
              class="ml-auto h-4 border-0 px-1.5 py-0 text-xs {!activeTimelineKey
                ? 'text-primary-foreground bg-white/20'
                : 'bg-muted-surface text-text-brand-muted'}"
            >
              {timelineTotalCount.toLocaleString()}
            </Badge>
          </Button>

          <Separator class="bg-border-brand-muted h-[2px]" />

          {#each timelineData.days as bucket, i (bucket.key)}
            {@const color = getGroupColor(i)}
            {@const isActive = activeTimelineKey === bucket.key}
            <Button
              variant="ghost"
              class="flex h-auto w-full cursor-pointer items-center justify-start gap-2 rounded-none border-[2px] px-3 py-2 shadow-[2px_2px_0_transparent] hover:shadow-[2px_2px_0_var(--brutal-shadow)] {isActive
                ? 'border-brutal-border text-primary-foreground hover:text-primary-foreground'
                : 'hover:border-border-brand hover:bg-muted-surface border-transparent'}"
              style={isActive ? `background-color: ${getColorVar(color)};` : ''}
              disabled={bucket.count === 0}
              onclick={() => selectTimelineBucket(bucket)}
            >
              <span
                class="h-2 w-2 shrink-0 rounded-full"
                style={isActive
                  ? 'background-color: white;'
                  : `background-color: ${getColorVar(color)};`}
              ></span>
              <span
                class="font-inter truncate text-sm"
                class:font-semibold={isActive}
                class:font-medium={!isActive}
                class:text-text-brand-secondary={!isActive}
              >
                {timelineBucketLabel(bucket, 'day')}
              </span>
              <Badge
                variant="secondary"
                class="ml-auto h-4 shrink-0 border-0 px-1.5 py-0 text-xs {isActive
                  ? 'text-primary-foreground bg-white/20'
                  : 'bg-muted-surface text-text-brand-muted'}"
              >
                {bucket.count.toLocaleString()}
              </Badge>
            </Button>
          {/each}

          <Button
            variant="ghost"
            class="hover:border-border-brand hover:bg-muted-surface text-text-brand-secondary flex h-auto w-full cursor-pointer items-center justify-start gap-2 rounded-none border-[2px] border-transparent px-3 py-2 font-semibold"
            disabled={timelineData.older.count === 0}
            onclick={toggleOlder}
          >
            {#if olderExpanded}<ChevronDown class="size-4" />{:else}<ChevronRight
                class="size-4"
              />{/if}
            <span class="font-inter text-sm">Older</span>
            <Badge
              variant="secondary"
              class="bg-muted-surface text-text-brand-muted ml-auto h-4 border-0 px-1.5 py-0 text-xs"
            >
              {timelineData.older.count.toLocaleString()}
            </Badge>
          </Button>

          {#if olderExpanded}
            <TimelinePeriodRows
              {...timelinePeriodRendererProps}
              buckets={populatedMonths}
              colorOffset={timelineData.days.length}
            />
            {#if timelineLoading}
              <p class="font-inter text-text-brand-muted px-3 py-1 text-xs">Loading months...</p>
            {/if}
          {/if}

          {#if timelineError}
            <p class="font-inter text-hister-coral px-1 text-xs">{timelineError}</p>
          {/if}
        </div>
      </ScrollArea>
    {/if}

    <!-- Mobile timeline: horizontal scrollable filter chips -->
    {#if !previewFullscreen}
      <div class="border-brutal-border bg-page-bg shrink-0 border-b-[3px] px-3 py-2 md:hidden">
        <div class="flex items-center gap-2 overflow-x-auto">
          <span class="text-text-brand-muted flex shrink-0 items-center">
            <ListFilter class="size-4" />
          </span>
          <Button
            variant="ghost"
            size="sm"
            class="font-inter border-brutal-border h-8 shrink-0 rounded-none border-[2px] px-3 text-xs font-bold {!activeTimelineKey
              ? 'bg-hister-indigo hover:bg-hister-indigo/90 text-primary-foreground hover:text-primary-foreground'
              : 'text-text-brand-secondary hover:bg-muted-surface'}"
            onclick={showAll}
          >
            All ({timelineTotalCount.toLocaleString()})
          </Button>
          {#each timelineData.days.filter((bucket) => bucket.count > 0) as bucket, i (bucket.key)}
            {@const color = getGroupColor(i)}
            {@const isActive = activeTimelineKey === bucket.key}
            <Button
              variant="ghost"
              size="sm"
              class="font-inter border-brutal-border h-8 shrink-0 rounded-none border-[2px] px-3 text-xs font-semibold {isActive
                ? 'text-primary-foreground hover:text-primary-foreground'
                : 'text-text-brand-secondary hover:bg-muted-surface'}"
              style={isActive ? `background-color: ${getColorVar(color)};` : ''}
              onclick={() => selectTimelineBucket(bucket)}
            >
              {timelineBucketLabel(bucket, 'day')} ({bucket.count.toLocaleString()})
            </Button>
          {/each}
          {#if timelineData.older.count > 0}
            <Button
              variant="ghost"
              size="sm"
              class="font-inter border-brutal-border text-text-brand-secondary hover:bg-muted-surface h-8 shrink-0 rounded-none border-[2px] px-3 text-xs font-semibold"
              onclick={toggleOlder}
            >
              {olderExpanded
                ? 'Hide older'
                : `Older (${timelineData.older.count.toLocaleString()})`}
            </Button>
          {/if}
          {#if olderExpanded}
            <TimelinePeriodChips
              {...timelinePeriodRendererProps}
              buckets={populatedMonths}
              colorOffset={timelineData.days.length}
            />
          {/if}
        </div>
      </div>
    {/if}

    <div class="flex min-h-0 flex-1 overflow-hidden" bind:this={splitContainerEl}>
      {#if !previewFullscreen}
        <ScrollArea
          orientation="vertical"
          class="min-h-0 max-w-full min-w-0 flex-1 overflow-x-hidden"
        >
          <div
            class="mx-auto w-full max-w-5xl space-y-5 overflow-hidden px-3 py-3 md:space-y-7 md:px-6 md:py-5"
          >
            {#if groups.length === 0}
              <StatusMessage message="No entries in this period" type="empty" />
            {/if}
            {#each groups as group (group.key)}
              {@const color = getGlobalGroupColor(group.key)}
              <section id="group-{encodeURIComponent(group.key)}" class="history-group">
                <div class="history-group-header" style="--history-color: {getColorVar(color)};">
                  <div class="min-w-0">
                    <h2
                      class="font-outfit text-text-brand truncate text-base font-black md:text-lg"
                    >
                      {group.label}
                    </h2>
                    <p class="font-fira text-text-brand-muted text-xs">
                      {groupCount(group).toLocaleString()} entries
                    </p>
                  </div>
                  <span class="history-group-count">{groupCount(group).toLocaleString()}</span>
                </div>

                <div class="history-stack">
                  {#each group.items as item, ii (item)}
                    {@const itemColor = color}
                    {@const flatIdx = group.offset + ii}
                    {@const timestamp = itemTimestamp(item)}
                    <article
                      data-result
                      class="history-row flex items-start gap-3 px-3 py-3 transition-all duration-150 md:px-4"
                      class:history-row-active={flatIdx === highlightIdx}
                      style="--history-color: {getColorVar(itemColor)};"
                    >
                      <ResultFavicon
                        favicon={item.favicon}
                        faviconKey={item.favicon_key}
                        class="mt-1 md:mt-0"
                      />
                      <div class="w-0 min-w-0 flex-1 space-y-1">
                        <a
                          data-result-link={item.url}
                          href={historyResultUrl(item)}
                          class="history-title font-outfit text-hister-cyan line-clamp-2 text-base font-bold no-underline hover:underline md:text-lg"
                          title={item.title || item.url}
                          target="_blank"
                          rel="noopener"
                          onclick={() => (highlightIdx = flatIdx)}
                        >
                          {displayText(item.title || item.url).replace(/<[^>]*>/g, '')}
                        </a>
                        <div
                          class="flex min-w-0 flex-col gap-1 md:flex-row md:items-center md:gap-2"
                        >
                          {#if timestamp}
                            <span
                              class="font-inter bg-muted-surface text-text-brand-secondary w-fit px-1.5 py-0.5 text-xs font-semibold whitespace-nowrap"
                              title={formatTimestamp(timestamp)}
                              >{formatRelativeTime(timestamp)}</span
                            >
                          {/if}
                          {#if (item.add_count ?? 0) > 1}
                            <span
                              class="font-inter bg-muted-surface text-text-brand-secondary w-fit px-1.5 py-0.5 text-xs font-semibold whitespace-nowrap"
                              title={`Added ${item.add_count?.toLocaleString()} times`}
                              >x{item.add_count?.toLocaleString()}</span
                            >
                          {/if}
                          <span
                            class="history-url font-fira text-text-brand-muted line-clamp-1 min-w-0 flex-1 text-xs md:text-sm"
                            title={item.url}>{displayText(item.url)}</span
                          >
                        </div>
                      </div>
                      <nav class="flex shrink-0 items-center gap-1 self-start">
                        {#if !disablePreviews}
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            class="text-text-brand-muted hover:bg-muted-surface hover:text-hister-teal size-8 shrink-0"
                            aria-label="Preview entry"
                            title="Preview"
                            onclick={() => {
                              highlightIdx = flatIdx;
                              openPreview(
                                item.url,
                                item.title || item.url,
                                historyDocumentId(item),
                              );
                            }}
                          >
                            <Eye class="size-3.5" />
                          </Button>
                        {/if}
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          class="text-text-brand-muted hover:bg-muted-surface hover:text-hister-rose size-8 shrink-0"
                          aria-label="Delete entry"
                          title="Delete"
                          onclick={() => deleteItem(item)}
                        >
                          <Trash2 class="size-3.5" />
                        </Button>
                      </nav>
                    </article>
                  {/each}
                </div>
              </section>
            {/each}
          </div>
          {#if loading && items.length > 0}
            <div class="flex justify-center py-4">
              <span class="font-inter text-text-brand-muted animate-pulse text-xs">Loading...</span>
            </div>
          {/if}
          <div bind:this={sentinel} class="h-px w-full"></div>
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
            onnavigate={handlePanelNavigate}
          />
        {:else if panelOpen && isDesktop}
          <!-- Drag handle to resize the split-screen panel -->
          <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
          <div
            class="hover:bg-hister-indigo/40 w-1.5 shrink-0 cursor-col-resize bg-transparent transition-colors"
            onmousedown={startPanelResize}
            role="separator"
            aria-label="Resize preview panel"
          ></div>
          <div
            style="width: min({panelWidthPct}%, max(0px, calc(100% - 28rem))); flex: none;"
            class="flex min-h-0 overflow-hidden"
          >
            <PreviewPanel
              url={panelUrl}
              documentId={panelDocumentId}
              hintTitle={panelHintTitle}
              fullscreen={false}
              onclose={() => {
                panelOpen = false;
                localStorage.setItem('hister-history-panel-open', 'false');
              }}
              onfullscreentoggle={enterFullscreen}
              initialViewingVersionId={panelViewingVersion}
              onviewingversionchange={(id) => {
                panelViewingVersion = id;
              }}
              onnavigate={handlePanelNavigate}
            />
          </div>
        {/if}
      {/if}
    </div>
  </div>
{/if}

<style>
  .history-group {
    min-width: 0;
  }

  .history-group-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.5rem 0 0.65rem;
    border-bottom: 3px solid var(--history-color);
  }

  .history-group-count {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 2rem;
    height: 1.55rem;
    padding: 0 0.45rem;
    flex-shrink: 0;
    color: var(--text-primary-brand);
    font-family: var(--font-fira);
    font-size: 0.72rem;
    font-weight: 700;
    background: color-mix(in srgb, var(--history-color) 18%, var(--muted-surface));
    border: 2px solid color-mix(in srgb, var(--history-color) 55%, var(--border-brand));
  }

  .history-stack {
    padding-top: 0.75rem;
  }

  .history-row {
    border: 2px solid var(--border-muted-brand);
    background-color: var(--card-surface);
    box-shadow: 0 1px 0 color-mix(in srgb, white 6%, transparent) inset;
  }

  .history-title,
  .history-url {
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .history-row + .history-row {
    border-top: 0;
  }

  .history-row-active {
    border-color: color-mix(in srgb, var(--history-color) 60%, var(--border-brand));
    background:
      linear-gradient(
        90deg,
        color-mix(in srgb, var(--history-color) 12%, transparent),
        transparent 42%
      ),
      var(--card-surface);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--history-color) 32%, transparent) inset;
  }

  :global(.dark) .history-row {
    box-shadow: 0 1px 0 color-mix(in srgb, white 8%, transparent) inset;
  }
</style>
