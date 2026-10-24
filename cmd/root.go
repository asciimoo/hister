// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/logging"
	"github.com/asciimoo/hister/server"
	"github.com/asciimoo/hister/server/extractor"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"

	"charm.land/lipgloss/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const versionBase = "v0.20.0"

var Version = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return fmt.Sprintf("%s (%s)", versionBase, s.Value[:7])
			}
		}
	}
	return versionBase
}()

var (
	cliErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	cliSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	cliInfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	cliBoldStyle    = lipgloss.NewStyle().Bold(true)
)

var (
	cfgFile   string
	cfg       *config.Config
	UserAgent = fmt.Sprintf("Mozilla/5.0 (compatible; Hister/%s; +https://hister.org/)", Version)
)

// stringToAnyMap converts map[string]string to map[string]any, used when
// applying --backend-option flag values to crawler config.
func stringToAnyMap(m map[string]string) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// parseCookieFlag parses a Set-Cookie header value (e.g. "session=abc; Domain=example.com; Path=/")
// into a CrawlerCookie. Domain is required.
func parseCookieFlag(s string) (config.CrawlerCookie, error) {
	c, err := http.ParseSetCookie(s)
	if err != nil {
		return config.CrawlerCookie{}, fmt.Errorf("cookie %q: %w", s, err)
	}
	if c.Domain == "" {
		return config.CrawlerCookie{}, fmt.Errorf("cookie %q: Domain attribute is required", s)
	}
	path := c.Path
	if path == "" {
		path = "/"
	}
	return config.CrawlerCookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: path}, nil
}

// applyCrawlerBackendFlags reads --backend, --backend-option, --proxy, --header, and --cookie
// flags from cmd and applies them to cfg.Crawler, overriding any config-file values.
func applyCrawlerBackendFlags(cmd *cobra.Command) {
	if b, _ := cmd.Flags().GetString("backend"); b != "" {
		cfg.Crawler.Backend = b
	}
	if opts, _ := cmd.Flags().GetStringToString("backend-option"); len(opts) > 0 {
		cfg.Crawler.BackendOptions = stringToAnyMap(opts)
	}
	if cmd.Flags().Changed("proxy") {
		cfg.Crawler.Proxy, _ = cmd.Flags().GetString("proxy")
	}
	if headers, _ := cmd.Flags().GetStringToString("header"); len(headers) > 0 {
		if cfg.Crawler.Headers == nil {
			cfg.Crawler.Headers = make(map[string]string)
		}
		maps.Copy(cfg.Crawler.Headers, headers)
	}
	if cookies, _ := cmd.Flags().GetStringArray("cookie"); len(cookies) > 0 {
		for _, raw := range cookies {
			ck, err := parseCookieFlag(raw)
			if err != nil {
				exit(1, err.Error())
			}
			cfg.Crawler.Cookies = append(cfg.Crawler.Cookies, ck)
		}
	}
}

func addCrawlerBackendFlags(cmd *cobra.Command) {
	cmd.Flags().String("backend", "", "Crawler backend to use (\"http\", \"chromedp\", or \"bidi\")")
	cmd.Flags().StringToString("backend-option", nil, "Crawler backend option as key=value (repeatable, e.g. --backend-option exec_path=/usr/bin/chromium)")
	cmd.Flags().String("proxy", "", "Proxy URL for crawler requests (http:// or socks5://)")
	cmd.Flags().StringToString("header", nil, "Extra HTTP header as KEY=VALUE (repeatable, e.g. --header Accept-Language=en)")
	cmd.Flags().StringArray("cookie", nil, "HTTP cookie as Set-Cookie value (repeatable, e.g. --cookie \"session=abc; Domain=example.com\")")
}

func addServiceImportFlags(cmd *cobra.Command, serviceName, tokenEnv string) {
	addDocumentImportFlags(cmd)
	addCrawlerBackendFlags(cmd)
	cmd.Flags().String("api-token", "", serviceName+" API token (default: "+tokenEnv+")")
}

const ignoreRulesFlagUsage = "Bypass URL indexing rules and preserve submitted documents during reindexing"

