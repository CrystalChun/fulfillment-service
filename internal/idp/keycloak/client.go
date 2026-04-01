/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package keycloak

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

// Client is a Keycloak-specific implementation of the idp.Client interface.
type Client struct {
	logger      *slog.Logger
	baseURL     string
	tokenSource auth.TokenSource
	httpClient  *http.Client
}

// ClientBuilder builds a Keycloak client.
type ClientBuilder struct {
	logger      *slog.Logger
	baseURL     string
	tokenSource auth.TokenSource
	caPool      *x509.CertPool
	httpClient  *http.Client
}

// Ensure Client implements idp.Client at compile time
var _ idp.Client = (*Client)(nil)

// NewClient creates a builder for a Keycloak admin client.
func NewClient() *ClientBuilder {
	return &ClientBuilder{}
}

// SetLogger sets the logger.
func (b *ClientBuilder) SetLogger(value *slog.Logger) *ClientBuilder {
	b.logger = value
	return b
}

// SetBaseURL sets the base URL of the Keycloak server.
func (b *ClientBuilder) SetBaseURL(value string) *ClientBuilder {
	b.baseURL = value
	return b
}

// SetTokenSource sets the token source for authentication.
func (b *ClientBuilder) SetTokenSource(value auth.TokenSource) *ClientBuilder {
	b.tokenSource = value
	return b
}

// SetCaPool sets the CA certificate pool.
func (b *ClientBuilder) SetCaPool(value *x509.CertPool) *ClientBuilder {
	b.caPool = value
	return b
}

// SetHttpClient sets a custom HTTP client.
func (b *ClientBuilder) SetHttpClient(value *http.Client) *ClientBuilder {
	b.httpClient = value
	return b
}

// Build creates the Keycloak client.
func (b *ClientBuilder) Build() (result *Client, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.baseURL == "" {
		err = errors.New("base URL is mandatory")
		return
	}
	if b.tokenSource == nil {
		err = errors.New("token source is mandatory")
		return
	}

	httpClient := b.httpClient
	if httpClient == nil {
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    b.caPool,
				MinVersion: tls.VersionTLS12,
			},
		}
		httpClient = &http.Client{
			Transport: transport,
		}
	}

	result = &Client{
		logger:      b.logger,
		baseURL:     strings.TrimSuffix(b.baseURL, "/"),
		tokenSource: b.tokenSource,
		httpClient:  httpClient,
	}
	return
}

// doRequest performs an HTTP request with authentication.
func (c *Client) doRequest(ctx context.Context, method, path string, body any) (response *http.Response, err error) {
	token, err := c.tokenSource.Token(ctx)
	if err != nil {
		err = fmt.Errorf("failed to get authentication token: %w", err)
		return
	}

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			err = fmt.Errorf("failed to marshal request body: %w", marshalErr)
			return
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	requestURL := c.baseURL + path
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		err = fmt.Errorf("failed to create HTTP request: %w", err)
		return
	}

	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.Access))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	c.logger.DebugContext(ctx, "Keycloak API request",
		slog.String("method", method),
		slog.String("path", path),
	)

	response, err = c.httpClient.Do(request)
	if err != nil {
		err = fmt.Errorf("failed to send HTTP request: %w", err)
		return
	}

	if response.StatusCode >= 400 {
		defer response.Body.Close()
		bodyBytes, _ := io.ReadAll(response.Body)
		err = fmt.Errorf("Keycloak API error: status=%d body=%s", response.StatusCode, string(bodyBytes))
		return
	}

	return
}

