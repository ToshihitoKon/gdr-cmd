package cmd

import (
	"fmt"
	"os"

	"github.com/ToshihitoKon/gdr-cmd/internal/auth"
	"github.com/ToshihitoKon/gdr-cmd/internal/config"
	"github.com/spf13/cobra"
)

var authNoBrowser bool

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
	Long:  "Log in to, log out of, and check the status of your Google account via OAuth.",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to your Google account",
	Long: `Run the OAuth flow to obtain and save a token.

By default it launches a browser automatically and receives the authorization
code on a temporary local port. In environments where a browser cannot be opened
locally (e.g. over SSH), pass --no-browser to switch to the manual method: open
the shown URL in a browser on any device and paste back the redirected URL.

You must first create an OAuth client of type "Desktop app" in the GCP Console
and configure the credentials via environment variables or credentials.json
(see the README for details).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		authn, err := auth.New()
		if err != nil {
			return err
		}
		return authn.Login(cmd.Context(), authNoBrowser)
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Delete the saved token",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		authn, err := auth.New()
		if err != nil {
			return err
		}
		if err := authn.Logout(); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Logged out.")
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tokenPath, err := config.TokenPath()
		if err != nil {
			return err
		}
		authn, err := auth.New()
		if err != nil {
			// Missing credentials also lands here; treat it as not configured.
			fmt.Fprintln(os.Stderr, "credentials are not configured:", err)
			return nil
		}
		if authn.HasToken() {
			fmt.Printf("Logged in (token: %s)\n", tokenPath)
		} else {
			fmt.Println("Not logged in. Run `gdr auth login`.")
		}
		return nil
	},
}

func init() {
	authLoginCmd.Flags().BoolVar(&authNoBrowser, "no-browser", false,
		"authenticate by manual copy-paste instead of launching a browser (for environments without a browser, e.g. over SSH)")

	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
