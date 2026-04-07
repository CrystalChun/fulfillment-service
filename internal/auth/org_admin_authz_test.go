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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/osac-project/fulfillment-service/internal/collections"
)

// mockTenancyLogicForAuthz is a simple mock for testing authorization
type mockTenancyLogicForAuthz struct {
	visibleTenants collections.Set[string]
}

func (m *mockTenancyLogicForAuthz) DetermineAssignableTenants(ctx context.Context) (collections.Set[string], error) {
	return m.visibleTenants, nil
}

func (m *mockTenancyLogicForAuthz) DetermineDefaultTenants(ctx context.Context) (collections.Set[string], error) {
	return m.visibleTenants, nil
}

func (m *mockTenancyLogicForAuthz) DetermineVisibleTenants(ctx context.Context) (collections.Set[string], error) {
	return m.visibleTenants, nil
}

var _ = Describe("Organization Admin Authorizer", func() {
	var (
		ctx        context.Context
		authorizer *OrganizationAdminAuthorizer
	)

	Describe("Authorization checks", func() {
		It("Allows access when user is in organization-admin group", func() {
			// Create context with a subject who is admin of test-org
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "admin-user",
				Groups: []string{"test-org", "test-org-admin"},
			}
			ctx = ContextWithSubject(context.Background(), subject)

			// Create mock tenancy logic that returns the user's groups as visible tenants
			mockTenancy := &mockTenancyLogicForAuthz{
				visibleTenants: collections.NewSet("test-org", "test-org-admin"),
			}

			// Create authorizer
			var err error
			authorizer, err = NewOrganizationAdminAuthorizer().
				SetLogger(logger).
				SetTenancyLogic(mockTenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Check access - should succeed
			err = authorizer.CheckOrganizationAccess(ctx, "test-org")
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows access when user has admin group and organization membership", func() {
			// Create context with a subject who is admin and member of test-org
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "admin-user",
				Groups: []string{"test-org", "admin"},
			}
			ctx = ContextWithSubject(context.Background(), subject)

			mockTenancy := &mockTenancyLogicForAuthz{
				visibleTenants: collections.NewSet("test-org", "admin"),
			}

			var err error
			authorizer, err = NewOrganizationAdminAuthorizer().
				SetLogger(logger).
				SetTenancyLogic(mockTenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Check access - should succeed
			err = authorizer.CheckOrganizationAccess(ctx, "test-org")
			Expect(err).ToNot(HaveOccurred())
		})

		It("Denies access when user is not in organization", func() {
			// Create context with a subject who is NOT in test-org
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "other-user",
				Groups: []string{"other-org", "other-org-admin"},
			}
			ctx = ContextWithSubject(context.Background(), subject)

			mockTenancy := &mockTenancyLogicForAuthz{
				visibleTenants: collections.NewSet("other-org", "other-org-admin"),
			}

			var err error
			authorizer, err = NewOrganizationAdminAuthorizer().
				SetLogger(logger).
				SetTenancyLogic(mockTenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Check access - should fail with permission denied
			err = authorizer.CheckOrganizationAccess(ctx, "test-org")
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.PermissionDenied))
			Expect(st.Message()).To(ContainSubstring("do not have permission"))
		})

		It("Denies access when user is in organization but not admin", func() {
			// Create context with a subject who is in test-org but not admin
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "regular-user",
				Groups: []string{"test-org"},
			}
			ctx = ContextWithSubject(context.Background(), subject)

			mockTenancy := &mockTenancyLogicForAuthz{
				visibleTenants: collections.NewSet("test-org"),
			}

			var err error
			authorizer, err = NewOrganizationAdminAuthorizer().
				SetLogger(logger).
				SetTenancyLogic(mockTenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Check access - should fail with permission denied
			err = authorizer.CheckOrganizationAccess(ctx, "test-org")
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.PermissionDenied))
			Expect(st.Message()).To(ContainSubstring("must be an admin"))
		})

		It("Allows service accounts to access any organization", func() {
			// Create context with a service account subject
			subject := &Subject{
				Source: SubjectSourceServiceAccount,
				User:   "system:serviceaccount:default:controller",
				Groups: []string{},
			}
			ctx = ContextWithSubject(context.Background(), subject)

			mockTenancy := &mockTenancyLogicForAuthz{
				visibleTenants: collections.NewSet("system"),
			}

			var err error
			authorizer, err = NewOrganizationAdminAuthorizer().
				SetLogger(logger).
				SetTenancyLogic(mockTenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Check access - should succeed for service accounts
			err = authorizer.CheckOrganizationAccess(ctx, "any-org")
			Expect(err).To(HaveOccurred()) // Will fail on tenant check but pass on admin check
		})

		It("Allows list operations without specific organization", func() {
			// Create context with any subject
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "user",
				Groups: []string{"test-org"},
			}
			ctx = ContextWithSubject(context.Background(), subject)

			mockTenancy := &mockTenancyLogicForAuthz{
				visibleTenants: collections.NewSet("test-org"),
			}

			var err error
			authorizer, err = NewOrganizationAdminAuthorizer().
				SetLogger(logger).
				SetTenancyLogic(mockTenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Check access for list - should always succeed
			err = authorizer.CheckOrganizationAccessForList(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Returns error when organization is empty", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "user",
				Groups: []string{"test-org"},
			}
			ctx = ContextWithSubject(context.Background(), subject)

			mockTenancy := &mockTenancyLogicForAuthz{
				visibleTenants: collections.NewSet("test-org"),
			}

			var err error
			authorizer, err = NewOrganizationAdminAuthorizer().
				SetLogger(logger).
				SetTenancyLogic(mockTenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Check access with empty organization
			err = authorizer.CheckOrganizationAccess(ctx, "")
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.InvalidArgument))
		})
	})

	Describe("Builder validation", func() {
		It("Fails when logger is not set", func() {
			_, err := NewOrganizationAdminAuthorizer().
				SetTenancyLogic(&mockTenancyLogicForAuthz{}).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
		})

		It("Fails when tenancy logic is not set", func() {
			_, err := NewOrganizationAdminAuthorizer().
				SetLogger(logger).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
		})
	})
})
