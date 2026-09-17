// SPDX-License-Identifier: AGPL-3.0-or-later

export interface QueryFilter {
  start: number;
  end: number;
  field: string;
  value: string;
  negated: boolean;
}

// Keep quoted phrases and grouped expressions intact when editing standalone filters.
export function queryFilters(query: string, fields: string[]): QueryFilter[] {
  const allowedFields = new Set(fields);
  const filters: QueryFilter[] = [];
  let start = 0;
  while (start < query.length) {
    if (/\s/.test(query[start])) {
      start += 1;
      continue;
    }
    let end = start;
    let quoted = false;
    let escaped = false;
    let depth = 0;
    for (; end < query.length; end += 1) {
      const char = query[end];
      if (escaped) {
        escaped = false;
        continue;
      }
      if (char === '\\') {
        escaped = true;
        continue;
      }
      if (char === '"') quoted = !quoted;
      if (!quoted) {
        if (char === '(') depth += 1;
        if (char === ')') depth -= 1;
        if (/\s/.test(char) && depth === 0) break;
      }
    }
    const token = query.slice(start, end);
    const match = token.match(/^(-?)([a-z_]+):(.+)$/s);
    if (!quoted && depth === 0 && match && allowedFields.has(match[2])) {
      const rawValue = match[3];
      if (!rawValue.startsWith('(')) {
        let value = '';
        let valueEscaped = false;
        for (const char of rawValue) {
          if (valueEscaped) {
            value += char;
            valueEscaped = false;
          } else if (char === '\\') {
            valueEscaped = true;
          } else if (char !== '"') {
            value += char;
          }
        }
        if (value) {
          filters.push({ start, end, field: match[2], value, negated: match[1] === '-' });
        }
      }
    }
    start = end;
  }
  return filters;
}

export function removeQueryFilters(query: string, filters: QueryFilter[]): string {
  let remaining = query;
  for (const filter of [...filters].sort((a, b) => b.start - a.start)) {
    const before = remaining.slice(0, filter.start).trimEnd();
    const after = remaining.slice(filter.end).trimStart();
    remaining = [before, after].filter(Boolean).join(' ');
  }
  return remaining.trim();
}

export function toggleQueryFilter(query: string, field: string, value: string): string {
  const matches = queryFilters(query, [field]).filter(
    (filter) => !filter.negated && filter.value === value,
  );
  if (matches.length > 0) return removeQueryFilters(query, matches) || '*';
  const escapedValue = /[\s"\\()]/.test(value) ? `"${value.replace(/["\\]/g, '\\$&')}"` : value;
  return `${query.trim()} ${field}:${escapedValue}`.trim();
}
