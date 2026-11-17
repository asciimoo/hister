// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"fmt"

	"github.com/asciimoo/hister/server/types"

	"github.com/spf13/cobra"
)

var updateDocumentsCmd = &cobra.Command{
	Use:   "update QUERY",
	Short: "Update attributes of documents matching a query",
	Long: `Update mutable attributes of every document matching a search query.

Supported attributes are owner user ID, label, title, and language. An empty
label or title clears that value. A language choice is pinned across reindexing
and resubmission. Use "unknown" to pin the default analyzer, or "auto" to clear
the pin and detect the language of the stored text using the server settings.

User ID 0 is reserved for global documents and does not represent a user
account. The query "user_id:0" selects global documents, while "--user-id 0"
makes the matching documents global and visible to every user.

Changing an owner requires administrator access. A move is skipped when the
destination user already owns a document with the same URL. Local file moves
also require the configured watched directory owner to match the destination.

Examples:
  nogon update "user_id:0" --user-id 2
  nogon update "domain:example.com" --label research
  nogon update "language:unknown" --language en
  nogon update "domain:example.com" --language auto
  nogon update "label:old" --label ""`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		changes := documentChangesFromFlags(cmd)
		if changes.Empty() {
			exit(1, "no changes specified; use --user-id, --label, --title, or --language")
		}
		request := types.UpdateDocumentsRequest{Query: args[0], Changes: changes}
		dry, _ := cmd.Flags().GetBool("dry")
		yes, _ := cmd.Flags().GetBool("yes")
		c := newClient()

		if dry || !yes {
			request.DryRun = true
			preview, err := c.UpdateDocuments(request)
			if err != nil {
				exit(1, "Failed to inspect documents: "+err.Error())
			}
			printDocumentUpdateSummary(preview, true)
			if dry || preview.Updated == 0 {
				return
			}
			if !yesNoPrompt(fmt.Sprintf("Update %d document(s)?", preview.Updated), false) {
				fmt.Println("Update cancelled")
				return
			}
		}

		request.DryRun = false
		result, err := c.UpdateDocuments(request)
		if err != nil {
			exit(1, "Failed to update documents: "+err.Error())
		}
		printDocumentUpdateSummary(result, false)
	},
}

func documentChangesFromFlags(cmd *cobra.Command) types.DocumentChanges {
	changes := types.DocumentChanges{}
	if cmd.Flags().Changed("user-id") {
		value, _ := cmd.Flags().GetUint("user-id")
		changes.UserID = &value
	}
	if cmd.Flags().Changed("label") {
		value, _ := cmd.Flags().GetString("label")
		changes.Label = &value
	}
	if cmd.Flags().Changed("title") {
		value, _ := cmd.Flags().GetString("title")
		changes.Title = &value
	}
	if cmd.Flags().Changed("language") {
		value, _ := cmd.Flags().GetString("language")
		changes.Language = &value
	}
	return changes
}

func printDocumentUpdateSummary(result *types.UpdateDocumentsResult, dryRun bool) {
	action := "would be updated"
	if !dryRun {
		action = "updated"
	}
	fmt.Printf("%d document(s) matched; %d %s", result.Matched, result.Updated, action)
	if result.Unchanged > 0 {
		fmt.Printf("; %d unchanged", result.Unchanged)
	}
	if result.Conflicts > 0 {
		fmt.Printf("; %d ownership conflict(s) skipped", result.Conflicts)
	}
	fmt.Println()
}
