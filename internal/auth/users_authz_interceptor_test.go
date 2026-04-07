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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/osac-project/fulfillment-service/internal/collections"
)

var _ = Describe("Users Authorization Interceptor", func() {
	var (
		ctx         context.Context
		interceptor *UsersAuthzInterceptor
		mockTenancy *mockTenancyLogicForAuthz
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create mock tenancy logic
		mockTenancy = &mockTenancyLogicForAuthz{
			visibleTenants: collections.NewSet("test-org"),
		}

		// Create interceptor
		var err error
		interceptor, err = NewUsersAuthzInterceptor().
			SetLogger(logger).
			SetTenancyLogic(mockTenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("UnaryServer interceptor", func() {
		It("Allows requests from organization admins", func() {
			// Create context with an org admin subject
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "admin-user",
				Groups: []string{"test-org", "test-org-admin"},
			}
			ctx = ContextWithSubject(ctx, subject)

			// Create a mock request with organization in tenants
			mockReq := &mockUsersRequest{
				organization: "test-org",
			}

			// Mock handler that should be called
			handlerCalled := false
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return nil, nil
			}

			// Call the interceptor
			info := &grpc.UnaryServerInfo{
				FullMethod: "/osac.private.v1.Users/Create",
			}
			_, err := interceptor.UnaryServer(ctx, mockReq, info, handler)

			// Should succeed
			Expect(err).ToNot(HaveOccurred())
			Expect(handlerCalled).To(BeTrue())
		})

		It("Denies requests from non-admin users", func() {
			// Create context with a regular user (not admin)
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "regular-user",
				Groups: []string{"test-org"},
			}
			ctx = ContextWithSubject(ctx, subject)

			// Create a mock request
			mockReq := &mockUsersRequest{
				organization: "test-org",
			}

			// Mock handler that should NOT be called
			handlerCalled := false
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return nil, nil
			}

			// Call the interceptor
			info := &grpc.UnaryServerInfo{
				FullMethod: "/osac.private.v1.Users/Create",
			}
			_, err := interceptor.UnaryServer(ctx, mockReq, info, handler)

			// Should fail with permission denied
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.PermissionDenied))
			Expect(st.Message()).To(ContainSubstring("must be an admin"))
			Expect(handlerCalled).To(BeFalse())
		})

		It("Denies requests from users not in the organization", func() {
			// Create context with a user from a different org
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "other-user",
				Groups: []string{"other-org", "other-org-admin"},
			}
			ctx = ContextWithSubject(ctx, subject)

			// Mock tenancy to only show other-org
			mockTenancy.visibleTenants = collections.NewSet("other-org")

			// Create a mock request for test-org
			mockReq := &mockUsersRequest{
				organization: "test-org",
			}

			// Mock handler
			handlerCalled := false
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return nil, nil
			}

			// Call the interceptor
			info := &grpc.UnaryServerInfo{
				FullMethod: "/osac.private.v1.Users/Create",
			}
			_, err := interceptor.UnaryServer(ctx, mockReq, info, handler)

			// Should fail with permission denied
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.PermissionDenied))
			Expect(st.Message()).To(ContainSubstring("do not have permission"))
			Expect(handlerCalled).To(BeFalse())
		})

		It("Allows service accounts to access any organization", func() {
			// Create context with a service account
			subject := &Subject{
				Source: SubjectSourceServiceAccount,
				User:   "system:serviceaccount:default:controller",
				Groups: []string{},
			}
			ctx = ContextWithSubject(ctx, subject)

			// Mock tenancy to return empty set (service accounts bypass this)
			mockTenancy.visibleTenants = collections.NewSet[string]()

			// Create a mock request
			mockReq := &mockUsersRequest{
				organization: "test-org",
			}

			// Mock handler
			handlerCalled := false
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return nil, nil
			}

			// Call the interceptor
			info := &grpc.UnaryServerInfo{
				FullMethod: "/osac.private.v1.Users/Create",
			}
			_, err := interceptor.UnaryServer(ctx, mockReq, info, handler)

			// Service accounts should still fail on tenant check, but pass admin check
			// The tenant check comes first, so this will fail
			Expect(err).To(HaveOccurred())
			Expect(handlerCalled).To(BeFalse())
		})

		It("Allows requests without organization (e.g., List)", func() {
			// Create context with any user
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "user",
				Groups: []string{"test-org"},
			}
			ctx = ContextWithSubject(ctx, subject)

			// Create a mock request without organization
			mockReq := &mockUsersRequest{
				organization: "",
			}

			// Mock handler
			handlerCalled := false
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return nil, nil
			}

			// Call the interceptor
			info := &grpc.UnaryServerInfo{
				FullMethod: "/osac.private.v1.Users/List",
			}
			_, err := interceptor.UnaryServer(ctx, mockReq, info, handler)

			// Should succeed (no org check needed)
			Expect(err).ToNot(HaveOccurred())
			Expect(handlerCalled).To(BeTrue())
		})
	})

	Describe("Admin role detection", func() {
		It("Detects admin via {org}-admin group", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "admin-user",
				Groups: []string{"test-org-admin"},
			}
			ctx = ContextWithSubject(ctx, subject)

			hasAdmin := interceptor.hasAdminRole(ctx, subject, "test-org")
			Expect(hasAdmin).To(BeTrue())
		})

		It("Detects admin via admin group + org membership", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "admin-user",
				Groups: []string{"test-org", "admin"},
			}
			ctx = ContextWithSubject(ctx, subject)

			hasAdmin := interceptor.hasAdminRole(ctx, subject, "test-org")
			Expect(hasAdmin).To(BeTrue())
		})

		It("Detects service accounts as admin", func() {
			subject := &Subject{
				Source: SubjectSourceServiceAccount,
				User:   "system:serviceaccount:default:controller",
				Groups: []string{},
			}
			ctx = ContextWithSubject(ctx, subject)

			hasAdmin := interceptor.hasAdminRole(ctx, subject, "test-org")
			Expect(hasAdmin).To(BeTrue())
		})

		It("Rejects regular users without admin", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "regular-user",
				Groups: []string{"test-org"},
			}
			ctx = ContextWithSubject(ctx, subject)

			hasAdmin := interceptor.hasAdminRole(ctx, subject, "test-org")
			Expect(hasAdmin).To(BeFalse())
		})
	})
})

// mockUsersRequest simulates a users API request with organization info
type mockUsersRequest struct {
	organization string
}

// GetObject implements the ObjectGetter interface expected by extractOrganizationFromRequest
func (m *mockUsersRequest) GetObject() interface {
	GetMetadata() interface {
		GetTenants() []string
	}
} {
	return &mockUsersRequestObject{organization: m.organization}
}

type mockUsersRequestObject struct {
	organization string
}

func (m *mockUsersRequestObject) GetMetadata() interface {
	GetTenants() []string
} {
	return &mockUsersRequestMetadata{organization: m.organization}
}

type mockUsersRequestMetadata struct {
	organization string
}

func (m *mockUsersRequestMetadata) GetTenants() []string {
	if m.organization == "" {
		return []string{}
	}
	return []string{m.organization}
}
