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

const Version = "v0.5.0"

var (
	cliErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	cliSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	cliInfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	cliWarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	cliBoldStyle    = lipgloss.NewStyle().Bold(true)
)

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
	Use:   "import BROWSER_TYPE DB_PATH",
	Short: "Import Chrome or Firefox browsing history",
	Long: `
The Firefox URL database file is usually located at /home/[USER]/.mozilla/[PROFILE]/places.sqlite
The Chrome/Chromium URL database fiel is usually located at /home/[USER]/.config/chromium/Default/History
`,
	Args: cobra.ExactArgs(2),
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
		req, err := newHisterRequest("GET", "/search?q="+url.QueryEscape(qs), nil)
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
			req, err := newHisterRequest("POST", "/delete", strings.NewReader(formData.Encode()))
			if err != nil {
				exit(1, "Failed to create request: "+err.Error())
			}
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
		initIndex()
	},
	Run: func(cmd *cobra.Command, args []string) {
		skipSensitive := false
		if b, err := cmd.Flags().GetBool("exclude-sensitive"); err == nil {
			skipSensitive = b
		}
		err := indexer.Reindex(cfg.IndexPath(), cfg.FullPath("tmp_index.db"), cfg.Rules, skipSensitive)
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
	rootCmd.AddCommand(indexCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(reindexCmd)
	rootCmd.AddCommand(deleteCmd)

	listenCmd.Flags().StringP("address", "a", dcfg.Server.Address, "Listen address")

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
	if v, _ := rootCmd.PersistentFlags().GetString("search-url"); v != "" && (rootCmd.Flags().Changed("search-url") || cfg.App.SearchURL == "") {
		cfg.App.SearchURL = v
	}
	if v, _ := rootCmd.PersistentFlags().GetString("server-url"); v != "" && (rootCmd.Flags().Changed("server-url") || cfg.App.SearchURL == "") {
		cfg.Server.BaseURL = v
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
	req, err := newRequest("GET", u, nil)
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
		err := d.DownloadFavicon(UserAgent)
		if err != nil {
			log.Warn().Err(err).Str("URL", d.URL).Msg("failed to download favicon")
		}
	}
	dj, err := json.Marshal(d)
	if err != nil {
		return errors.New(`failed to encode document to JSON: ` + err.Error())
	}
	histerClient := &http.Client{}
	req, err = newHisterRequest("POST", "/add", bytes.NewBuffer(dj))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
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

func importHistory(cmd *cobra.Command, args []string) {
	browser := args[0]
	if browser != "firefox" && browser != "chrome" {
		exit(1, "Invalid browser type it should be 'firefox' or 'chrome'")
	}
	dbFile := args[1]
	table := "urls"
	if browser == "firefox" {
		table = "moz_places"
	}
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?immutable=1", dbFile))
	if err != nil {
		exit(1, "Failed to open database: "+err.Error())
	}
	defer db.Close()
	q := fmt.Sprintf("SELECT DISTINCT url FROM %s WHERE 1=1", table)
	if i, err := cmd.Flags().GetInt("min-visit"); err == nil && i > 1 {
		q += fmt.Sprintf(" AND visit_count >= %d", i)
	}

	cq := strings.Replace(q, "DISTINCT url", "DISTINCT count(url)", 1)
	row := db.QueryRow(cq)
	var count int
	if err := row.Scan(&count); err != nil {
		log.Debug().Str("query", cq).Msg("count query")
		exit(1, "Failed to execute database query: "+err.Error())
	}

	if count < 1 {
		exit(1, "No URLs found")
	}

	if !yesNoPrompt(fmt.Sprintf("%d URLs found. Start import", count), true) {
		return
	}

	q += " ORDER BY visit_count DESC"

	fmt.Println(cliBoldStyle.Render("IMPORTING"))

	rows, err := db.Query(q)
	if err != nil {
		exit(1, "Failed to execute database query: "+err.Error())
	}
	defer rows.Close()
	i := 1
	client := &http.Client{}
	for rows.Next() {
		var u string
		err = rows.Scan(&u)
		if err != nil {
			exit(1, "Failed to retreive URL: "+err.Error())
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			continue
		}
		req, err := newHisterRequest("GET", "/document?url="+url.QueryEscape(u), nil)
		if err != nil {
			log.Warn().Err(err).Str("URL", u).Msg("Failed to create request, skipping ")
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Warn().Err(err).Str("URL", u).Msg("Failed to get info about URL, skipping")
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			// skip already added URLs
			continue
		}
		fmt.Printf("[%d/%d] %s\n", i, count, u)
		if err := indexURL(u); err != nil {
			log.Warn().Err(err).Msg("Failed to index URL")
		}
		i += 1
	}

	// TODO optional date filter
	//vf := "last_visit_time"
	//if browser == "firefox" {
	//	vf = "last_visit_date"
	//}
	//q += fmt.Sprintf(" AND %s >= datetime('now', 'localtime', '-1 month')", vf)
}

func newRequest(method, u string, payload io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, u, payload)
	if err != nil {
		return req, err
	}
	req.Header.Set("User-Agent", UserAgent)
	return req, nil
}

func newHisterRequest(method, u string, payload io.Reader) (*http.Request, error) {
	req, err := newRequest(method, cfg.BaseURL(u), payload)
	if err != nil {
		return req, err
	}
	req.Header.Set("Origin", "hister://")
	return req, nil
}

func main() {
	rootCmd.Execute()
}
