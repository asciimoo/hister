import { writable } from 'svelte/store';
import type { AppConfig } from '#lib/api.js';

export const showHelp = writable(false);
export const publicConfig = writable<AppConfig | null>(null);
