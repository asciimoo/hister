package cmd

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var listURLsCmd = &cobra.Command{
	Use:   "list-urls",
	Short: "List indexed URLs",
	Long:  `List all indexed URLs by fetching them from the running server`,
	Run: func(cmd *cobra.Command, _ []string) {
		offline, _ := cmd.Flags().GetBool("offline")
		if offline {
			idx := initIndex(model.ReadOnly)
			defer idx.Close()
			idx.Iterate(func(doc *document.Document) {
				fmt.Println(doc.URL)
			})
			return
		}
		c := newClient(client.WithTimeout(0))
		pageKey := ""
		for {
			res, err := c.Search(&indexer.Query{Text: "*", PageKey: pageKey, Sort: "domain"})
			if err != nil {
				exit(1, "Failed to fetch URLs: "+err.Error())
			}
			for _, doc := range res.Documents {
				fmt.Println(doc.URL)
			}
			if res.PageKey == "" || len(res.Documents) == 0 {
				break
			}
			pageKey = res.PageKey
		}
	},
}

var listFilesCmd = &cobra.Command{
	Use:   "list-files",
	Short: "List all watched files for indexing",
	Long:  `List all files that match the configured directory watch patterns`,
	Run: func(cmd *cobra.Command, _ []string) {
		if len(cfg.Indexer.Directories) == 0 {
			exit(1, "No directories configured for watching")
		}
		relative, _ := cmd.Flags().GetBool("relative")
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
					outputPath := path
					if relative {
						var relErr error
						outputPath, relErr = filepath.Rel(expanded, path)
						if relErr != nil {
							log.Warn().Err(relErr).Str("path", path).Msg("Failed to make path relative")
							outputPath = path
						}
					}
					fmt.Println(outputPath)
				}
				return nil
			})
			if err != nil {
				log.Error().Err(err).Str("directory", expanded).Msg("Failed to walk directory")
			}
		}
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete QUERY",
	Short: "Remove documents from the index",
	Long: `Remove documents from the index using the search query language.

The QUERY syntax is the same as the search queries.

Examples:
  hister delete "url:https://example.com/page"
  hister delete "url:file:///home/user/file.pdf"
  hister delete "domain:example.com"
  hister delete "language:en domain:example.com"
  hister delete --dry "updated:>90d"

Non-admin users are restricted to their own documents by the server.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c := newClient()
		dry, _ := cmd.Flags().GetBool("dry")
		verbose, _ := cmd.Flags().GetBool("verbose")
		yes, _ := cmd.Flags().GetBool("yes")
		var total uint64
		if verbose {
			var pageKey string
			for {
				res, err := c.Search(&indexer.Query{Text: args[0], PageKey: pageKey, Sort: "domain"})
				if err != nil {
					exit(1, "Failed to search: "+err.Error())
				}
				if total == 0 {
					total = res.Total
				}
				for _, doc := range res.Documents {
					fmt.Println(doc.URL)
				}
				if res.PageKey == "" || len(res.Documents) == 0 {
					break
				}
				pageKey = res.PageKey
			}
			if dry {
				fmt.Printf("%d document(s) would be deleted\n", total)
				return
			}
		} else {
			res, err := c.Search(&indexer.Query{Text: args[0]})
			if err != nil {
				exit(1, "Failed to search: "+err.Error())
			}
			total = res.Total
			if dry {
				fmt.Printf("%d document(s) would be deleted\n", total)
				return
			}
		}
		if total == 0 {
			fmt.Println("No documents matched the query")
			return
		}
		if !yes && !yesNoPrompt(fmt.Sprintf("Delete %d document(s)?", total), false) {
			fmt.Println("Deletion cancelled")
			return
		}
		if err := c.DeleteDocuments(args[0]); err != nil {
			exit(1, "Failed to delete: "+err.Error())
		}
	},
}
