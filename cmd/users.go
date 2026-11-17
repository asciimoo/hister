// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/asciimoo/hister/client"
	"github.com/asciimoo/hister/server/model"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage user accounts",
	Long:  "Create, show, update, or delete user accounts on the configured Nogon server. Use user create --offline with the server stopped to create an account directly in its database.",
}

var createUserCmd = &cobra.Command{
	Use:   "create USERNAME",
	Short: "Create a user on the server",
	Long:  "Create a user account on the configured running Nogon server. Requires user handling on that server and an administrator token. Use --offline to create an account directly in the configured database with the server stopped. Offline administrator creation prints the new access token for bootstrap or recovery.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		username := args[0]
		if strings.TrimSpace(username) == "" || strings.ContainsFunc(username, unicode.IsControl) {
			return errors.New("username must not be blank or contain control characters")
		}
		offline, _ := cmd.Flags().GetBool("offline")
		if offline && !cfg.App.UserHandling {
			return errors.New("user_handling is not enabled in configuration")
		}
		password, err := promptConfirmedPassword("Password: ", "Confirm password: ")
		if err != nil {
			return err
		}
		isAdmin, _ := cmd.Flags().GetBool("admin")
		var bootstrapToken string
		if offline {
			if err := model.Init(cfg); err != nil {
				return fmt.Errorf("failed to initialize database: %w", err)
			}
			user, err := model.CreateUser(username, password, isAdmin)
			if err != nil {
				return fmt.Errorf("failed to create user: %w", err)
			}
			if isAdmin {
				bootstrapToken = user.Token
			}
		} else if _, err := newClient().CreateUser(cmd.Context(), username, password, isAdmin); err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
		output := cliSuccessStyle.Render("✓") + " User created: " + cliInfoStyle.Render(username)
		if bootstrapToken != "" {
			output += "\n" + cliInfoStyle.Render("Token:      ") + bootstrapToken
		}
		_, err = lipgloss.Fprintln(cmd.OutOrStdout(), output)
		return err
	},
}

var deleteUserCmd = &cobra.Command{
	Use:   "delete USERNAME",
	Short: "Delete a user on the server",
	Long:  "Delete a user account on the configured running Nogon server. Requires user handling on that server and an administrator token. Use --purge to also remove all indexed documents belonging to the user.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		username := args[0]
		if strings.TrimSpace(username) == "" || strings.ContainsFunc(username, unicode.IsControl) {
			return errors.New("username must not be blank or contain control characters")
		}
		purge, _ := cmd.Flags().GetBool("purge")
		c := newClient()
		user, err := c.GetUser(cmd.Context(), username)
		if err != nil {
			return fmt.Errorf("failed to get user: %w", err)
		}
		result, err := c.DeleteUser(cmd.Context(), user.ID, purge)
		if err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}
		if result.DeletedDocuments > 0 {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s Purged %d document(s) for user %s\n", cliSuccessStyle.Render("✓"), result.DeletedDocuments, cliInfoStyle.Render(result.Username)); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), cliSuccessStyle.Render("✓")+" User deleted: "+cliInfoStyle.Render(result.Username))
		return err
	},
}

var showUserCmd = &cobra.Command{
	Use:   "show USERNAME",
	Short: "Show user information from the server",
	Long:  "Display information about a user account on the configured running Nogon server. Requires user handling on that server and an administrator token. Use --show-token to also retrieve and display the user's access token.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		u, err := c.GetUser(cmd.Context(), args[0])
		if err != nil {
			return fmt.Errorf("failed to get user: %w", err)
		}
		admin := "no"
		if u.IsAdmin {
			admin = "yes"
		}
		lines := []string{
			cliInfoStyle.Render("Username:   ") + u.Username,
			cliInfoStyle.Render("ID:         ") + fmt.Sprintf("%d", u.ID),
			cliInfoStyle.Render("Admin:      ") + admin,
		}
		if showToken, _ := cmd.Flags().GetBool("show-token"); showToken {
			token, err := c.GetUserToken(cmd.Context(), u.ID)
			if err != nil {
				return fmt.Errorf("failed to get user token: %w", err)
			}
			lines = append(lines, cliInfoStyle.Render("Token:      ")+token)
		}
		lines = append(lines,
			cliInfoStyle.Render("Created at: ")+formatLocalTimestamp(u.CreatedAt),
			cliInfoStyle.Render("Updated at: ")+formatLocalTimestamp(u.UpdatedAt),
		)
		_, err = lipgloss.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
		return err
	},
}

