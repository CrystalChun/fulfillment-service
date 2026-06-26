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
	"strings"

	"github.com/golang-jwt/jwt/v5"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

// UserProvisioner provisions user records in the database.
type UserProvisioner interface {
	Provision(ctx context.Context, username, tenant string, claims jwt.MapClaims) error
}

// DAOUserProvisionerBuilder builds a DAOUserProvisioner.
type DAOUserProvisionerBuilder struct {
	usersDAO *dao.GenericDAO[*privatev1.User]
}

// DAOUserProvisioner implements UserProvisioner using a GenericDAO.
type DAOUserProvisioner struct {
	usersDAO *dao.GenericDAO[*privatev1.User]
}

// NewDAOUserProvisioner creates a new builder.
func NewDAOUserProvisioner() *DAOUserProvisionerBuilder {
	return &DAOUserProvisionerBuilder{}
}

// SetUsersDAO sets the users DAO.
func (b *DAOUserProvisionerBuilder) SetUsersDAO(value *dao.GenericDAO[*privatev1.User]) *DAOUserProvisionerBuilder {
	b.usersDAO = value
	return b
}

// Build creates the provisioner.
func (b *DAOUserProvisionerBuilder) Build() (result *DAOUserProvisioner, err error) {
	if b.usersDAO == nil {
		return nil, fmt.Errorf("users DAO is mandatory")
	}
	result = &DAOUserProvisioner{
		usersDAO: b.usersDAO,
	}
	return result, nil
}

// Provision creates a user record if it doesn't exist.
func (p *DAOUserProvisioner) Provision(ctx context.Context, username, tenant string, claims jwt.MapClaims) error {
	// Check if user exists
	filter := fmt.Sprintf("this.spec.username=='%s'", username)
	listResponse, err := p.usersDAO.List().
		SetFilter(filter).
		SetLimit(1).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if user exists: %w", err)
	}

	// User already exists
	if listResponse.GetSize() > 0 {
		return nil
	}

	// Extract claims
	email, _ := claims["email"].(string)
	emailVerified, _ := claims["email_verified"].(bool)
	name, _ := claims["name"].(string)

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
	}.Build()

	_, err = p.usersDAO.Create().
		SetObject(user).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

var _ UserProvisioner = (*DAOUserProvisioner)(nil)
