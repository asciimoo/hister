package cmd

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/document"
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
			res, err := c.Search(&client.SearchQuery{Text: "*", PageKey: pageKey, Sort: "domain"})
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
	Short: "List files matching configured directory watch rules",
	Long: `List all files that match the configured directory watch patterns.

This lists indexing candidates, including files rejected by sensitive content,
size, or format checks. It does not report whether a file is indexed.
Rejected files remain watched and are checked again when they change.`,
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
  nogon delete "url:https://example.com/page"
  nogon delete "url:file:///home/user/file.pdf"
  nogon delete "domain:example.com"
  nogon delete "language:en domain:example.com"
  nogon delete --dry "updated:>90d"

Non-admin users are restricted to their own documents by the server.
Dry runs and confirmation counts use the server's deletion scope. Administrators
can delete across users; use user_id:ID to restrict the query to one owner.
Verbose output lists URLs visible in search, which may differ from the documents
selected for deletion.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		dry, _ := cmd.Flags().GetBool("dry")
		verbose, _ := cmd.Flags().GetBool("verbose")
		yes, _ := cmd.Flags().GetBool("yes")
		total, err := c.CountDocumentsForDeletion(args[0])
		if err != nil {
			return fmt.Errorf("failed to inspect documents for deletion: %w", err)
		}
		if verbose {
			cmd.PrintErrln("URLs visible in search may differ from the documents selected for deletion; the count uses deletion scope.")
			var pageKey string
			for {
				res, err := c.Search(&client.SearchQuery{Text: args[0], PageKey: pageKey, Sort: "domain"})
				if err != nil {
					return fmt.Errorf("failed to list URLs visible in search: %w", err)
				}
				for _, doc := range res.Documents {
					fmt.Println(doc.URL)
				}
				if res.PageKey == "" || len(res.Documents) == 0 {
					break
				}
				pageKey = res.PageKey
			}
		}
		if dry {
			fmt.Printf("%d document(s) would be deleted\n", total)
			return nil
		}
		if total == 0 {
			fmt.Println("No documents matched the query")
			return nil
		}
		if !yes && !yesNoPrompt(fmt.Sprintf("Delete %d document(s)?", total), false) {
			fmt.Println("Deletion cancelled")
			return nil
		}
		if err := c.DeleteDocuments(args[0]); err != nil {
			return fmt.Errorf("failed to delete: %w", err)
		}
		return nil
	},
}