// CreateOrganization creates a new organization (Keycloak realm).
func (c *Client) CreateOrganization(ctx context.Context, org *idp.Organization) error {
	kcRealm := toKeycloakRealm(org)
	response, err := c.doRequest(ctx, http.MethodPost, "/admin/realms", kcRealm)
	if err != nil {
		return fmt.Errorf("failed to create organization: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// GetOrganization retrieves an organization (Keycloak realm) by name.
func (c *Client) GetOrganization(ctx context.Context, name string) (*idp.Organization, error) {
	response, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s", url.PathEscape(name)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}
	defer response.Body.Close()

	var kcRealm keycloakRealm
	if err = json.NewDecoder(response.Body).Decode(&kcRealm); err != nil {
		return nil, fmt.Errorf("failed to decode organization response: %w", err)
	}
	return fromKeycloakRealm(&kcRealm), nil
}

// DeleteOrganization deletes an organization (Keycloak realm) by name.
func (c *Client) DeleteOrganization(ctx context.Context, name string) error {
	response, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s", url.PathEscape(name)), nil)
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// CreateUser creates a new user in an organization.
// On success, populates user.ID with the ID of the created user.
func (c *Client) CreateUser(ctx context.Context, organization string, user *idp.User) error {
	kcUser := toKeycloakUser(user)
	response, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users", url.PathEscape(organization)), kcUser)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	defer response.Body.Close()

	// Extract user ID from Location header
	// Location format: https://keycloak.example.com/admin/realms/{realm}/users/{user-id}
	location := response.Header.Get("Location")
	if location != "" {
		// Extract the last segment of the URL path as the user ID
		parts := strings.Split(strings.TrimSuffix(location, "/"), "/")
		if len(parts) > 0 {
			user.ID = parts[len(parts)-1]
		}
	}

	return nil
}

// GetUser retrieves a user by ID.
func (c *Client) GetUser(ctx context.Context, organization, userID string) (*idp.User, error) {
	response, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/users/%s", url.PathEscape(organization), url.PathEscape(userID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	defer response.Body.Close()

	var kcUser keycloakUser
	if err = json.NewDecoder(response.Body).Decode(&kcUser); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}
	return fromKeycloakUser(&kcUser), nil
}

// ListUsers lists all users in an organization.
// Fetches all pages to ensure no users are missed due to Keycloak's pagination.
func (c *Client) ListUsers(ctx context.Context, organization string) ([]*idp.User, error) {
	var allUsers []*idp.User
	const maxPerPage = 100
	first := 0

	for {
		// Fetch one page of users
		path := fmt.Sprintf("/admin/realms/%s/users?first=%d&max=%d",
			url.PathEscape(organization), first, maxPerPage)

		response, err := c.doRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list users: %w", err)
		}

		var kcUsers []keycloakUser
		err = json.NewDecoder(response.Body).Decode(&kcUsers)
		response.Body.Close()

		if err != nil {
			return nil, fmt.Errorf("failed to decode users response: %w", err)
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

// DeleteUser deletes a user by ID.
func (c *Client) DeleteUser(ctx context.Context, organization, userID string) error {
	response, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s", url.PathEscape(organization), url.PathEscape(userID)), nil)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// ListRealmRoles lists all realm-level roles in an organization.
func (c *Client) ListRealmRoles(ctx context.Context, organization string) ([]*idp.Role, error) {
	response, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/roles", url.PathEscape(organization)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list realm roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode realm roles response: %w", err)
	}

	roles := make([]*idp.Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// ListClientRoles lists all roles for a specific client.
// clientID can be either the Keycloak internal ID or the clientId (e.g., "realm-management").
// If it looks like a UUID, it's treated as an internal ID. Otherwise, it's looked up by clientId.
func (c *Client) ListClientRoles(ctx context.Context, organization, clientID string) ([]*idp.Role, error) {
	// Check if this looks like an internal UUID or a clientId
	internalID := clientID
	if !isUUID(clientID) {
		// Look up the internal ID by clientId
		client, err := c.GetClientByClientID(ctx, organization, clientID)
		if err != nil {
			return nil, fmt.Errorf("failed to find client: %w", err)
		}
		internalID = client.ID
	}

	response, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/clients/%s/roles", url.PathEscape(organization), url.PathEscape(internalID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list client roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode client roles response: %w", err)
	}

	roles := make([]*idp.Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// isUUID checks if a string looks like a UUID.
func isUUID(s string) bool {
	// Simple check: UUIDs are 36 chars with hyphens in specific positions
	if len(s) != 36 {
		return false
	}
	return s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}

// AssignRealmRolesToUser assigns realm-level roles to a user.
func (c *Client) AssignRealmRolesToUser(ctx context.Context, organization, userID string, roles []*idp.Role) error {
	kcRoles := make([]keycloakRole, len(roles))
	for i, role := range roles {
		kcRoles[i] = *toKeycloakRole(role)
	}

	response, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", url.PathEscape(organization), url.PathEscape(userID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to assign realm roles to user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// AssignClientRolesToUser assigns client-level roles to a user.
// clientID can be either the internal ID or the clientId (e.g., "realm-management").
func (c *Client) AssignClientRolesToUser(ctx context.Context, organization, userID, clientID string, roles []*idp.Role) error {
	// Resolve to internal ID if needed
	internalID := clientID
	if !isUUID(clientID) {
		client, err := c.GetClientByClientID(ctx, organization, clientID)
		if err != nil {
			return fmt.Errorf("failed to find client: %w", err)
		}
		internalID = client.ID
	}

	kcRoles := make([]keycloakRole, len(roles))
	for i, role := range roles {
		kcRoles[i] = *toKeycloakRole(role)
	}

	response, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/clients/%s", url.PathEscape(organization), url.PathEscape(userID), url.PathEscape(internalID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to assign client roles to user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// RemoveRealmRolesFromUser removes realm-level roles from a user.
func (c *Client) RemoveRealmRolesFromUser(ctx context.Context, organization, userID string, roles []*idp.Role) error {
	kcRoles := make([]keycloakRole, len(roles))
	for i, role := range roles {
		kcRoles[i] = *toKeycloakRole(role)
	}

	response, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", url.PathEscape(organization), url.PathEscape(userID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to remove realm roles from user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// RemoveClientRolesFromUser removes client-level roles from a user.
// clientID can be either the internal ID or the clientId (e.g., "realm-management").
func (c *Client) RemoveClientRolesFromUser(ctx context.Context, organization, userID, clientID string, roles []*idp.Role) error {
	// Resolve to internal ID if needed
	internalID := clientID
	if !isUUID(clientID) {
		client, err := c.GetClientByClientID(ctx, organization, clientID)
		if err != nil {
			return fmt.Errorf("failed to find client: %w", err)
		}
		internalID = client.ID
	}

	kcRoles := make([]keycloakRole, len(roles))
	for i, role := range roles {
		kcRoles[i] = *toKeycloakRole(role)
	}

	response, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/clients/%s", url.PathEscape(organization), url.PathEscape(userID), url.PathEscape(internalID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to remove client roles from user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// GetUserRealmRoles gets the realm-level roles assigned to a user.
func (c *Client) GetUserRealmRoles(ctx context.Context, organization, userID string) ([]*idp.Role, error) {
	response, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", url.PathEscape(organization), url.PathEscape(userID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user realm roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode user realm roles response: %w", err)
	}

	roles := make([]*idp.Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// GetUserClientRoles gets the client-level roles assigned to a user.
// clientID can be either the internal ID or the clientId (e.g., "realm-management").
func (c *Client) GetUserClientRoles(ctx context.Context, organization, userID, clientID string) ([]*idp.Role, error) {
	// Resolve to internal ID if needed
	internalID := clientID
	if !isUUID(clientID) {
		client, err := c.GetClientByClientID(ctx, organization, clientID)
		if err != nil {
			return nil, fmt.Errorf("failed to find client: %w", err)
		}
		internalID = client.ID
	}

	response, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/clients/%s", url.PathEscape(organization), url.PathEscape(userID), url.PathEscape(internalID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user client roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode user client roles response: %w", err)
	}

	roles := make([]*idp.Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// ClientApp is defined here to support GetClientByClientID which is needed for role operations.
type ClientApp struct {
	ID       string
	ClientID string
}

// GetClientByClientID is a helper method to get a client's internal ID by its clientId.
// This is needed because role operations require the internal ID, not the clientId.
func (c *Client) GetClientByClientID(ctx context.Context, organization, clientID string) (*ClientApp, error) {
	// List all clients and find the one with matching clientId
	response, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/clients?clientId=%s", url.PathEscape(organization), url.QueryEscape(clientID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get client by clientId: %w", err)
	}
	defer response.Body.Close()

	var kcClients []keycloakClient
	if err = json.NewDecoder(response.Body).Decode(&kcClients); err != nil {
		return nil, fmt.Errorf("failed to decode clients response: %w", err)
	}

	if len(kcClients) == 0 {
		return nil, fmt.Errorf("client with clientId '%s' not found", clientID)
	}

	return &ClientApp{
		ID:       kcClients[0].ID,
		ClientID: kcClients[0].ClientID,
	}, nil
}
