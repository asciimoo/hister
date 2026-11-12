package server

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This file pins the MCP endpoint's client-facing wire contract: advertised tool
// schemas, invalid-argument handling, the result envelope and trust boundary,
// and the initialize response. Change these expectations only for deliberate
// contract changes.

func mcpSchemaObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("schema value = %#v, want object", value)
	}
	return object
}

func mcpSchemaProperty(t *testing.T, schema any, name string) map[string]any {
	t.Helper()
	properties := mcpSchemaObject(t, mcpSchemaObject(t, schema)["properties"])
	property, ok := properties[name]
	if !ok {
		t.Fatalf("schema has no %q property", name)
	}
	return mcpSchemaObject(t, property)
}

func mcpStringList(t *testing.T, value any) []string {
	t.Helper()
	values, ok := value.([]any)
	if !ok {
		t.Fatalf("value = %#v, want array", value)
	}
	out := make([]string, len(values))
	for i, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("value[%d] = %#v, want string", i, value)
		}
		out[i] = text
	}
	return out
}

func mcpUntypedJSON(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %#v: %v", value, err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return out
}

func mcpTextFallbackJSON(t *testing.T, text string) any {
	t.Helper()
	start := strings.IndexByte(text, '{')
	if start < 0 {
		t.Fatalf("text fallback has no JSON: %q", text)
	}
	decoder := json.NewDecoder(strings.NewReader(text[start:]))
	var out any
	if err := decoder.Decode(&out); err != nil {
		t.Fatalf("text fallback JSON: %v", err)
	}
	if decoder.More() {
		t.Fatalf("text fallback has trailing data after JSON: %q", text[start:])
	}
	return out
}

func mcpToolByName(t *testing.T, tools []*mcp.Tool, name string) *mcp.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not advertised", name)
	return nil
}

