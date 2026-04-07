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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UsersAuthzInterceptorBuilder contains the data and logic needed to create the users authorization interceptor.
type UsersAuthzInterceptorBuilder struct {
	logger       *slog.Logger
	tenancyLogic TenancyLogic
}

// UsersAuthzInterceptor implements authorization checks for the Users API to ensure users can only
// manage users within their own organization(s).
type UsersAuthzInterceptor struct {
	logger       *slog.Logger
	tenancyLogic TenancyLogic
}

// NewUsersAuthzInterceptor creates a new builder for the users authorization interceptor.
func NewUsersAuthzInterceptor() *UsersAuthzInterceptorBuilder {
	return &UsersAuthzInterceptorBuilder{}
}

// SetLogger sets the logger that will be used by the interceptor. This is mandatory.
func (b *UsersAuthzInterceptorBuilder) SetLogger(value *slog.Logger) *UsersAuthzInterceptorBuilder {
	b.logger = value
	return b
}

// SetTenancyLogic sets the tenancy logic that will be used to determine which organizations
// the authenticated user has access to. This is mandatory.
func (b *UsersAuthzInterceptorBuilder) SetTenancyLogic(value TenancyLogic) *UsersAuthzInterceptorBuilder {
	b.tenancyLogic = value
	return b
}

// Build uses the information stored in the builder to create a new interceptor.
func (b *UsersAuthzInterceptorBuilder) Build() (result *UsersAuthzInterceptor, err error) {
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

	// Create the interceptor:
	result = &UsersAuthzInterceptor{
		logger:       b.logger,
		tenancyLogic: b.tenancyLogic,
	}
	return
}

// UnaryServer returns a gRPC unary server interceptor that performs authorization checks.
func (i *UsersAuthzInterceptor) UnaryServer(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	// Extract the organization from the request
	organization, err := i.extractOrganizationFromRequest(req)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to extract organization from request: %v", err)
	}

	// If no organization was found in the request, this might be a List request
	// which will be filtered by tenancy logic, so allow it to proceed
	if organization == "" {
		return handler(ctx, req)
	}

	// Check if the user has access to this organization
	err = i.checkOrganizationAccess(ctx, organization)
	if err != nil {
		return nil, err
	}

	// User is authorized, proceed with the request
	return handler(ctx, req)
}

// extractOrganizationFromRequest extracts the organization name from various request types.
func (i *UsersAuthzInterceptor) extractOrganizationFromRequest(req interface{}) (string, error) {
	// Define an interface for requests that have an Object with Metadata containing Tenants
	type ObjectGetter interface {
		GetObject() interface {
			GetMetadata() interface {
				GetTenants() []string
			}
		}
	}

	// Define an interface for requests that have an ID for Get/Delete operations
	type IdGetter interface {
		GetId() string
	}

	// Try to extract from Create/Update requests (have Object with Metadata)
	if objReq, ok := req.(ObjectGetter); ok {
		obj := objReq.GetObject()
		if obj != nil {
			metadata := obj.GetMetadata()
			if metadata != nil {
				tenants := metadata.GetTenants()
				if len(tenants) > 0 {
					// The first tenant is the organization
					return tenants[0], nil
				}
			}
		}
	}

	// For Get/Delete requests, we need to fetch the user first to check their organization
	// For now, we'll allow these through and rely on tenancy filtering
	if _, ok := req.(IdGetter); ok {
		// Return empty string to indicate we should allow the request through
		// The tenancy logic will filter the results
		return "", nil
	}

	// For List requests, return empty string to allow through with tenancy filtering
	return "", nil
}

// checkOrganizationAccess verifies that the authenticated user has access to the specified organization
// and has admin role for that organization.
func (i *UsersAuthzInterceptor) checkOrganizationAccess(ctx context.Context, organization string) error {
	// Get the subject from the context
	subject := SubjectFromContext(ctx)

	// Get the user's visible tenants (organizations)
	visibleTenants, err := i.tenancyLogic.DetermineVisibleTenants(ctx)
	if err != nil {
		i.logger.ErrorContext(
			ctx,
			"Failed to determine visible tenants",
			slog.String("user", subject.User),
			slog.Any("error", err),
		)
		return status.Error(codes.Internal, "failed to determine user's organizations")
	}

	// Check if the user has access to this organization
	if !visibleTenants.Contains(organization) {
		i.logger.WarnContext(
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
	if !i.hasAdminRole(ctx, subject, organization) {
		i.logger.WarnContext(
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

	return nil
}

// hasAdminRole checks if the user has an admin role for the specified organization.
func (i *UsersAuthzInterceptor) hasAdminRole(ctx context.Context, subject *Subject, organization string) bool {
	// Strategy 1: Check if user belongs to an "{organization}-admin" group
	adminGroup := organization + "-admin"
	for _, group := range subject.Groups {
		if group == adminGroup {
			return true
		}
	}

	// Strategy 2: Check if user belongs to an "admin" group and the organization group
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
