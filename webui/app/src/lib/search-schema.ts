// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

export type SearchFieldKind =
  'text' | 'keyword' | 'enum' | 'numeric_range' | 'time' | 'integer' | 'regexp';

export type SearchFacetKind = 'terms' | 'numeric_ranges' | 'date_ranges';

export interface SearchFieldDefinition {
  name: string;
  label: string;
  description: string;
  kind: SearchFieldKind;
  facet?: string;
  valueSet?: string;
}

export interface SearchValueDefinition {
  value: string;
  label?: string;
  facetBucket?: string;
}

export interface SearchFacetDefinition {
  name: string;
  label: string;
  queryField: string;
  kind: SearchFacetKind;
  icon?: string;
  defaultSize: number;
  valueSet?: string;
}

export interface SearchSortDefinition {
  value: string;
  label: string;
  visible: boolean;
  default?: boolean;
}

export interface SearchSortCapabilities {
  field: string;
  label: string;
  description: string;
  options: SearchSortDefinition[];
}

export interface SearchCapabilities {
  version: number;
  fields: SearchFieldDefinition[];
  facets: SearchFacetDefinition[];
  sort: SearchSortCapabilities;
  valueSets: Record<string, SearchValueDefinition[]>;
}

export function emptySearchCapabilities(): SearchCapabilities {
  return {
    version: 0,
    fields: [],
    facets: [],
    sort: { field: '', label: '', description: '', options: [] },
    valueSets: {},
  };
}

export function valuesForField(
  capabilities: SearchCapabilities,
  field: SearchFieldDefinition,
): SearchValueDefinition[] {
  return field.valueSet ? (capabilities.valueSets[field.valueSet] ?? []) : [];
}

export function valuesForFacet(
  capabilities: SearchCapabilities,
  facet: SearchFacetDefinition,
): SearchValueDefinition[] {
  return facet.valueSet ? (capabilities.valueSets[facet.valueSet] ?? []) : [];
}
