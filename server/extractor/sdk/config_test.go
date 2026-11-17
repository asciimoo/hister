package sdk_test

import (
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/extractor/sdk"
)

func TestOptionMetadataRequiresExplicitPolicies(t *testing.T) {
	valid := sdk.OptionMetadata{
		Key: "labels", Label: "Labels", Description: "Labels attached to extracted documents.", Effect: "runtime",
		Schema:  config.SettingSchema{Type: "list", Control: "list", Collection: "replace", Sensitivity: "none", Items: &config.SettingSchema{Type: "string", Control: "text", Collection: "none", Sensitivity: "none"}},
		Default: []string{},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}
	for _, change := range []func(*sdk.OptionMetadata){
		func(m *sdk.OptionMetadata) { m.Schema.Collection = "" },
		func(m *sdk.OptionMetadata) { m.Schema.Sensitivity = "" },
		func(m *sdk.OptionMetadata) { m.Schema.Items = nil },
		func(m *sdk.OptionMetadata) { m.Key = "options.nested" },
		func(m *sdk.OptionMetadata) { m.Default = "not a list" },
		func(m *sdk.OptionMetadata) { m.Schema.Min = new(1) },
		func(m *sdk.OptionMetadata) { m.Schema.MaxLength = 10 },
		func(m *sdk.OptionMetadata) {
			item := *m.Schema.Items
			item.Sensitivity = "secret"
			item.Control = "secret"
			m.Schema.Items = &item
		},
	} {
		m := valid
		change(&m)
		if err := m.Validate(); err == nil {
			t.Fatal("accepted incomplete or inconsistent option metadata")
		}
	}
	secret := sdk.OptionMetadata{Key: "credential", Label: "Credential", Description: "Authentication credential.", Effect: "runtime", Schema: config.SettingSchema{Type: "string", Control: "secret", Collection: "none", Sensitivity: "secret"}}
	if err := secret.Validate(); err != nil {
		t.Fatalf("explicit secret metadata rejected: %v", err)
	}
	secret.Schema.Control = "text"
	if err := secret.Validate(); err == nil {
		t.Fatal("secret accepted a plain text control")
	}
	secret.Schema.Control = "secret"
	secret.Schema.Choices = []string{"must-not-be-published"}
	if err := secret.Validate(); err == nil {
		t.Fatal("secret choices could be exposed in metadata")
	}
	endpoint := sdk.OptionMetadata{Key: "endpoint", Label: "Endpoint", Description: "Service URL.", Effect: "runtime", Default: "javascript:private-credential", Schema: config.SettingSchema{Type: "string", Control: "url", Collection: "none", Sensitivity: "url"}}
	if err := endpoint.Validate(); err == nil {
		t.Fatal("URL metadata accepted a non-HTTP default")
	}
}

func TestConfigSupportZeroValue(t *testing.T) {
	var store sdk.ConfigSupport

	config := store.GetConfig()
	if !config.Enable {
		t.Fatal("zero value store should be enabled")
	}
	if config.Options == nil {
		t.Fatal("zero value store should return a nonnil options map")
	}
	if len(config.Options) != 0 {
		t.Fatalf("zero value store returned options: %v", config.Options)
	}
}

func TestConfigSupportCustomDefaults(t *testing.T) {
	store := sdk.NewConfigSupport(sdk.Config{
		Enable: false,
		Options: map[string]any{
			"timeout": 10,
		},
	}, "optional")

	first := store.GetConfig()
	if first.Enable {
		t.Fatal("custom default should be disabled")
	}
	if first.Options["timeout"] != 10 {
		t.Fatalf("unexpected default options: %v", first.Options)
	}

	first.Options["timeout"] = 20
	second := store.GetConfig()
	if second.Options["timeout"] != 10 {
		t.Fatalf("default options were mutated: %v", second.Options)
	}

	configured := &sdk.Config{
		Enable: true,
		Options: map[string]any{
			"timeout":  30,
			"optional": true,
		},
	}
	if err := store.SetConfig(configured); err != nil {
		t.Fatalf("SetConfig returned an error: %v", err)
	}
	if store.GetConfig() != configured {
		t.Fatal("SetConfig should retain the supplied config pointer")
	}
}

func TestConfigSupportRejectsUnknownOptions(t *testing.T) {
	store := sdk.NewConfigSupport(sdk.Config{
		Enable:  true,
		Options: map[string]any{"known": 1},
	})

	configured := &sdk.Config{
		Enable:  true,
		Options: map[string]any{"unknown": 1},
	}
	if err := store.SetConfig(configured); err == nil {
		t.Fatal("SetConfig accepted an unknown option")
	}
	if store.GetConfig() == configured {
		t.Fatal("SetConfig stored a rejected config")
	}
}

func TestConfigSupportDetachesCollectionDefaults(t *testing.T) {
	defaults := sdk.Config{Enable: true, Options: map[string]any{"labels": []string{"original"}}}
	store := sdk.NewConfigSupport(defaults)
	first := store.GetConfig()
	first.Options["labels"].([]string)[0] = "changed"
	if defaults.Options["labels"].([]string)[0] != "original" || store.GetConfig().Options["labels"].([]string)[0] != "original" {
		t.Fatal("default collections share mutable state")
	}
}

func TestConfigSupportDetachesArrayDefaults(t *testing.T) {
	defaults := sdk.Config{Enable: true, Options: map[string]any{"labels": [1][]string{{"original"}}}}
	store := sdk.NewConfigSupport(defaults)
	defaults.Options["labels"].([1][]string)[0][0] = "caller mutation"
	first := store.GetConfig()
	if first.Options["labels"].([1][]string)[0][0] != "original" {
		t.Fatal("creating a configuration store retained mutable array members")
	}
	first.Options["labels"].([1][]string)[0][0] = "returned mutation"
	if store.GetConfig().Options["labels"].([1][]string)[0][0] != "original" {
		t.Fatal("returned array members share mutable defaults")
	}
}

func TestConfigSupportOptionalOptionsWithoutDefaults(t *testing.T) {
	store := sdk.NewConfigSupport(sdk.Config{Enable: false}, "timeout")
	configured := store.GetConfig()
	if configured.Options == nil {
		t.Fatal("custom defaults must allow assigning optional settings")
	}
	configured.Options["timeout"] = 10
	if len(store.GetConfig().Options) != 0 {
		t.Fatal("editing optional settings changed the defaults")
	}
	if err := store.SetConfig(configured); err != nil {
		t.Fatalf("accepted option rejected: %v", err)
	}
}
