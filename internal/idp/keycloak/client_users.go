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
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/osac-project/fulfillment-service/internal/apiclient"
)

func (c *Client) AddUserToOrganization(ctx context.Context, tenantName string, userID string) error {
	org, err := c.GetTenant(ctx, tenantName)
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/members", c.realmName, org.ID)
	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, userID)
	if err != nil {
		return fmt.Errorf("failed to add user to organization: %w", err)
	}
	defer response.Body.Close()
	return nil
}

func (c *Client) CreateUserInRealm(ctx context.Context, user *User) (*User, error) {
	kcUser := toKeycloakUser(user)
	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users", c.realmName), kcUser)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	defer response.Body.Close()

	location := response.Header.Get("Location")
	if location == "" {
		return nil, fmt.Errorf("Location header not present in create user response") //nolint:staticcheck // ST1005: Location is an HTTP header name
	}

	// Extract the user ID from the Location header (e.g., "/admin/realms/osac/users/user-123" -> "user-123")
	parts := strings.Split(strings.TrimSuffix(location, "/"), "/")
	userID := parts[len(parts)-1]
	kcUser.ID = userID
	return fromKeycloakUser(kcUser), nil
}

// CreateUser creates a new user in the OSAC realm and adds them to an organization.
// Returns the created user with ID populated.
// If adding to the organization fails, the user is still created in the realm and can be
// added to the organization later using AddUserToOrganization.
func (c *Client) CreateUser(ctx context.Context, tenantName string, user *User) (*User, error) {
	// Step 1: Create user in the OSAC realm
	createdUser, err := c.CreateUserInRealm(ctx, user)
	if err != nil {
		return nil, err
	}

	// Step 2: Add user to the organization
	err = c.AddUserToOrganization(ctx, tenantName, createdUser.ID)
	if err != nil {
		c.logger.WarnContext(ctx, "User created but failed to add to organization",
			slog.String("user_id", createdUser.ID),
			slog.String("organization", tenantName),
			slog.Any("error", err),
		)
		return createdUser, fmt.Errorf("failed to add user to organization (user %s created in realm): %w", createdUser.ID, err)
	}

	return createdUser, nil
}

// GetUser retrieves a user by ID from the realm.
func (c *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/users/%s", c.realmName, url.PathEscape(userID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user in Keycloak: %w", err)
	}
	defer response.Body.Close()

	var kcUser keycloakUser
	if err = json.NewDecoder(response.Body).Decode(&kcUser); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}
	return fromKeycloakUser(&kcUser), nil
}

// ListUsers lists all users (members) in the Keycloak organization.
func (c *Client) ListUsers(ctx context.Context, tenantName string) ([]*User, error) {
	var allUsers []*User
	const maxPerPage = 100
	first := 0

	// Fetches all pages to ensure no users are missed due to Keycloak's pagination.
	for {
		// Check if context is cancelled before making the next API call
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Fetch one page of organization members
		path := fmt.Sprintf("/admin/realms/%s/organizations/%s/members?first=%d&max=%d",
			c.realmName,
			url.PathEscape(tenantName), first, maxPerPage)

		response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list organization members: %w", err)
		}

		var kcUsers []keycloakUser
		err = json.NewDecoder(response.Body).Decode(&kcUsers)
		response.Body.Close()

		if err != nil {
			return nil, fmt.Errorf("failed to decode organization members response: %w", err)
		}

		// Convert and append this page
		for _, kcUser := range kcUsers {
			allUsers = append(allUsers, fromKeycloakUser(&kcUser))
		}

		// If we got fewer than max, we've reached the last page
		if len(kcUsers) < maxPerPage {
			break
		}

		// Move to next page
		first += maxPerPage
	}

	return allUsers, nil
}

// RemoveUserFromOrganization removes a user (member) from the Keycloak organization.
func (c *Client) RemoveUserFromOrganization(ctx context.Context, tenantName, userID string) error {
	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/organizations/%s/members/%s", c.realmName, url.PathEscape(tenantName), url.PathEscape(userID)), nil)
	if err != nil {
		var apiErr *apiclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("user %q not found in Keycloak organization %q: %w", userID, tenantName, err)
		}
		return fmt.Errorf("failed to remove user %q from Keycloak organization %q: %w", userID, tenantName, err)
	}
	defer response.Body.Close()
	return nil
}

// DeleteUser deletes a user by ID from the Keycloak realm.
func (c *Client) DeleteUser(ctx context.Context, userID string) error {
	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s", c.realmName, url.PathEscape(userID)), nil)
	if err != nil {
		return fmt.Errorf("failed to delete user %q from Keycloak realm: %w", userID, err)
	}
	defer response.Body.Close()
	return nil
}

// GetUserByUsername implements the Client interface method.
func (c *Client) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	query := url.Values{}
	query.Add("username", username)
	query.Add("exact", "true")
	path := fmt.Sprintf("/admin/realms/%s/users?%s", c.realmName, query.Encode())

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query user by username: %w", err)
	}
	defer response.Body.Close()

	var kcUsers []keycloakUser
	if err = json.NewDecoder(response.Body).Decode(&kcUsers); err != nil {
		return nil, fmt.Errorf("failed to decode user query response: %w", err)
	}

	if len(kcUsers) == 0 {
		// User not found - return nil without error
		return nil, nil
	}

	return fromKeycloakUser(&kcUsers[0]), nil
}

// deleteBreakGlassAccount is a Keycloak-specific helper that deletes the break-glass account.
// In Keycloak, the break-glass account belongs to the realm (not the organization),
// so it must be explicitly deleted and won't be cascade-deleted with the organization.
func (c *Client) deleteBreakGlassAccount(ctx context.Context, breakGlassUsername string) error {
	// Query for the break-glass user by username
	user, err := c.GetUserByUsername(ctx, breakGlassUsername)
	if err != nil {
		return fmt.Errorf("failed to get user by username: %w", err)
	}

	if user == nil {
		// Break-glass account not found - may have been already deleted
		// This is not an error, just return success
		return nil
	}

	// Delete the break-glass user
	err = c.DeleteUser(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("failed to delete break-glass user: %w", err)
	}

	return nil
}
