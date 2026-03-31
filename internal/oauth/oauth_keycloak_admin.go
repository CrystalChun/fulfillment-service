/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package oauth

import (
	"crypto/x509"
	"fmt"
	"log/slog"

	"github.com/osac-project/fulfillment-service/internal/auth"
)

// NewKeycloakAdminClientFromConfig builds a [auth.KeycloakAdminClient] using OAuth client credentials against cfg.TokenIssuer,
// for any Keycloak Admin REST API usage.
// cfg must be [auth.KeycloakAdminConfig.FullyConfigured]; otherwise an error is returned.
func NewKeycloakAdminClientFromConfig(
	logger *slog.Logger,
	cfg auth.KeycloakAdminConfig,
	caPool *x509.CertPool,
	userAgent string,
) (*auth.KeycloakAdminClient, error) {
	if !cfg.FullyConfigured() {
		return nil, fmt.Errorf("keycloak admin configuration is incomplete")
	}

	store, err := auth.NewMemoryTokenStore().
		SetLogger(logger).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create token store: %w", err)
	}

	tokenSource, err := NewTokenSource().
		SetLogger(logger).
		SetFlow(CredentialsFlow).
		SetIssuer(cfg.TokenIssuer).
		SetClientId(cfg.ClientID).
		SetClientSecret(cfg.ClientSecret).
		SetScopes(cfg.Scopes...).
		SetInsecure(cfg.InsecureTLS).
		SetCaPool(caPool).
		SetStore(store).
		SetInteractive(false).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create OAuth token source for Keycloak admin: %w", err)
	}

	return auth.NewKeycloakAdminClient().
		SetLogger(logger).
		SetBaseURL(cfg.BaseURL).
		SetTokenSource(tokenSource).
		SetCaPool(caPool).
		SetInsecureTLS(cfg.InsecureTLS).
		SetUserAgent(userAgent).
		Build()
}
