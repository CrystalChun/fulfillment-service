/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"os"
	"strings"

	"github.com/spf13/pflag"
)

// Environment variable names (used when the corresponding flag is empty).
const (
	EnvKeycloakAdminBaseURL      = "KEYCLOAK_ADMIN_BASE_URL"
	EnvKeycloakAdminTokenIssuer  = "KEYCLOAK_ADMIN_TOKEN_ISSUER"
	EnvKeycloakAdminClientID     = "KEYCLOAK_ADMIN_CLIENT_ID"
	EnvKeycloakAdminClientSecret = "KEYCLOAK_ADMIN_CLIENT_SECRET"
	EnvKeycloakAdminScopes       = "KEYCLOAK_ADMIN_SCOPES"
	EnvKeycloakAdminInsecureTLS  = "KEYCLOAK_ADMIN_INSECURE_TLS"
)

// Flag names registered by [AddKeycloakAdminFlags].
const (
	FlagKeycloakAdminBaseURL      = "keycloak-admin-base-url"
	FlagKeycloakAdminTokenIssuer  = "keycloak-admin-token-issuer"
	FlagKeycloakAdminClientID     = "keycloak-admin-client-id"
	FlagKeycloakAdminClientSecret = "keycloak-admin-client-secret"
	FlagKeycloakAdminScopes       = "keycloak-admin-scopes"
	FlagKeycloakAdminInsecureTLS  = "keycloak-admin-insecure-tls"
)

// KeycloakAdminConfig holds settings for the Keycloak Admin REST API (any automation that needs an admin token).
type KeycloakAdminConfig struct {
	BaseURL      string
	TokenIssuer  string
	ClientID     string
	ClientSecret string
	Scopes       []string
	InsecureTLS  bool
}

// AddKeycloakAdminFlags registers flags for the Keycloak Admin REST API client (service-account access).
func AddKeycloakAdminFlags(flags *pflag.FlagSet) {
	flags.String(
		FlagKeycloakAdminBaseURL,
		"",
		"Keycloak server base URL (scheme and host, no path) for Admin REST API requests. "+
			"May be set with "+EnvKeycloakAdminBaseURL+".",
	)
	flags.String(
		FlagKeycloakAdminTokenIssuer,
		"",
		"OAuth/OpenID issuer URL for client-credentials tokens (for example https://keycloak.example.com/realms/master "+
			"or another realm that issues tokens for your admin client). "+
			"May be set with "+EnvKeycloakAdminTokenIssuer+".",
	)
	flags.String(
		FlagKeycloakAdminClientID,
		"",
		"OAuth client id for Keycloak Admin API access (typically a confidential client with service account roles). "+
			"May be set with "+EnvKeycloakAdminClientID+".",
	)
	flags.String(
		FlagKeycloakAdminClientSecret,
		"",
		"OAuth client secret paired with --"+FlagKeycloakAdminClientID+". "+
			"May be set with "+EnvKeycloakAdminClientSecret+".",
	)
	flags.StringSlice(
		FlagKeycloakAdminScopes,
		nil,
		"OAuth scopes when requesting the admin token (optional). "+
			"May be set with "+EnvKeycloakAdminScopes+" as a comma-separated list.",
	)
	flags.Bool(
		FlagKeycloakAdminInsecureTLS,
		false,
		"Skip TLS verification for HTTPS to Keycloak (including token and admin endpoints). "+
			"May be set with "+EnvKeycloakAdminInsecureTLS+" (true/false).",
	)
}

// KeycloakAdminConfigFromFlags reads Keycloak admin settings from parsed flags and environment variables.
// Flag values take precedence when non-empty; otherwise the corresponding environment variable is used.
func KeycloakAdminConfigFromFlags(flags *pflag.FlagSet) (cfg KeycloakAdminConfig, err error) {
	baseURL, err := flags.GetString(FlagKeycloakAdminBaseURL)
	if err != nil {
		return cfg, err
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv(EnvKeycloakAdminBaseURL))
	}

	tokenIssuer, err := flags.GetString(FlagKeycloakAdminTokenIssuer)
	if err != nil {
		return cfg, err
	}
	if tokenIssuer == "" {
		tokenIssuer = strings.TrimSpace(os.Getenv(EnvKeycloakAdminTokenIssuer))
	}

	clientID, err := flags.GetString(FlagKeycloakAdminClientID)
	if err != nil {
		return cfg, err
	}
	if clientID == "" {
		clientID = strings.TrimSpace(os.Getenv(EnvKeycloakAdminClientID))
	}

	clientSecret, err := flags.GetString(FlagKeycloakAdminClientSecret)
	if err != nil {
		return cfg, err
	}
	if clientSecret == "" {
		clientSecret = strings.TrimSpace(os.Getenv(EnvKeycloakAdminClientSecret))
	}

	scopes, err := flags.GetStringSlice(FlagKeycloakAdminScopes)
	if err != nil {
		return cfg, err
	}
	if len(scopes) == 0 {
		if envScopes := strings.TrimSpace(os.Getenv(EnvKeycloakAdminScopes)); envScopes != "" {
			scopes = splitKeycloakCommaSeparated(envScopes)
		}
	}

	insecureTLS, err := flags.GetBool(FlagKeycloakAdminInsecureTLS)
	if err != nil {
		return cfg, err
	}
	if !flags.Changed(FlagKeycloakAdminInsecureTLS) {
		if v := strings.TrimSpace(os.Getenv(EnvKeycloakAdminInsecureTLS)); v != "" {
			insecureTLS = strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		}
	}

	cfg = KeycloakAdminConfig{
		BaseURL:      strings.TrimSpace(baseURL),
		TokenIssuer:  strings.TrimSpace(tokenIssuer),
		ClientID:     strings.TrimSpace(clientID),
		ClientSecret: clientSecret,
		Scopes:       scopes,
		InsecureTLS:  insecureTLS,
	}
	return cfg, nil
}

func splitKeycloakCommaSeparated(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// PartiallyConfigured reports whether any Keycloak admin setting was provided.
func (c KeycloakAdminConfig) PartiallyConfigured() bool {
	return c.BaseURL != "" || c.TokenIssuer != "" || c.ClientID != "" || c.ClientSecret != ""
}

// FullyConfigured reports whether all required Keycloak admin settings are present.
func (c KeycloakAdminConfig) FullyConfigured() bool {
	return c.BaseURL != "" && c.TokenIssuer != "" && c.ClientID != "" && c.ClientSecret != ""
}
