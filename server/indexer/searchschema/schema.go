// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package searchschema contains the authoritative search field, filter,
// facet, and sort definitions shared by the indexer, query builder, API, and
// user interfaces.
package searchschema

import (
	"slices"
	"strings"
	"time"

	"github.com/asciimoo/nogon/server/document"
)

const Version = 3

type FieldKind string

const (
	FieldKindText         FieldKind = "text"
	FieldKindKeyword      FieldKind = "keyword"
	FieldKindEnum         FieldKind = "enum"
	FieldKindNumericRange FieldKind = "numeric_range"
	FieldKindTime         FieldKind = "time"
	FieldKindInteger      FieldKind = "integer"
	FieldKindRegexp       FieldKind = "regexp"
	FieldKindSite         FieldKind = "site"
	FieldKindExists       FieldKind = "exists"
)

type FacetKind string

const (
	FacetKindTerms         FacetKind = "terms"
	FacetKindNumericRanges FacetKind = "numeric_ranges"
	FacetKindDateRanges    FacetKind = "date_ranges"
)

type FieldDefinition struct {
	Name        string    `json:"name"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	Kind        FieldKind `json:"kind"`
	Facet       string    `json:"facet,omitempty"`
	ValueSet    string    `json:"valueSet,omitempty"`

	IndexField        string   `json:"-"`
	Aliases           []string `json:"-"`
	Weight            float64  `json:"-"`
	Visible           bool     `json:"-"`
	Phrase            bool     `json:"-"`
	NormalizeFilePath bool     `json:"-"`
	DefaultSearch     bool     `json:"-"`
	DefaultWildcard   bool     `json:"-"`
}

type ValueDefinition struct {
	Value       string `json:"value"`
	Label       string `json:"label,omitempty"`
	FacetBucket string `json:"facetBucket,omitempty"`

	Aliases []string      `json:"-"`
	Min     *float64      `json:"-"`
	Max     *float64      `json:"-"`
	Age     time.Duration `json:"-"`
}

type FacetDefinition struct {
	Name        string    `json:"name"`
	Label       string    `json:"label"`
	QueryField  string    `json:"queryField"`
	Kind        FacetKind `json:"kind"`
	Icon        string    `json:"icon,omitempty"`
	DefaultSize int       `json:"defaultSize"`
	ValueSet    string    `json:"valueSet,omitempty"`
}

type SortDefinition struct {
	Value   string `json:"value"`
	Label   string `json:"label"`
	Visible bool   `json:"visible"`
	Default bool   `json:"default,omitempty"`

	Fields  []string `json:"-"`
	ByScore bool     `json:"-"`
}

type SortCapabilities struct {
	Field       string           `json:"field"`
	Label       string           `json:"label"`
	Description string           `json:"description"`
	Options     []SortDefinition `json:"options"`
}

type Capabilities struct {
	Version   int                          `json:"version"`
	Fields    []FieldDefinition            `json:"fields"`
	Facets    []FacetDefinition            `json:"facets"`
	Sort      SortCapabilities             `json:"sort"`
	ValueSets map[string][]ValueDefinition `json:"valueSets"`
}

var timeValues = []ValueDefinition{
	{Value: "<24h", Label: "Last 24 hours", FacetBucket: "last_24h", Age: 24 * time.Hour},
	{Value: "<7d", Label: "Last 7 days", FacetBucket: "last_7d", Age: 7 * 24 * time.Hour},
	{Value: "<30d", Label: "Last 30 days", FacetBucket: "last_30d", Age: 30 * 24 * time.Hour},
	{Value: "<365d", Label: "Last year", FacetBucket: "last_year", Age: 365 * 24 * time.Hour},
	{Value: ">365d", Label: "Older", FacetBucket: "older", Age: 365 * 24 * time.Hour},
}

var valueSets = map[string][]ValueDefinition{
	"document_types": {
		{
			Value: document.Web.String(),
			Min:   new(float64(document.Web)),
			Max:   new(float64(document.Local)),
		},
		{
			Value: document.Local.String(),
			Min:   new(float64(document.Local)),
			Max:   new(float64(document.RemoteFile)),
		},
		{
			Value:   document.RemoteFile.String(),
			Aliases: []string{"remote_file"},
			Min:     new(float64(document.RemoteFile)),
			Max:     new(float64(document.RemoteFile) + 1),
		},
	},
	"visit_counts": {
		{Value: "1", Label: "1 visit", Min: new(float64(1)), Max: new(float64(2))},
		{Value: "2..4", Label: "2 to 4", Min: new(float64(2)), Max: new(float64(5))},
		{Value: "5..9", Label: "5 to 9", Min: new(float64(5)), Max: new(float64(10))},
		{Value: "10..", Label: "10 or more", Min: new(float64(10))},
	},
	"time_ranges": timeValues,
	"presence_fields": {
		{Value: "metadata.author", Label: "Metadata author"},
		{Value: "metadata.source", Label: "Metadata source"},
	},
}

var fields = []FieldDefinition{
	{Name: "domain", Label: "Domain", Description: "Limit results to one domain", Kind: FieldKindKeyword, IndexField: "domain", Weight: 8, Visible: true, DefaultWildcard: true},
	{Name: "site", Label: "Site", Description: "Match a domain and its subdomains", Kind: FieldKindSite, IndexField: "domain", Visible: true},
	{Name: "has", Label: "Has value", Description: "Find documents with a nonempty field or metadata value", Kind: FieldKindExists, ValueSet: "presence_fields", Visible: true},
	{Name: "title", Label: "Title", Description: "Search only page titles", Kind: FieldKindText, IndexField: "title", Weight: 12, Visible: true, Phrase: true, DefaultSearch: true},
	{Name: "url", Label: "URL", Description: "Search only page addresses", Kind: FieldKindKeyword, IndexField: "url", Weight: 4, Visible: true, NormalizeFilePath: true, DefaultWildcard: true},
	{Name: "url_re", Label: "URL regexp", Description: "Search page addresses with a Go regular expression", Kind: FieldKindRegexp, IndexField: "url", Weight: 4, Visible: true},
	{Name: "text", Label: "Text", Description: "Search only document content", Kind: FieldKindText, IndexField: "text", Weight: 1, Visible: true, Phrase: true, DefaultSearch: true},
	{Name: "type", Label: "Type", Description: "Filter web, local, or remote file documents", Kind: FieldKindEnum, ValueSet: "document_types", IndexField: "type", Visible: true},
	{Name: "language", Label: "Language", Description: "Filter by language tag, including more specific tags: en includes en-US; no aliases nb", Kind: FieldKindText, IndexField: "language", Weight: 1, Visible: true},
	{Name: "label", Label: "Label", Description: "Search assigned labels", Kind: FieldKindText, IndexField: "label", Weight: 1, Visible: true},
	{Name: "visits", Label: "Visits", Description: "Filter by visit count", Kind: FieldKindNumericRange, ValueSet: "visit_counts", IndexField: "add_count", Aliases: []string{"add_count"}, Visible: true},
	{Name: "updated", Label: "Updated", Description: "Filter by last update time", Kind: FieldKindTime, ValueSet: "time_ranges", IndexField: "updated", Visible: true},
	{Name: "added", Label: "Added", Description: "Filter by indexed time", Kind: FieldKindTime, ValueSet: "time_ranges", IndexField: "added", Visible: true},
	{Name: "user_id", Label: "User", Description: "Filter by document owner", Kind: FieldKindInteger, IndexField: "user_id"},
}

var facets = []FacetDefinition{
	{Name: "domains", Label: "Domains", QueryField: "domain", Kind: FacetKindTerms, Icon: "globe", DefaultSize: 10},
	{Name: "languages", Label: "Languages", QueryField: "language", Kind: FacetKindTerms, Icon: "globe", DefaultSize: 10},
	{Name: "types", Label: "Type", QueryField: "type", Kind: FacetKindNumericRanges, Icon: "tag", DefaultSize: 3},
	{Name: "visits", Label: "Visits", QueryField: "visits", Kind: FacetKindNumericRanges, Icon: "eye", DefaultSize: 4},
	{Name: "updated", Label: "Updated", QueryField: "updated", Kind: FacetKindDateRanges, Icon: "calendar", DefaultSize: 5},
}

var sortCapabilities = SortCapabilities{
	Field:       "sort",
	Label:       "Sort",
	Description: "Change result ordering",
	Options: []SortDefinition{
		{Value: "relevance", Label: "Relevance", Visible: true, Default: true, Fields: []string{"-_score", "-updated", "_id"}, ByScore: true},
		{Value: "-relevance", Label: "Least relevant", Fields: []string{"_score", "updated", "_id"}, ByScore: true},
		{Value: "visits", Label: "Most visited", Visible: true, Fields: []string{"-add_count", "-updated", "_id"}},
		{Value: "-visits", Label: "Least visited", Visible: true, Fields: []string{"add_count", "updated", "_id"}},
		{Value: "date", Label: "Date (newest first)", Visible: true, Fields: []string{"-updated", "_id"}},
		{Value: "-date", Label: "Date (oldest first)", Visible: true, Fields: []string{"updated", "_id"}},
		{Value: "domain", Label: "Domain (A to Z)", Visible: true, Fields: []string{"domain", "_id"}},
		{Value: "-domain", Label: "Domain (Z to A)", Visible: true, Fields: []string{"-domain", "_id"}},
	},
}

func Fields() []FieldDefinition {
	return slices.Clone(fields)
}

func Field(name string) (FieldDefinition, bool) {
	for _, field := range fields {
		if field.Name == name || slices.Contains(field.Aliases, name) {
			return field, true
		}
	}
	return FieldDefinition{}, false
}

// PresenceField resolves fields accepted by has: without exposing arbitrary
// internal storage fields. Metadata paths may name a leaf or a nested object.
func PresenceField(name string) (string, bool) {
	if key, ok := strings.CutPrefix(name, "metadata."); ok && key != "" {
		return name, true
	}
	field, ok := Field(name)
	if !ok {
		return "", false
	}
	switch field.Kind {
	case FieldKindText, FieldKindKeyword, FieldKindEnum, FieldKindNumericRange, FieldKindTime, FieldKindInteger:
		return field.IndexField, true
	default:
		return "", false
	}
}

func Facets() []FacetDefinition {
	return slices.Clone(facets)
}

func Facet(name string) (FacetDefinition, bool) {
	for _, facet := range facets {
		if facet.Name == name {
			return facet, true
		}
	}
	return FacetDefinition{}, false
}

func Values(name string) []ValueDefinition {
	if name == "presence_fields" {
		values := make([]ValueDefinition, 0, len(fields))
		for _, field := range fields {
			if _, ok := PresenceField(field.Name); ok && field.Visible {
				values = append(values, ValueDefinition{Value: field.Name, Label: field.Label})
			}
		}
		return append(values, valueSets[name]...)
	}
	return slices.Clone(valueSets[name])
}

func Value(valueSet, value string) (ValueDefinition, bool) {
	if valueSet == "document_types" && value == "file" {
		return ValueDefinition{
			Value: value,
			Min:   new(float64(document.Local)),
			Max:   new(float64(document.RemoteFile) + 1),
		}, true
	}
	for _, definition := range valueSets[valueSet] {
		if definition.Value == value || slices.Contains(definition.Aliases, value) {
			return definition, true
		}
	}
	return ValueDefinition{}, false
}

func (definition ValueDefinition) BucketName() string {
	if definition.FacetBucket != "" {
		return definition.FacetBucket
	}
	return definition.Value
}

func FacetValue(facetName, bucket string) (ValueDefinition, bool) {
	facet, ok := Facet(facetName)
	if !ok {
		return ValueDefinition{}, false
	}
	field, ok := Field(facet.QueryField)
	if !ok {
		return ValueDefinition{}, false
	}
	for _, definition := range valueSets[field.ValueSet] {
		if definition.BucketName() == bucket {
			return definition, true
		}
	}
	return ValueDefinition{}, false
}

func (definition ValueDefinition) RelativeTimeBounds(now time.Time) (min, max *float64, ok bool) {
	if definition.Age <= 0 || len(definition.Value) == 0 {
		return nil, nil, false
	}
	cutoff := float64(now.Add(-definition.Age).Unix())
	switch definition.Value[0] {
	case '<':
		return &cutoff, nil, true
	case '>':
		return nil, &cutoff, true
	default:
		return nil, nil, false
	}
}

func Sort(value string) SortDefinition {
	if definition, ok := LookupSort(value); ok {
		return definition
	}
	definition, _ := LookupSort("")
	return definition
}

func LookupSort(value string) (SortDefinition, bool) {
	for _, definition := range sortCapabilities.Options {
		if definition.Value == value || value == "" && definition.Default {
			definition.Fields = slices.Clone(definition.Fields)
			return definition, true
		}
	}
	return SortDefinition{}, false
}

func CapabilitiesDefinition() Capabilities {
	publicFacets := Facets()
	facetsByField := make(map[string]FacetDefinition, len(publicFacets))
	for index, facet := range publicFacets {
		field, _ := Field(facet.QueryField)
		publicFacets[index].ValueSet = field.ValueSet
		facetsByField[facet.QueryField] = publicFacets[index]
	}
	publicFields := make([]FieldDefinition, 0, len(fields))
	for _, field := range fields {
		if field.Visible {
			if facet, ok := facetsByField[field.Name]; ok {
				field.Facet = facet.Name
			}
			publicFields = append(publicFields, field)
		}
	}
	publicValues := make(map[string][]ValueDefinition, len(valueSets))
	for name := range valueSets {
		publicValues[name] = Values(name)
	}
	publicSort := sortCapabilities
	publicSort.Options = slices.Clone(sortCapabilities.Options)
	return Capabilities{
		Version:   Version,
		Fields:    publicFields,
		Facets:    publicFacets,
		Sort:      publicSort,
		ValueSets: publicValues,
	}
}
