// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package cmd

import (
	"errors"
	"fmt"

	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/server/model"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
)

var createUserCmd = &cobra.Command{
	Use:    "create-user USERNAME",
	Short:  "Create a new user",
	Long:   "Create a new user account (requires user_handling to be enabled)",
	Args:   cobra.ExactArgs(1),
	PreRun: requireUserHandlingAndInitDB(model.ReadWrite),
	Run: func(cmd *cobra.Command, args []string) {
		username := args[0]
		password, err := promptConfirmedPassword("Password: ", "Confirm password: ")
		if err != nil {
			exit(1, err.Error())
		}
		isAdmin, _ := cmd.Flags().GetBool("admin")
		if _, err := model.CreateUser(username, password, isAdmin); err != nil {
			exit(1, "Failed to create user: "+err.Error())
		}
		cliPrintln(cliSuccessStyle.Render("✓") + " User created: " + cliInfoStyle.Render(username))
	},
}

var deleteUserCmd = &cobra.Command{
	Use:    "delete-user USERNAME",
	Short:  "Delete a user",
	Long:   "Delete a user account (requires user_handling to be enabled). Use --purge to also remove all indexed documents belonging to the user.",
	Args:   cobra.ExactArgs(1),
	PreRun: requireUserHandlingAndInitDB(model.ReadWrite),
	Run: func(cmd *cobra.Command, args []string) {
		username := args[0]
		u, err := model.GetUser(username)
		if err != nil {
			exit(1, "Failed to get user: "+err.Error())
		}
		c := newClient()
		q := fmt.Sprintf("user_id:%d", u.ID)
		res, err := c.Search(&indexer.Query{Text: q})
		if err != nil {
			exit(1, "Failed to check user documents: "+err.Error())
		}
		if res.Total > 0 {
			purge, _ := cmd.Flags().GetBool("purge")
			if !purge {
				exit(1, fmt.Sprintf("User %q has %d indexed document(s). Use --purge to delete them along with the user.", username, res.Total))
			}
			if err := c.DeleteDocuments(q); err != nil {
				exit(1, "Failed to purge user documents: "+err.Error())
			}
			cliPrintf("%s Purged %d document(s) for user %s\n", cliSuccessStyle.Render("✓"), res.Total, cliInfoStyle.Render(username))
		}
		if err := model.DeleteUser(username); err != nil {
			exit(1, "Failed to delete user: "+err.Error())
		}
		cliPrintln(cliSuccessStyle.Render("✓") + " User deleted: " + cliInfoStyle.Render(username))
	},
}

var showUserCmd = &cobra.Command{
	Use:    "show-user USERNAME",
	Short:  "Show user information",
	Long:   "Display information about a user account (requires user_handling to be enabled)",
	Args:   cobra.ExactArgs(1),
	PreRun: requireUserHandlingAndInitDB(model.ReadOnly),
	Run: func(cmd *cobra.Command, args []string) {
		u, err := model.GetUser(args[0])
		if err != nil {
			exit(1, "Failed to get user: "+err.Error())
		}
		admin := "no"
		if u.IsAdmin {
			admin = "yes"
		}
		cliPrintln(cliInfoStyle.Render("Username:   ") + u.Username)
		cliPrintln(cliInfoStyle.Render("ID:         ") + fmt.Sprintf("%d", u.ID))
		cliPrintln(cliInfoStyle.Render("Admin:      ") + admin)
		if showToken, _ := cmd.Flags().GetBool("token"); showToken {
			cliPrintln(cliInfoStyle.Render("Token:      ") + u.Token)
		}
		cliPrintln(cliInfoStyle.Render("Created at: ") + u.CreatedAt.Format("2006-01-02 15:04:05"))
		cliPrintln(cliInfoStyle.Render("Updated at: ") + u.UpdatedAt.Format("2006-01-02 15:04:05"))
	},
}

var updateUserCmd = &cobra.Command{
	Use:    "update-user USERNAME",
	Short:  "Update a user",
	Long:   "Update a user account (requires user_handling to be enabled). Use flags to change username or password, regenerate token, or toggle admin status.",
	Args:   cobra.ExactArgs(1),
	PreRun: requireUserHandlingAndInitDB(model.ReadWrite),
	Run: func(cmd *cobra.Command, args []string) {
		username := args[0]
		changed := false

		if newUsername, _ := cmd.Flags().GetString("username"); newUsername != "" {
			if err := model.UpdateUsername(username, newUsername); err != nil {
				exit(1, "Failed to update username: "+err.Error())
			}
			cliPrintln(cliSuccessStyle.Render("✓") + " Username changed: " + cliInfoStyle.Render(username) + " → " + cliInfoStyle.Render(newUsername))
			username = newUsername
			changed = true
		}

		if changePassword, _ := cmd.Flags().GetBool("password"); changePassword {
			password, err := promptConfirmedPassword("New password: ", "Confirm new password: ")
			if err != nil {
				exit(1, err.Error())
			}
			if err := model.UpdatePassword(username, password); err != nil {
				exit(1, "Failed to update password: "+err.Error())
			}
			cliPrintln(cliSuccessStyle.Render("✓") + " Password changed for " + cliInfoStyle.Render(username))
			changed = true
		}

		if regen, _ := cmd.Flags().GetBool("regen-token"); regen {
			token, err := model.RegenerateTokenByUsername(username)
			if err != nil {
				exit(1, "Failed to regenerate token: "+err.Error())
			}
			cliPrintln(cliSuccessStyle.Render("✓") + " New token for " + cliInfoStyle.Render(username) + ": " + cliInfoStyle.Render(token))
			changed = true
		}

		if toggle, _ := cmd.Flags().GetBool("toggle-admin"); toggle {
			isAdmin, err := model.ToggleAdmin(username)
			if err != nil {
				exit(1, "Failed to toggle admin: "+err.Error())
			}
			status := "disabled"
			if isAdmin {
				status = "enabled"
			}
			cliPrintln(cliSuccessStyle.Render("✓") + " Admin " + status + " for " + cliInfoStyle.Render(username))
			changed = true
		}

		if !changed {
			exit(1, "no changes specified - use --username, --password, --regen-token, or --toggle-admin")
		}
	},
}

func promptConfirmedPassword(passwordPrompt, confirmationPrompt string) (string, error) {
	password, err := promptPassword(passwordPrompt)
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	if len(password) < 8 {
		return "", errors.New("password must be at least 8 characters long")
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

func promptPassword(prompt string) (string, error) {
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
