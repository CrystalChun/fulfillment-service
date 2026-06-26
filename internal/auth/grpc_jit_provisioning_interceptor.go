/*
Copyright (c) 2026 Red Hat Inc.

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

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/osac-project/fulfillment-service/internal/reflection"
)

// UserProvisioner provisions user records in the database.
type UserProvisioner interface {
	Provision(ctx context.Context, username, tenant string, claims jwt.MapClaims) error
}

// GrpcJitProvisioningInterceptorBuilder builds a GrpcJitProvisioningInterceptor.
type GrpcJitProvisioningInterceptorBuilder struct {
	logger      *slog.Logger
	provisioner UserProvisioner
}

// GrpcJitProvisioningInterceptor performs just-in-time user provisioning for authenticated requests.
// It runs after authentication and authorization, provisioning users who have been granted access.
type GrpcJitProvisioningInterceptor struct {
	logger      *slog.Logger
	provisioner UserProvisioner
}

// NewGrpcJitProvisioningInterceptor creates a new builder.
func NewGrpcJitProvisioningInterceptor() *GrpcJitProvisioningInterceptorBuilder {
	return &GrpcJitProvisioningInterceptorBuilder{}
}

// SetLogger sets the logger.
func (b *GrpcJitProvisioningInterceptorBuilder) SetLogger(value *slog.Logger) *GrpcJitProvisioningInterceptorBuilder {
	b.logger = value
	return b
}

// SetProvisioner sets the user provisioner.
func (b *GrpcJitProvisioningInterceptorBuilder) SetProvisioner(value UserProvisioner) *GrpcJitProvisioningInterceptorBuilder {
	b.provisioner = reflection.NormalizeNil(value)
	return b
}

// Build creates the interceptor.
func (b *GrpcJitProvisioningInterceptorBuilder) Build() (result *GrpcJitProvisioningInterceptor, err error) {
	if b.logger == nil {
		return nil, fmt.Errorf("logger is mandatory")
	}
	result = &GrpcJitProvisioningInterceptor{
		logger:      b.logger,
		provisioner: b.provisioner,
	}
	return result, nil
}

// UnaryInterceptor returns a gRPC unary interceptor that provisions users just-in-time.
func (i *GrpcJitProvisioningInterceptor) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Skip if no provisioner configured
		if i.provisioner == nil {
			return handler(ctx, req)
		}

		// Get subject from context (set by authz interceptor)
		subject := SubjectFromContext(ctx)
		if subject == nil {
			// No subject means anonymous or unauthenticated request - skip provisioning
			return handler(ctx, req)
		}

		// Only provision for users with exactly one tenant
		// Users with zero tenants (unauthenticated), multiple tenants (cloud provider admins),
		// or special tenants (system/shared) should not have user records
		if !subject.Tenants.Finite() {
			return handler(ctx, req)
		}

		tenants := subject.Tenants.Inclusions()
		if len(tenants) != 1 {
			// Zero or multiple tenants - not a regular user
			return handler(ctx, req)
		}

		tenant := tenants[0]
		if tenant == SystemTenant || tenant == SharedTenant {
			// System and shared tenants don't have user records
			return handler(ctx, req)
		}

		// Get username from subject
		username := subject.User

		// Get JWT token from context for additional claims
		token := TokenFromContext(ctx)
		if token == nil {
			// No token in context - skip provisioning
			return handler(ctx, req)
		}

		// Extract JWT claims for email, name, etc.
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			i.logger.ErrorContext(ctx, "Failed to extract claims from JWT token for user provisioning")
			return handler(ctx, req)
		}

		// Provision the user
		err := i.provisioner.Provision(ctx, username, tenant, claims)
		if err != nil {
			i.logger.ErrorContext(ctx, "Failed to provision user",
				slog.String("username", username),
				slog.String("tenant", tenant),
				slog.Any("error", err),
			)
			return nil, grpcstatus.Error(grpccodes.Internal, "failed to provision user")
		}

		// Continue to handler
		return handler(ctx, req)
	}
}
