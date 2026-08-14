package config

import (
	"os"
	"regexp"
	"testing"
)

func restoreEnv(key, value string, existed bool) {
	if existed {
		_ = os.Setenv(key, value)
		return
	}
	_ = os.Unsetenv(key)
}

func TestServerDefaults(t *testing.T) {
	oldAddress := DefaultServerAddress
	oldBaseURL := DefaultServerBaseURL
	t.Cleanup(func() {
		DefaultServerAddress = oldAddress
		DefaultServerBaseURL = oldBaseURL
	})

	DefaultServerAddress = "127.0.0.1:5544"
	DefaultServerBaseURL = "https://defaults.example.com"

	cfg := CreateDefaultConfig()
	if cfg.Server.Address != DefaultServerAddress {
		t.Fatalf("default server address=%q, want %q", cfg.Server.Address, DefaultServerAddress)
	}
	if cfg.Server.BaseURL != DefaultServerBaseURL {
		t.Fatalf("default server base_url=%q, want %q", cfg.Server.BaseURL, DefaultServerBaseURL)
	}
	if cfg.Server.MaxBatchBodySize != DefaultMaxBatchBodySize {
		t.Fatalf("default server max_batch_body_size=%d, want %d", cfg.Server.MaxBatchBodySize, DefaultMaxBatchBodySize)
	}
	if cfg.Server.MaxBatchBodyBytes() != 40<<20 {
		t.Fatalf("default server batch body bytes=%d, want %d", cfg.Server.MaxBatchBodyBytes(), 40<<20)
	}
}

