package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/document"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestExpandImportInputsExpandsDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"b.html",
		"a.json",
		"c.7z",
		"d.htm",
		"ignored.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "nested.html"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}

	inputs, err := expandImportInputs([]string{"first.json", dir, "last"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"first.json",
		filepath.Join(dir, "a.json"),
		filepath.Join(dir, "b.html"),
		filepath.Join(dir, "c.7z"),
		filepath.Join(dir, "d.htm"),
		"last",
	}
	if !reflect.DeepEqual(inputs, want) {
		t.Fatalf("expandImportInputs() = %#v, want %#v", inputs, want)
	}
}

func TestIsSupportedImportInput(t *testing.T) {
	tests := map[string]bool{
		"export.json": true,
		"backup.7z":   true,
		"page.html":   true,
		"page.htm":    true,
		"page.HTML":   true,
		"notes.txt":   false,
		"README":      false,
	}

	for input, want := range tests {
		if got := isSupportedImportInput(input); got != want {
			t.Fatalf("isSupportedImportInput(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestImportJSONFileUsesConfiguredBatchSize(t *testing.T) {
	var batchSizes []int
	var receivedLabels []string
	var receivedMetadata map[string]any
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/batch" {
			t.Errorf("request path = %q, want /api/batch", r.URL.Path)
		}
		var req struct {
			Ops []struct {
				Op        string         `json:"op"`
				Label     string         `json:"label"`
				Metadata  map[string]any `json:"metadata"`
				Processed bool           `json:"processed"`
			} `json:"ops"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, fmt.Errorf("decode request: %w", err)
		}
		batchSizes = append(batchSizes, len(req.Ops))
		for _, op := range req.Ops {
			receivedLabels = append(receivedLabels, op.Label)
		}
		if len(batchSizes) == 1 && len(req.Ops) > 0 {
			receivedMetadata = req.Ops[0].Metadata
		}
		for _, op := range req.Ops {
			if !op.Processed {
				t.Error("imported JSON document was not marked as processed")
			}
		}
		results := make([]map[string]any, len(req.Ops))
		for i, op := range req.Ops {
			if op.Op != "add" {
				t.Errorf("operation = %q, want add", op.Op)
			}
			results[i] = map[string]any{"status": http.StatusCreated}
		}
		var response bytes.Buffer
		if err := json.NewEncoder(&response).Encode(map[string]any{"results": results}); err != nil {
			return nil, fmt.Errorf("encode response: %w", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(response.Bytes())),
			Request:    r,
		}, nil
	})}

	var input strings.Builder
	for i := range 23 {
		doc := &document.Document{
			URL:   fmt.Sprintf("https://example.com/%d", i),
			Title: fmt.Sprintf("Document %d", i),
		}
		if i == 0 {
			doc.Label = "reference"
			doc.Metadata = map[string]any{"source": "export"}
		}
		line, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(line)
		input.WriteByte('\n')
	}
	inputFile := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(inputFile, []byte(input.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	imported, skipped, errCount := importJSONFile(
		client.New("http://hister.test", client.WithHTTPClient(httpClient), client.WithMaxBatchBodyBytes(40<<20)),
		inputFile,
		false,
		0,
		0,
		10,
		documentLabelOverride{},
	)
	if imported != 23 || skipped != 0 || errCount != 0 {
		t.Fatalf("importJSONFile() = (%d, %d, %d), want (23, 0, 0)", imported, skipped, errCount)
	}
	if want := []int{10, 10, 3}; !reflect.DeepEqual(batchSizes, want) {
		t.Fatalf("batch sizes = %v, want %v", batchSizes, want)
	}
	if len(receivedLabels) != 23 {
		t.Fatalf("received %d labels, want 23", len(receivedLabels))
	}
	if receivedLabels[0] != "reference" {
		t.Fatalf("stored label = %q, want reference", receivedLabels[0])
	}
	for i, label := range receivedLabels[1:] {
		if label != "import" {
			t.Fatalf("label %d = %q, want import", i+1, label)
		}
	}
	if receivedMetadata["source"] != "export" {
		t.Fatalf("metadata source = %v, want export", receivedMetadata["source"])
	}
}

func TestDocumentImportFlags(t *testing.T) {
	flags := importFileCmd.Flags()

	if flags.Lookup("start-date") == nil {
		t.Fatal("start-date flag not found")
	}
	if flags.Lookup("end-date") == nil {
		t.Fatal("end-date flag not found")
	}
	if flags.Lookup("batch-size") == nil {
		t.Fatal("batch-size flag not found")
	}
	if flags.Lookup("skip-existing") == nil {
		t.Fatal("skip-existing flag not found")
	}
	if flags.Lookup("global") == nil {
		t.Fatal("global flag not found")
	}
	if flags.Lookup("user-id") == nil {
		t.Fatal("user-id flag not found")
	}
	if flags.Lookup("label") == nil {
		t.Fatal("label flag not found")
	}

}

func TestImportBatchSizeDefault(t *testing.T) {
	batchSize, err := importFileCmd.Flags().GetInt("batch-size")
	if err != nil {
		t.Fatal(err)
	}
	if batchSize != 10 {
		t.Fatalf("batch size default = %d, want 10", batchSize)
	}
}

func TestDocumentLabelOverride(t *testing.T) {
	d := &document.Document{Label: "exported"}
	documentLabelOverride{}.apply(d, "import")
	if d.Label != "exported" {
		t.Fatalf("unset override changed label to %q", d.Label)
	}

	d.Label = ""
	documentLabelOverride{}.apply(d, "import")
	if d.Label != "import" {
		t.Fatalf("fallback label = %q, want import", d.Label)
	}

	override := documentLabelOverride{value: "imported", set: true}
	override.apply(d, "import")
	if d.Label != "imported" {
		t.Fatalf("label = %q, want imported", d.Label)
	}
	if got := (documentLabelOverride{}).resolve("stored", "browser"); got != "stored" {
		t.Fatalf("resolved stored label = %q, want stored", got)
	}
	if got := (documentLabelOverride{}).resolve("", "browser"); got != "browser" {
		t.Fatalf("resolved fallback label = %q, want browser", got)
	}
	if got := override.resolve("stored", "browser"); got != "imported" {
		t.Fatalf("resolved override label = %q, want imported", got)
	}
}

func TestDocumentLabelOverrideReadsInheritedFlag(t *testing.T) {
	parent := &cobra.Command{Use: "import"}
	child := &cobra.Command{Use: "source"}
	parent.AddCommand(child)
	parent.PersistentFlags().String("label", "", "")
	if err := child.ParseFlags([]string{"--label", "reading"}); err != nil {
		t.Fatal(err)
	}

	override := newDocumentLabelOverride(child)
	if !override.set || override.value != "reading" {
		t.Fatalf("label override = %+v, want explicitly set reading label", override)
	}
}

func TestImportCommandHierarchy(t *testing.T) {
	tests := map[string]*cobra.Command{
		"file":       importFileCmd,
		"browser":    importBrowserCmd,
		"linkding":   importLinkdingCmd,
		"linkwarden": importLinkwardenCmd,
		"karakeep":   importKarakeepCmd,
		"shaarli":    importShaarliCmd,
		"wallabag":   importWallabagCmd,
	}
	for name, want := range tests {
		got, _, err := importCmd.Find([]string{name})
		if err != nil {
			t.Fatalf("import %s command lookup failed: %v", name, err)
		}
		if got != want {
			t.Fatalf("import %s command = %q, want %q", name, got.Name(), want.Name())
		}
	}
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "import-browser" {
			t.Fatal("legacy import-browser command remains registered at the root")
		}
	}
}

func TestImportSubcommandFlagOwnership(t *testing.T) {
	if indexCmd.Flags().Lookup("proxy") == nil {
		t.Error("index is missing --proxy")
	}
	if importCmd.PersistentFlags().Lookup("label") == nil {
		t.Fatal("import is missing --label")
	}
	for _, importCommand := range []*cobra.Command{importFileCmd, importBrowserCmd, importLinkdingCmd, importLinkwardenCmd, importKarakeepCmd, importShaarliCmd, importWallabagCmd} {
		if importCommand.InheritedFlags().Lookup("label") == nil {
			t.Errorf("import %s does not inherit --label", importCommand.Name())
		}
	}
	for _, name := range []string{"batch-size", "start-date", "end-date", "skip-existing", "global", "user-id"} {
		for _, importCommand := range []*cobra.Command{importFileCmd, importLinkdingCmd, importLinkwardenCmd, importKarakeepCmd, importShaarliCmd, importWallabagCmd} {
			if importCommand.Flags().Lookup(name) == nil {
				t.Errorf("import %s is missing --%s", importCommand.Name(), name)
			}
		}
		if name != "start-date" && importBrowserCmd.Flags().Lookup(name) != nil {
			t.Errorf("import browser unexpectedly has --%s", name)
		}
	}
	if importBrowserCmd.Flags().Lookup("start-date") == nil {
		t.Error("import browser is missing --start-date")
	}
	if importBrowserCmd.Flags().Lookup("min-visit") == nil {
		t.Error("import browser is missing --min-visit")
	}
	for _, name := range []string{"backend", "backend-option", "proxy", "header", "cookie"} {
		for _, importCommand := range []*cobra.Command{importBrowserCmd, importLinkdingCmd, importLinkwardenCmd, importKarakeepCmd, importShaarliCmd} {
			if importCommand.Flags().Lookup(name) == nil {
				t.Errorf("import %s is missing --%s", importCommand.Name(), name)
			}
		}
		if importFileCmd.Flags().Lookup(name) != nil {
			t.Errorf("import file unexpectedly has --%s", name)
		}
	}
	for _, importCommand := range []*cobra.Command{importLinkdingCmd, importLinkwardenCmd, importKarakeepCmd, importShaarliCmd} {
		if importCommand.Flags().Lookup("api-token") == nil {
			t.Errorf("import %s is missing --api-token", importCommand.Name())
		}
		if importCommand.Flags().Lookup("source-token") != nil {
			t.Errorf("import %s has the old --source-token flag", importCommand.Name())
		}
	}
	for _, importCommand := range []*cobra.Command{importFileCmd, importBrowserCmd} {
		if importCommand.Flags().Lookup("api-token") != nil {
			t.Errorf("import %s unexpectedly has --api-token", importCommand.Name())
		}
	}
}
