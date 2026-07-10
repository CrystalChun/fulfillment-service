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
	"slices"
	"time"
)

// TenantManager handles the lifecycle of tenants in Keycloak.
type TenantManager struct {
	logger *slog.Logger
	client ClientInterface
}

// TenantManagerBuilder builds the manager.
type TenantManagerBuilder struct {
	logger *slog.Logger
	client ClientInterface
}

// NewTenantManager creates a builder for the tenant manager.
func NewTenantManager() *TenantManagerBuilder {
	return &TenantManagerBuilder{}
}

// SetLogger sets the logger.
func (b *TenantManagerBuilder) SetLogger(value *slog.Logger) *TenantManagerBuilder {
	b.logger = value
	return b
}

// SetClient sets the Keycloak client.
func (b *TenantManagerBuilder) SetClient(value ClientInterface) *TenantManagerBuilder {
	b.client = value
	return b
}

// Build creates the manager.
func (b *TenantManagerBuilder) Build() (result *TenantManager, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.client == nil {
		err = errors.New("client is mandatory")
		return
	}

	result = &TenantManager{
		logger: b.logger,
		client: b.client,
	}
	return
}

// TenantConfig contains configuration for creating a tenant in the identity provider.
type TenantConfig struct {
	Name        string
	DisplayName string
	Enabled     *bool
	Domains     []string

	BreakGlassUsername string
	BreakGlassEmail    string
	BreakGlassPassword string
}

// BreakGlassCredentials contains the credentials for the break-glass account.
type BreakGlassCredentials struct {
	UserID   string
	Username string
	Email    string
	Password string `json:"-"`
}

// CreateTenant creates a complete IdP tenant setup with a break-glass account.
func (m *TenantManager) CreateTenant(ctx context.Context, config *TenantConfig) (*BreakGlassCredentials, error) {
	if config == nil {
		return nil, errors.New("TenantConfig is mandatory")
	}

	m.logger.InfoContext(ctx, "Creating IdP tenant",
		slog.String("tenant", config.Name),
	)

	var (
		tenantCreated bool
		credentials   *BreakGlassCredentials
		err           error
	)

	defer func() {
		if err != nil {
			m.logger.ErrorContext(ctx, "Error creating tenant in IdP",
				slog.String("tenant", config.Name),
				slog.Any("error", err),
			)
			m.rollback(ctx, config.Name, tenantCreated)
		}
	}()

	enabled := true
	if config.Enabled != nil {
		enabled = *config.Enabled
	}
	tenant := &Tenant{
		Name:        config.Name,
		DisplayName: config.DisplayName,
		Enabled:     enabled,
		Domains:     config.Domains,
	}
	createdTenant, err := m.client.CreateTenant(ctx, tenant)
	if err != nil {
		return nil, fmt.Errorf("failed to create tenant in IdP: %w", err)
	}
	tenantCreated = true
	m.logger.InfoContext(ctx, "Tenant created in IdP",
		slog.String("tenant", createdTenant.Name),
	)

	credentials, err = m.createBreakGlassAccount(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create break-glass account: %w", err)
	}

	err = m.assignIdpManagerPermissions(ctx, credentials.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to assign IdP manager permissions: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP tenant created successfully",
		slog.String("tenant", createdTenant.Name),
	)
	return credentials, nil
}

// UpdateTenant updates an existing tenant in the identity provider.
func (m *TenantManager) UpdateTenant(ctx context.Context, name string, domains []string) error {
	if name == "" {
		return errors.New("tenant name is mandatory")
	}

	m.logger.InfoContext(ctx, "Updating IdP tenant domains",
		slog.String("tenant", name),
	)

	tenant, err := m.client.GetTenant(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to get tenant from IdP for update: %w", err)
	}
	if tenant == nil {
		return fmt.Errorf("tenant '%s' not found in IdP", name)
	}

	currentDomains := slices.Clone(tenant.Domains)
	desiredDomains := slices.Clone(domains)
	slices.Sort(currentDomains)
	slices.Sort(desiredDomains)
	if slices.Equal(currentDomains, desiredDomains) {
		m.logger.DebugContext(ctx, "IdP tenant domains already up to date, skipping update",
			slog.String("tenant", name),
		)
		return nil
	}

	tenant.Domains = domains
	_, err = m.client.UpdateTenant(ctx, tenant)
	if err != nil {
		return fmt.Errorf("failed to update tenant in IdP: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP tenant domains updated successfully",
		slog.String("tenant", name),
	)
	return nil
}

func (m *TenantManager) rollback(ctx context.Context, tenantName string, deleteTenant bool) {
	if !deleteTenant {
		return
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m.logger.WarnContext(ctx, "Rolling back tenant creation in IdP",
		slog.String("tenant", tenantName),
	)

	if err := m.client.DeleteTenant(cleanupCtx, tenantName); err != nil {
		m.logger.ErrorContext(ctx, "Failed to rollback tenant creation in IdP",
			slog.String("tenant", tenantName),
			slog.Any("error", err),
		)
	} else {
		m.logger.InfoContext(ctx, "Rolled back tenant creation in IdP",
			slog.String("tenant", tenantName),
		)
	}
}

func (m *TenantManager) createBreakGlassAccount(ctx context.Context, config *TenantConfig) (*BreakGlassCredentials, error) {
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
			slog.String("tenant", config.Name),
			slog.String("username", username),
		)
	}

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
				Temporary: true,
			},
		},
	}

	createdUser, err := m.client.CreateUser(ctx, config.Name, user)
	if err != nil {
		return nil, err
	}

	credentials := &BreakGlassCredentials{
		UserID:   createdUser.ID,
		Username: username,
		Email:    email,
		Password: password,
	}

	m.logger.InfoContext(ctx, "Break-glass account created for tenant",
		slog.String("tenant_name", config.Name),
		slog.String("username", username),
		slog.String("user_id", createdUser.ID),
	)

	return credentials, nil
}

func (m *TenantManager) assignIdpManagerPermissions(ctx context.Context, userID string) error {
	m.logger.InfoContext(ctx, "Assigning IdP manager permissions to user",
		slog.String("user_id", userID),
	)

	err := m.client.AssignIdpManagerPermissions(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to assign IdP manager permissions: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP manager permissions assigned",
		slog.String("user_id", userID),
	)
	return nil
}

// DeleteTenant deletes a tenant from the IdP and all of its resources.
func (m *TenantManager) DeleteTenant(ctx context.Context, tenantName string) error {
	m.logger.InfoContext(ctx, "Deleting tenant from IdP",
		slog.String("tenant", tenantName),
	)

	err := m.client.DeleteTenant(ctx, tenantName)
	if err != nil {
		return fmt.Errorf("failed to delete tenant from IdP: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP tenant deleted successfully",
		slog.String("tenant", tenantName),
	)
	return nil
}
