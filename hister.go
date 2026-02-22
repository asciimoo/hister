package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/hister/config"
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

var (
	cliErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	cliSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	cliInfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	cliWarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	cliBoldStyle    = lipgloss.NewStyle().Bold(true)
)

var (
	cfgFile string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:     "hister",
	Short:   "Web history on steroids",
	Long:    ui.Banner,
	Version: "v0.1.0",
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
		setStrArg(cmd, "address", &cfg.Server.Address)
		server.Listen(cfg)
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

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import browsing history from all supported browsers",
	Long: `Automatically detects and imports browsing history from all supported browsers:
Chrome, Brave, Edge, Firefox, Safari.

For Safari on macOS, Full Disk Access must be granted to the terminal in:
System Settings > Privacy & Security > Full Disk Access
`,
	Args: cobra.MaximumNArgs(0),
	Run:  importHistory,
}

var searchCmd = &cobra.Command{
	Use:   "search [search terms]",
	Short: "Command line search interface",
	Long:  "Command line search interface.\nRun it without arguments to use the TUI interface or pass search terms as arguments to get results on the STDOUT.",
	Args:  cobra.MinimumNArgs(0),
	Run: func(_ *cobra.Command, args []string) {
		if len(args) == 0 {
			if err := ui.SearchTUI(cfg); err != nil {
				exit(1, err.Error())
			}
			return
		}
		qs := strings.Join(args, " ")
		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequest("GET", cfg.BaseURL("/search?q="+url.QueryEscape(qs)), nil)
		if err != nil {
			exit(1, "Failed to create request: "+err.Error())
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req)
		if err != nil {
			exit(1, "Failed to send request to hister: "+err.Error())
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			exit(1, err.Error())
		}
		var res *indexer.Results
		err = json.Unmarshal(body, &res)
		if err != nil {
			exit(1, err.Error())
		}
		for _, r := range res.Documents {
			fmt.Printf("%s\n%s\n\n", r.Title, r.URL)
		}
	},
}

