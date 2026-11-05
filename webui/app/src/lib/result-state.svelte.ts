// SPDX-License-Identifier: AGPL-3.0-or-later

import { apiFetch, getUserId } from './api';
import { deleteDocuments, previewDocumentDeletion } from './document-delete';
import { fetchRules, saveRuleLists } from './rules';
import { buildUrlSkipPattern, buildDomainSkipPattern } from '@hister/components';

interface AddSkipRuleOptions {
  url: string;
  domain: string;
  type: 'url' | 'domain';
  deleteMatches: boolean;
  removeResult: (url: string) => void;
  removeResultsByDomain: (domain: string) => void;
  confirmDeletion: (matched: number) => Promise<boolean>;
}

async function saveSkipRule(pattern: string): Promise<void> {
  const rules = await fetchRules();
  if (rules.skip.includes(pattern)) return;
  await saveRuleLists({ ...rules, skip: [...rules.skip, pattern] });
}

function skipRuleDeleteQuery(url: string, domain: string, type: 'url' | 'domain'): string {
  const uid = getUserId();
  const userFilter = uid === undefined ? '' : ` user_id:${uid}`;
  return type === 'url'
    ? `url:"${url.replaceAll('"', '\\"')}"${userFilter}`
    : `domain:${domain}${userFilter}`;
}

async function deleteSkipRuleDocuments(
  query: string,
  confirmDeletion: (matched: number) => Promise<boolean>,
  onDeleted: () => void,
): Promise<string> {
  const matched = await previewDocumentDeletion(query);
  if (matched === 0) return 'Skip rule added. No matching documents found.';
  if (!(await confirmDeletion(matched))) {
    return 'Skip rule added. Matching documents were not deleted.';
  }
  const deleted = await deleteDocuments(query);
  onDeleted();
  return `Skip rule added. ${deleted} matching document${deleted === 1 ? '' : 's'} deleted.`;
}

export class ResultState {
  snippetExpanded = $state<boolean | undefined>(undefined);
  snippetOverflows = $state(false);

  labelInput = $state('');
  labelMessage = $state<string | null>(null);
  labelError = $state(false);
  displayLabel = $state<string | undefined>(undefined);

  actionsQuery = $state('');
  actionsMessage = $state<string | null>(null);
  actionsError = $state(false);

  constructor(initialLabel?: string) {
    this.displayLabel = initialLabel || undefined;
    this.labelInput = initialLabel ?? '';
  }

  onOpen() {
    this.actionsMessage = null;
    this.actionsError = false;
    this.labelMessage = null;
    this.labelError = false;
  }

  async copyURL(url: string) {
    this.actionsMessage = null;
    this.actionsError = false;
    try {
      await navigator.clipboard.writeText(url);
      this.actionsMessage = 'URL copied.';
    } catch {
      this.actionsMessage = 'Could not copy URL. Please try again.';
      this.actionsError = true;
    }
  }

  async updateLabel(url: string) {
    this.labelMessage = null;
    this.labelError = false;
    const label = this.labelInput;
    try {
      const res = await apiFetch('/label', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url, label }),
      });
      if (!res.ok) throw new Error('Failed to save label');
      this.displayLabel = label || undefined;
      this.labelMessage = label ? 'Label saved.' : 'Label cleared.';
    } catch {
      this.labelMessage = 'Failed to save label.';
      this.labelError = true;
    }
  }

  async pin(url: string, title: string, currentQuery: string, remove = false) {
    const q = this.actionsQuery || currentQuery;
    if (!q) return;
    try {
      const res = await apiFetch('/history', {
        method: 'POST',
        headers: { 'Content-type': 'application/json; charset=UTF-8' },
        body: JSON.stringify({ url, title, query: q, pin: !remove }),
      });
      if (!res.ok) throw new Error('Failed to update priority');
      this.actionsMessage = `Priority result ${remove ? 'removed' : 'added'}.`;
      this.actionsError = false;
    } catch {
      this.actionsMessage = 'Failed to update priority.';
      this.actionsError = true;
    }
  }

  async forgetForQuery(url: string, query: string): Promise<boolean> {
    if (!query) return false;
    try {
      const res = await apiFetch('/history', {
        method: 'POST',
        headers: { 'Content-type': 'application/json; charset=UTF-8' },
        body: JSON.stringify({ url, query, delete: true }),
      });
      if (!res.ok) throw new Error('Failed to forget result for query');
      this.actionsMessage = 'Result forgotten for this query.';
      this.actionsError = false;
      return true;
    } catch {
      this.actionsMessage = 'Failed to forget result for this query.';
      this.actionsError = true;
      return false;
    }
  }

  async addSkipRule(options: AddSkipRuleOptions) {
    const {
      url,
      domain,
      type,
      deleteMatches,
      removeResult,
      removeResultsByDomain,
      confirmDeletion,
    } = options;
    this.actionsMessage = null;
    this.actionsError = false;
    const pattern = type === 'url' ? buildUrlSkipPattern(url) : buildDomainSkipPattern(url);
    try {
      await saveSkipRule(pattern);
    } catch {
      this.actionsMessage = 'Failed to add skip rule.';
      this.actionsError = true;
      return;
    }
    if (!deleteMatches) {
      this.actionsMessage = 'Skip rule added.';
      return;
    }
    try {
      const removeMatches =
        type === 'url' ? () => removeResult(url) : () => removeResultsByDomain(domain);
      this.actionsMessage = await deleteSkipRuleDocuments(
        skipRuleDeleteQuery(url, domain, type),
        confirmDeletion,
        removeMatches,
      );
    } catch {
      this.actionsMessage = 'Skip rule added, but matching documents could not be deleted.';
      this.actionsError = true;
    }
  }
}