func TestServerMaxBatchBodySizeConfig(t *testing.T) {
	cfg, err := parseConfig([]byte("server:\n  max_batch_body_size: 12\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.MaxBatchBodyBytes() != 12<<20 {
		t.Fatalf("server batch body bytes=%d, want %d", cfg.Server.MaxBatchBodyBytes(), 12<<20)
	}

	if _, err := parseConfig([]byte("server:\n  max_batch_body_size: 0\n")); err == nil {
		t.Fatal("zero server.max_batch_body_size was accepted")
	}
}

func TestServerMaxBatchBodySizeEnvironmentOverride(t *testing.T) {
	const envName = "HISTER__SERVER__MAX_BATCH_BODY_SIZE"
	oldValue, existed := os.LookupEnv(envName)
	t.Cleanup(func() { restoreEnv(envName, oldValue, existed) })
	if err := os.Setenv(envName, "24"); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseConfig([]byte("server:\n  max_batch_body_size: 12\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.MaxBatchBodySize != 24 {
		t.Fatalf("server.max_batch_body_size=%d, want environment value 24", cfg.Server.MaxBatchBodySize)
	}
}

func TestIndexerDefaults(t *testing.T) {
	cfg := CreateDefaultConfig()
	if !cfg.Indexer.DetectLanguages {
		t.Fatal("default indexer.detect_languages=false, want true")
	}
	if cfg.Indexer.KeepStopwords {
		t.Fatal("default indexer.keep_stopwords=true, want false")
	}

	cfg, err := parseConfig([]byte("indexer:\n  keep_stopwords: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Indexer.KeepStopwords {
		t.Fatal("configured indexer.keep_stopwords=false, want true")
	}
}

func TestIndexerValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		indexer Indexer
		wantErr bool
	}{
		{"no languages detects everything", Indexer{}, false},
		{"two languages", Indexer{Languages: []string{"en", "de"}}, false},
		{"one language", Indexer{Languages: []string{"en"}}, true},
		// A repeated code resolves to a single language, which would leave the
		// detector disabled rather than narrowed.
		{"repeated language", Indexer{Languages: []string{"en", " EN "}}, true},
		{"unknown accuracy", Indexer{LanguageDetectionAccuracy: "medium"}, true},
		{"low accuracy", Indexer{LanguageDetectionAccuracy: "low"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.indexer.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestDirectoryLabelConfig(t *testing.T) {
	cfg, err := parseConfig([]byte("indexer:\n  directories:\n    - path: /srv/docs\n      label: reference\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Indexer.Directories) != 1 {
		t.Fatalf("directory count=%d, want 1", len(cfg.Indexer.Directories))
	}
	if got := cfg.Indexer.Directories[0].Label; got != "reference" {
		t.Fatalf("directory label=%q, want %q", got, "reference")
	}
}

func TestSemanticSearchRequestDefaults(t *testing.T) {
	cfg, err := parseConfig([]byte("semantic_search:\n  max_context_length: 256\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.SemanticSearch.EmbeddingTimeout, 300; got != want {
		t.Errorf("semantic_search.embedding_timeout = %d, want %d", got, want)
	}
	if got, want := cfg.SemanticSearch.MaxEmbeddingBatchSize, 8; got != want {
		t.Errorf("semantic_search.max_embedding_batch_size = %d, want %d", got, want)
	}
	if got, want := cfg.SemanticSearch.MaxEmbeddingConcurrency, 2; got != want {
		t.Errorf("semantic_search.max_embedding_concurrency = %d, want %d", got, want)
	}
}

func TestCrawlerProxyConfigAndEnvironment(t *testing.T) {
	const envName = "HISTER__CRAWLER__PROXY"
	oldProxy, hadProxy := os.LookupEnv(envName)
	t.Cleanup(func() {
		restoreEnv(envName, oldProxy, hadProxy)
	})
	if err := os.Unsetenv(envName); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseConfig([]byte("crawler:\n  proxy: http://file-proxy.example:8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Crawler.Proxy, "http://file-proxy.example:8080"; got != want {
		t.Fatalf("crawler proxy = %q, want %q", got, want)
	}

	if err := os.Setenv(envName, "socks5://env-proxy.example:1080"); err != nil {
		t.Fatal(err)
	}
	cfg, err = parseConfig([]byte("crawler:\n  proxy: http://file-proxy.example:8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Crawler.Proxy, "socks5://env-proxy.example:1080"; got != want {
		t.Fatalf("crawler proxy = %q, want %q", got, want)
	}
}

func TestAppTitleDefaultsAndOverrides(t *testing.T) {
	cfg := CreateDefaultConfig()
	if cfg.App.Title != "Hister" {
		t.Fatalf("default app title=%q, want %q", cfg.App.Title, "Hister")
	}
	if cfg.App.Subtitle != "Your own search engine" {
		t.Fatalf("default app subtitle=%q, want %q", cfg.App.Subtitle, "Your own search engine")
	}

	cfg, err := parseConfig([]byte("app:\n  title: Team Archive\n  subtitle: Internal search\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App.Title != "Team Archive" {
		t.Fatalf("app title=%q, want %q", cfg.App.Title, "Team Archive")
	}
	if cfg.App.Subtitle != "Internal search" {
		t.Fatalf("app subtitle=%q, want %q", cfg.App.Subtitle, "Internal search")
	}
}

func TestAppColorScheme(t *testing.T) {
	if got := CreateDefaultConfig().App.ColorScheme; got != "automatic" {
		t.Fatalf("default app color_scheme=%q, want %q", got, "automatic")
	}

	for _, scheme := range []string{"automatic", "dark", "light"} {
		t.Run(scheme, func(t *testing.T) {
			cfg, err := parseConfig([]byte("app:\n  color_scheme: " + scheme + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.App.ColorScheme != scheme {
				t.Fatalf("app color_scheme=%q, want %q", cfg.App.ColorScheme, scheme)
			}
		})
	}

	if _, err := parseConfig([]byte("app:\n  color_scheme: sepia\n")); err == nil {
		t.Fatal("invalid app.color_scheme was accepted")
	}
}

func TestConfigFileOverridesServerDefaults(t *testing.T) {
	oldAddress := DefaultServerAddress
	oldBaseURL := DefaultServerBaseURL
	oldEnvAddress, hadEnvAddress := os.LookupEnv("HISTER__SERVER__ADDRESS")
	oldEnvBaseURL, hadEnvBaseURL := os.LookupEnv("HISTER__SERVER__BASE_URL")
	t.Cleanup(func() {
		DefaultServerAddress = oldAddress
		DefaultServerBaseURL = oldBaseURL
		restoreEnv("HISTER__SERVER__ADDRESS", oldEnvAddress, hadEnvAddress)
		restoreEnv("HISTER__SERVER__BASE_URL", oldEnvBaseURL, hadEnvBaseURL)
	})

	DefaultServerAddress = "127.0.0.1:4433"
	DefaultServerBaseURL = "http://defaults.example.com"
	_ = os.Unsetenv("HISTER__SERVER__ADDRESS")
	_ = os.Unsetenv("HISTER__SERVER__BASE_URL")

	cfg, err := parseConfig([]byte("server:\n  address: 0.0.0.0:9999\n  base_url: https://config.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Address != "0.0.0.0:9999" {
		t.Fatalf("server address=%q, want config file value %q", cfg.Server.Address, "0.0.0.0:9999")
	}
	if cfg.Server.BaseURL != "https://config.example.com" {
		t.Fatalf("server base_url=%q, want config file value %q", cfg.Server.BaseURL, "https://config.example.com")
	}
}

func TestEnvironmentOverridesConfigFile(t *testing.T) {
	oldEnvAddress, hadEnvAddress := os.LookupEnv("HISTER__SERVER__ADDRESS")
	oldEnvBaseURL, hadEnvBaseURL := os.LookupEnv("HISTER__SERVER__BASE_URL")
	t.Cleanup(func() {
		restoreEnv("HISTER__SERVER__ADDRESS", oldEnvAddress, hadEnvAddress)
		restoreEnv("HISTER__SERVER__BASE_URL", oldEnvBaseURL, hadEnvBaseURL)
	})

	if err := os.Setenv("HISTER__SERVER__ADDRESS", "0.0.0.0:9999"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("HISTER__SERVER__BASE_URL", "https://env.example.com"); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseConfig([]byte("server:\n  address: 127.0.0.1:4433\n  base_url: https://config.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Address != "0.0.0.0:9999" {
		t.Fatalf("server address=%q, want environment value %q", cfg.Server.Address, "0.0.0.0:9999")
	}
	if cfg.Server.BaseURL != "https://env.example.com" {
		t.Fatalf("server base_url=%q, want environment value %q", cfg.Server.BaseURL, "https://env.example.com")
	}
}

func TestCLIFlagsOverrideEnvironment(t *testing.T) {
	oldEnvAddress, hadEnvAddress := os.LookupEnv("HISTER__SERVER__ADDRESS")
	oldEnvBaseURL, hadEnvBaseURL := os.LookupEnv("HISTER__SERVER__BASE_URL")
	t.Cleanup(func() {
		restoreEnv("HISTER__SERVER__ADDRESS", oldEnvAddress, hadEnvAddress)
		restoreEnv("HISTER__SERVER__BASE_URL", oldEnvBaseURL, hadEnvBaseURL)
	})

	if err := os.Setenv("HISTER__SERVER__ADDRESS", "0.0.0.0:9999"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("HISTER__SERVER__BASE_URL", "https://env.example.com"); err != nil {
		t.Fatal(err)
	}

	cfg, err := parseConfig([]byte("server:\n  address: 127.0.0.1:4433\n  base_url: https://config.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Address != "0.0.0.0:9999" {
		t.Fatalf("precondition: server address=%q, want environment value %q", cfg.Server.Address, "0.0.0.0:9999")
	}
	if cfg.Server.BaseURL != "https://env.example.com" {
		t.Fatalf("precondition: server base_url=%q, want environment value %q", cfg.Server.BaseURL, "https://env.example.com")
	}

	if err := cfg.UpdateBaseURL("https://cli.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.UpdateListenAddress("127.0.0.1:7777"); err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Address != "127.0.0.1:7777" {
		t.Fatalf("server address=%q, want CLI flag value %q", cfg.Server.Address, "127.0.0.1:7777")
	}
	if cfg.Server.BaseURL != "https://cli.example.com" {
		t.Fatalf("server base_url=%q, want CLI flag value %q", cfg.Server.BaseURL, "https://cli.example.com")
	}
}

func TestBasePathPrefix(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		prefix string
	}{
		{name: "root-no-slash", base: "https://example.com", prefix: ""},
		{name: "root-with-slash", base: "https://example.com/", prefix: ""},
		{name: "subfolder", base: "https://example.com/subfolder", prefix: "/subfolder"},
		{name: "subfolder-trailing", base: "https://example.com/subfolder/", prefix: "/subfolder"},
		{name: "nested", base: "https://example.com/a/b", prefix: "/a/b"},
		{name: "nested-trailing", base: "https://example.com/a/b/", prefix: "/a/b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Server: Server{BaseURL: tt.base}}
			if got := cfg.BasePathPrefix(); got != tt.prefix {
				t.Fatalf("BasePathPrefix()=%q, want %q", got, tt.prefix)
			}
		})
	}
}

func TestSensitiveContentPatterns(t *testing.T) {
	patterns := CreateDefaultConfig().SensitiveContentPatterns
	tests := []struct {
		name    string
		pattern string
		input   string
		match   bool
	}{
		{name: "aws_access_key/quoted", pattern: "aws_access_key", input: `key: "AKIAIOSFODNN7EXAMPLE"`, match: true},
		{name: "aws_access_key/whitespace", pattern: "aws_access_key", input: "token AKIAIOSFODNN7EXAMPLE end", match: true},
		{name: "aws_access_key/single-quoted", pattern: "aws_access_key", input: `'AKIAIOSFODNN7EXAMPLE'`, match: true},
		{name: "aws_access_key/start-of-string", pattern: "aws_access_key", input: "AKIAIOSFODNN7EXAMPLE ", match: true},
		{name: "aws_access_key/end-of-string", pattern: "aws_access_key", input: " AKIAIOSFODNN7EXAMPLE", match: true},
		{name: "aws_access_key/base64-blob", pattern: "aws_access_key", input: "d09GMgABAAAAAKIAIOSFODNN7EXAMPLEXYZABCDEF", match: false},
		{name: "aws_access_key/css-font", pattern: "aws_access_key", input: "url(data:font/woff2;base64,d09GMgABAAAAAKIA1234567890ABCDEF)", match: false},
		{name: "github_token/valid", pattern: "github_token", input: "ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ", match: true},
		{name: "generic_private_key", pattern: "generic_private_key", input: "-----BEGIN RSA PRIVATE KEY-----", match: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, ok := patterns[tt.pattern]
			if !ok {
				t.Fatalf("pattern %q not in defaults", tt.pattern)
			}
			re := regexp.MustCompile(raw)
			if got := re.MatchString(tt.input); got != tt.match {
				t.Fatalf("MatchString(%q) = %v, want %v", tt.input, got, tt.match)
			}
		})
	}
}

func TestParseEnvValue(t *testing.T) {
	tests := []struct {
		in   string
		want any
	}{
		{"true", true},
		{"false", false},
		{"True", true},
		{"FALSE", false},
		{"15", 15},
		{"0", 0},
		{"-3", -3},
		{"1.5", 1.5},
		// stays string: not a bool keyword
		{"yes", "yes"},
		{"1", 1},
		// stays string: numeric round-trip fails (data-loss guard)
		{"007", "007"},
		{"+5", "+5"},
		{"1e3", "1e3"},
		{"1.50", "1.50"},
		// plain strings
		{"en", "en"},
		{"https://env.example.com", "https://env.example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := parseEnvValue(tt.in)
			if got != tt.want {
				t.Fatalf("parseEnvValue(%q) = %#v (%T), want %#v (%T)", tt.in, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestEnvExtractorOptionTypes(t *testing.T) {
	const (
		enableKey = "HISTER__EXTRACTORS__ytdlp__ENABLE"
		subsKey   = "HISTER__EXTRACTORS__ytdlp__OPTIONS__FETCH_SUBTITLES"
		toKey     = "HISTER__EXTRACTORS__ytdlp__OPTIONS__TIMEOUT"
		jobsKey   = "HISTER__EXTRACTORS__ytdlp__OPTIONS__MAX_CONCURRENT_JOBS"
		langKey   = "HISTER__EXTRACTORS__ytdlp__OPTIONS__SUB_LANGUAGE"
	)
	for _, k := range []string{enableKey, subsKey, toKey, jobsKey, langKey} {
		old, had := os.LookupEnv(k)
		t.Cleanup(func() { restoreEnv(k, old, had) })
	}
	_ = os.Setenv(enableKey, "true")
	_ = os.Setenv(subsKey, "true")
	_ = os.Setenv(toKey, "30")
	_ = os.Setenv(jobsKey, "2")
	_ = os.Setenv(langKey, "en")

	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	ex := cfg.Extractors["ytdlp"]
	if ex == nil {
		t.Fatalf("ytdlp extractor missing; extractors=%v", cfg.Extractors)
	}
	if !ex.Enable {
		t.Errorf("Enable = false, want true")
	}
	if v, ok := ex.Options["fetch_subtitles"].(bool); !ok || !v {
		t.Errorf("fetch_subtitles = %#v (%T), want bool true", ex.Options["fetch_subtitles"], ex.Options["fetch_subtitles"])
	}
	if v, ok := ex.Options["timeout"].(int); !ok || v != 30 {
		t.Errorf("timeout = %#v (%T), want int 30", ex.Options["timeout"], ex.Options["timeout"])
	}
	if v, ok := ex.Options["max_concurrent_jobs"].(int); !ok || v != 2 {
		t.Errorf("max_concurrent_jobs = %#v (%T), want int 2", ex.Options["max_concurrent_jobs"], ex.Options["max_concurrent_jobs"])
	}
	if v, ok := ex.Options["sub_language"].(string); !ok || v != "en" {
		t.Errorf("sub_language = %#v (%T), want string \"en\"", ex.Options["sub_language"], ex.Options["sub_language"])
	}
}

func TestEnvTypedStringFieldNotCoerced(t *testing.T) {
	const key = "HISTER__APP__ACCESS_TOKEN"
	old, had := os.LookupEnv(key)
	t.Cleanup(func() { restoreEnv(key, old, had) })

	for _, token := range []string{"True", "false", "5", "0123"} {
		_ = os.Setenv(key, token)
		cfg, err := parseConfig(nil)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.App.AccessToken != token {
			t.Errorf("AccessToken = %q, want verbatim %q", cfg.App.AccessToken, token)
		}
	}
}

func TestPublicModeConfig(t *testing.T) {
	cfg := CreateDefaultConfig()
	if cfg.App.Public {
		t.Fatal("Public default = true, want false")
	}

	cfg, err := parseConfig([]byte("app:\n  public: true\n  access_token: secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.App.Public {
		t.Fatal("Public = false, want true")
	}
	if err := cfg.ValidatePublicMode(); err != nil {
		t.Fatalf("ValidatePublicMode() error = %v, want nil", err)
	}
}

func TestPublicModeEnvironmentOverride(t *testing.T) {
	const key = "HISTER__APP__PUBLIC"
	old, had := os.LookupEnv(key)
	t.Cleanup(func() { restoreEnv(key, old, had) })

	if err := os.Setenv(key, "true"); err != nil {
		t.Fatal(err)
	}
	cfg, err := parseConfig([]byte("app:\n  access_token: secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.App.Public {
		t.Fatal("Public = false, want true")
	}
}

func TestPublicModeRequiresAuth(t *testing.T) {
	cfg := CreateDefaultConfig()
	cfg.App.Public = true
	cfg.App.AccessToken = ""
	cfg.App.UserHandling = false
	if err := cfg.ValidatePublicMode(); err == nil {
		t.Fatal("ValidatePublicMode() error = nil, want error")
	}

	cfg.App.AccessToken = "secret"
	if err := cfg.ValidatePublicMode(); err != nil {
		t.Fatalf("ValidatePublicMode() with access token error = %v, want nil", err)
	}

	cfg.App.AccessToken = ""
	cfg.App.UserHandling = true
	if err := cfg.ValidatePublicMode(); err != nil {
		t.Fatalf("ValidatePublicMode() with user handling error = %v, want nil", err)
	}
}

func TestWebSocketURLHonorsBasePath(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "http-root", base: "http://example.com:1234", want: "ws://example.com:1234/search"},
		{name: "https-root", base: "https://example.com", want: "wss://example.com/search"},
		{name: "http-subfolder", base: "http://example.com/subfolder", want: "ws://example.com/subfolder/search"},
		{name: "https-nested", base: "https://example.com/a/b/", want: "wss://example.com/a/b/search"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Server: Server{BaseURL: tt.base}}
			if got := cfg.WebSocketURL(); got != tt.want {
				t.Fatalf("WebSocketURL()=%q, want %q", got, tt.want)
			}
		})
	}
}
