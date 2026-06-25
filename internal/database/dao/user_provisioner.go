/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package dao

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
)

// DAOUserProvisionerBuilder builds a DAOUserProvisioner.
type DAOUserProvisionerBuilder struct {
	logger   *slog.Logger
	usersDAO *GenericDAO[*privatev1.User]
}

// DAOUserProvisioner implements auth.UserProvisioner using a GenericDAO to store users.
type DAOUserProvisioner struct {
	logger   *slog.Logger
	usersDAO *GenericDAO[*privatev1.User]
}

// NewDAOUserProvisioner creates a new DAOUserProvisionerBuilder.
func NewDAOUserProvisioner() *DAOUserProvisionerBuilder {
	return &DAOUserProvisionerBuilder{}
}

// SetLogger sets the logger for the provisioner.
func (b *DAOUserProvisionerBuilder) SetLogger(value *slog.Logger) *DAOUserProvisionerBuilder {
	b.logger = value
	return b
}

// SetUsersDAO sets the users DAO for the provisioner.
func (b *DAOUserProvisionerBuilder) SetUsersDAO(value *GenericDAO[*privatev1.User]) *DAOUserProvisionerBuilder {
	b.usersDAO = value
	return b
}

// Build creates the DAOUserProvisioner.
func (b *DAOUserProvisionerBuilder) Build() (result *DAOUserProvisioner, err error) {
	if b.logger == nil {
		return nil, fmt.Errorf("logger is mandatory")
	}
	// Note: usersDAO is a GenericDAO interface, so we can't use nil check. We trust the caller to set it.
	// If not set, the zero value will cause a panic at runtime when calling methods, which is acceptable.

	result = &DAOUserProvisioner{
		logger:   b.logger,
		usersDAO: b.usersDAO,
	}
	return result, nil
}

// EnsureUserExists checks if a user exists and creates them if not. This is best-effort and does not return errors.
func (p *DAOUserProvisioner) EnsureUserExists(ctx context.Context, username, tenant string, claims jwt.MapClaims) {
	// Try to find the user by username using a filter
	filter := fmt.Sprintf("this.spec.username=='%s'", username)
	listResponse, err := p.usersDAO.List().
		SetFilter(filter).
		SetLimit(1).
		Do(ctx)
	if err != nil {
		p.logger.InfoContext(ctx, "Failed to check if user exists (degraded mode - continuing without JIT provisioning)",
			slog.String("username", username),
			slog.Any("error", err),
		)
		return
	}

	// If user already exists, nothing to do
	if listResponse.GetSize() > 0 {
		p.logger.DebugContext(ctx, "User already exists, skipping JIT provisioning",
			slog.String("username", username),
		)
		return
	}

	// User doesn't exist - create it from JWT claims
	email, _ := claims["email"].(string)
	emailVerified, _ := claims["email_verified"].(bool)
	name, _ := claims["name"].(string)

	// Try to parse name into first_name and last_name
	var firstName, lastName string
	if name != "" {
		parts := strings.Fields(name)
		if len(parts) > 0 {
			firstName = parts[0]
		}
		if len(parts) > 1 {
			lastName = strings.Join(parts[1:], " ")
		}
	}

	// Create the user
	user := privatev1.User_builder{
		Metadata: privatev1.Metadata_builder{
			Name:   username,
			Tenant: tenant,
		}.Build(),
		Spec: privatev1.UserSpec_builder{
			Username:      username,
			Email:         email,
			EmailVerified: emailVerified,
			Enabled:       true, // Federated users are enabled by default
			FirstName:     firstName,
			LastName:      lastName,
			Organization:  tenant,
		}.Build(),
	}.Build()

	_, err = p.usersDAO.Create().
		SetObject(user).
		Do(ctx)
	if err != nil {
		// Log the error but don't fail the request - the user was authenticated by Keycloak
		p.logger.InfoContext(ctx, "Failed to create user record during JIT provisioning (degraded mode - user can still authenticate)",
			slog.String("username", username),
			slog.String("tenant", tenant),
			slog.Any("error", err),
		)
		return
	}

	p.logger.InfoContext(ctx, "Created user via just-in-time provisioning",
		slog.String("username", username),
		slog.String("tenant", tenant),
		slog.String("email", email),
	)
}

// Verify that DAOUserProvisioner implements auth.UserProvisioner
var _ auth.UserProvisioner = (*DAOUserProvisioner)(nil)
