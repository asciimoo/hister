package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/ui"

	"github.com/charmbracelet/lipgloss"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const Version = "v0.10.0"

var (
	cliErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	cliSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	cliInfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	cliWarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	cliBoldStyle    = lipgloss.NewStyle().Bold(true)
)

type browserDBCandidates struct {
	name             string
	table_name       string
	paths_candidates []string
}

type browserDB struct {
	name       string
	table_name string
	paths      []string
}

var (
	cfgFile   string
	cfg       *config.Config
	UserAgent = fmt.Sprintf("Mozilla/5.0 (compatible; Hister/%s; +https://hister.org/)", Version)
)

var rootCmd = &cobra.Command{
	Use:     "hister",
	Short:   "Your own search engine",
	Long:    "hister - your own search engine",
	Version: Version,
	//Run: func(_ *cobra.Command, _ []string) {
	//},
}

var listenCmd = &cobra.Command{
	Use:   "listen",
	Short: "Start server",
	Long:  ``,
	PreRun: func(_ *cobra.Command, _ []string) {
		initIndex()
	},
	Run: func(cmd *cobra.Command, _ []string) {
		if a, err := cmd.Flags().GetString("address"); err == nil && cmd.Flags().Changed("address") {
			if err := cfg.UpdateListenAddress(a); err != nil {
				exit(1, `Failed to set server address: `+err.Error())
			}
		}
		if cfg.App.AccessToken != "" && strings.HasPrefix(cfg.BaseURL(""), "http://") {
			log.Warn().Msg("Using authentication token without https. Token is sent plain-text in network requests.")
		}

		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		if len(cfg.Indexer.Directories) > 0 {
			wg.Go(func() {
				if err := indexer.IndexAll(ctx, cfg.Indexer.Directories); err != nil && !errors.Is(err, context.Canceled) {
					log.Error().Err(err).Msg("Initial indexing failed")
				}
				select {
				case <-ctx.Done():
					return
				default:
				}
				if err := files.WatchDirectories(ctx, cfg.Indexer.Directories, func(path string) {
					if err := indexer.IndexFile(path); err != nil {
						log.Debug().Err(err).Str("path", path).Msg("Failed to index file")
					}
				}); err != nil && !errors.Is(err, context.Canceled) {
					log.Error().Err(err).Msg("File watcher failed")
				}
			})
		}
		server.Listen(cfg, func() {
			cancel()
			wg.Wait()
			indexer.Close()
		})
	},
}

var createConfigCmd = &cobra.Command{
	Use:   "create-config [FILENAME]",
	Short: "Create default configuration file",
	Args:  cobra.MaximumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		dcfg := config.CreateDefaultConfig()
		cb, err := yaml.Marshal(dcfg)
		if err != nil {
			panic(err)
		}
		if len(args) > 0 {
			fname := args[0]
			if _, err := os.Stat(fname); err == nil {
				exit(1, fmt.Sprintf(`File "%s" already exists`, fname))
			}
			if err := os.WriteFile(fname, cb, 0o600); err != nil {
				exit(1, `Failed to create config file: `+err.Error())
			}
			fmt.Println(cliSuccessStyle.Render("✓") + " Config file created: " + cliInfoStyle.Render(fname))
		} else {
			fmt.Print(string(cb))
		}
	},
}

var listURLsCmd = &cobra.Command{
	Use:   "list-urls",
	Short: "List indexed URLs",
	Long:  `List indexed URLs - server should be stopped`,
	PreRun: func(_ *cobra.Command, _ []string) {
		initIndex()
	},
	Run: func(_ *cobra.Command, _ []string) {
		indexer.Iterate(func(d *indexer.Document) {
			fmt.Println(d.URL)
		})
	},
}

var listFilesCmd = &cobra.Command{
	Use:   "list-files",
	Short: "List all watched files for indexing",
	Long:  `List all files that match the configured directory watch patterns`,
	Run: func(_ *cobra.Command, _ []string) {
		if len(cfg.Indexer.Directories) == 0 {
			exit(1, "No directories configured for watching")
		}
		for _, dir := range cfg.Indexer.Directories {
			expanded := files.ExpandHome(dir.Path)
			err := filepath.WalkDir(expanded, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					log.Warn().Err(err).Str("path", path).Msg("Error accessing path")
					return nil
				}
				if d.IsDir() {
					if path != expanded && files.ShouldSkipDir(d.Name(), dir.Excludes, dir.IncludeHidden) {
						return filepath.SkipDir
					}
					return nil
				}
				if dir.IsMatching(d.Name()) {
					fmt.Println(path)
				}
				return nil
			})
			if err != nil {
				log.Error().Err(err).Str("directory", expanded).Msg("Failed to walk directory")
			}
		}
	},
}

