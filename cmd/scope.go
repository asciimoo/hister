package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

type executionScope string

const (
	executionScopeAnnotation  = "hister_execution_scope"
	applicableFlagsAnnotation = "hister_applicable_global_flags"

	executionScopeLocal  executionScope = "local"
	executionScopeRemote executionScope = "remote"
	executionScopeHybrid executionScope = "hybrid"
)

var baseApplicableGlobalFlags = []string{"config", "log-level"}

var connectionGlobalFlags = []string{"server-url", "token"}

var scopedGlobalFlagNames = []string{
	"client-timeout",
	"config",
	"log-level",
	"search-url",
	"server-url",
	"token",
}

func configureCommandScopes() {
	setCommandScope(listenCmd, executionScopeLocal, "server-url", "token", "search-url")
	setCommandScope(createConfigCmd, executionScopeLocal)
	setCommandScope(listURLsCmd, executionScopeHybrid)
	setCommandScope(listFilesCmd, executionScopeLocal)
	setCommandScope(indexCmd, executionScopeHybrid, "client-timeout")
	setCommandScope(exportCmd, executionScopeRemote)
	setCommandScope(importCmd, executionScopeHybrid)
	setCommandScope(searchCmd, executionScopeRemote, "client-timeout", "search-url")
	setCommandScope(reindexCmd, executionScopeRemote)
	setCommandScope(cleanupCmd, executionScopeRemote)
	setCommandScope(deleteCmd, executionScopeRemote, "client-timeout")
	setCommandScope(updateDocumentsCmd, executionScopeRemote, "client-timeout")

	setCommandScope(createUserCmd, executionScopeLocal)
	setCommandScope(deleteUserCmd, executionScopeHybrid, "client-timeout")
	setCommandScope(showUserCmd, executionScopeLocal)
	setCommandScope(updateUserCmd, executionScopeLocal)

	setCommandScope(crawlCmd, executionScopeLocal)
	setCommandScope(crawlListCmd, executionScopeLocal)
	setCommandScope(crawlShowCmd, executionScopeLocal)
	setCommandScope(crawlErrorsCmd, executionScopeLocal)
	setCommandScope(crawlQueueCmd, executionScopeLocal)
	setCommandScope(crawlURLsCmd, executionScopeLocal)
	setCommandScope(crawlDeleteCmd, executionScopeLocal)

	setCommandScope(companionCmd, executionScopeRemote, "client-timeout")
	setCommandScope(statsDomainsCmd, executionScopeRemote, "client-timeout")
	setCommandScope(companionQutebrowserCmd, executionScopeRemote, "client-timeout")

	setCommandScope(importFileCmd, executionScopeHybrid)
	setCommandScope(importBrowserCmd, executionScopeHybrid, "client-timeout")
	setCommandScope(importLinkdingCmd, executionScopeRemote)
	setCommandScope(importLinkwardenCmd, executionScopeRemote)
	setCommandScope(importKarakeepCmd, executionScopeRemote)
	setCommandScope(importReadeckCmd, executionScopeRemote)
	setCommandScope(importShaarliCmd, executionScopeRemote)
	setCommandScope(importWallabagCmd, executionScopeRemote)

	configureScopeGroups(rootCmd)
	configureScopeGroups(importCmd)
	configureScopeGroups(crawlCmd)
	configureScopeGroups(companionCmd)
	configureScopeGroups(statsCmd)
	configureScopedHelp()
}

func configureScopeGroups(parent *cobra.Command) {
	present := make(map[string]bool)
	for _, cmd := range parent.Commands() {
		if scope := cmd.Annotations[executionScopeAnnotation]; scope != "" {
			cmd.GroupID = scope
			present[scope] = true
		}
	}
	groups := []*cobra.Group{
		{ID: string(executionScopeLocal), Title: "Local Commands:"},
		{ID: string(executionScopeRemote), Title: "Remote Commands:"},
		{ID: string(executionScopeHybrid), Title: "Hybrid Commands:"},
	}
	for _, group := range groups {
		if present[group.ID] {
			parent.AddGroup(group)
		}
	}
}

func setCommandScope(cmd *cobra.Command, scope executionScope, extraGlobalFlags ...string) {
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations[executionScopeAnnotation] = string(scope)

	flags := append([]string{}, baseApplicableGlobalFlags...)
	if scope == executionScopeRemote || scope == executionScopeHybrid {
		flags = append(flags, connectionGlobalFlags...)
	}
	flags = append(flags, extraGlobalFlags...)
	cmd.Annotations[applicableFlagsAnnotation] = strings.Join(flags, ",")

	long := cmd.Long
	if long == "" {
		long = cmd.Short
	}
	cmd.Long = strings.TrimRight(long, "\n") + "\n\nExecution scope:\n  " + executionScopeDescription(scope)
}

func executionScopeDescription(scope executionScope) string {
	switch scope {
	case executionScopeLocal:
		return "Local. Runs against configured files and local Hister data without contacting the Hister HTTP server."
	case executionScopeRemote:
		return "Remote. Uses the configured Hister HTTP server without opening the local Hister database or search index."
	case executionScopeHybrid:
		return "Hybrid. Uses or can use both local Hister state and the configured Hister HTTP server."
	default:
		return ""
	}
}

func configureScopedHelp() {
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		restore := hideInapplicableGlobalFlags(cmd)
		defer restore()
		defaultHelp(cmd, args)
	})
}

func hideInapplicableGlobalFlags(cmd *cobra.Command) func() {
	applicable, scoped := applicableGlobalFlags(cmd)
	if !scoped {
		return func() {}
	}

	type hiddenState struct {
		name   string
		hidden bool
	}
	states := make([]hiddenState, 0, len(scopedGlobalFlagNames))
	flags := cmd.InheritedFlags()
	for _, name := range scopedGlobalFlagNames {
		flag := flags.Lookup(name)
		if flag == nil || applicable[name] {
			continue
		}
		states = append(states, hiddenState{name: name, hidden: flag.Hidden})
		flag.Hidden = true
	}
	return func() {
		for _, state := range states {
			if flag := flags.Lookup(state.name); flag != nil {
				flag.Hidden = state.hidden
			}
		}
	}
}

func applicableGlobalFlags(cmd *cobra.Command) (map[string]bool, bool) {
	raw, ok := cmd.Annotations[applicableFlagsAnnotation]
	if !ok {
		return nil, false
	}
	result := make(map[string]bool)
	for name := range strings.SplitSeq(raw, ",") {
		result[name] = true
	}
	return result, true
}
