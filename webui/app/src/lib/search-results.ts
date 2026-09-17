// SPDX-License-Identifier: AGPL-3.0-or-later

import type { SearchResult, SearchResults, SemanticHit } from './search';

export interface MergedResult extends SearchResult {
  semanticScore?: number;
  finalScore: number;
  sourceType: 'keyword' | 'semantic' | 'both';
}

interface MergeOptions {
  semanticEnabled: boolean;
  weight: number;
  sort: string;
  userId?: number;
}

function compareStrings(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

function compareResults(a: MergedResult, b: MergedResult, sort: string): number {
  const direction = sort.startsWith('-') ? -1 : 1;
  const field = sort.replace(/^-/, '');
  const dateDifference = (b.updated ?? b.added ?? 0) - (a.updated ?? a.added ?? 0);
  let difference: number;
  // Match the ordering and tie breakers in server/indexer/searchschema.
  switch (field) {
    case 'date':
      difference = dateDifference;
      break;
    case 'visits':
      difference = (b.add_count ?? 1) - (a.add_count ?? 1) || dateDifference;
      break;
    case 'domain':
      difference = compareStrings(a.domain, b.domain);
      break;
    default:
      difference = b.finalScore - a.finalScore || dateDifference;
  }
  return direction * difference || compareStrings(a.id ?? a.url, b.id ?? b.url);
}

export function mergeSearchResults(
  docs: SearchResult[],
  hits: SemanticHit[] | undefined,
  { semanticEnabled, weight, sort, userId }: MergeOptions,
): MergedResult[] {
  if (!semanticEnabled || !hits?.length) {
    return docs.map((doc) => ({ ...doc, finalScore: doc.score ?? 0, sourceType: 'keyword' }));
  }

  const maxScore = Math.max(...docs.map((doc) => doc.score ?? 0), 1);
  const semanticScores = new Map(hits.map((hit) => [hit.doc_id, hit.similarity]));
  const merged = new Map<string, MergedResult>();

  for (const doc of docs) {
    const owner = doc.user_id ?? userId;
    const id = doc.id ?? (owner ? `${owner}:${doc.url}` : doc.url);
    const semanticScore = semanticScores.get(id) ?? semanticScores.get(doc.url);
    merged.set(doc.url, {
      ...doc,
      semanticScore,
      finalScore: (1 - weight) * ((doc.score ?? 0) / maxScore) + weight * (semanticScore ?? 0),
      sourceType: semanticScore === undefined ? 'keyword' : 'both',
    });
  }

  for (const hit of hits) {
    if (!hit.document || merged.has(hit.document.url)) continue;
    merged.set(hit.document.url, {
      ...hit.document,
      id: hit.document.id || hit.doc_id,
      semanticScore: hit.similarity,
      finalScore: weight * hit.similarity,
      sourceType: 'semantic',
    });
  }

  return [...merged.values()].sort((a, b) => compareResults(a, b, sort));
}

export function removeSearchResults(
  results: SearchResults,
  matches: (doc: SearchResult) => boolean,
): SearchResults {
  const removedURLs = new Set(
    [
      ...(results.documents ?? []),
      ...(results.history ?? []),
      ...(results.semantic_hits ?? []).flatMap((hit) => (hit.document ? [hit.document] : [])),
    ]
      .filter(matches)
      .map((doc) => doc.url),
  );
  const keepDocument = (doc: SearchResult) => !removedURLs.has(doc.url);

  return {
    ...results,
    documents: results.documents?.filter(keepDocument),
    history: results.history?.filter(keepDocument),
    semantic_hits: results.semantic_hits?.filter(
      (hit) => !removedURLs.has(hit.document?.url ?? hit.doc_id.replace(/^\d+:/, '')),
    ),
    total: results.total === undefined ? undefined : Math.max(0, results.total - removedURLs.size),
    facets: undefined,
  };
}