var updateUserCmd = &cobra.Command{
	Use:   "update USERNAME",
	Short: "Update a user on the server",
	Long:  "Update a user account on the configured running Nogon server. Requires user handling on that server and an administrator token. Combine flags to change the username or password, regenerate the token, or toggle administrator status in one update. Concurrent account changes are rejected; review the account before retrying.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		username := args[0]
		if strings.TrimSpace(username) == "" || strings.ContainsFunc(username, unicode.IsControl) {
			return errors.New("username must not be blank or contain control characters")
		}
		var changes client.UserUpdate
		if cmd.Flags().Changed("username") {
			newUsername, _ := cmd.Flags().GetString("username")
			if strings.TrimSpace(newUsername) == "" || strings.ContainsFunc(newUsername, unicode.IsControl) {
				return errors.New("new username must not be blank or contain control characters")
			}
			changes.Username = &newUsername
		}
		changePassword, _ := cmd.Flags().GetBool("password")
		changes.RegenerateToken, _ = cmd.Flags().GetBool("regen-token")
		toggleAdmin, _ := cmd.Flags().GetBool("toggle-admin")
		if changes.Username == nil && !changePassword && !changes.RegenerateToken && !toggleAdmin {
			return errors.New("no changes specified; use --username, --password, --regen-token, or --toggle-admin")
		}
		c := newClient()
		user, err := c.GetUser(cmd.Context(), username)
		if err != nil {
			return fmt.Errorf("failed to get user: %w", err)
		}
		if user.Revision == 0 {
			return errors.New("server did not provide an account revision; upgrade the server to support user updates")
		}
		changes.Revision = user.Revision
		if toggleAdmin {
			changes.IsAdmin = new(!user.IsAdmin)
		}
		if changePassword {
			password, err := promptConfirmedPassword("New password: ", "Confirm new password: ")
			if err != nil {
				return err
			}
			changes.Password = &password
		}
		updated, err := c.UpdateUser(cmd.Context(), user.ID, changes)
		if err != nil {
			return fmt.Errorf("failed to update user: %w", err)
		}
		var lines []string
		if changes.Username != nil {
			lines = append(lines, cliSuccessStyle.Render("✓")+" Username changed: "+cliInfoStyle.Render(username)+" → "+cliInfoStyle.Render(updated.Username))
		}
		if changePassword {
			lines = append(lines, cliSuccessStyle.Render("✓")+" Password changed for "+cliInfoStyle.Render(updated.Username))
		}
		if changes.RegenerateToken {
			lines = append(lines, cliSuccessStyle.Render("✓")+" New token for "+cliInfoStyle.Render(updated.Username)+": "+cliInfoStyle.Render(updated.Token))
		}
		if toggleAdmin {
			status := "disabled"
			if updated.IsAdmin {
				status = "enabled"
			}
			lines = append(lines, cliSuccessStyle.Render("✓")+" Admin "+status+" for "+cliInfoStyle.Render(updated.Username))
		}
		_, err = lipgloss.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
		return err
	},
}

func promptConfirmedPassword(passwordPrompt, confirmationPrompt string) (string, error) {
	password, err := promptPassword(passwordPrompt)
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	if len(password) < 8 || len(password) > 72 {
		return "", errors.New("password must contain between 8 and 72 UTF-8 bytes")
	}
	confirmation, err := promptPassword(confirmationPrompt)
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	if password != confirmation {
		return "", errors.New("passwords do not match")
	}
	return password, nil
}

type passwordModel struct {
	input textinput.Model
	done  bool
	err   error
}

func (m passwordModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m passwordModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			m.done = true
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.err = errors.New("cancelled")
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m passwordModel) View() tea.View {
	if m.done || m.err != nil {
		return tea.NewView("")
	}
	return tea.NewView(m.input.View() + "\n")
}

var promptPassword = func(prompt string) (string, error) {
	ti := textinput.New()
	ti.Placeholder = ""
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '*'
	ti.Prompt = prompt
	ti.Focus()

	m := passwordModel{input: ti}
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return "", err
	}
	final := result.(passwordModel)
	if final.err != nil {
		return "", final.err
	}
	return final.input.Value(), nil
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
