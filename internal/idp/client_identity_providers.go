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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
)

// CreateIdentityProvider creates an identity provider for a specific organization.
// In Keycloak, this creates the IdP at realm level and links it to the organization.
func (c *Client) CreateIdentityProvider(ctx context.Context, tenantName string, idpProvider *IdentityProvider) (*IdentityProvider, error) {
	if idpProvider == nil {
		return nil, fmt.Errorf("identity provider is nil")
	}
	c.logger.InfoContext(ctx, "Creating identity provider",
		slog.String("realm", c.realmName),
		slog.String("organization", tenantName),
		slog.String("alias", idpProvider.Alias),
		slog.String("type", idpProvider.Type),
	)

	// Step 1: Create at realm level
	path := fmt.Sprintf("/admin/realms/%s/identity-provider/instances", url.PathEscape(c.realmName))
	kcIdp := toKeycloakIdentityProvider(idpProvider)

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, kcIdp)
	if err != nil {
		return nil, fmt.Errorf("failed to create identity provider: %w", err)
	}
	defer response.Body.Close()

	// Step 2: Link to organization
	err = c.linkIdentityProviderToOrganization(ctx, tenantName, idpProvider.Alias)
	if err != nil {
		// Try to clean up the realm-level IdP if linking fails
		if cleanupErr := c.deleteIdentityProviderFromRealm(ctx, idpProvider.Alias); cleanupErr != nil {
			return nil, fmt.Errorf("failed to link identity provider to organization: %w (cleanup also failed: %w)", err, cleanupErr)
		}
		return nil, fmt.Errorf("failed to link identity provider to organization: %w", err)
	}

	// Step 3: Fetch and return (Keycloak returns empty body on creation)
	result, err := c.GetIdentityProvider(ctx, tenantName, idpProvider.Alias)
	if err != nil {
		// IdP was successfully created and linked - treat read failure as non-fatal
		c.logger.WarnContext(ctx, "Created identity provider but failed to fetch it back",
			slog.String("organization", tenantName),
			slog.String("alias", idpProvider.Alias),
			slog.String("error", err.Error()),
		)
		// Return a sanitized copy without sensitive Config data
		return &IdentityProvider{
			Alias:       idpProvider.Alias,
			DisplayName: idpProvider.DisplayName,
			Type:        idpProvider.Type,
			Enabled:     idpProvider.Enabled,
			Config:      nil, // Secrets are automatically filtered in GET responses
		}, nil
	}
	return result, nil
}

// GetIdentityProvider retrieves an identity provider for a specific organization.
func (c *Client) GetIdentityProvider(ctx context.Context, tenantName, alias string) (*IdentityProvider, error) {
	c.logger.InfoContext(ctx, "Getting identity provider",
		slog.String("realm", c.realmName),
		slog.String("organization", tenantName),
		slog.String("alias", alias),
	)

	// Get the organization to obtain its ID
	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}

	// Get the IdP from the organization endpoint
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/identity-providers/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(org.ID),
		url.PathEscape(alias),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity provider: %w", err)
	}
	defer response.Body.Close()

	var kcIdp keycloakIdentityProvider
	if err := json.NewDecoder(response.Body).Decode(&kcIdp); err != nil {
		return nil, fmt.Errorf("failed to decode identity provider response: %w", err)
	}

	return fromKeycloakIdentityProvider(&kcIdp), nil
}

// DeleteIdentityProvider deletes an identity provider for a specific organization.
// In Keycloak, this deletes the IdP at realm level (which auto-removes from all organizations).
func (c *Client) DeleteIdentityProvider(ctx context.Context, tenantName, alias string) error {
	c.logger.InfoContext(ctx, "Deleting identity provider",
		slog.String("realm", c.realmName),
		slog.String("organization", tenantName),
		slog.String("alias", alias),
	)

	// Delete at realm level - this automatically removes from all organizations
	return c.deleteIdentityProviderFromRealm(ctx, alias)
}

// deleteIdentityProviderFromRealm is an internal helper that deletes at realm level.
func (c *Client) deleteIdentityProviderFromRealm(ctx context.Context, alias string) error {
	path := fmt.Sprintf("/admin/realms/%s/identity-provider/instances/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(alias),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete identity provider: %w", err)
	}
	defer response.Body.Close()

	return nil
}

// linkIdentityProviderToOrganization is an internal helper that links an IdP to an organization.
func (c *Client) linkIdentityProviderToOrganization(ctx context.Context, tenantName, alias string) error {
	c.logger.InfoContext(ctx, "Linking identity provider to organization",
		slog.String("organization", tenantName),
		slog.String("alias", alias),
	)

	// Get the organization to obtain its ID
	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}

	// Link the IdP to the organization
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/identity-providers",
		url.PathEscape(c.realmName),
		url.PathEscape(org.ID),
	)

	// The body is just the alias as a JSON string (Keycloak expects "alias" not {"alias": "value"})
	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, alias)
	if err != nil {
		return fmt.Errorf("failed to link identity provider to organization: %w", err)
	}
	defer response.Body.Close()

	return nil
}

// ListIdentityProviders lists all identity providers for a specific organization.
func (c *Client) ListIdentityProviders(ctx context.Context, tenantName string) ([]*IdentityProvider, error) {
	c.logger.InfoContext(ctx, "Listing organization identity providers",
		slog.String("organization", tenantName),
	)

	// Get the organization to obtain its ID
	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/identity-providers",
		url.PathEscape(c.realmName),
		url.PathEscape(org.ID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list organization identity providers: %w", err)
	}
	defer response.Body.Close()

	var kcIdps []keycloakIdentityProvider
	if err := json.NewDecoder(response.Body).Decode(&kcIdps); err != nil {
		return nil, fmt.Errorf("failed to decode organization identity providers response: %w", err)
	}

	idps := make([]*IdentityProvider, 0, len(kcIdps))
	for i := range kcIdps {
		idps = append(idps, fromKeycloakIdentityProvider(&kcIdps[i]))
	}

	c.logger.InfoContext(ctx, "Listed organization identity providers",
		slog.String("organization", tenantName),
		slog.Int("count", len(idps)),
	)

	return idps, nil
}