func TestMCPSchemaContract(t *testing.T) {
	cfg, handler := newTokenTestServer(t, false)
	session := newMCPTestSession(t, cfg, handler, "secret")
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if want := []string{"get_facets", "get_history", "get_preview", "search"}; !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}

	required := map[string][]string{
		"search":      {"query"},
		"get_facets":  nil,
		"get_preview": {"url"},
		"get_history": nil,
	}
	inputTypes := map[string]map[string]string{
		"search": {
			"query": "string", "fields": "array", "limit": "integer", "page_key": "string",
			"date_from": "string", "date_to": "string", "semantic": "boolean",
		},
		"get_facets": {
			"query": "string", "limit": "integer", "date_from": "string", "date_to": "string",
		},
		"get_preview": {
			"url": "string", "extractor": "string", "fields": "array",
			"max_chars": "integer", "text_offset": "integer", "html_offset": "integer",
		},
		"get_history": {
			"mode": "string", "filter": "string", "limit": "integer", "page_key": "string",
			"last_id": "integer", "last_updated_at": "string", "date_from": "string", "date_to": "string",
		},
	}
	outputRequired := []string{"schema_version", "tool", "security", "trusted", "untrusted_content"}

	for _, name := range names {
		tool := mcpToolByName(t, res.Tools, name)
		input := mcpSchemaObject(t, tool.InputSchema)
		if _, ok := input["additionalProperties"]; ok {
			t.Fatalf("%s input schema restricts additional properties; update the contract", name)
		}
		gotRequired := []string(nil)
		if value, ok := input["required"]; ok {
			gotRequired = mcpStringList(t, value)
		}
		if !slices.Equal(gotRequired, required[name]) {
			t.Fatalf("%s required = %v, want %v", name, gotRequired, required[name])
		}
		if input["type"] != "object" {
			t.Fatalf("%s input schema type = %#v, want object", name, input["type"])
		}
		properties := mcpSchemaObject(t, input["properties"])
		gotTypes := make(map[string]string, len(properties))
		for property := range properties {
			propertyType, _ := mcpSchemaProperty(t, tool.InputSchema, property)["type"].(string)
			gotTypes[property] = propertyType
		}
		if !reflect.DeepEqual(gotTypes, inputTypes[name]) {
			t.Fatalf("%s input property types = %v, want %v", name, gotTypes, inputTypes[name])
		}
		if field, ok := properties["fields"]; ok {
			if got := mcpSchemaObject(t, mcpSchemaObject(t, field)["items"])["type"]; got != "string" {
				t.Fatalf("%s fields items type = %#v, want string", name, got)
			}
		}

		output := mcpSchemaObject(t, tool.OutputSchema)
		if output["type"] != "object" {
			t.Fatalf("%s output schema type = %#v, want object", name, output["type"])
		}
		if got := mcpStringList(t, output["required"]); !slices.Equal(got, outputRequired) {
			t.Fatalf("%s output required = %v, want %v", name, got, outputRequired)
		}
		if output["additionalProperties"] != false {
			t.Fatalf("%s output schema allows additional properties", name)
		}
		if mcpSchemaProperty(t, tool.OutputSchema, "security")["additionalProperties"] != false {
			t.Fatalf("%s security schema allows additional properties", name)
		}
		untrusted := mcpSchemaObject(t, mcpSchemaProperty(t, tool.OutputSchema, "untrusted_content")["items"])
		if untrusted["additionalProperties"] != false {
			t.Fatalf("%s untrusted record allows additional properties", name)
		}
		if got := mcpStringList(t, mcpSchemaProperty(t, tool.OutputSchema, "tool")["enum"]); !slices.Equal(got, []string{name}) {
			t.Fatalf("%s output tool enum = %v, want [%s]", name, got, name)
		}
	}

	t.Run("search fields", func(t *testing.T) {
		tool := mcpToolByName(t, res.Tools, "search")
		items := mcpSchemaObject(t, mcpSchemaProperty(t, tool.InputSchema, "fields")["items"])
		want := []string{"text", "html", "language", "label", "domain", "score", "type"}
		if got := mcpStringList(t, items["enum"]); !slices.Equal(got, want) {
			t.Fatalf("fields enum = %v, want %v", got, want)
		}
	})

	t.Run("preview fields and minimums", func(t *testing.T) {
		tool := mcpToolByName(t, res.Tools, "get_preview")
		items := mcpSchemaObject(t, mcpSchemaProperty(t, tool.InputSchema, "fields")["items"])
		want := []string{"text", "html", "metadata"}
		if got := mcpStringList(t, items["enum"]); !slices.Equal(got, want) {
			t.Fatalf("fields enum = %v, want %v", got, want)
		}
		for _, name := range []string{"max_chars", "text_offset", "html_offset"} {
			if got := mcpSchemaProperty(t, tool.InputSchema, name)["minimum"]; got != float64(0) {
				t.Fatalf("%s minimum = %#v, want 0", name, got)
			}
		}
	})

	t.Run("history mode and last id", func(t *testing.T) {
		tool := mcpToolByName(t, res.Tools, "get_history")
		if got := mcpStringList(t, mcpSchemaProperty(t, tool.InputSchema, "mode")["enum"]); !slices.Equal(got, []string{"opened", "indexed"}) {
			t.Fatalf("mode enum = %v, want [opened indexed]", got)
		}
		if _, ok := mcpSchemaProperty(t, tool.InputSchema, "last_id")["minimum"]; ok {
			t.Fatal("last_id gained a minimum; update the contract")
		}
	})
}

