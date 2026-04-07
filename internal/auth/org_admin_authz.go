/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OrganizationAdminAuthorizerBuilder contains the data and logic needed to create an organization admin authorizer.
type OrganizationAdminAuthorizerBuilder struct {
	logger       *slog.Logger
	tenancyLogic TenancyLogic
}

// OrganizationAdminAuthorizer provides methods to check if a user has admin access to an organization.
type OrganizationAdminAuthorizer struct {
	logger       *slog.Logger
	tenancyLogic TenancyLogic
}

// NewOrganizationAdminAuthorizer creates a new builder for the organization admin authorizer.
func NewOrganizationAdminAuthorizer() *OrganizationAdminAuthorizerBuilder {
	return &OrganizationAdminAuthorizerBuilder{}
}

// SetLogger sets the logger that will be used by the authorizer. This is mandatory.
func (b *OrganizationAdminAuthorizerBuilder) SetLogger(value *slog.Logger) *OrganizationAdminAuthorizerBuilder {
	b.logger = value
	return b
}

// SetTenancyLogic sets the tenancy logic that will be used to determine which organizations
// the authenticated user has access to. This is mandatory.
func (b *OrganizationAdminAuthorizerBuilder) SetTenancyLogic(value TenancyLogic) *OrganizationAdminAuthorizerBuilder {
	b.tenancyLogic = value
	return b
}

// Build uses the information stored in the builder to create a new authorizer.
func (b *OrganizationAdminAuthorizerBuilder) Build() (result *OrganizationAdminAuthorizer, err error) {
	// Check that the logger has been set:
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}

	// Check that the tenancy logic has been set:
	if b.tenancyLogic == nil {
		err = fmt.Errorf("tenancy logic is mandatory")
		return
	}

	// Create the authorizer:
	result = &OrganizationAdminAuthorizer{
		logger:       b.logger,
		tenancyLogic: b.tenancyLogic,
	}
	return
}

// CheckOrganizationAccess verifies that the authenticated user has admin access to the specified organization.
// Returns a gRPC error if the user does not have access.
func (a *OrganizationAdminAuthorizer) CheckOrganizationAccess(ctx context.Context, organization string) error {
	if organization == "" {
		return status.Error(codes.InvalidArgument, "organization is required")
	}

	// Get the subject from the context
	subject := SubjectFromContext(ctx)

	// Get the user's visible tenants (organizations)
	visibleTenants, err := a.tenancyLogic.DetermineVisibleTenants(ctx)
	if err != nil {
		a.logger.ErrorContext(
			ctx,
			"Failed to determine visible tenants",
			slog.String("user", subject.User),
			slog.Any("error", err),
		)
		return status.Error(codes.Internal, "failed to determine user's organizations")
	}

	// Check if the user has access to this organization
	if !visibleTenants.Contains(organization) {
		a.logger.WarnContext(
			ctx,
			"User attempted to access organization they don't belong to",
			slog.String("user", subject.User),
			slog.String("organization", organization),
			slog.Any("user_groups", subject.Groups),
		)
		return status.Errorf(
			codes.PermissionDenied,
			"you do not have permission to manage users in organization '%s'",
			organization,
		)
	}

	// Check if the user has admin role for this organization
	// In Keycloak, realm roles are often stored in the groups or we can check resource_access
	// For now, we'll check if the user's groups contain an admin indicator
	hasAdminRole := a.hasAdminRole(ctx, subject, organization)
	if !hasAdminRole {
		a.logger.WarnContext(
			ctx,
			"User attempted to manage users without admin role",
			slog.String("user", subject.User),
			slog.String("organization", organization),
			slog.Any("user_groups", subject.Groups),
		)
		return status.Errorf(
			codes.PermissionDenied,
			"you must be an admin of organization '%s' to manage its users",
			organization,
		)
	}

	a.logger.DebugContext(
		ctx,
		"User authorized to manage organization",
		slog.String("user", subject.User),
		slog.String("organization", organization),
	)

	return nil
}

// hasAdminRole checks if the user has an admin role for the specified organization.
// This can be extended to check specific claims from the JWT token.
// For now, it checks if the user belongs to a group that indicates admin access.
func (a *OrganizationAdminAuthorizer) hasAdminRole(ctx context.Context, subject *Subject, organization string) bool {
	// Strategy 1: Check if user belongs to an "{organization}-admin" group
	adminGroup := organization + "-admin"
	for _, group := range subject.Groups {
		if group == adminGroup {
			return true
		}
	}

	// Strategy 2: Check if user belongs to an "admin" group and the organization group
	// This means they're a member of the org and have admin privileges
	hasOrgMembership := false
	hasAdminGroup := false
	for _, group := range subject.Groups {
		if group == organization {
			hasOrgMembership = true
		}
		if strings.HasSuffix(group, "-admin") || group == "admin" {
			hasAdminGroup = true
		}
	}

	if hasOrgMembership && hasAdminGroup {
		return true
	}

	// Strategy 3: For development/testing, allow system users
	if subject.Source == SubjectSourceServiceAccount || subject.Source == SubjectSourceNone {
		return true
	}

	return false
}

// CheckOrganizationAccessForList checks if the user can list users.
// For list operations, we allow the request but rely on tenancy filtering.
func (a *OrganizationAdminAuthorizer) CheckOrganizationAccessForList(ctx context.Context) error {
	// Get the subject from the context
	subject := SubjectFromContext(ctx)

	a.logger.DebugContext(
		ctx,
		"User listing users (will be filtered by tenancy)",
		slog.String("user", subject.User),
		slog.Any("user_groups", subject.Groups),
	)

	// Allow list operations - tenancy logic will filter results
	return nil
}