func documentSubmissionClientOptions(cmd *cobra.Command) []client.Option {
	var options []client.Option
	if allowSensitive, _ := cmd.Flags().GetBool("allow-sensitive"); allowSensitive {
		options = append(options, client.WithAllowSensitive())
	}
	if ignoreRules, _ := cmd.Flags().GetBool("ignore-rules"); ignoreRules {
		options = append(options, client.WithIgnoreRules())
	}
	return options
}

func targetUserIDClientOptions(cmd *cobra.Command, global bool) []client.Option {
	targetUserID, _ := cmd.Flags().GetUint("user-id")
	userIDChanged := cmd.Flags().Changed("user-id")
	if global && userIDChanged {
		exit(1, "--global and --user-id are mutually exclusive")
	}
	if global || (!userIDChanged && cfg.App.Public) {
		return []client.Option{client.WithTargetUserID(0)}
	}
	if userIDChanged {
		return []client.Option{client.WithTargetUserID(targetUserID)}
	}
	return nil
}

var rootCmd = &cobra.Command{
	Use:     "hister",
	Short:   "Your own search engine",
	Long:    "Hister - your own search engine",
	Version: Version,
	//Run: func(_ *cobra.Command, _ []string) {
	//},
}

var listenCmd = &cobra.Command{
	Use:   "listen",
	Short: "Start the Hister HTTP server",
	Long:  `Start the Hister HTTP server and watch configured directories for file changes.`,
	PreRun: func(cmd *cobra.Command, _ []string) {
		if public, _ := cmd.Flags().GetBool("public"); public {
			cfg.App.Public = true
		}
		if err := cfg.ValidatePublicMode(); err != nil {
			exit(1, "Failed to initialize config: "+err.Error())
		}
	},
	RunE: func(cmd *cobra.Command, _ []string) error {
		idx := initIndex(model.ReadWrite)
		if a, err := cmd.Flags().GetString("address"); err == nil && cmd.Flags().Changed("address") {
			if err := cfg.UpdateListenAddress(a); err != nil {
				exit(1, `Failed to set server address: `+err.Error())
			}
		}
		if (cfg.App.AccessToken != "" || cfg.App.UserHandling) && strings.HasPrefix(cfg.BaseURL(""), "http://") {
			log.Warn().Msg("Using authentication without https. Credentials and sessions are sent in plain text network requests.")
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		server.Version = Version
		return server.Listen(ctx, cfg, idx, listenConfigOverrides(cmd))
	},
}

func exit(errno int, msg string) {
	if errno != 0 {
		_, _ = lipgloss.Fprintln(os.Stderr, cliErrorStyle.Render("Error!")+" "+msg)
	} else {
		cliPrintln(msg)
	}
	os.Exit(errno)
}

// cliPrintln sends styled human output through Lip Gloss's
// profile-aware writer. This strips CSI/SGR sequences from pipes and honors
// NO_COLOR while retaining color (downsampled as needed) on terminals.
func cliPrintln(v ...any) {
	_, _ = lipgloss.Fprintln(os.Stdout, v...)
}

func isConnectionError(err error) bool {
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

type dateRangeFlags struct {
	From int64
	To   int64
}

func parseDateRangeFlags(cmd *cobra.Command) (dateRangeFlags, error) {
	var r dateRangeFlags
	if v, _ := cmd.Flags().GetString("start-date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return r, fmt.Errorf("invalid --start-date: %w", err)
		}
		r.From = t.Unix()
	}
	if v, _ := cmd.Flags().GetString("end-date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return r, fmt.Errorf("invalid --end-date: %w", err)
		}
		r.To = t.AddDate(0, 0, 1).Unix() - 1
	}
	return r, nil
}

