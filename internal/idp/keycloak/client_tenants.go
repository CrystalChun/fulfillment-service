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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/osac-project/fulfillment-service/internal/apiclient"
)

// CreateTenant creates a new tenant (Keycloak organization in the configured realm).
// Returns the created tenant with server-assigned ID and any server defaults.
func (c *Client) CreateTenant(ctx context.Context, tenant *Tenant) (*Tenant, error) {
	kcOrg := toKeycloakOrganization(tenant)
	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/organizations", c.realmName), kcOrg)
	if err != nil {
		var apiErr *apiclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
			return nil, fmt.Errorf("organization %q already exists: %w", tenant.Name, err)
		}
		return nil, fmt.Errorf("failed to create organization: %w", err)
	}
	response.Body.Close()

	// Keycloak's POST /admin/realms returns 201 with no body, so we fetch the created organization
	// to get the server-assigned ID and verify the organization was actually created
	return c.GetTenant(ctx, tenant.Name)
}

// GetTenant retrieves a tenant (Keycloak organization in the configured realm) by name.
func (c *Client) GetTenant(ctx context.Context, name string) (*Tenant, error) {
	query := url.Values{}
	query.Add("search", name)
	query.Add("exact", "true")
	path := fmt.Sprintf("/admin/realms/%s/organizations?%s", c.realmName, query.Encode())
	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}
	defer response.Body.Close()

	var kcOrgs []keycloakOrganization
	if err = json.NewDecoder(response.Body).Decode(&kcOrgs); err != nil {
		return nil, fmt.Errorf("failed to decode organization response: %w", err)
	}
	if len(kcOrgs) == 0 {
		return nil, fmt.Errorf("organization %q not found", name)
	}
	kcOrg := kcOrgs[0]
	return fromKeycloakOrganization(&kcOrg), nil
}

// UpdateTenant updates an existing tenant (Keycloak organization in the configured realm).
// The tenant must have a non-empty ID.
func (c *Client) UpdateTenant(ctx context.Context, tenant *Tenant) (*Tenant, error) {
	if tenant == nil {
		return nil, fmt.Errorf("organization is required for update")
	}
	if tenant.ID == "" {
		return nil, fmt.Errorf("organization ID is required for update")
	}
	kcOrg := toKeycloakOrganization(tenant)
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s", c.realmName, url.PathEscape(tenant.ID))
	response, err := c.httpClient.DoRequest(ctx, http.MethodPut, path, kcOrg)
	if err != nil {
		return nil, fmt.Errorf("failed to update organization: %w", err)
	}
	response.Body.Close()

	return c.GetTenant(ctx, tenant.Name)
}

// DeleteTenant deletes a tenant (Keycloak organization in the configured realm) by name.
func (c *Client) DeleteTenant(ctx context.Context, tenantName string) error {
	// Delete the break-glass account first (Keycloak-specific: it belongs to realm, not organization)
	breakGlassUsername := fmt.Sprintf("%s-osac-break-glass", tenantName)
	if err := c.deleteBreakGlassAccount(ctx, breakGlassUsername); err != nil {
		return fmt.Errorf("failed to delete break-glass account: %w", err)
	}

	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return fmt.Errorf("failed to get organization: %w", err)
	}
	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/organizations/%s", c.realmName, org.ID), nil)
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}
	defer response.Body.Close()

	return nil
}
