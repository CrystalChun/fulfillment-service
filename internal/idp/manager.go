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
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"

	"github.com/osac-project/fulfillment-service/internal/apiclient"
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

	// BreakGlassUsername is the username for the break-glass account
	// If empty, defaults to "osac-break-glass"
	BreakGlassUsername string

	// BreakGlassEmail is the email for the break-glass account
	// If empty, defaults to "break-glass@{organization-name}.osac.local"
	BreakGlassEmail string

	// BreakGlassPassword is the temporary password for the break-glass account
	// This is mandatory and must be changed on first login
	BreakGlassPassword string
}

// BreakGlassCredentials contains the credentials for the break-glass account.
//
// SECURITY NOTES:
//   - Password is plaintext and MUST be handled securely
//   - DO NOT log the password
//   - Store in a secrets manager (Vault, Kubernetes Secrets, AWS Secrets Manager)
//   - Transmit only over TLS
//   - Clear from memory immediately after use
//   - Password is temporary and must be changed on first login
type BreakGlassCredentials struct {
	// UserID is the unique identifier for the break-glass user in the IdP
	UserID string

	// Username is the username for the break-glass account
	Username string

	// Email is the email address for the break-glass account
	Email string

	// Password is the temporary password that must be changed on first login.
	// This field is intentionally excluded from JSON marshaling to prevent
	// accidental logging or exposure.
	Password string `json:"-"`
}