var importCmd = &cobra.Command{
	Use:   "import BROWSER_TYPE DB_PATH",
	Short: "Import Chrome, Firefox or auto-detect browsing history",
	Long: `
The Firefox URL database file is usually located at /home/[USER]/.mozilla/[PROFILE]/places.sqlite
The Chrome/Chromium URL database fiel is usually located at /home/[USER]/.config/chromium/Default/History
Leave BROWSER_TYPE and DB_PATH empty for auto detection
`,
	Args: ZeroOrTwoArgs(),
	Run:  importHistory,
}

var searchCmd = &cobra.Command{
	Use:   "search [search terms]",
	Short: "Command line search interface",
	Long:  "Command line search interface.\nRun it without arguments to use the TUI interface or pass search terms as arguments to get results on the STDOUT.",
	Args:  cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if err := ui.SearchTUI(cfg); err != nil {
				exit(1, err.Error())
			}
			return
		}
		qs := strings.Join(args, " ")
		c := newClient()
		res, err := c.Search(qs)
		if err != nil {
			exit(1, "Search failed: "+err.Error())
		}
		format, _ := cmd.Flags().GetString("format")
		switch format {
		case "json":
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(res); err != nil {
				exit(1, "Failed to encode JSON: "+err.Error())
			}
		case "csv":
			w := csv.NewWriter(os.Stdout)
			if err := w.Write([]string{"title", "url", "domain", "score", "added", "language", "text"}); err != nil {
				exit(1, "Failed to write CSV header: "+err.Error())
			}
			for _, r := range res.Documents {
				if err := w.Write([]string{
					r.Title,
					r.URL,
					r.Domain,
					strconv.FormatFloat(r.Score, 'f', -1, 64),
					strconv.FormatInt(r.Added, 10),
					r.Language,
					r.Text,
				}); err != nil {
					exit(1, "Failed to write CSV row: "+err.Error())
				}
			}
			w.Flush()
			if err := w.Error(); err != nil {
				exit(1, "Failed to write CSV: "+err.Error())
			}
		default:
			for _, r := range res.Documents {
				fmt.Printf("%s\n%s\n\n", r.Title, r.URL)
			}
		}
	},
}

var indexCmd = &cobra.Command{
	Use:   "index URL [URL...]",
	Short: "Index URL [URL...]",
	Long:  "Index one or more URLs",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		for _, u := range args {
			if err := indexURL(u); err != nil {
				exit(1, "Failed to index URL: "+err.Error())
			}
		}
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete URL [URL...]",
	Short: "Remove page from the index",
	Long:  "Remove one or more pages from the index",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := newClient()
		for _, u := range args {
			if u == "" {
				log.Warn().Msg("URL must not be empty")
				continue
			}
			if !strings.Contains(u, "://") {
				absPath, err := filepath.Abs(u)
				if err != nil {
					exit(1, "Failed to resolve path: "+err.Error())
				}
				u = "file://" + absPath
			}
			if err := c.DeleteDocument(u); err != nil {
				exit(1, "Failed to delete URL: "+err.Error())
			}
		}
	},
}

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Reindex",
	Long:  `Recreate index - server should be stopped`,
	PreRun: func(_ *cobra.Command, _ []string) {
		initDB()
	},
	Run: func(cmd *cobra.Command, args []string) {
		skipSensitive := false
		if b, err := cmd.Flags().GetBool("exclude-sensitive"); err == nil {
			skipSensitive = b
		}
		err := indexer.Reindex(cfg.FullPath(""), cfg.Rules, skipSensitive, cfg.Indexer.DetectLanguages)
		if err != nil {
			exit(1, "Indexer error: "+err.Error())
		}
		if err := model.SetIndexerVersion(indexer.Version); err != nil {
			exit(1, "Failed to update indexer version: "+err.Error())
		}
	},
}

func exit(errno int, msg string) {
	if errno != 0 {
		fmt.Println(cliErrorStyle.Render("Error!") + " " + msg)
	} else {
		fmt.Println(msg)
	}
	os.Exit(errno)
}

