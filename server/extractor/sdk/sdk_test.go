package sdk_test

import (
	"errors"
	"testing"

	"github.com/asciimoo/nogon/server/extractor/sdk"
)

type testExtractor struct {
	config *sdk.Config
}

func (e *testExtractor) Name() string { return "Test" }

func (e *testExtractor) Description() string { return "Test extractor" }

func (e *testExtractor) Capabilities() sdk.Capabilities {
	return sdk.Capabilities{Extract: true, Preview: true}
}

func (e *testExtractor) Match(*sdk.Document) bool { return true }

func (e *testExtractor) Extract(*sdk.Document) sdk.ExtractResult {
	return sdk.Extracted()
}

func (e *testExtractor) Preview(*sdk.Document) sdk.PreviewResult {
	return sdk.Previewed(sdk.PreviewResponse{Content: "preview"})
}

func (e *testExtractor) GetConfig() *sdk.Config {
	if e.config == nil {
		return &sdk.Config{Enable: true, Options: map[string]any{}}
	}
	return e.config
}

func (e *testExtractor) SetConfig(config *sdk.Config) error {
	e.config = config
	return nil
}

func TestSDKExtractorContract(t *testing.T) {
	var candidate sdk.Extractor = &testExtractor{}
	doc := &sdk.Document{URL: "https://example.com"}
	if result := candidate.Extract(doc); result.Decision() != sdk.ExtractorSuccess {
		t.Fatalf("extraction decision = %v, want success", result.Decision())
	}
	result := candidate.Preview(doc)
	if result.Decision() != sdk.ExtractorSuccess || result.Response().Content != "preview" {
		t.Fatalf("preview result = %#v, want successful preview", result)
	}
}

func TestExtractResultConstructors(t *testing.T) {
	diagnostic := errors.New("not applicable")
	tests := []struct {
		name       string
		result     sdk.ExtractResult
		decision   sdk.Decision
		wantErr    error
		wantAnyErr bool
	}{
		{name: "success", result: sdk.Extracted(), decision: sdk.ExtractorSuccess},
		{name: "fallback", result: sdk.ExtractFallback(diagnostic), decision: sdk.ExtractorFallback, wantErr: diagnostic},
		{name: "abort", result: sdk.AbortExtraction(diagnostic), decision: sdk.ExtractorAbort, wantErr: diagnostic},
		{name: "abort without error", result: sdk.AbortExtraction(nil), decision: sdk.ExtractorAbort, wantAnyErr: true},
		{name: "zero value", result: sdk.ExtractResult{}, decision: sdk.ExtractorInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.result.Decision(); got != test.decision {
				t.Fatalf("Decision() = %v, want %v", got, test.decision)
			}
			if test.wantErr != nil && !errors.Is(test.result.Err(), test.wantErr) {
				t.Fatalf("Err() = %v, want %v", test.result.Err(), test.wantErr)
			}
			if test.wantAnyErr && test.result.Err() == nil {
				t.Fatal("Err() = nil, want an error")
			}
		})
	}
}

func TestPreviewResultConstructors(t *testing.T) {
	diagnostic := errors.New("not applicable")
	response := sdk.PreviewResponse{Content: "preview", Template: "custom"}

	success := sdk.Previewed(response)
	if success.Decision() != sdk.ExtractorSuccess {
		t.Fatalf("Decision() = %v, want %v", success.Decision(), sdk.ExtractorSuccess)
	}
	if got := success.Response(); got != response {
		t.Fatalf("Response() = %#v, want %#v", got, response)
	}

	fallback := sdk.PreviewFallback(diagnostic)
	if fallback.Decision() != sdk.ExtractorFallback || !errors.Is(fallback.Err(), diagnostic) {
		t.Fatalf("PreviewFallback() = decision %v, error %v", fallback.Decision(), fallback.Err())
	}

	abort := sdk.AbortPreview(nil)
	if abort.Decision() != sdk.ExtractorAbort || abort.Err() == nil {
		t.Fatalf("AbortPreview(nil) = decision %v, error %v", abort.Decision(), abort.Err())
	}

	var zero sdk.PreviewResult
	if zero.Decision() != sdk.ExtractorInvalid {
		t.Fatalf("zero Decision() = %v, want %v", zero.Decision(), sdk.ExtractorInvalid)
	}
}
