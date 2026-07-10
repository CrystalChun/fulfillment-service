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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/osac-project/fulfillment-service/internal/apiclient"
)

// CreateGroup creates a Keycloak organization group with hierarchical path support.
// Creates the full hierarchy if parent groups don't exist.
// Path examples: "/web-app/system:viewers", "/web-app/api/system:managers"
func (c *Client) CreateGroup(ctx context.Context, tenantName, groupPath string) (string, error) {
	c.logger.DebugContext(ctx, "Creating tenant group",
		slog.String("tenantName", tenantName),
		slog.String("groupPath", groupPath),
	)

	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		return "", fmt.Errorf("failed to get organization: %w", err)
	}

	// Parse the path to create parent groups if needed
	// Path format: /web-app/system:viewers
	// We need to ensure /web-app exists, then create system:viewers under it
	// Use a cache to avoid redundant API calls within the same operation
	cache := make(map[string]string) // path -> groupID
	err = c.ensureGroupHierarchyWithCache(ctx, org.ID, groupPath, cache)
	if err != nil {
		return "", fmt.Errorf("failed to ensure group hierarchy: %w", err)
	}

	// Get the created group ID from the cache using the normalized path
	// Normalize the path the same way ensureGroupHierarchyWithCache does
	normalizedPath := "/" + strings.Trim(groupPath, "/")
	normalizedPath = strings.ReplaceAll(normalizedPath, "//", "/")
	groupID, ok := cache[normalizedPath]
	if !ok {
		return "", fmt.Errorf("group was created but ID not found in cache: %s (normalized: %s)", groupPath, normalizedPath)
	}

	c.logger.DebugContext(ctx, "Created tenant group",
		slog.String("tenantName", tenantName),
		slog.String("groupPath", groupPath),
		slog.String("groupID", groupID),
	)

	return groupID, nil
}

// DeleteGroup deletes a Keycloak organization group by ID.
func (c *Client) DeleteGroup(ctx context.Context, tenantName, groupID string) error {
	c.logger.DebugContext(ctx, "Deleting organization group",
		slog.String("tenantName", tenantName),
		slog.String("groupID", groupID),
	)

	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}

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

	c.logger.DebugContext(ctx, "Deleted organization group",
		slog.String("tenantName", tenantName),
		slog.String("groupID", groupID),
	)

	return nil
}

func (c *Client) ensureGroupHierarchyWithCache(ctx context.Context, orgID, groupPath string, cache map[string]string) error {
	normalizedPath := strings.Trim(groupPath, "/")
	normalizedPath = strings.ReplaceAll(normalizedPath, "//", "/")

	segments := strings.Split(normalizedPath, "/")
	if len(segments) == 0 || (len(segments) == 1 && segments[0] == "") {
		return fmt.Errorf("invalid group path: %s", groupPath)
	}

	var currentPath string
	var parentID string

	for _, segment := range segments {
		currentPath = currentPath + "/" + segment

		if cachedID, exists := cache[currentPath]; exists {
			parentID = cachedID
			continue
		}

		groupID, err := c.createOrganizationGroupWithParent(ctx, orgID, segment, parentID)
		if err != nil {
			var apiErr *apiclient.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
				groupID, lookupErr := c.getGroupIDByName(ctx, orgID, parentID, segment)
				if lookupErr != nil {
					return fmt.Errorf(
						"group %s already exists but failed to look up ID: %w",
						currentPath, lookupErr,
					)
				}
				cache[currentPath] = groupID
				parentID = groupID
				continue
			}
			return fmt.Errorf("failed to create group %s: %w", currentPath, err)
		}

		cache[currentPath] = groupID
		parentID = groupID
	}

	return nil
}

func (c *Client) createOrganizationGroupWithParent(ctx context.Context, orgID, name, parentID string) (string, error) {
	var path string
	if parentID == "" {
		path = fmt.Sprintf("/admin/realms/%s/organizations/%s/groups",
			url.PathEscape(c.realmName),
			url.PathEscape(orgID),
		)
	} else {
		path = fmt.Sprintf("/admin/realms/%s/organizations/%s/groups/%s/children",
			url.PathEscape(c.realmName),
			url.PathEscape(orgID),
			url.PathEscape(parentID),
		)
	}

	groupPayload := map[string]interface{}{
		"name": name,
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, groupPayload)
	if err != nil {
		return "", fmt.Errorf("failed to create organization group: %w", err)
	}
	defer response.Body.Close()

	location := response.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no Location header in create group response")
	}

	parts := strings.Split(location, "/")
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid Location header: %s", location)
	}
	groupID := parts[len(parts)-1]

	return groupID, nil
}

