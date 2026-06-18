// Package config resolves the storage paths for gdr-cmd's settings and
// credentials.
//
// Storage follows the XDG Base Directory specification:
//   - settings/token: $XDG_CONFIG_HOME/gdr-cmd/ (default ~/.config/gdr-cmd/)
//
// OAuth client information (client_id / client_secret) is read in the following
// order of precedence:
//  1. environment variables GDR_CLIENT_ID / GDR_CLIENT_SECRET
//  2. the settings file credentials.json (the format downloaded from the GCP Console)
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// appDir is the application-specific directory name created under the XDG config directory.
	appDir = "gdr-cmd"

	credentialsFileName = "credentials.json"
	tokenFileName       = "token.json"

	envClientID     = "GDR_CLIENT_ID"
	envClientSecret = "GDR_CLIENT_SECRET"
)

// ClientCredentials holds the OAuth client's credentials.
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
}

// Dir returns the absolute path of gdr-cmd's config directory.
// If $XDG_CONFIG_HOME is unset, it uses ~/.config.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appDir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".config", appDir), nil
}

// EnsureDir creates the config directory and returns its absolute path.
// Because it holds tokens and credentials, the permission is set to 0700.
func EnsureDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create config directory (%s): %w", dir, err)
	}
	return dir, nil
}

// TokenPath returns the path of the OAuth token cache file.
func TokenPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tokenFileName), nil
}

// gcpCredentialsFile is the schema of the credentials.json that the GCP Console
// makes you download. For the desktop app type the values live under the
// "installed" key, and for the web app type under the "web" key, so both are
// accepted.
type gcpCredentialsFile struct {
	Installed *oauthClientSection `json:"installed"`
	Web       *oauthClientSection `json:"web"`
}

type oauthClientSection struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// LoadClientCredentials resolves and returns the OAuth client information.
//
// If both GDR_CLIENT_ID / GDR_CLIENT_SECRET are set, they take precedence.
// Otherwise it reads credentials.json. If neither is available, it returns an
// error that explains how to configure them.
func LoadClientCredentials() (ClientCredentials, error) {
	if id := os.Getenv(envClientID); id != "" {
		secret := os.Getenv(envClientSecret)
		if secret == "" {
			return ClientCredentials{}, fmt.Errorf("%s is set but %s is not", envClientID, envClientSecret)
		}
		return ClientCredentials{ClientID: id, ClientSecret: secret}, nil
	}

	dir, err := Dir()
	if err != nil {
		return ClientCredentials{}, err
	}
	credPath := filepath.Join(dir, credentialsFileName)
	data, err := os.ReadFile(credPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ClientCredentials{}, fmt.Errorf(
				"OAuth client information not found.\n"+
					"Set the environment variables %s / %s, or place the\n"+
					"credentials JSON downloaded from the GCP Console at %s.",
				envClientID, envClientSecret, credPath)
		}
		return ClientCredentials{}, fmt.Errorf("failed to read credentials file (%s): %w", credPath, err)
	}

	var f gcpCredentialsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return ClientCredentials{}, fmt.Errorf("failed to parse credentials file (%s): %w", credPath, err)
	}
	section := f.Installed
	if section == nil {
		section = f.Web
	}
	if section == nil || section.ClientID == "" || section.ClientSecret == "" {
		return ClientCredentials{}, fmt.Errorf(
			"client_id / client_secret not found in credentials file (%s).\n"+
				"Download an OAuth client of the \"Desktop app\" type from the GCP Console.", credPath)
	}
	return ClientCredentials{ClientID: section.ClientID, ClientSecret: section.ClientSecret}, nil
}

// CredentialsPath returns the expected path of credentials.json (for guidance display).
func CredentialsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFileName), nil
}