func TestMCPInvalidArgumentsReturnRPCError(t *testing.T) {
	cfg, handler := newTokenTestServer(t, false)
	session := newMCPTestSession(t, cfg, handler, "secret")
	cases := []struct {
		name string
		tool string
		args string
	}{
		{"search missing query", "search", `{}`},
		{"search unknown field", "search", `{"query":"x","fields":["unknown"]}`},
		{"search invalid page key", "search", `{"query":"x","page_key":"invalid"}`},
		{"preview missing url", "get_preview", `{}`},
		{"preview unknown field", "get_preview", `{"url":"https://example.com/missing","fields":["unknown"]}`},
		{"preview negative max chars", "get_preview", `{"url":"https://example.com/missing","max_chars":-1}`},
		{"history invalid mode", "get_history", `{"mode":"invalid"}`},
		{"history invalid date", "get_history", `{"date_from":"yesterday"}`},
		{"history reversed range", "get_history", `{"mode":"opened","date_from":"2026-02-01","date_to":"2026-01-31"}`},
		{"search invalid query type", "search", `{"query":42}`},
		{"search invalid limit type", "search", `{"query":"x","limit":"10"}`},
		{"search invalid semantic type", "search", `{"query":"x","semantic":"true"}`},
		{"search invalid fields type", "search", `{"query":"x","fields":"text"}`},
		{"preview invalid url type", "get_preview", `{"url":42}`},
		{"preview invalid max chars type", "get_preview", `{"url":"https://example.com/missing","max_chars":"10"}`},
		{"history invalid mode type", "get_history", `{"mode":1}`},
		{"history invalid last id type", "get_history", `{"mode":"opened","last_id":"1"}`},
		{"facets invalid query type", "get_facets", `{"query":42}`},
		{"facets invalid limit type", "get_facets", `{"limit":"10"}`},
		{"facets invalid date", "get_facets", `{"date_from":"yesterday"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.tool, Arguments: json.RawMessage(tc.args)})
			if err == nil {
				t.Fatal("error = nil, want invalid argument")
			}
			if code := mcpTestWireError(t, err).Code; code != mcpErrInvalidParam {
				t.Fatalf("code = %d, want invalid argument", code)
			}
		})
	}
}

func TestMCPToolResultContract(t *testing.T) {
	cfg := testutil.InitModel(t)
	cfg.App.AccessToken = "secret"
	idx := newServerTestIndexer(t, cfg)
	handler := registerEndpoints(cfg, idx)
	doc := &document.Document{
		URL:       "https://example.com/contract",
		Title:     "Contract",
		Text:      "contract needle",
		HTML:      "<p>contract needle</p>",
		Processed: true,
	}
	if err := idx.Add(doc); err != nil {
		t.Fatal(err)
	}
	if err := model.UpdateHistory(0, "contract", doc.URL, doc.Title); err != nil {
		t.Fatal(err)
	}
	session := newMCPTestSession(t, cfg, handler, "secret")

	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"search", "search", map[string]any{"query": "contract needle"}},
		{"facets", "get_facets", map[string]any{}},
		{"preview", "get_preview", map[string]any{"url": doc.URL, "extractor": "Basic"}},
		{"history", "get_history", map[string]any{"mode": "opened"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := mcpTestCall(t, session, tc.tool, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError {
				t.Fatal("unexpected error result")
			}
			if res.StructuredContent == nil {
				t.Fatal("missing structured content")
			}
			if len(res.Content) != 1 {
				t.Fatalf("content blocks = %d, want 1", len(res.Content))
			}
			text, ok := res.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatalf("content block = %T, want *mcp.TextContent", res.Content[0])
			}
			if !strings.HasPrefix(text.Text, "SECURITY NOTICE: ") {
				t.Fatalf("text fallback = %q, want security notice", text.Text)
			}
			// Compare untyped JSON so keys mcpStructuredResult doesn't model still count.
			if textJSON, structuredJSON := mcpTextFallbackJSON(t, text.Text), mcpUntypedJSON(t, res.StructuredContent); !reflect.DeepEqual(textJSON, structuredJSON) {
				t.Fatalf("text fallback = %#v, want structured content %#v", textJSON, structuredJSON)
			}
			structured := mcpTestStructured(t, res)
			if structured.Security.UntrustedPath != "untrusted_content[*].fields" {
				t.Fatalf("untrusted path = %q", structured.Security.UntrustedPath)
			}
			if structured.Security.Instruction != mcpUntrustedContentInstruction {
				t.Fatalf("instruction = %q", structured.Security.Instruction)
			}
			if len(structured.UntrustedContent) == 0 {
				t.Fatal("no untrusted content")
			}
			for _, record := range structured.UntrustedContent {
				if record.Trust != "untrusted" || record.TrustScope == "" || record.SourceType == "" || record.Fields == nil {
					t.Fatalf("invalid trust boundary: %+v", record)
				}
			}
		})
	}
}

func TestMCPInitializeContract(t *testing.T) {
	_, handler := newTokenTestServer(t, false)
	rec := testutil.ServeHTTP(t, handler, http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`), mcpTestHeaders(map[string]string{
		"X-Access-Token": "secret",
	}))
	var body struct {
		Result struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Capabilities    json.RawMessage `json:"capabilities"`
			Instructions    string          `json:"instructions"`
		} `json:"result"`
		Error *mcpRPCError `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != nil {
		t.Fatalf("MCP error = %+v", body.Error)
	}
	if body.Result.ProtocolVersion != "2025-06-18" {
		t.Fatalf("protocol version = %q", body.Result.ProtocolVersion)
	}
	if got := string(body.Result.Capabilities); got != `{"tools":{}}` {
		t.Fatalf("capabilities = %s, want {\"tools\":{}}", got)
	}
	if body.Result.Instructions != "" {
		t.Fatalf("instructions = %q, want empty", body.Result.Instructions)
	}
}
