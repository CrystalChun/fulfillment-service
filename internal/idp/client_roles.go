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
	"net/http"
	"net/url"

	"github.com/google/uuid"
)

// ListTenantRoles lists all tenant-level roles.
// Note: Organizations in Keycloak don't have their own roles - they use realm roles.
func (c *Client) ListTenantRoles(ctx context.Context, tenantName string) ([]*Role, error) {
	// TODO: implement function
	return nil, nil
}

// ListClientRoles lists all roles for a specific client.
//
// The clientID parameter accepts either format for convenience:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
func (c *Client) ListClientRoles(ctx context.Context, tenantName, clientID string) ([]*Role, error) {
	// Resolve to internal UUID
	internalID, err := c.GetRealmClientByClientID(ctx, clientID, c.realmName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve client ID: %w", err)
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/clients/%s/roles", c.realmName, url.PathEscape(internalID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list client roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode client roles response: %w", err)
	}

	roles := make([]*Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// AssignTenantRolesToUser adds tenant-level roles to a user.
func (c *Client) AssignTenantRolesToUser(ctx context.Context, tenantName, userID string, roles []*Role) error {
	// Fetch full role objects from Keycloak to get their IDs
	kcRoles, err := c.fetchAndConvertRealmRoles(ctx, roles)
	if err != nil {
		return err
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", c.realmName, url.PathEscape(userID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to assign realm roles to user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// AssignClientRolesToUser adds client-level roles to a user.
//
// The clientID parameter accepts either format:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
func (c *Client) AssignClientRolesToUser(ctx context.Context, tenantName, userID, clientID string, roles []*Role) error {
	// Resolve to internal UUID
	internalID, err := c.GetRealmClientByClientID(ctx, clientID, c.realmName)
	if err != nil {
		return fmt.Errorf("failed to resolve client ID: %w", err)
	}

	kcRoles := c.convertRolesToKeycloak(roles)

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/clients/%s", c.realmName, url.PathEscape(userID), url.PathEscape(internalID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to assign client roles to user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// RemoveTenantRolesFromUser removes tenant-level roles from a user.
func (c *Client) RemoveTenantRolesFromUser(ctx context.Context, tenantName, userID string, roles []*Role) error {
	// Fetch full role objects from Keycloak to get their IDs
	kcRoles, err := c.fetchAndConvertRealmRoles(ctx, roles)
	if err != nil {
		return err
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", c.realmName, url.PathEscape(userID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to remove realm roles from user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// RemoveRealmRolesFromUser removes realm-level roles from a user.
func (c *Client) RemoveRealmRolesFromUser(ctx context.Context, userID string, roles []*Role) error {
	kcRoles := c.convertRolesToKeycloak(roles)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", c.realmName, url.PathEscape(userID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to remove realm roles from user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// RemoveClientRolesFromUser removes client-level roles from a user.
//
// The clientID parameter accepts either format:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
func (c *Client) RemoveClientRolesFromUser(ctx context.Context, tenantName, userID, clientID string, roles []*Role) error {
	// Resolve to internal UUID
	internalID, err := c.GetRealmClientByClientID(ctx, clientID, c.realmName)
	if err != nil {
		return fmt.Errorf("failed to resolve client ID: %w", err)
	}

	kcRoles := c.convertRolesToKeycloak(roles)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/clients/%s", c.realmName, url.PathEscape(userID), url.PathEscape(internalID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to remove client roles from user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// GetUserTenantRoles gets the tenant-level roles assigned to a user.
func (c *Client) GetUserTenantRoles(ctx context.Context, tenantName, userID string) ([]*Role, error) {
	// TODO: implement function
	return nil, nil
}

// GetUserClientRoles gets the client-level roles assigned to a user.
//
// The clientID parameter accepts either format:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
func (c *Client) GetUserClientRoles(ctx context.Context, tenantName, userID, clientID string) ([]*Role, error) {
	// Resolve to internal UUID
	internalID, err := c.GetRealmClientByClientID(ctx, clientID, c.realmName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve client ID: %w", err)
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/clients/%s", c.realmName, url.PathEscape(userID), url.PathEscape(internalID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user client roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode user client roles response: %w", err)
	}

	roles := make([]*Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// GetRealmClientByClientID resolves a client identifier to its internal UUID.
//
// The clientID parameter accepts either format:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
//
// The method first checks if clientID is a valid UUID. If so, it returns it immediately
// (no API call needed).
//
// This is needed because Keycloak's role-mapping API endpoints require the internal UUID,
// but we use the human-readable clientId "realm-management".
//
// Example:
//
//	uuid, err := client.GetRealmClientByClientID(ctx, "realm-management", "osac")
//
// "realm-management" is the human-readable clientId
// "osac" is the realm name
//
//	// Returns: "a1b2c3d4-e5f6-7890-..." (internal UUID)
func (c *Client) GetRealmClientByClientID(ctx context.Context, clientID, realmName string) (string, error) {
	// Check if clientID is already a valid UUID (internal ID)
	// If so, return it immediately without making an API call
	if _, err := uuid.Parse(clientID); err == nil {
		return clientID, nil
	}

	// For realm-management client, use the cached client ID if available
	if clientID == realmManagementClientID {
		if c.realmManagementClientID != "" {
			return c.realmManagementClientID, nil
		}
	}

	// Look up the client UUID via API
	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/clients?clientId=%s", realmName, url.QueryEscape(clientID)), nil)
	if err != nil {
		return "", fmt.Errorf("failed to get client by clientId: %w", err)
	}
	defer response.Body.Close()

	var kcClients []keycloakClient
	if err = json.NewDecoder(response.Body).Decode(&kcClients); err != nil {
		return "", fmt.Errorf("failed to decode clients response: %w", err)
	}

	if len(kcClients) == 0 {
		return "", fmt.Errorf("client %q not found", clientID)
	}

	internalUUID := kcClients[0].ID
	// For realm-management client, cache the client ID if not already cached
	if clientID == realmManagementClientID {
		c.realmManagementClientID = internalUUID
	}
	return internalUUID, nil
}

// AssignTenantAdminPermissions grants administrative access to a tenant for the specified user.
//
// For Keycloak, this assigns organization-level admin roles to the user.
func (c *Client) AssignTenantAdminPermissions(ctx context.Context, tenantName, userID string) error {
	// TODO: implement function
	return nil
}

func (c *Client) GetRealmRole(ctx context.Context, roleName string) (*Role, error) {
	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/roles/%s", c.realmName, url.PathEscape(roleName)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get role: %w", err)
	}
	defer response.Body.Close()
	var kcRole keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRole); err != nil {
		return nil, fmt.Errorf("failed to decode role response: %w", err)
	}
	return fromKeycloakRole(&kcRole), nil
}

// AssignIdpManagerPermissions grants limited IdP management permissions to the specified user.
//
// For Keycloak, this assigns a tenant-idp-manager role to the user.
// Intended for the break-glass account which can manage user roles and identity providers but cannot modify critical
// organization settings, realm settings, or authorization policies.
func (c *Client) AssignIdpManagerPermissions(ctx context.Context, userID string) error {
	domainRole, err := c.GetRealmRole(ctx, "tenant-idp-manager")
	if err != nil {
		return fmt.Errorf("failed to get tenant-idp-manager role from Keycloak: %w", err)
	}
	// Keycloak role assignment API expects an array of roles
	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", c.realmName, url.PathEscape(userID)), []keycloakRole{*toKeycloakRole(domainRole)})
	if err != nil {
		return fmt.Errorf("failed to assign role to user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// fetchAndConvertRealmRoles fetches full realm role objects from Keycloak by name
// and converts them to keycloakRole format. This is needed when assigning/removing
// realm roles because Keycloak requires the full role representation including ID.
func (c *Client) fetchAndConvertRealmRoles(ctx context.Context, roles []*Role) ([]keycloakRole, error) {
	kcRoles := make([]keycloakRole, 0, len(roles))
	for _, role := range roles {
		domainRole, err := c.GetRealmRole(ctx, role.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get realm role %s: %w", role.Name, err)
		}
		kcRoles = append(kcRoles, *toKeycloakRole(domainRole))
	}
	return kcRoles, nil
}

// convertRolesToKeycloak converts domain Role objects to keycloakRole format.
// This is used when the caller already has complete role information (including ID).
func (c *Client) convertRolesToKeycloak(roles []*Role) []keycloakRole {
	kcRoles := make([]keycloakRole, len(roles))
	for i, role := range roles {
		kcRoles[i] = *toKeycloakRole(role)
	}
	return kcRoles
}