func init() {
	dcfg := config.CreateDefaultConfig()
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yml", "config file (default paths: ./config.yml or $HOME/.histerrc or $HOME/.config/hister/config.yml)")
	rootCmd.PersistentFlags().StringP("log-level", "l", "info", "set log level (possible options: error, warning, info, debug, trace)")
	rootCmd.PersistentFlags().StringP("search-url", "s", dcfg.App.SearchURL, "set default search engine url")
	rootCmd.PersistentFlags().StringP("server-url", "u", dcfg.Server.BaseURL, "hister server URL")

	rootCmd.AddCommand(listenCmd)
	rootCmd.AddCommand(createConfigCmd)
	rootCmd.AddCommand(listURLsCmd)
	rootCmd.AddCommand(listFilesCmd)
	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(reindexCmd)
	rootCmd.AddCommand(deleteCmd)

	listenCmd.Flags().StringP("address", "a", dcfg.Server.Address, "Listen address")

	importCmd.Flags().IntP("min-visit", "m", 1, "only import URLs that were opened at least 'min-visit' times")

	reindexCmd.Flags().BoolP("exclude-sensitive", "x", false, "don't add documents that contain sensitive content matched by config.SensitiveContentPatterns")

	searchCmd.Flags().StringP("format", "f", "text", "output format: text, json, csv")

	cobra.OnInitialize(initialize)

	lout := zerolog.ConsoleWriter{
		Out: os.Stderr,
		FormatTimestamp: func(i any) string {
			return i.(string)
		},
		FormatLevel: func(i any) string {
			level := strings.ToUpper(fmt.Sprintf("%-6s", i))
			var color lipgloss.Color
			switch i {
			case "trace":
				color = lipgloss.Color("240") // dark gray
			case "debug":
				color = lipgloss.Color("12") // bright blue
			case "info":
				color = lipgloss.Color("10") // bright green
			case "warn", "warning":
				color = lipgloss.Color("11") // bright yellow
			case "error":
				color = lipgloss.Color("9") // bright red
			case "fatal", "panic":
				color = lipgloss.Color("196") // bold red
			default:
				color = lipgloss.Color("15") // white
			}
			return fmt.Sprintf("| %s |", lipgloss.NewStyle().Foreground(color).Bold(true).Render(level))
		},
	}
	zerolog.CallerMarshalFunc = func(_ uintptr, file string, line int) string {
		dir, fn := filepath.Split(file)
		if dir == "" {
			return fn + ":" + strconv.Itoa(line)
		}
		_, subdir := filepath.Split(strings.TrimSuffix(dir, "/"))
		return subdir + "/" + fn + ":" + strconv.Itoa(line)
	}
	log.Logger = log.With().Caller().Logger()
	log.Logger = log.Output(lout)
}

func initialize() {
	initConfig()
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
}

func initLog() {
	switch cfg.App.LogLevel {
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "warning":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Warn().Str("Invalid config log level", cfg.App.LogLevel)
	}
}

func initDB() {
	err := model.Init(cfg)
	if err != nil {
		exit(1, err.Error())
	}
	log.Debug().Msg("Database initialization complete")
}

func initIndex() {
	initDB()
	if err := indexer.Init(cfg); err != nil {
		exit(1, "Indexer initialization error: "+err.Error())
	}
	v, err := model.GetIndexerVersion()
	if err != nil {
		exit(1, "Failed to retrieve indexer version: "+err.Error())
	}
	if indexer.Version > v {
		log.Warn().Msg(cliWarningStyle.Render("There is a new indexer version. Run `hister reindex` to update your index."))
	}
	log.Debug().Msg("Indexer initialization complete")
}

func yesNoPrompt(label string, def bool) bool {
	choices := "Y/n"
	if !def {
		choices = "y/N"
	}

	prompt := fmt.Appendf(nil, "%s [%s] ", label, choices)
	r := bufio.NewReader(os.Stdin)
	var s string

	for {
		if _, err := os.Stderr.Write(prompt); err != nil {
			return def
		}
		s, _ = r.ReadString('\n')
		s = strings.TrimSpace(s)
		if s == "" {
			return def
		}
		s = strings.ToLower(s)
		if s == "y" || s == "yes" {
			return true
		}
		if s == "n" || s == "no" {
			return false
		}
	}
}

