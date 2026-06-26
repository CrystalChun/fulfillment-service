/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package provisioners

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

// UserProvisionerBuilder builds a UserProvisioner.
type UserProvisionerBuilder struct {
	logger   *slog.Logger
	usersDAO *dao.GenericDAO[*privatev1.User]
}

// UserProvisioner implements auth.UserProvisioner using a GenericDAO.
type UserProvisioner struct {
	logger   *slog.Logger
	usersDAO *dao.GenericDAO[*privatev1.User]
}

// NewUserProvisioner creates a new builder.
func NewUserProvisioner() *UserProvisionerBuilder {
	return &UserProvisionerBuilder{}
}

// SetLogger sets the logger.
func (b *UserProvisionerBuilder) SetLogger(value *slog.Logger) *UserProvisionerBuilder {
	b.logger = value
	return b
}

// SetUsersDAO sets the users DAO.
func (b *UserProvisionerBuilder) SetUsersDAO(value *dao.GenericDAO[*privatev1.User]) *UserProvisionerBuilder {
	b.usersDAO = value
	return b
}

// Build creates the provisioner.
func (b *UserProvisionerBuilder) Build() (result *UserProvisioner, err error) {
	if b.logger == nil {
		return nil, fmt.Errorf("logger is mandatory")
	}
	if b.usersDAO == nil {
		return nil, fmt.Errorf("users DAO is mandatory")
	}
	result = &UserProvisioner{
		logger:   b.logger,
		usersDAO: b.usersDAO,
	}
	return result, nil
}

// Provision creates a user record if it doesn't exist.
func (p *UserProvisioner) Provision(ctx context.Context, username, tenant string, claims jwt.MapClaims) error {
	// Check if user exists
	filter := fmt.Sprintf("this.spec.username=='%s'", username)
	listResponse, err := p.usersDAO.List().
		SetFilter(filter).
		SetLimit(1).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if user exists: %w", err)
	}

	// Extract claims
	email, _ := claims["email"].(string)
	emailVerified, _ := claims["email_verified"].(bool)
	name, _ := claims["name"].(string)
	sub, _ := claims["sub"].(string) // Keycloak user ID

	p.logger.InfoContext(ctx, "Provisioning user",
		slog.String("username", username),
		slog.String("tenant", tenant),
		slog.String("keycloak_user_id", sub),
		slog.String("email", email),
	)

	// User already exists
	if listResponse.GetSize() > 0 {
		return nil
	}

	// Parse name into first/last
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

	// Create user
	user := privatev1.User_builder{
		Metadata: privatev1.Metadata_builder{
			Name:   username,
			Tenant: tenant,
		}.Build(),
		Spec: privatev1.UserSpec_builder{
			Username:      username,
			Email:         email,
			EmailVerified: emailVerified,
			Enabled:       true,
			FirstName:     firstName,
			LastName:      lastName,
			Organization:  tenant,
		}.Build(),
		Status: privatev1.UserStatus_builder{
			KeycloakUserId: sub,
		}.Build(),
	}.Build()

	_, err = p.usersDAO.Create().
		SetObject(user).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

var _ auth.UserProvisioner = (*UserProvisioner)(nil)
