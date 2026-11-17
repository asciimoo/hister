// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package searchschema

import (
	"testing"

	"github.com/asciimoo/nogon/server/document"
)

func TestDefinitionsAreInternallyConsistent(t *testing.T) {
	seenFields := make(map[string]bool)
	for _, field := range Fields() {
		if seenFields[field.Name] {
			t.Fatalf("duplicate field %q", field.Name)
		}
		seenFields[field.Name] = true
		if field.IndexField == "" && field.Kind != FieldKindExists {
			t.Fatalf("field %q has no index field", field.Name)
		}
		if field.ValueSet != "" && len(Values(field.ValueSet)) == 0 {
			t.Fatalf("field %q references empty value set %q", field.Name, field.ValueSet)
		}
	}

	seenFacets := make(map[string]bool)
	for _, facet := range Facets() {
		if seenFacets[facet.Name] {
			t.Fatalf("duplicate facet %q", facet.Name)
		}
		seenFacets[facet.Name] = true
		field, ok := Field(facet.QueryField)
		if !ok {
			t.Fatalf("facet %q references unknown query field %q", facet.Name, facet.QueryField)
		}
		if field.ValueSet != "" && len(Values(field.ValueSet)) == 0 {
			t.Fatalf("facet %q references field with empty value set %q", facet.Name, field.ValueSet)
		}
	}

	defaultSorts := 0
	seenSorts := make(map[string]bool)
	for _, sort := range CapabilitiesDefinition().Sort.Options {
		if seenSorts[sort.Value] {
			t.Fatalf("duplicate sort %q", sort.Value)
		}
		seenSorts[sort.Value] = true
		if len(sort.Fields) == 0 {
			t.Fatalf("sort %q has no Bleve fields", sort.Value)
		}
		if sort.Default {
			defaultSorts++
		}
	}
	if defaultSorts != 1 {
		t.Fatalf("default sort count = %d, want 1", defaultSorts)
	}
}

func TestCapabilitiesContainOnlyVisibleFields(t *testing.T) {
	capabilities := CapabilitiesDefinition()
	if capabilities.Version != Version {
		t.Fatalf("capability version = %d, want %d", capabilities.Version, Version)
	}
	for _, field := range capabilities.Fields {
		if !field.Visible {
			t.Fatalf("hidden field %q exposed in capabilities", field.Name)
		}
	}
	fieldsByName := make(map[string]FieldDefinition, len(capabilities.Fields))
	for _, field := range capabilities.Fields {
		fieldsByName[field.Name] = field
	}
	for _, facet := range capabilities.Facets {
		field := fieldsByName[facet.QueryField]
		if field.Facet != facet.Name {
			t.Fatalf("public field %q references facet %q, want %q", field.Name, field.Facet, facet.Name)
		}
		if facet.ValueSet != field.ValueSet {
			t.Fatalf("public facet %q value set = %q, want %q", facet.Name, facet.ValueSet, field.ValueSet)
		}
	}
	if _, ok := Field("user_id"); !ok {
		t.Fatal("internal user_id field is missing")
	}
	for _, field := range capabilities.Fields {
		if field.Name == "user_id" {
			t.Fatal("internal user_id field is exposed")
		}
	}
}

func TestSortFallsBackToDefault(t *testing.T) {
	defaultSort := Sort("")
	if !defaultSort.Default {
		t.Fatalf("Sort with empty value returned %q, want default", defaultSort.Value)
	}

	unknownSort := Sort("unknown")
	if unknownSort.Value != defaultSort.Value {
		t.Fatalf("Sort with unknown value returned %q, want %q", unknownSort.Value, defaultSort.Value)
	}

	if _, ok := LookupSort("unknown"); ok {
		t.Fatal("LookupSort accepted an unknown value")
	}
}

func TestDocumentTypeValues(t *testing.T) {
	remote, ok := Value("document_types", "remote")
	if !ok || remote.Min == nil || remote.Max == nil || *remote.Min != float64(document.RemoteFile) || *remote.Max != float64(document.RemoteFile)+1 {
		t.Fatalf("remote type range = %#v", remote)
	}

	remoteAlias, ok := Value("document_types", "remote_file")
	if !ok || remoteAlias.Value != "remote" {
		t.Fatalf("remote_file alias = %#v, %v", remoteAlias, ok)
	}

	file, ok := Value("document_types", "file")
	if !ok || file.Min == nil || file.Max == nil || *file.Min != float64(document.Local) || *file.Max != float64(document.RemoteFile)+1 {
		t.Fatalf("file aggregate range = %#v", file)
	}

	for _, value := range Values("document_types") {
		if value.Value == "file" {
			t.Fatal("aggregate file filter must not appear as an overlapping facet bucket")
		}
	}
}

func TestConvenienceFilterCapabilities(t *testing.T) {
	capabilities := CapabilitiesDefinition()
	visible := make(map[string]FieldDefinition)
	for _, field := range capabilities.Fields {
		visible[field.Name] = field
	}
	if site, ok := visible["site"]; !ok || site.Description == "" {
		t.Error("site filter is missing from search capabilities")
	}
	has, ok := visible["has"]
	if !ok {
		t.Fatal("has filter is missing from search capabilities")
	}
	values := make(map[string]bool)
	for _, value := range capabilities.ValueSets[has.ValueSet] {
		values[value.Value] = true
	}
	for _, value := range []string{"label", "title", "visits", "metadata.author"} {
		if !values[value] {
			t.Errorf("has filter does not suggest %q", value)
		}
	}
	for _, value := range []string{"has", "site", "url_re", "user_id"} {
		if values[value] {
			t.Errorf("has filter suggests nonpublic or virtual field %q", value)
		}
	}
}