//func stringPrompt(label string) string {
//	var s string
//	r := bufio.NewReader(os.Stdin)
//	for {
//		fmt.Fprint(os.Stderr, label+" ")
//		s, _ = r.ReadString('\n')
//		if s != "" {
//			break
//		}
//	}
//	return strings.TrimSpace(s)
//}
//
//func intPrompt(label string, def int64) int64 {
//	var s string
//	r := bufio.NewReader(os.Stdin)
//	prompt := fmt.Sprintf("%s [%d] ", label, def)
//	for {
//		fmt.Fprint(os.Stderr, prompt)
//		s, _ = r.ReadString('\n')
//		s = strings.TrimSpace(s)
//		if s == "" {
//			return def
//		}
//		i, err := strconv.ParseInt("12345", 10, 64)
//		if err != nil {
//			log.Error().Err(err).Msg("Invalid integer")
//		} else {
//			return i
//		}
//	}
//}
//
//func choicePrompt(label string, choices []string) string {
//	prompt := []byte(fmt.Sprintf("%s [%s,%s] ", label, strings.ToUpper(choices[0]), strings.Join(choices[1:], ",")))
//
//	r := bufio.NewReader(os.Stdin)
//	var s string
//
//	for {
//		_, _ = os.Stderr.Write(prompt)
//		s, _ = r.ReadString('\n')
//		s = strings.TrimSpace(s)
//		if s == "" {
//			return choices[0]
//		}
//		s = strings.ToLower(s)
//		if slices.Contains(choices, s) {
//			return s
//		}
//	}
//}

func indexURL(u string) error {
	httpClient := &http.Client{
		// Websites can be slow or unreachable, we don't want to wait too long for each of them, especially if we are indexing a lot of URLs during import.
		Timeout: 5 * time.Second,
	}
	if u == "" {
		log.Warn().Msg("URL must not be empty")
		return nil
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return errors.New(`failed to download file: ` + err.Error())
	}
	req.Header.Set("User-Agent", UserAgent)
	r, err := httpClient.Do(req)
	if err != nil {
		return errors.New(`failed to download file: ` + err.Error())
	}
	defer func() {
		if cerr := r.Body.Close(); cerr != nil {
			log.Warn().Err(cerr).Msg("failed to close response body")
		}
	}()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid response code: %d", r.StatusCode)
	}
	contentType := r.Header.Get("Content-type")
	if !strings.Contains(contentType, "html") {
		return errors.New("invalid content type: " + contentType)
	}
	buf := bytes.NewBuffer(nil)
	_, err = io.Copy(buf, r.Body)
	if err != nil {
		return errors.New(`failed to read response body: ` + err.Error())
	}

	d := &indexer.Document{
		URL:  u,
		HTML: buf.String(),
	}
	if err := d.Process(nil); err != nil {
		return errors.New(`failed to process document: ` + err.Error())
	}
	if d.Favicon == "" {
		err := d.DownloadFavicon(UserAgent)
		if err != nil {
			log.Warn().Err(err).Str("URL", d.URL).Msg("failed to download favicon")
		}
	}
	c := newClient()
	if err := c.AddDocumentJSON(d); err != nil {
		return fmt.Errorf("failed to send page to hister: %w", err)
	}
	return nil
}

func importHistory(cmd *cobra.Command, args []string) {
	// TODO: get skip rules from server

	var browser string
	if len(args) == 0 {
		browser = ""
	} else {
		browser = strings.ToLower(args[0])
	}

	var foundDBs []browserDB
	var table string
	var dbFiles []string
	switch browser {
	case "firefox":
		table = "moz_places"
		dbFiles = append(dbFiles, args[1])
	case "chrome":
		table = "urls"
		dbFiles = append(dbFiles, args[1])
	default:
		if len(args) > 0 {
			log.Warn().Str("Browser", browser).Msg("Unknown browser, failing back to auto-detect")
		}
		table = "auto-detect"
	}

	if table == "auto-detect" {
		foundDBs = getDBPaths()
		for _, browser := range foundDBs {
			for _, path := range browser.paths {
				importDB(path, browser.table_name, cmd)
			}
		}

	} else {
		for _, path := range dbFiles {
			importDB(path, table, cmd)
		}
	}

	// TODO optional date filter
	//vf := "last_visit_time"
	//if browser == "firefox" {
	//	vf = "last_visit_date"
	//}
	//q += fmt.Sprintf(" AND %s >= datetime('now', 'localtime', '-1 month')", vf)
}

