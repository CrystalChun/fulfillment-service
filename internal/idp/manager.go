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
	"errors"
	"fmt"
	"log/slog"
)

// OrganizationManager handles the lifecycle of IdP realms for organizations.
// It works with any IdP client implementation.
type OrganizationManager struct {
	logger *slog.Logger
	client Client
}

// OrganizationManagerBuilder builds the manager.
type OrganizationManagerBuilder struct {
	logger *slog.Logger
	client Client
}

// NewOrganizationManager creates a builder for the organization manager.
func NewOrganizationManager() *OrganizationManagerBuilder {
	return &OrganizationManagerBuilder{}
}

// SetLogger sets the logger.
func (b *OrganizationManagerBuilder) SetLogger(value *slog.Logger) *OrganizationManagerBuilder {
	b.logger = value
	return b
}

// SetClient sets the IdP client implementation.
func (b *OrganizationManagerBuilder) SetClient(value Client) *OrganizationManagerBuilder {
	b.client = value
	return b
}

// Build creates the manager.
func (b *OrganizationManagerBuilder) Build() (result *OrganizationManager, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.client == nil {
		err = errors.New("IdP client is mandatory")
		return
	}

	result = &OrganizationManager{
		logger: b.logger,
		client: b.client,
	}
	return
}

// OrganizationConfig contains configuration for creating an organization realm.
type OrganizationConfig struct {
	// Name is the unique identifier for the organization (used as realm name)
	Name string

	// DisplayName is the human-readable name
	DisplayName string

	// AdminEmail is the email for the initial admin user
	AdminEmail string

	// AdminUsername is the username for the initial admin user
	AdminUsername string

	// AdminPassword is the initial password
	AdminPassword string

	// AssignRealmManagementRoles indicates whether to assign realm management permissions to the admin user
	// This grants the admin user full control over the organization in the IdP
	AssignRealmManagementRoles bool
}

// CreateOrganizationRealm creates a complete IdP organization setup.
func (m *OrganizationManager) CreateOrganizationRealm(ctx context.Context, config *OrganizationConfig) error {
	m.logger.InfoContext(ctx, "Creating IdP organization",
		slog.String("organization", config.Name),
	)

	// Step 1: Create the organization
	org := &Organization{
		Name:        config.Name,
		DisplayName: config.DisplayName,
		Enabled:     true,
	}
	err := m.client.CreateOrganization(ctx, org)
	if err != nil {
		return fmt.Errorf("failed to create organization: %w", err)
	}
	m.logger.InfoContext(ctx, "Organization created",
		slog.String("organization", config.Name),
	)

	// Step 2: Create initial admin user for the organization
	userID, err := m.createAdminUser(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	// Step 3: Assign realm management roles if requested
	if config.AssignRealmManagementRoles && userID != "" {
		err = m.assignRealmManagementRoles(ctx, config.Name, userID)
		if err != nil {
			return fmt.Errorf("failed to assign realm management roles: %w", err)
		}
	}

	m.logger.InfoContext(ctx, "IdP organization created successfully",
		slog.String("organization", config.Name),
	)
	return nil
}

// createAdminUser creates the initial admin user for an organization and returns the user ID.
func (m *OrganizationManager) createAdminUser(ctx context.Context, config *OrganizationConfig) (string, error) {
	user := &User{
		Username:      config.AdminUsername,
		Email:         config.AdminEmail,
		EmailVerified: true,
		Enabled:       true,
		FirstName:     "Organization",
		LastName:      "Administrator",
		Credentials: []*Credential{
			{
				Type:      "password",
				Value:     config.AdminPassword,
				Temporary: true, // User must change on first login
			},
		},
	}

	err := m.client.CreateUser(ctx, config.Name, user)
	if err != nil {
		return "", err
	}

	// CreateUser should populate user.ID with the created user's ID
	if user.ID == "" {
		return "", fmt.Errorf("user created but ID not returned by IdP")
	}

	m.logger.InfoContext(ctx, "Admin user created for organization",
		slog.String("organization_name", config.Name),
		slog.String("username", config.AdminUsername),
		slog.String("user_id", user.ID),
	)

	return user.ID, nil
}

// assignRealmManagementRoles assigns realm management application roles to a user.
// This grants the user full administrative control over the organization.
func (m *OrganizationManager) assignRealmManagementRoles(ctx context.Context, organizationName, userID string) error {
	m.logger.InfoContext(ctx, "Assigning realm management roles to admin user",
		slog.String("organization", organizationName),
		slog.String("user_id", userID),
	)

	// Get all roles from the realm-management client
	// Note: The client ID for realm-management varies by IdP provider
	// For Keycloak, we need to get the client ID first
	roles, err := m.client.ListClientRoles(ctx, organizationName, "realm-management")
	if err != nil {
		return fmt.Errorf("failed to list realm-management roles: %w", err)
	}

	// Filter for the key management roles
	// Common roles: manage-realm, manage-users, manage-clients, view-realm, view-users, etc.
	managementRoleNames := []string{
		"manage-realm",
		"manage-users",
		"manage-clients",
		"manage-identity-providers",
		"manage-authorization",
		"manage-events",
		"view-realm",
		"view-users",
		"view-clients",
		"view-identity-providers",
		"view-authorization",
		"view-events",
	}

	var rolesToAssign []*Role
	for _, role := range roles {
		for _, roleName := range managementRoleNames {
			if role.Name == roleName {
				rolesToAssign = append(rolesToAssign, role)
				break
			}
		}
	}

	if len(rolesToAssign) == 0 {
		m.logger.WarnContext(ctx, "No realm management roles found to assign")
		return nil
	}

	// Assign the roles to the user
	err = m.client.AssignClientRolesToUser(ctx, organizationName, userID, "realm-management", rolesToAssign)
	if err != nil {
		return fmt.Errorf("failed to assign roles: %w", err)
	}

	m.logger.InfoContext(ctx, "Realm management roles assigned",
		slog.String("organization", organizationName),
		slog.String("user_id", userID),
		slog.Int("role_count", len(rolesToAssign)),
	)
	return nil
}

// DeleteOrganizationRealm deletes an IdP organization and all its resources.
func (m *OrganizationManager) DeleteOrganizationRealm(ctx context.Context, organizationName string) error {
	m.logger.InfoContext(ctx, "Deleting IdP organization",
		slog.String("organization", organizationName),
	)

	err := m.client.DeleteOrganization(ctx, organizationName)
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP organization deleted successfully",
		slog.String("organization", organizationName),
	)
	return nil
}
