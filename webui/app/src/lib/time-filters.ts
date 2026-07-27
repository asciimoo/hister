export const DATE_BUCKET_FILTERS: Record<string, string> = {
  last_24h: '<24h',
  last_7d: '<7d',
  last_30d: '<30d',
  last_year: '<365d',
  older: '>365d',
};

const updatedTimeFilterPattern =
  /(?:^|\s)updated:(<=|>=|<|>)(\d+[smhdwSMHDW]|\d{4}-\d{2}-\d{2})(?=\s|$)/g;

interface UpdatedTimeFilter {
  comparison: string;
  value: string;
}

export function shiftISODate(value: string, days: number): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return '';
  const date = new Date(`${value}T00:00:00Z`);
  if (Number.isNaN(date.getTime()) || date.toISOString().slice(0, 10) !== value) return '';
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}

function isValidUpdatedTimeValue(value: string): boolean {
  return /^\d+[smhdw]$/i.test(value) || shiftISODate(value, 0) !== '';
}

function isInsideQuotedText(text: string, offset: number): boolean {
  return [...text.slice(0, offset).matchAll(/(?<!\\)(?:\\\\)*"/g)].length % 2 === 1;
}

export function updatedTimeFilters(text: string): UpdatedTimeFilter[] {
  return [...text.matchAll(updatedTimeFilterPattern)]
    .filter((match) => {
      const tokenOffset = (match.index ?? 0) + match[0].indexOf('updated:');
      return !isInsideQuotedText(text, tokenOffset) && isValidUpdatedTimeValue(match[2]);
    })
    .map((match) => ({ comparison: match[1], value: match[2] }));
}

export function removeUpdatedTimeFilters(text: string): string {
  return text
    .replace(updatedTimeFilterPattern, (match, _comparison, value, offset) => {
      const tokenOffset = offset + match.indexOf('updated:');
      return !isInsideQuotedText(text, tokenOffset) && isValidUpdatedTimeValue(value) ? ' ' : match;
    })
    .replace(/\s+/g, ' ')
    .trim();
}

export function replaceUpdatedTimeFilters(text: string, filters: string[]): string {
  const remaining = removeUpdatedTimeFilters(text);
  const tokens = filters.map((filter) => `updated:${filter}`);
  return [...(remaining ? [remaining] : []), ...tokens].join(' ');
}

export function customDatesFromQuery(text: string): { from: string; to: string } {
  let from = '';
  let to = '';
  for (const filter of updatedTimeFilters(text)) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(filter.value)) continue;
    if (filter.comparison === '>=') from = filter.value;
    if (filter.comparison === '<') to = shiftISODate(filter.value, -1);
  }
  return { from, to };
}
