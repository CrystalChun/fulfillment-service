/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package idp

import (
	"fmt"
	"os"
	"strings"
)

// Config holds identity provider configuration.
type Config struct {
	// URL is the base URL of the identity provider.
	URL string

	// Type is the type of identity provider (e.g., "keycloak").
	Type string

	// AuthFlow is the OAuth flow to use ("credentials" or "password").
	AuthFlow string

	// ClientID is the OAuth client ID.
	ClientID string

	// ClientSecret is the OAuth client secret.
	ClientSecret string

	// Username is the username for password flow authentication.
	Username string

	// Password is the password for password flow authentication.
	Password string

	// Realm is the IDP realm/tenant name.
	Realm string

	// CAFile is the path to a CA certificate file for TLS verification.
	CAFile string
}

// LoadConfigFromEnv loads identity provider configuration from environment variables.
// Returns nil if IDP_URL is not set, indicating no IDP is configured.
func LoadConfigFromEnv() (*Config, error) {
	url := os.Getenv("IDP_URL")
	if url == "" {
		return nil, nil
	}

	cfg := &Config{
		URL:          url,
		Type:         getEnvOrDefault("IDP_TYPE", "keycloak"),
		AuthFlow:     getEnvOrDefault("IDP_AUTH_FLOW", "credentials"),
		ClientID:     os.Getenv("IDP_CLIENT_ID"),
		ClientSecret: os.Getenv("IDP_CLIENT_SECRET"),
		Username:     os.Getenv("IDP_USERNAME"),
		Password:     os.Getenv("IDP_PASSWORD"),
		Realm:        getEnvOrDefault("IDP_REALM", "master"),
		CAFile:       os.Getenv("IDP_CA_FILE"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that the configuration has all required fields based on the auth flow.
func (c *Config) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("IDP_URL is required")
	}

	switch strings.ToLower(c.AuthFlow) {
	case "credentials":
		if c.ClientID == "" {
			return fmt.Errorf("IDP_CLIENT_ID is required when IDP_AUTH_FLOW is 'credentials'")
		}
		if c.ClientSecret == "" {
			return fmt.Errorf("IDP_CLIENT_SECRET is required when IDP_AUTH_FLOW is 'credentials'")
		}
	case "password":
		if c.ClientID == "" {
			c.ClientID = "admin-cli" // Default for password flow
		}
		if c.Username == "" {
			return fmt.Errorf("IDP_USERNAME is required when IDP_AUTH_FLOW is 'password'")
		}
		if c.Password == "" {
			return fmt.Errorf("IDP_PASSWORD is required when IDP_AUTH_FLOW is 'password'")
		}
	default:
		return fmt.Errorf(
			"unsupported IDP_AUTH_FLOW '%s', valid values are 'credentials' or 'password'",
			c.AuthFlow,
		)
	}

	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