// CreateOrganization creates a complete IdP organization setup with a break-glass account.
// Returns the break-glass account credentials and error.
//
// This method is idempotent - it can be safely retried if it fails partway through.
// If the organization already exists, it will verify and complete any missing steps
// (break-glass account creation, permission assignment).
func (m *OrganizationManager) CreateOrganization(ctx context.Context, config *OrganizationConfig) (*BreakGlassCredentials, error) {
	if config == nil {
		return nil, errors.New("OrganizationConfig is mandatory")
	}

	m.logger.InfoContext(ctx, "Creating IdP organization",
		slog.String("organization", config.Name),
	)

	// Step 1: Create or verify the organization exists
	org := &Organization{
		Name:        config.Name,
		DisplayName: config.DisplayName,
		Enabled:     true,
	}
	createdOrg, err := m.client.CreateOrganization(ctx, org)
	if err != nil {
		// Check if this is a "organization already exists" error by checking for HTTP 409 Conflict
		// or error message containing "already exists"
		isConflict := isOrganizationExistsError(err)
		if !isConflict {
			// Not a conflict error - this is a real error, don't try to continue
			m.logger.ErrorContext(ctx, "Failed to create organization",
				slog.String("organization", config.Name),
				slog.Any("error", err),
			)
			return nil, fmt.Errorf("failed to create organization: %w", err)
		}

		// Organization already exists, verify it and continue with idempotent setup
		existingOrg, getErr := m.client.GetOrganization(ctx, config.Name)
		if getErr != nil {
			m.logger.ErrorContext(ctx, "Failed to get organization",
				slog.String("organization", config.Name),
				slog.Any("error", getErr),
			)
			return nil, fmt.Errorf("failed to create organization: %w (verified it exists but failed to retrieve: %w)", err, getErr)
		}
		createdOrg = existingOrg
		m.logger.InfoContext(ctx, "Organization already exists, continuing setup",
			slog.String("organization", createdOrg.Name),
		)
	} else {
		m.logger.InfoContext(ctx, "Organization created",
			slog.String("organization", createdOrg.Name),
		)
	}

	// Step 2: Create or verify break-glass account exists
	credentials, err := m.ensureBreakGlassAccount(ctx, config)
	if err != nil {
		m.logger.ErrorContext(ctx, "Failed to ensure break-glass account",
			slog.String("organization", config.Name),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("failed to ensure break-glass account: %w", err)
	}

	// Step 3: Ensure IdP manager permissions are assigned
	err = m.ensureIdpManagerPermissions(ctx, createdOrg.Name, credentials.UserID)
	if err != nil {
		m.logger.ErrorContext(ctx, "Failed to ensure IdP manager permissions",
			slog.String("organization", config.Name),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("failed to ensure IdP manager permissions: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP organization setup complete",
		slog.String("organization", createdOrg.Name),
	)
	return credentials, nil
}

// ensureBreakGlassAccount creates or verifies the break-glass account for an organization.
// Returns the break-glass credentials and error.
// The break-glass account is a built-in OSAC user with limited privileges (idp-manager role)
// that can manage IdP configuration and roles.
//
// This method is idempotent - if the account already exists, it returns the existing account's
// credentials (password will be regenerated if not provided in config).
func (m *OrganizationManager) ensureBreakGlassAccount(ctx context.Context, config *OrganizationConfig) (*BreakGlassCredentials, error) {
	// Set defaults if not provided
	username := config.BreakGlassUsername
	if username == "" {
		username = fmt.Sprintf("%s-osac-break-glass", config.Name)
	}

	email := config.BreakGlassEmail
	if email == "" {
		email = fmt.Sprintf("break-glass@%s.osac.local", config.Name)
	}
	password := config.BreakGlassPassword
	if password == "" {
		// Generate a secure random password using crypto/rand
		const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%"
		const passwordLength = 24
		b := make([]byte, passwordLength)
		for i := range b {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
			if err != nil {
				return nil, fmt.Errorf("failed to generate random password: %w", err)
			}
			b[i] = charset[n.Int64()]
		}
		password = string(b)
		m.logger.DebugContext(ctx, "Generated temporary break-glass password because it was not provided",
			slog.String("organization", config.Name),
			slog.String("username", username),
		)
	}

	// Check if break-glass account already exists
	existingUsers, err := m.client.ListUsers(ctx, config.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	var existingUser *User
	for _, u := range existingUsers {
		if u.Username == username {
			existingUser = u
			break
		}
	}

	var userID string
	if existingUser != nil {
		// User already exists
		userID = existingUser.ID
		m.logger.InfoContext(ctx, "Break-glass account already exists",
			slog.String("organization", config.Name),
			slog.String("username", username),
			slog.String("user_id", userID),
		)
	} else {
		// Create new user
		user := &User{
			Username:      username,
			Email:         email,
			EmailVerified: true,
			Enabled:       true,
			FirstName:     "OSAC",
			LastName:      "Break-Glass",
			Credentials: []*Credential{
				{
					Type:      "password",
					Value:     password,
					Temporary: true, // User must change password on first login
				},
			},
		}

		createdUser, err := m.client.CreateUser(ctx, config.Name, user)
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		userID = createdUser.ID

		m.logger.InfoContext(ctx, "Break-glass account created for organization",
			slog.String("organization_name", config.Name),
			slog.String("username", username),
			slog.String("user_id", userID),
		)
	}

	credentials := &BreakGlassCredentials{
		UserID:   userID,
		Username: username,
		Email:    email,
		Password: password,
	}

	return credentials, nil
}

// ensureIdpManagerPermissions ensures limited IdP manager permissions are assigned to a user.
// This grants the user permissions to manage users and identity providers but not
// critical realm settings.
// The implementation is provider-specific (delegated to the IdP client).
//
// This method is idempotent - it will not fail if permissions are already assigned.
func (m *OrganizationManager) ensureIdpManagerPermissions(ctx context.Context, organizationName, userID string) error {
	m.logger.InfoContext(ctx, "Ensuring IdP manager permissions for user",
		slog.String("organization", organizationName),
		slog.String("user_id", userID),
	)

	err := m.client.AssignIdpManagerPermissions(ctx, organizationName, userID)
	if err != nil {
		return fmt.Errorf("failed to assign IdP manager permissions: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP manager permissions ensured",
		slog.String("organization", organizationName),
		slog.String("user_id", userID),
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

// isOrganizationExistsError checks if an error indicates the organization already exists.
// This checks for HTTP 409 Conflict status or error messages containing "already exists".
func isOrganizationExistsError(err error) bool {
	if err == nil {
		return false
	}

	// Check if the error is an APIError with HTTP 409 Conflict status
	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
		return true
	}

	// Check if the error message contains "already exists"
	return strings.Contains(err.Error(), "already exists")
}