func importDB(dbFile string, table string, cmd *cobra.Command) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?immutable=1", dbFile))
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to open database")
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close database")
		}
	}()

	// Fetch skip rules from the server.
	c := newClient()
	resp, err := c.FetchRules()
	if err != nil {
		log.Error().Err(err).Msg("Unable to obtain skip rules from server; using local ones instead")
	} else {
		// TODO: let the user know that their local rules are being overwritten?
		cfg.Rules.Skip.ReStrs = resp.Skip
		if err := cfg.Rules.Skip.Compile(); err != nil {
			log.Error().Err(err).Msg("Unable to compile skip rules from server")
			return
		}
	}

	q := fmt.Sprintf("SELECT DISTINCT count(url) FROM %s WHERE url LIKE 'http://%%' OR url LIKE 'https://%%'", table)
	if i, err := cmd.Flags().GetInt("min-visit"); err == nil && i > 1 {
		q += fmt.Sprintf(" AND visit_count >= %d", i)
	}
	// TODO: apply skip rules to get a more precise count?
	row := db.QueryRow(q)
	var count int
	if err := row.Scan(&count); err != nil {
		log.Debug().Str("query", q).Msg("count query")
		log.Error().Err(err).Msg("Failed to execute counting query")
		return
	}

	if count < 1 {
		exit(1, "No URLs found to import")
	}

	if !yesNoPrompt(fmt.Sprintf("%d URLs found. Start import form "+dbFile, count), true) {
		return
	}

	q = strings.Replace(q, "count(url)", "url", 1)
	q += " ORDER BY visit_count DESC"

	fmt.Println(cliBoldStyle.Render("IMPORTING"))

	rows, err := db.Query(q, "url")
	if err != nil {
		log.Error().Err(err).Msg("Failed to execute database query")
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close database rows")
		}
	}()
	i := 0
	skipped := 0
	for rows.Next() {
		i += 1
		var u string
		err = rows.Scan(&u)
		if err != nil {
			log.Error().Err(err).Msg("Failed to scan database row")
			return
		}
		if cfg.Rules.IsSkip(u) {
			log.Debug().Str("URL", u).Msg("skip importing URL by rule")
			continue
		}
		exists, err := c.DocumentExists(u)
		if err != nil {
			log.Warn().Err(err).Str("URL", u).Msg("Failed to get info about URL, skipping")
			skipped += 1
			continue
		}
		if exists {
			// skip already added URLs
			continue
		}
		fmt.Printf("[%d/%d] %s\n", i, count, u)
		if err := indexURL(u); err != nil {
			log.Warn().Err(err).Msg("Failed to index URL")
		}
	}

	if skipped != 0 {
		log.Info().Msgf("Skipped %d URLs", skipped)
	}
}

