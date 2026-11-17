export interface TimelineBucket {
  key: string;
  from?: number;
  to: number;
  count: number;
}

export interface HistoryTimeline {
  days: TimelineBucket[];
  older: TimelineBucket;
  months: TimelineBucket[];
}

export type TimelineBucketKind = 'day' | 'month';

export interface TimelinePeriodRendererProps {
  buckets: TimelineBucket[];
  colorOffset: number;
  activeKey: string;
  expandedPeriods: Set<string>;
  periodDays: Map<string, TimelineBucket[]>;
  loadingPeriods: Set<string>;
  periodErrors: Map<string, string>;
  onToggle: (bucket: TimelineBucket) => void | Promise<void>;
  onSelect: (bucket: TimelineBucket) => void;
}

const groupColors = [
  'nogon-indigo',
  'nogon-coral',
  'nogon-teal',
  'nogon-amber',
  'nogon-rose',
  'nogon-cyan',
  'nogon-lime',
];

export function getGroupColor(index: number): string {
  return groupColors[index % groupColors.length];
}

export function getColorVar(color: string): string {
  return `var(--${color})`;
}

export function formatDateLabel(timestamp: number): string {
  if (!timestamp) return 'Unknown';

  const date = new Date(timestamp * 1000);
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const yesterday = new Date(today);
  yesterday.setDate(yesterday.getDate() - 1);
  const itemDate = new Date(date.getFullYear(), date.getMonth(), date.getDate());

  if (itemDate.getTime() === today.getTime()) return 'Today';
  if (itemDate.getTime() === yesterday.getTime()) return 'Yesterday';
  return itemDate.toLocaleDateString(undefined, {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  });
}

export function timelineBucketLabel(bucket: TimelineBucket, kind: TimelineBucketKind): string {
  if (kind === 'day') return formatDateLabel(bucket.from ?? 0);

  const from = new Date((bucket.from ?? 0) * 1000);
  return from.toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
}
