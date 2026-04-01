/*
Copyright (c) 2025 Red Hat Inc.

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
)

// Client is the generic interface for identity provider admin operations.
// Different IdP providers (Keycloak, Auth0, Okta, etc.) implement this interface.
type Client interface {
	// Organization operations
	// An organization in the IdP maps to: Keycloak realm, Auth0 tenant, Okta org, Azure AD tenant
	CreateOrganization(ctx context.Context, org *Organization) error
	GetOrganization(ctx context.Context, name string) (*Organization, error)
	DeleteOrganization(ctx context.Context, name string) error

	// User operations
	// CreateUser creates a user and populates user.ID with the created user's ID
	CreateUser(ctx context.Context, organization string, user *User) error
	GetUser(ctx context.Context, organization, userID string) (*User, error)
	ListUsers(ctx context.Context, organization string) ([]*User, error)
	DeleteUser(ctx context.Context, organization, userID string) error

	// Role operations
	// Roles can be at the organization level or application level
	ListRealmRoles(ctx context.Context, organization string) ([]*Role, error)
	ListClientRoles(ctx context.Context, organization, clientID string) ([]*Role, error)

	// User role assignments
	// For realm management, typically assign application roles from the "realm-management" application
	AssignRealmRolesToUser(ctx context.Context, organization, userID string, roles []*Role) error
	AssignClientRolesToUser(ctx context.Context, organization, userID, clientID string, roles []*Role) error
	RemoveRealmRolesFromUser(ctx context.Context, organization, userID string, roles []*Role) error
	RemoveClientRolesFromUser(ctx context.Context, organization, userID, clientID string, roles []*Role) error
	GetUserRealmRoles(ctx context.Context, organization, userID string) ([]*Role, error)
	GetUserClientRoles(ctx context.Context, organization, userID, clientID string) ([]*Role, error)
}

// Ensure the interface is not empty at compile time
var _ Client = (*client)(nil)

// client is a dummy implementation to ensure the interface compiles
type client struct{}

func (c *client) CreateOrganization(ctx context.Context, org *Organization) error { return nil }
func (c *client) GetOrganization(ctx context.Context, name string) (*Organization, error) {
	return nil, nil
}
func (c *client) DeleteOrganization(ctx context.Context, name string) error             { return nil }
func (c *client) CreateUser(ctx context.Context, organization string, user *User) error { return nil }
func (c *client) GetUser(ctx context.Context, organization, userID string) (*User, error) {
	return nil, nil
}
func (c *client) ListUsers(ctx context.Context, organization string) ([]*User, error) {
	return nil, nil
}
func (c *client) DeleteUser(ctx context.Context, organization, userID string) error { return nil }
func (c *client) ListRealmRoles(ctx context.Context, organization string) ([]*Role, error) {
	return nil, nil
}
func (c *client) ListClientRoles(ctx context.Context, organization, clientID string) ([]*Role, error) {
	return nil, nil
}
func (c *client) AssignRealmRolesToUser(ctx context.Context, organization, userID string, roles []*Role) error {
	return nil
}
func (c *client) AssignClientRolesToUser(ctx context.Context, organization, userID, clientID string, roles []*Role) error {
	return nil
}
func (c *client) RemoveRealmRolesFromUser(ctx context.Context, organization, userID string, roles []*Role) error {
	return nil
}
func (c *client) RemoveClientRolesFromUser(ctx context.Context, organization, userID, clientID string, roles []*Role) error {
	return nil
}
func (c *client) GetUserRealmRoles(ctx context.Context, organization, userID string) ([]*Role, error) {
	return nil, nil
}
func (c *client) GetUserClientRoles(ctx context.Context, organization, userID, clientID string) ([]*Role, error) {
	return nil, nil
}
