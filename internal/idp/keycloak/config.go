/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package keycloak

import (
	"context"
	"crypto/x509"
	"fmt"
	"log/slog"
	"strings"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/idp"
	"github.com/osac-project/fulfillment-service/internal/oauth"
)

// NewClientFromConfig creates a new Keycloak client from an IDP configuration.
func NewClientFromConfig(
	ctx context.Context,
	logger *slog.Logger,
	cfg *idp.Config,
	caPool *x509.CertPool,
) (*Client, error) {
	// Create token source for authentication
	tokenSource, err := createTokenSource(logger, cfg, caPool)
	if err != nil {
		return nil, fmt.Errorf("failed to create token source: %w", err)
	}

	// Create the Keycloak client
	return NewClient().
		SetLogger(logger).
		SetBaseURL(cfg.URL).
		SetTokenSource(tokenSource).
		SetCaPool(caPool).
		Build()
}

// createTokenSource creates an OAuth token source for Keycloak authentication.
func createTokenSource(logger *slog.Logger, cfg *idp.Config, caPool *x509.CertPool) (auth.TokenSource, error) {
	// Create token store
	tokenStore, err := auth.NewMemoryTokenStore().
		SetLogger(logger).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create token store: %w", err)
	}

	// Build token source with common settings
	issuer := fmt.Sprintf("%s/realms/%s", cfg.URL, cfg.Realm)
	builder := oauth.NewTokenSource().
		SetLogger(logger).
		SetIssuer(issuer).
		SetClientId(cfg.ClientID).
		SetStore(tokenStore).
		SetCaPool(caPool).
		SetInteractive(false)

	// Configure flow-specific settings
	switch strings.ToLower(cfg.AuthFlow) {
	case "credentials":
		builder.SetFlow(oauth.CredentialsFlow).
			SetClientSecret(cfg.ClientSecret)
	case "password":
		builder.SetFlow(oauth.PasswordFlow).
			SetClientSecret(cfg.ClientSecret).
			SetUsername(cfg.Username).
			SetPassword(cfg.Password)
	default:
		return nil, fmt.Errorf("unsupported auth flow '%s' for Keycloak", cfg.AuthFlow)
	}

	return builder.Build()
}