func init() {
	dcfg := config.CreateDefaultConfig()
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yml", "config file (default paths: ./config.yml or $HOME/.histerrc or $HOME/.config/hister/config.yml)")
	rootCmd.PersistentFlags().StringP("log-level", "l", "info", "set log level (possible options: error, warning, info, debug, trace)")
	rootCmd.PersistentFlags().StringP("search-url", "s", dcfg.App.SearchURL, "set default search engine url")
	rootCmd.PersistentFlags().StringP("server-url", "u", dcfg.Server.BaseURL, "hister server URL")
	rootCmd.PersistentFlags().StringP("token", "t", "", "access token (overrides config access_token)")
	rootCmd.PersistentFlags().Int("client-timeout", 0, "HTTP client timeout in seconds (default 10; explicitly set 0 to disable the timeout)")

	rootCmd.AddCommand(listenCmd)
	rootCmd.AddCommand(createConfigCmd)
	rootCmd.AddCommand(configCmd, doctorCmd)
	configCmd.AddCommand(configCreateCmd, configPathCmd, configShowCmd, configValidateCmd)
	addOutputFormatFlag(doctorCmd)
	rootCmd.AddCommand(listURLsCmd)
	rootCmd.AddCommand(listFilesCmd)
	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(reindexCmd)
	rootCmd.AddCommand(cleanupCmd)
	rootCmd.AddCommand(reloadCmd)
	rootCmd.AddCommand(checkUpdateCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(updateDocumentsCmd)
	rootCmd.AddCommand(userCmd)
	userCmd.AddCommand(createUserCmd, deleteUserCmd, showUserCmd, updateUserCmd)
	rootCmd.AddCommand(createUserAliasCmd, deleteUserAliasCmd, showUserAliasCmd, updateUserAliasCmd)
	rootCmd.AddCommand(crawlCmd)
	rootCmd.AddCommand(companionCmd)
	crawlCmd.AddCommand(crawlListCmd)
	crawlCmd.AddCommand(crawlShowCmd)
	crawlCmd.AddCommand(crawlErrorsCmd)
	crawlCmd.AddCommand(crawlQueueCmd)
	crawlCmd.AddCommand(crawlURLsCmd)
	crawlCmd.AddCommand(crawlDeleteCmd)
	companionCmd.AddCommand(companionQutebrowserCmd)
	companionCmd.AddCommand(companionSafariCmd)
	addSafariCompanionFlags(companionSafariCmd)
	importCmd.AddCommand(importFileCmd)
	importCmd.AddCommand(importSitemapCmd)
	importCmd.AddCommand(importBrowserCmd)
	importBrowserCmd.AddCommand(importBrowserHistoryCmd)
	importBrowserCmd.AddCommand(importBookmarksCmd)
	importCmd.AddCommand(importLinkdingCmd)
	importCmd.AddCommand(importLinkwardenCmd)
	importCmd.AddCommand(importKarakeepCmd)
	importCmd.AddCommand(importRaindropCmd)
	importCmd.AddCommand(importReadeckCmd)
	importCmd.AddCommand(importReadwiseCmd)
	importCmd.AddCommand(importShaarliCmd)
	importCmd.AddCommand(importWallabagCmd)
	importCmd.PersistentFlags().String("label", "", "Label to attach to all imported documents")
	importCmd.PersistentFlags().Bool("ignore-rules", false, ignoreRulesFlagUsage)

	listenCmd.Flags().StringP("address", "a", dcfg.Server.Address, "Listen address (host:port or unix:/absolute/path; Unix sockets require --server-url or server.base_url)")
	listenCmd.Flags().Bool("public", false, "allow unauthenticated access to public search interfaces")

	listURLsCmd.Flags().Bool("offline", false, "connect to the indexer directly without using the HTTP API (server should be stopped)")
	listFilesCmd.Flags().Bool("relative", false, "print paths relative to each configured indexing directory")

	importBrowserCmd.Flags().IntP("min-visit", "m", 1, "only import URLs visited at least this many times")
	importBrowserCmd.Flags().String("start-date", "", "only import URLs visited on or after this date (YYYY-MM-DD)")
	addCrawlerBackendFlags(importBrowserCmd)
	importBrowserHistoryCmd.Flags().String("browser", "", "limit autodetection to a supported browser")
	importBrowserHistoryCmd.Flags().String("db", "", "browser history database path")
	importBrowserHistoryCmd.Flags().IntP("min-visit", "m", 1, "only import URLs visited at least this many times")
	importBrowserHistoryCmd.Flags().String("start-date", "", "only import URLs visited on or after this date (YYYY-MM-DD)")
	addCrawlerBackendFlags(importBrowserHistoryCmd)
	importBookmarksCmd.Flags().String("browser", "", "limit autodetection to a supported browser")
	importBookmarksCmd.Flags().String("db", "", "bookmark store path (places.sqlite, Bookmarks, or Bookmarks.json)")
	addCrawlerBackendFlags(importBookmarksCmd)

	crawlQueueCmd.Flags().BoolP("count", "c", false, "only print the number of queued URLs")
	crawlURLsCmd.Flags().String("status", "", "filter URLs by status (pending, failed, done, or skipped)")
	crawlURLsCmd.Flags().BoolP("count", "c", false, "only print the number of matching URLs")
	for _, cmd := range []*cobra.Command{crawlListCmd, crawlShowCmd, crawlErrorsCmd, crawlQueueCmd, crawlURLsCmd} {
		addOutputFormatFlag(cmd)
	}

	addDocumentImportFlags(importFileCmd)
	importFileCmd.Flags().Bool("watch", false, "Keep importing created and changed file snapshots until interrupted")
	addCommonImportFlags(importRaindropCmd)
	addCrawlerBackendFlags(importRaindropCmd)
	importRaindropCmd.Flags().String("api-token", "", "Raindrop API token (default: "+raindropTokenEnv+")")
	importRaindropCmd.Flags().String("input", "", "Read a Raindrop CSV export instead of the API (- for stdin)")
	importRaindropCmd.MarkFlagsMutuallyExclusive("input", "api-token")
	addServiceImportFlags(importLinkdingCmd, "Linkding", linkdingTokenEnv)
	addServiceImportFlags(importLinkwardenCmd, "Linkwarden", linkwardenTokenEnv)
	addServiceImportFlags(importKarakeepCmd, "Karakeep", karakeepTokenEnv)
	addServiceImportFlags(importReadeckCmd, "Readeck", readeckTokenEnv)
	addServiceImportFlags(importReadwiseCmd, "Readwise", readwiseTokenEnv)
	addServiceImportFlags(importShaarliCmd, "Shaarli", shaarliSecretEnv)
	addServiceImportFlags(importWallabagCmd, "wallabag", wallabagTokenEnv)
	addQutebrowserCompanionFlags(companionQutebrowserCmd)

	exportCmd.Flags().String("start-date", "", "only export documents updated on or after this date (YYYY-MM-DD)")
	exportCmd.Flags().String("end-date", "", "only export documents updated on or before this date (YYYY-MM-DD)")

	for _, cmd := range []*cobra.Command{createUserCmd, createUserAliasCmd} {
		cmd.Flags().Bool("admin", false, "create user with admin privileges")
		cmd.Flags().Bool("offline", false, "create user directly in the configured database (server must be stopped); prints the new token with --admin")
	}

	for _, cmd := range []*cobra.Command{updateUserCmd, updateUserAliasCmd} {
		cmd.Flags().String("username", "", "new username")
		cmd.Flags().Bool("password", false, "change password")
		cmd.Flags().Bool("regen-token", false, "regenerate access token")
		cmd.Flags().Bool("toggle-admin", false, "toggle admin status")
	}

	deleteCmd.Flags().Bool("dry", false, "display the number of documents that would be deleted without actually deleting them")
	deleteCmd.Flags().BoolP("verbose", "v", false, "list all URLs that would be deleted before performing the deletion. Can be used with --dry")
	deleteCmd.Flags().BoolP("yes", "y", false, "delete without prompting for confirmation")

	updateDocumentsCmd.Flags().Uint("user-id", 0, "new owner user ID; 0 makes documents global")
	updateDocumentsCmd.Flags().String("label", "", "new label; an empty value clears the label")
	updateDocumentsCmd.Flags().String("title", "", "new title; an empty value clears the title")
	updateDocumentsCmd.Flags().String("language", "", "pin a language tag such as en-US, nb, or unknown; use auto to resume detection")
	updateDocumentsCmd.Flags().Bool("dry", false, "display the number of affected documents without updating them")
	updateDocumentsCmd.Flags().BoolP("yes", "y", false, "update without prompting for confirmation")

	for _, cmd := range []*cobra.Command{deleteUserCmd, deleteUserAliasCmd} {
		cmd.Flags().Bool("purge", false, "also delete all indexed documents belonging to the user")
	}

	for _, cmd := range []*cobra.Command{showUserCmd, showUserAliasCmd} {
		cmd.Flags().Bool("show-token", false, "retrieve and display the user's access token")
	}

	reindexCmd.Flags().BoolP("exclude-sensitive", "x", false, "skip sensitive content checks during reindexing, allowing matching documents to be indexed")

	addOutputFormatFlag(searchCmd)
	searchCmd.Flags().StringP("fields", "F", "", "comma-separated list of document fields to display (id, url, title, domain, score, added, updated, language, type, text, favicon, favicon_key, user_id, html)")
	searchCmd.Flags().IntP("limit", "L", 0, "maximum number of results to display (0 means no limit)")
	searchCmd.Flags().String("sort", "relevance", "result order: relevance, date, domain, or visits")
	configureCommandScopes()

	rootCmd.PersistentPreRun = func(_ *cobra.Command, _ []string) { initialize() }

	zerolog.CallerMarshalFunc = func(_ uintptr, file string, line int) string {
		dir, fn := filepath.Split(file)
		if dir == "" {
			return fn + ":" + strconv.Itoa(line)
		}
		_, subdir := filepath.Split(strings.TrimSuffix(dir, "/"))
		return subdir + "/" + fn + ":" + strconv.Itoa(line)
	}
	log.Logger = log.With().Caller().Logger().Output(logging.Default)
}

func initialize() {
	if ll := os.Getenv("HISTER__APP__LOG_LEVEL"); ll != "debug" {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	initConfig()
	if cfg.Crawler.UserAgent != "" {
		UserAgent = cfg.Crawler.UserAgent
	}
	initLog()
	log.Debug().Str("filename", cfg.Filename()).Msg("Config initialization complete")
	log.Debug().Msg("Logging initialization complete")
}

func initConfig() {
	var err error

	if !rootCmd.PersistentFlags().Changed("config") {
		if envConfig := os.Getenv("HISTER_CONFIG"); envConfig != "" {
			cfgFile = envConfig
		}
	}

	cfg, err = config.Load(cfgFile)
	if err != nil {
		exit(1, "Failed to initialize config: "+err.Error())
	}

	if v, _ := rootCmd.PersistentFlags().GetString("log-level"); v != "" && (rootCmd.Flags().Changed("log-level") || cfg.App.LogLevel == "") {
		cfg.App.LogLevel = v
	}
	if v, _ := rootCmd.PersistentFlags().GetString("search-url"); v != "" && (rootCmd.Flags().Changed("search-url") || cfg.App.SearchURL == "") {
		cfg.App.SearchURL = v
	}
	if v, _ := rootCmd.PersistentFlags().GetString("server-url"); v != "" && rootCmd.Flags().Changed("server-url") {
		if err := cfg.UpdateBaseURL(v); err != nil {
			exit(1, "Failed to initialize config: "+err.Error())
		}
	}
	if v, _ := rootCmd.PersistentFlags().GetString("token"); rootCmd.PersistentFlags().Changed("token") {
		cfg.App.AccessToken = v
	}
	if err := cfg.ValidatePublicMode(); err != nil {
		exit(1, "Failed to initialize config: "+err.Error())
	}
}

func initLog() {
	settings := cfg.App
	switch settings.LogLevel {
	case "error", "err", "warning", "warn", "info", "debug", "trace":
	default:
		settings.LogLevel = "debug"
		log.Warn().Str("log_level", cfg.App.LogLevel).Msg("Invalid log level, using debug")
	}
	switch settings.LogFormat {
	case "json", "text", "":
	default:
		settings.LogFormat = "text"
		log.Warn().Str("log_format", cfg.App.LogFormat).Msg("Invalid log format, using text")
	}
	change, err := logging.Default.Prepare(settings)
	if err != nil {
		log.Error().Err(err).Str("log_file", settings.LogFile).Msg("Failed to open log file, falling back to stderr")
		settings.LogFile = ""
		change, err = logging.Default.Prepare(settings)
	}
	if err != nil {
		log.Error().Err(err).Msg("Failed to configure logging")
		return
	}
	defer func() {
		if err := change.Close(); err != nil {
			log.Warn().Err(err).Msg("Failed to close prepared log file")
		}
	}()
	if err := change.Apply(); err != nil {
		log.Warn().Err(err).Msg("Failed to close previous log file")
	}
}

func initDB(accessMode model.AccessMode) {
	initialize := model.Init
	if accessMode == model.ReadOnly {
		initialize = model.InitReadOnly
	}
	err := initialize(cfg)
	if err != nil {
		exit(1, err.Error())
	}
	log.Debug().Msg("Database initialization complete")
}

func initExtractor() {
	if err := extractor.Init(cfg.Extractors); err != nil {
		exit(1, "Extractor initialization error: "+err.Error())
	}
}

func initialAnalyzerFingerprint(indexerVersion int, detectLanguages, keepStopwords bool) string {
	if indexerVersion == -1 {
		return indexer.AnalyzerFingerprint(detectLanguages, keepStopwords)
	}
	// Existing indexes predate the keep_stopwords setting and always removed
	// stop words. Language detection already existed and retains its setting.
	return indexer.AnalyzerFingerprint(detectLanguages, false)
}

func embeddingConfigWarning(storedFingerprint, activeFingerprint string) string {
	if activeFingerprint == "" || storedFingerprint == "" || storedFingerprint == activeFingerprint {
		return ""
	}
	return "The semantic search embedding configuration differs from the indexed configuration. Run `hister reindex` to update your embeddings."
}

func initIndex(accessMode model.AccessMode) *indexer.Indexer {
	initDB(accessMode)
	initExtractor()
	indexCfg := cfg
	if accessMode == model.ReadOnly {
		// Offline URL listing does not need a vector store or embedding workers
		// that write to the SQL database.
		copy := *cfg
		copy.SemanticSearch.Enable = false
		indexCfg = &copy
	}
	idx, err := indexer.New(indexCfg)
	if err != nil {
		exit(1, "Indexer initialization error: "+err.Error())
	}
	v, storedFingerprint, err := idx.GetMetadata()
	if err != nil {
		exit(1, "Failed to retrieve index metadata: "+err.Error())
	}
	storedEmbeddingFingerprint, err := idx.GetEmbeddingFingerprint()
	if err != nil {
		exit(1, "Failed to retrieve embedding configuration metadata: "+err.Error())
	}
	metadataMissing := v == -1 || storedFingerprint == ""
	if metadataMissing {
		legacyMetadata, err := model.GetLegacyIndexerMetadata()
		if err != nil {
			exit(1, "Failed to retrieve legacy index metadata: "+err.Error())
		}
		if legacyMetadata != nil {
			if v == -1 {
				v = legacyMetadata.Version
			}
			if storedFingerprint == "" && legacyMetadata.AnalyzerFingerprint != "" {
				storedFingerprint = legacyMetadata.AnalyzerFingerprint
			}
		}
	}
	activeFingerprint := indexer.AnalyzerFingerprint(cfg.Indexer.DetectLanguages, cfg.Indexer.KeepStopwords)
	if storedFingerprint == "" {
		storedFingerprint = initialAnalyzerFingerprint(v, cfg.Indexer.DetectLanguages, cfg.Indexer.KeepStopwords)
	}
	if v == -1 {
		v = indexer.Version
	}
	if metadataMissing {
		if err := idx.SetMetadata(v, storedFingerprint); err != nil {
			exit(1, "Failed to store index metadata: "+err.Error())
		}
	}
	if indexer.Version > v {
		log.Warn().Msg("There is a new indexer version. Run `hister reindex` to update your index.")
	}
	if storedFingerprint != activeFingerprint {
		log.Warn().Msg("The analyzer configuration differs from the indexed configuration. Run `hister reindex` to update your index.")
	}
	activeEmbeddingFingerprint := cfg.SemanticSearch.EmbeddingFingerprint()
	if warning := embeddingConfigWarning(storedEmbeddingFingerprint, activeEmbeddingFingerprint); warning != "" {
		log.Warn().Msg(warning)
	}
	log.Debug().Msg("Indexer initialization complete")
	return idx
}

func newClient(extraOpts ...client.Option) *client.Client {
	return newClientForConfig(cfg, extraOpts...)
}

func newClientForConfig(c *config.Config, extraOpts ...client.Option) *client.Client {
	opts := []client.Option{client.WithUserAgent(UserAgent)}
	if c.App.AccessToken != "" {
		opts = append(opts, client.WithAccessToken(c.App.AccessToken))
	}
	if rootCmd.PersistentFlags().Changed("client-timeout") {
		t, _ := rootCmd.PersistentFlags().GetInt("client-timeout")
		opts = append(opts, client.WithTimeout(time.Duration(t)*time.Second))
	}
	opts = append(opts, extraOpts...)
	return client.New(c.BaseURL(""), opts...)
}

func Execute() error {
	defer func() { _ = logging.Default.Close() }()
	return rootCmd.Execute()
}
