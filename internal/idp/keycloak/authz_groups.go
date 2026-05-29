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
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
)

// CreateAuthorizationGroup creates a Keycloak organization group for authorization purposes.
// Organization groups are scoped to the organization and support hierarchical paths.
//
// Group path format:
//   - Top-level project: "/Projects/{project-name}/{Viewers|Managers}"
//   - Nested project: "/Projects/{parent-name}/{project-name}/{Viewers|Managers}"
//
// Organization groups are scoped per organization, so paths can be simple and readable.
// See https://www.keycloak.org/2026/04/org-groups for details.
func (c *Client) CreateAuthorizationGroup(ctx context.Context, organizationName, groupName, groupPath string) error {
	c.logger.InfoContext(ctx, "Creating organization authorization group",
		slog.String("organizationName", organizationName),
		slog.String("groupName", groupName),
		slog.String("groupPath", groupPath),
	)

	// Get the organization ID first
	org, err := c.GetOrganization(ctx, organizationName)
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}

	// Use organization groups API instead of realm groups
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/groups",
		url.PathEscape(c.realmName),
		url.PathEscape(org.ID),
	)

	groupPayload := map[string]interface{}{
		"name": groupName,
		"path": groupPath,
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, groupPayload)
	if err != nil {
		return fmt.Errorf("failed to create organization group: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Created organization authorization group",
		slog.String("organizationName", organizationName),
		slog.String("groupName", groupName),
	)

	return nil
}

// DeleteAuthorizationGroup deletes a Keycloak organization group by ID.
func (c *Client) DeleteAuthorizationGroup(ctx context.Context, organizationName, groupID string) error {
	c.logger.InfoContext(ctx, "Deleting organization authorization group",
		slog.String("organizationName", organizationName),
		slog.String("groupID", groupID),
	)

	// Get the organization ID first
	org, err := c.GetOrganization(ctx, organizationName)
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}

	// Use organization groups API instead of realm groups
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/groups/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(org.ID),
		url.PathEscape(groupID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete organization group: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Deleted organization authorization group",
		slog.String("organizationName", organizationName),
		slog.String("groupID", groupID),
	)

	return nil
}

// Helper methods

// GetGroupIDByPath gets a Keycloak organization group ID by its path.
// This is exposed for use by the ResourceManager.
func (c *Client) GetGroupIDByPath(ctx context.Context, organizationName, groupPath string) (string, error) {
	return c.getGroupIDByPath(ctx, organizationName, groupPath)
}

func (c *Client) getGroupIDByPath(ctx context.Context, organizationName, groupPath string) (string, error) {
	// Get the organization ID first
	org, err := c.GetOrganization(ctx, organizationName)
	if err != nil {
		return "", fmt.Errorf("failed to get organization: %w", err)
	}

	// Search for organization group by path
	// Note: Keycloak doesn't have a direct "get by path" API, so we search by name
	// and verify the path matches
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/groups?search=%s",
		url.PathEscape(c.realmName),
		url.PathEscape(org.ID),
		url.QueryEscape(groupPath),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to search organization groups: %w", err)
	}
	defer response.Body.Close()

	var groups []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(response.Body).Decode(&groups); err != nil {
		return "", fmt.Errorf("failed to decode organization groups: %w", err)
	}

	for _, group := range groups {
		if group.Path == groupPath {
			return group.ID, nil
		}
	}

	return "", fmt.Errorf("organization group not found: %s", groupPath)
}

// Helper methods for authorization permission management

func (c *Client) getScopeID(ctx context.Context, clientUUID, scopeName string) (string, error) {
	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/scope?name=%s",
		url.PathEscape(c.realmName),
		url.PathEscape(clientUUID),
		url.QueryEscape(scopeName),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get scope: %w", err)
	}
	defer response.Body.Close()

	var scopes []keycloakAuthorizationScope
	if err := json.NewDecoder(response.Body).Decode(&scopes); err != nil {
		return "", fmt.Errorf("failed to decode scopes: %w", err)
	}

	if len(scopes) == 0 {
		return "", fmt.Errorf("scope %s not found", scopeName)
	}

	return scopes[0].ID, nil
}

func (c *Client) getPermissionByName(ctx context.Context, clientUUID, permissionName string) (string, error) {
	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/permission?name=%s",
		url.PathEscape(c.realmName),
		url.PathEscape(clientUUID),
		url.QueryEscape(permissionName),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get permission: %w", err)
	}
	defer response.Body.Close()

	var permissions []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&permissions); err != nil {
		return "", fmt.Errorf("failed to decode permissions: %w", err)
	}

	if len(permissions) == 0 {
		return "", fmt.Errorf("permission not found")
	}

	return permissions[0].ID, nil
}