func (c *Client) getGroupIDByName(ctx context.Context, orgID, parentID, name string) (string, error) {
	var reqPath string
	if parentID == "" {
		reqPath = fmt.Sprintf("/admin/realms/%s/organizations/%s/groups",
			url.PathEscape(c.realmName),
			url.PathEscape(orgID),
		)
	} else {
		reqPath = fmt.Sprintf("/admin/realms/%s/organizations/%s/groups/%s/children",
			url.PathEscape(c.realmName),
			url.PathEscape(orgID),
			url.PathEscape(parentID),
		)
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, reqPath, nil)
	if err != nil {
		return "", fmt.Errorf("failed to list groups: %w", err)
	}
	defer response.Body.Close()

	var groups []groupNode
	if err := json.NewDecoder(response.Body).Decode(&groups); err != nil {
		return "", fmt.Errorf("failed to decode groups: %w", err)
	}

	for _, g := range groups {
		if g.Name == name {
			return g.ID, nil
		}
	}

	return "", fmt.Errorf("group %q not found among children of parent %q", name, parentID)
}

type groupNode struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	SubGroups []groupNode `json:"subGroups"`
}

func (c *Client) getGroupIDByPathWithOrgID(ctx context.Context, orgID, groupPath string) (result string, err error) {
	normalizedPath := strings.Trim(groupPath, "/")
	if normalizedPath == "" {
		err = fmt.Errorf("empty group path")
		return
	}
	segments := strings.Split(normalizedPath, "/")

	var currentParentID string
	for i, segment := range segments {
		groupID, lookupErr := c.getGroupIDByName(ctx, orgID, currentParentID, segment)
		if lookupErr != nil {
			err = fmt.Errorf("failed to find group segment %d '%s' (parent: %s): %w", i, segment, currentParentID, lookupErr)
			return
		}

		currentParentID = groupID
	}

	result = currentParentID
	return
}

func (c *Client) GetGroupIDByPath(ctx context.Context, tenantName, groupPath string) (string, error) {
	return c.getGroupIDByPath(ctx, tenantName, groupPath)
}

func (c *Client) getGroupIDByPath(ctx context.Context, tenantName, groupPath string) (string, error) {
	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		return "", fmt.Errorf("failed to get organization: %w", err)
	}

	return c.getGroupIDByPathWithOrgID(ctx, org.ID, groupPath)
}

// AddUserToGroup adds a user to an organization group by group ID.
// The idpUserID is the identity provider's user UUID from User.status.keycloak_user_id.
func (c *Client) AddUserToGroup(ctx context.Context, tenantName, idpUserID, groupID string) error {
	c.logger.DebugContext(ctx, "Adding user to organization group",
		slog.String("tenantName", tenantName),
		slog.String("!idpUserID", idpUserID),
		slog.String("groupID", groupID),
	)

	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}

	err = c.ensureOrganizationMember(ctx, org.ID, idpUserID)
	if err != nil {
		return fmt.Errorf("failed to ensure user is organization member: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/groups/%s/members/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(org.ID),
		url.PathEscape(groupID),
		url.PathEscape(idpUserID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodPut, path, nil)
	if err != nil {
		return fmt.Errorf("failed to add user to organization group: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Added user to organization group",
		slog.String("tenantName", tenantName),
		slog.String("!idpUserID", idpUserID),
		slog.String("groupID", groupID),
	)

	return nil
}

func (c *Client) ensureOrganizationMember(ctx context.Context, orgID, userUUID string) error {
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/members",
		url.PathEscape(c.realmName),
		url.PathEscape(orgID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, userUUID)
	if err != nil {
		var apiErr *apiclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
			c.logger.DebugContext(ctx, "User already member of organization",
				slog.String("!userUUID", userUUID),
				slog.String("!orgID", orgID),
			)
			return nil
		}
		return fmt.Errorf("failed to add user to organization: %w", err)
	}
	defer response.Body.Close()

	c.logger.DebugContext(ctx, "Added user to organization",
		slog.String("!userUUID", userUUID),
		slog.String("!orgID", orgID),
	)

	return nil
}

// RemoveUserFromGroup removes a user from an organization group by group ID.
func (c *Client) RemoveUserFromGroup(ctx context.Context, tenantName, idpUserID, groupID string) error {
	c.logger.DebugContext(ctx, "Removing user from organization group",
		slog.String("tenantName", tenantName),
		slog.String("!idpUserID", idpUserID),
		slog.String("groupID", groupID),
	)

	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/groups/%s/members/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(org.ID),
		url.PathEscape(groupID),
		url.PathEscape(idpUserID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to remove user from organization group: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Removed user from organization group",
		slog.String("tenantName", tenantName),
		slog.String("!idpUserID", idpUserID),
		slog.String("groupID", groupID),
	)

	return nil
}
