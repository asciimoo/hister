// SPDX-License-Identifier: AGPL-3.0-or-later

import { tick } from 'svelte';
import { base } from '$app/paths';

const previewDetailsOpenStorageKey = 'hister-preview-details-open';

/** Builds the preview page URL for a document. */
export function buildPreviewUrl(
  id: string,
  title: string,
  versionId?: number | null,
  documentId?: string,
): string {
  const versionParam = versionId != null ? `&version=${versionId}` : '';
  const documentParam = documentId ? `&document_id=${encodeURIComponent(documentId)}` : '';
  return `${base}/preview?id=${encodeURIComponent(id)}${title ? '&title=' + encodeURIComponent(title) : ''}${versionParam}${documentParam}`;
}

/** Position of a history entry inside a preview panel navigation flow (see PreviewPanel). */
export interface PanelNavTag {
  flow: string;
  index: number;
}

/** Returns the panel navigation tag of the current browser history entry, if any. */
export function currentPanelNavTag(): PanelNavTag | null {
  return (history.state as { panelNav?: PanelNavTag } | null)?.panelNav ?? null;
}

/** Pushes a preview entry onto the browser history stack. */
export function pushPreviewHistory(
  id: string,
  title: string,
  versionId?: number | null,
  documentId?: string,
  panelNav?: PanelNavTag,
) {
  history.pushState(
    {
      type: 'preview',
      id,
      title,
      versionId: versionId ?? null,
      documentId: documentId ?? '',
      panelNav,
    },
    '',
    buildPreviewUrl(id, title, versionId, documentId),
  );
}

/**
 * Replaces the current browser history entry with a preview entry.
 * Without an explicit `panelNav`, the entry keeps its panel navigation tag as long as it still
 * shows the same document (e.g. when only the viewed version changes).
 */
export function replacePreviewHistory(
  id: string,
  title: string,
  versionId?: number | null,
  documentId?: string,
  panelNav?: PanelNavTag,
) {
  const previous = history.state as { id?: string; panelNav?: PanelNavTag } | null;
  history.replaceState(
    {
      type: 'preview',
      id,
      title,
      versionId: versionId ?? null,
      documentId: documentId ?? '',
      panelNav: panelNav ?? (previous?.id === id ? previous.panelNav : undefined),
    },
    '',
    buildPreviewUrl(id, title, versionId, documentId),
  );
}

/** Returns whether document details should be expanded in preview panels. */
export function getStoredPreviewDetailsOpen(): boolean {
  return localStorage.getItem(previewDetailsOpenStorageKey) === 'true';
}

/** Persists the document details disclosure preference across preview panels. */
export function setStoredPreviewDetailsOpen(open: boolean): void {
  localStorage.setItem(previewDetailsOpenStorageKey, String(open));
}

/**
 * Wraps a history-mutating function so that reactive URL-update effects
 * are suppressed while it runs.
 *
 * @param skip  A mutable ref `{ value: boolean }` owned by the component.
 *              Set to `true` before `fn` runs, reset after the next tick.
 * @param fn    The history mutation to execute.
 */
export function withSkipUrl(skip: { value: boolean }, fn: () => void): void {
  skip.value = true;
  fn();
  tick().then(() => {
    skip.value = false;
  });
}

/**
 * Creates a mouse-drag resize handler for a panel on the right side of a flex container.
 * The panel width is expressed as a percentage of the container's total width.
 *
 * @param opts.getContainer  Returns the flex container element.
 * @param opts.onWidth       Called continuously during drag with the new percentage.
 * @param opts.onDone        Called once on mouseup with the final percentage (use for persistence).
 * @param opts.min           Minimum percentage (default 15).
 * @param opts.max           Maximum percentage (default 85).
 */
export function createResizeHandler(opts: {
  getContainer: () => HTMLElement | undefined;
  onWidth: (pct: number) => void;
  onDone: (pct: number) => void;
  min?: number;
  max?: number;
}): (e: MouseEvent) => void {
  return function startResize(e: MouseEvent) {
    e.preventDefault();
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    let lastPct = 50;
    const onMove = (e: MouseEvent) => {
      const container = opts.getContainer();
      if (!container) return;
      const rect = container.getBoundingClientRect();
      const fromRight = rect.right - e.clientX;
      lastPct = Math.min(opts.max ?? 85, Math.max(opts.min ?? 15, (fromRight / rect.width) * 100));
      opts.onWidth(lastPct);
    };
    const onUp = () => {
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      opts.onDone(lastPct);
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  };
}