var indexCmd = &cobra.Command{
	Use:   "index URL [URL...]",
	Short: "Index URL [URL...]",
	Long:  "Index one or more URLs",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		setStrArg(cmd, "server-url", &cfg.Server.BaseURL)
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
		for _, u := range args {
			if u == "" {
				log.Warn().Msg("URL must not be empty")
				continue
			}
			formData := url.Values{
				"url": {u},
			}
			client := &http.Client{Timeout: 5 * time.Second}
			req, err := http.NewRequest("POST", cfg.BaseURL("/delete"), strings.NewReader(formData.Encode()))
			if err != nil {
				exit(1, "Failed to create request: "+err.Error())
			}
			req.Header.Set("Origin", "hister://")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := client.Do(req)
			if err != nil {
				exit(1, "Failed to send request to hister: "+err.Error())
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				exit(1, fmt.Sprintf("failed to delete url: Invalid status code (%d)", resp.StatusCode))
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
		err := indexer.Reindex(cfg.IndexPath(), cfg.FullPath("tmp_index.db"), cfg.Rules, skipSensitive)
		if err != nil {
			exit(1, err.Error())
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
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yml", "config file (default paths: ./config.yml or $HOME/.histerrc or $HOME/.config/hister/config.yml)")
	rootCmd.PersistentFlags().StringP("log-level", "l", "info", "set log level (possible options: error, warning, info, debug, trace)")
	rootCmd.PersistentFlags().StringP("search-url", "s", "https://google.com/search?q={query}", "set default search engine url")

	rootCmd.AddCommand(listenCmd)
	rootCmd.AddCommand(createConfigCmd)
	rootCmd.AddCommand(listURLsCmd)
	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(reindexCmd)
	rootCmd.AddCommand(deleteCmd)

	dcfg := config.CreateDefaultConfig()
	listenCmd.Flags().StringP("address", "a", dcfg.Server.Address, "Listen address")
	indexCmd.Flags().StringP("server-url", "u", dcfg.Server.BaseURL, "hister server URL")

	importCmd.Flags().IntP("min-visit", "m", 1, "only import URLs that were opened at least 'min-visit' times")

	reindexCmd.Flags().BoolP("exclude-sensitive", "x", false, "don't add documents that contain sensitive content matched by config.SensitiveContentPatterns")

	cobra.OnInitialize(initialize)

	lout := zerolog.ConsoleWriter{
		Out: os.Stderr,
		FormatTimestamp: func(i any) string {
			return i.(string)
		},
		FormatLevel: func(i any) string {
			return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
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
	if v, _ := rootCmd.PersistentFlags().GetString("search-url"); v != "" && (rootCmd.Flags().Changed("log-level") || cfg.App.SearchURL == "") {
		cfg.App.SearchURL = v
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

func setStrArg(cmd *cobra.Command, arg string, dest *string) {
	if v, err := cmd.Flags().GetString(arg); err == nil && (cmd.Flags().Changed(arg) || *dest == "") {
		*dest = v
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
		exit(1, err.Error())
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
		os.Stderr.Write(prompt)
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
//		os.Stderr.Write(prompt)
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
	client := &http.Client{
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
	req.Header.Set("User-Agent", "Hister")
	r, err := client.Do(req)
	if err != nil {
		return errors.New(`failed to download file: ` + err.Error())
	}
	defer r.Body.Close()
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
	if err := d.Process(); err != nil {
		return errors.New(`failed to process document: ` + err.Error())
	}
	if d.Favicon == "" {
		err := d.DownloadFavicon()
		if err != nil {
			log.Warn().Err(err).Str("URL", d.URL).Msg("failed to download favicon")
		}
	}
	dj, err := json.Marshal(d)
	if err != nil {
		return errors.New(`failed to encode document to JSON: ` + err.Error())
	}
	histerClient := &http.Client{}
	req, err = http.NewRequest("POST", cfg.BaseURL("/add"), bytes.NewBuffer(dj))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Origin", "hister://")
	req.Header.Set("content-Type", "application/json")
	resp, err := histerClient.Do(req)
	if err != nil {
		return errors.New(`failed to send page to hister: ` + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to send page to hister: Invalid status code (%d)", resp.StatusCode)
	}
	return nil
}

type browserDB struct {
	name  string
	path  string
	query string
}

func copyToTemp(src string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()

	tmp, err := os.CreateTemp("", "hister-browser-*.db")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func detectBrowserDBs(minVisit int) []browserDB {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	chromiumQuery := fmt.Sprintf("SELECT DISTINCT url FROM urls WHERE visit_count >= %d ORDER BY visit_count DESC", minVisit)
	safariQuery := fmt.Sprintf(`SELECT DISTINCT h.url
FROM history_items h
INNER JOIN history_visits v ON h.id = v.history_item
GROUP BY h.url
HAVING COUNT(v.id) >= %d
ORDER BY COUNT(v.id) DESC`, minVisit)
	firefoxQuery := fmt.Sprintf("SELECT DISTINCT url FROM moz_places WHERE visit_count >= %d ORDER BY visit_count DESC", minVisit)

	candidates := []browserDB{
		{
			name:  "Chrome",
			path:  filepath.Join(home, "Library/Application Support/Google/Chrome/Default/History"),
			query: chromiumQuery,
		},
		{
			name:  "Brave",
			path:  filepath.Join(home, "Library/Application Support/BraveSoftware/Brave-Browser/Default/History"),
			query: chromiumQuery,
		},
		{
			name:  "Edge",
			path:  filepath.Join(home, "Library/Application Support/Microsoft Edge/Default/History"),
			query: chromiumQuery,
		},
		{
			name:  "Comet",
			path:  filepath.Join(home, "Library/Application Support/Comet/Default/History"),
			query: chromiumQuery,
		},
		{
			name:  "Safari",
			path:  filepath.Join(home, "Library/Safari/History.db"),
			query: safariQuery,
		},
	}

	// Firefox: glob for profile directory
	firefoxGlob := filepath.Join(home, "Library/Application Support/Firefox/Profiles/*.default*/places.sqlite")
	if matches, err := filepath.Glob(firefoxGlob); err == nil {
		for _, m := range matches {
			candidates = append(candidates, browserDB{
				name:  "Firefox",
				path:  m,
				query: firefoxQuery,
			})
		}
	}

	var found []browserDB
	for _, b := range candidates {
		if _, err := os.Stat(b.path); err == nil {
			found = append(found, b)
		}
	}
	return found
}

func importHistory(cmd *cobra.Command, _ []string) {
	minVisit, _ := cmd.Flags().GetInt("min-visit")
	if minVisit < 1 {
		minVisit = 1
	}

	browsers := detectBrowserDBs(minVisit)
	if len(browsers) == 0 {
		exit(1, "No supported browser databases found on this system")
	}

	type dbEntry struct {
		browser  browserDB
		tempPath string
		db       *sql.DB
		count    int
	}

	var entries []dbEntry
	totalCount := 0

	for _, b := range browsers {
		tempPath, err := copyToTemp(b.path)
		if err != nil {
			log.Warn().Err(err).Str("browser", b.name).Msg("Failed to copy database, skipping")
			continue
		}

		db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?immutable=1", tempPath))
		if err != nil {
			log.Warn().Err(err).Str("browser", b.name).Msg("Failed to open database, skipping")
			os.Remove(tempPath)
			continue
		}

		// Build count query from the select query
		countQuery := strings.Replace(b.query, "SELECT DISTINCT", "SELECT COUNT(DISTINCT", 1)
		// Wrap the subquery for Safari's GROUP BY / HAVING form
		if strings.Contains(b.query, "GROUP BY") {
			countQuery = fmt.Sprintf("SELECT COUNT(*) FROM (%s)", b.query)
		} else {
			countQuery = strings.Replace(b.query, "SELECT DISTINCT url", "SELECT COUNT(DISTINCT url)", 1)
			// strip ORDER BY for count
			if idx := strings.Index(countQuery, " ORDER BY"); idx >= 0 {
				countQuery = countQuery[:idx]
			}
		}

		var count int
		row := db.QueryRow(countQuery)
		if err := row.Scan(&count); err != nil {
			log.Warn().Err(err).Str("browser", b.name).Str("query", countQuery).Msg("Failed to count URLs, skipping")
			db.Close()
			os.Remove(tempPath)
			continue
		}

		if count < 1 {
			log.Warn().Str("browser", b.name).Msg("No URLs found matching criteria, skipping")
			db.Close()
			os.Remove(tempPath)
			continue
		}

		fmt.Printf("  %s: %s URLs\n", cliBoldStyle.Render(b.name), cliInfoStyle.Render(fmt.Sprintf("%d", count)))
		entries = append(entries, dbEntry{browser: b, tempPath: tempPath, db: db, count: count})
		totalCount += count
	}

	if totalCount < 1 {
		exit(1, "No URLs found in any browser")
	}

	if !yesNoPrompt(fmt.Sprintf("%d URLs found across %d browser(s). Start import", totalCount, len(entries)), true) {
		for _, e := range entries {
			e.db.Close()
			os.Remove(e.tempPath)
		}
		return
	}

	fmt.Println(cliBoldStyle.Render("IMPORTING"))

	globalIdx := 1
	for _, e := range entries {
		fmt.Printf("\n%s\n", cliInfoStyle.Render("=== "+e.browser.name+" ==="))
		rows, err := e.db.Query(e.browser.query)
		if err != nil {
			log.Warn().Err(err).Str("browser", e.browser.name).Msg("Failed to query URLs, skipping")
			e.db.Close()
			os.Remove(e.tempPath)
			continue
		}
		for rows.Next() {
			var u string
			if err := rows.Scan(&u); err != nil {
				log.Warn().Err(err).Msg("Failed to retrieve URL")
				continue
			}
			fmt.Printf("[%d/%d] %s\n", globalIdx, totalCount, u)
			if err := indexURL(u); err != nil {
				log.Warn().Err(err).Msg("Failed to index URL")
			}
			globalIdx++
		}
		rows.Close()
		e.db.Close()
		os.Remove(e.tempPath)
	}
}

func main() {
	rootCmd.Execute()
}