func getDBPaths() []browserDB {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var candidates []browserDBCandidates

	chromium_table := "urls"
	firefox_table := "moz_places"

	switch runtime.GOOS {
	default:
		log.Fatal().Msgf("Failed to dectect os")
	case "darwin":
		candidates = []browserDBCandidates{
			// firefox
			{
				"Firefox",
				firefox_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*.default*", "places.sqlite"),
					filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*.default-release*", "places.sqlite"),
				},
			},
			{
				"Firefox Developer Edition",
				firefox_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*.dev-edition-default*", "places.sqlite"),
				},
			},
			{
				"Zen",
				firefox_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "zen", "Profiles", "*Default*", "places.sqlite"),
				},
			},
			{
				"Waterfox",
				firefox_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Waterfox", "Profiles", "*.default*", "places.sqlite"),
				},
			},
			{
				"Chrome",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default", "History"),
					filepath.Join(home, "Library", "Application Support", "Google", "Chrome Beta", "Default", "History"),
					filepath.Join(home, "Library", "Application Support", "Google", "Chrome Canary", "Default", "History"),
				},
			},
			{
				"Chromium",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Chromium", "Default", "History"),
				},
			},
			{
				"Brave",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser", "Default", "History"),
					filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser-Beta", "Default", "History"),
				},
			},
			{
				"Edge",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Microsoft Edge", "Default", "History"),
					filepath.Join(home, "Library", "Application Support", "Microsoft Edge Beta", "Default", "History"),
				},
			},
			{
				"Vivaldi",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "Vivaldi", "Default", "History"),
				},
			},
			{
				"Opera",
				chromium_table,
				[]string{
					filepath.Join(home, "Library", "Application Support", "com.operasoftware.Opera", "Default", "History"),
				},
			},
		}
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		appData := os.Getenv("APPDATA")
		if localAppData != "" {
			candidates = []browserDBCandidates{
				{
					"firefox",
					firefox_table,
					[]string{
						filepath.Join(appData, "Mozilla", "Firefox", "Profiles", "*.default*", "places.sqlite"),
						filepath.Join(appData, "Mozilla", "Firefox", "Profiles", "*.default-release*", "places.sqlite"),
					},
				},
				{
					"Zen",
					firefox_table,
					[]string{
						filepath.Join(appData, "zen", "Profiles", "*.Default*", "places.sqlite"),
					},
				},
				{
					"Waterfox",
					firefox_table,
					[]string{
						filepath.Join(appData, "Waterfox", "Profiles", "*.default*", "places.sqlite"),
					},
				},
				{
					"Chrome",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "Google", "Chrome", "User Data", "Default", "History"),
						filepath.Join(localAppData, "Google", "Chrome Beta", "User Data", "Default", "History"),
					},
				},
				{
					"Chromium",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "Chromium", "User Data", "Default", "History"),
					},
				},
				{
					"Brave",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "User Data", "Default", "History"),
					},
				},
				{
					"Edge",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "Microsoft", "Edge", "User Data", "Default", "History"),
					},
				},
				{
					"Vivaldi",
					chromium_table,
					[]string{
						filepath.Join(localAppData, "Vivaldi", "User Data", "Default", "History"),
					},
				},
				{
					"Opera",
					chromium_table,
					[]string{
						filepath.Join(appData, "Opera Software", "Opera Stable", "History"),
					},
				},
			}
		}
	case "linux":
		candidates = []browserDBCandidates{
			{
				"firefox",
				firefox_table,
				[]string{
					filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox", "*.default*", "places.sqlite"),
					filepath.Join(home, ".mozilla", "firefox", "*.default*", "places.sqlite"),
				},
			},
			{
				"Firefox Developer Edition",
				firefox_table,
				[]string{
					filepath.Join(home, ".mozilla", "firefox", "*.dev-edition-default*", "places.sqlite"),
				},
			},
			{
				"Zen",
				firefox_table,
				[]string{
					filepath.Join(home, ".zen", "*.Default*", "places.sqlite"),
					filepath.Join(home, ".config", "zen", "*.Default*", "places.sqlite"),
				},
			},
			{
				"Waterfox",
				firefox_table,
				[]string{
					filepath.Join(home, ".waterfox", "Profiles", "*.default*", "places.sqlite"),
				},
			},
			{
				"Chrome",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "google-chrome", "Default", "History"),
					filepath.Join(home, ".config", "google-chrome-beta", "Default", "History"),
				},
			},
			{
				"Chromium",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "chromium", "Default", "History"),
					filepath.Join(home, "snap", "chromium", "common", "chromium", "Default", "History"),
				},
			},
			{
				"Brave",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "BraveSoftware", "Brave-Browser", "Default", "History"),
				},
			},
			{
				"Edge",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "microsoft-edge", "Default", "History"),
					filepath.Join(home, ".config", "microsoft-edge-beta", "Default", "History"),
				},
			},
			{
				"Vivaldi",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "vivaldi", "Default", "History"),
				},
			},
			{
				"Opera",
				chromium_table,
				[]string{
					filepath.Join(home, ".config", "opera", "Default", "History"),
				},
			},
		}
	}

	var dbFiles []browserDB
	var paths []string

	for _, canidate := range candidates {
		for _, globs := range canidate.paths_candidates {
			matches, _ := filepath.Glob(globs)
			for _, p := range matches {
				if _, err := os.Stat(p); err == nil {
					paths = append(paths, p)
				}
			}
		}

		if len(paths) != 0 {
			dbFiles = append(dbFiles, browserDB{canidate.name, canidate.table_name, paths})
		}
		paths = []string{}
	}
	return dbFiles
}

func newClient() *client.Client {
	opts := []client.Option{client.WithUserAgent(UserAgent)}
	if cfg.App.AccessToken != "" {
		opts = append(opts, client.WithAccessToken(cfg.App.AccessToken))
	}
	return client.New(cfg.BaseURL(""), opts...)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func ZeroOrTwoArgs() cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 && len(args) != 2 {
			return fmt.Errorf("accepts 0 or 2 arguments, received %d", len(args))
		}
		return nil
	}
}
