/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package organization

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

var _ = Describe("validateTenant", func() {
	It("should succeed when exactly one tenant is assigned", func() {
		t := &task{
			organization: privatev1.Organization_builder{
				Id: "test-org",
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"tenant-1"},
				}.Build(),
			}.Build(),
		}

		err := t.validateTenant()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail when no tenants are assigned", func() {
		t := &task{
			organization: privatev1.Organization_builder{
				Id: "test-org",
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{},
				}.Build(),
			}.Build(),
		}

		err := t.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("should fail when multiple tenants are assigned", func() {
		t := &task{
			organization: privatev1.Organization_builder{
				Id: "test-org",
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"tenant-1", "tenant-2"},
				}.Build(),
			}.Build(),
		}

		err := t.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("should fail when metadata is missing", func() {
		t := &task{
			organization: privatev1.Organization_builder{
				Id: "test-org",
			}.Build(),
		}

		err := t.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})
})

var _ = Describe("finalizer management", func() {
	It("should add finalizer when not present", func() {
		org := privatev1.Organization_builder{
			Id: "test-org",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{},
			}.Build(),
		}.Build()

		t := &task{organization: org}
		added := t.addFinalizer()

		Expect(added).To(BeTrue())
		Expect(org.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))
	})

	It("should not add finalizer when already present", func() {
		org := privatev1.Organization_builder{
			Id: "test-org",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
		}.Build()

		t := &task{organization: org}
		added := t.addFinalizer()

		Expect(added).To(BeFalse())
		Expect(org.GetMetadata().GetFinalizers()).To(HaveLen(1))
	})

	It("should remove finalizer when present", func() {
		org := privatev1.Organization_builder{
			Id: "test-org",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller, "other-finalizer"},
			}.Build(),
		}.Build()

		t := &task{organization: org}
		t.removeFinalizer()

		Expect(org.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
		Expect(org.GetMetadata().GetFinalizers()).To(ContainElement("other-finalizer"))
	})

	It("should handle removing finalizer when not present", func() {
		org := privatev1.Organization_builder{
			Id: "test-org",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{"other-finalizer"},
			}.Build(),
		}.Build()

		t := &task{organization: org}
		t.removeFinalizer()

		Expect(org.GetMetadata().GetFinalizers()).To(HaveLen(1))
		Expect(org.GetMetadata().GetFinalizers()).To(ContainElement("other-finalizer"))
	})
})

var _ = Describe("IDP sync", func() {
	const (
		orgID          = "test-org-id"
		orgName        = "test-org"
		tenantName     = "my-tenant"
		breakGlassUser = "user-123"
	)

	var (
		ctx        context.Context
		ctrl       *gomock.Controller
		mockIdpMgr *idp.MockOrganizationManagerInterface
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockIdpMgr = idp.NewMockOrganizationManagerInterface(ctrl)
		DeferCleanup(ctrl.Finish)
	})

	It("should sync organization to IDP successfully", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       orgName,
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Description: "Test organization",
			Status: privatev1.OrganizationStatus_builder{
				State: privatev1.OrganizationState_ORGANIZATION_STATE_PENDING,
			}.Build(),
		}.Build()

		mockIdpMgr.EXPECT().
			CreateOrganization(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, config *idp.OrganizationConfig) (*idp.BreakGlassCredentials, error) {
				Expect(config.Name).To(Equal(orgName))
				Expect(config.DisplayName).To(Equal("Test organization"))
				return &idp.BreakGlassCredentials{
					UserID:   breakGlassUser,
					Username: "break-glass",
					Email:    "break-glass@test.local",
					Password: "temp-password",
				}, nil
			})

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.syncToIDP(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Verify state updated to SYNCED
		Expect(organization.GetStatus().GetState()).To(Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED))
		Expect(organization.GetStatus().GetIdpOrganizationName()).To(Equal(orgName))
		Expect(organization.GetStatus().GetBreakGlassUserId()).To(Equal(breakGlassUser))
		Expect(organization.GetStatus().GetMessage()).To(BeEmpty())
	})

	It("should handle IDP sync failure", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       orgName,
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Description: "Test organization",
			Status: privatev1.OrganizationStatus_builder{
				State: privatev1.OrganizationState_ORGANIZATION_STATE_PENDING,
			}.Build(),
		}.Build()

		mockIdpMgr.EXPECT().
			CreateOrganization(gomock.Any(), gomock.Any()).
			Return(nil, errors.New("IDP connection timeout"))

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.syncToIDP(ctx)
		Expect(err).ToNot(HaveOccurred()) // Error captured in status, not returned

		// Verify state updated to FAILED with error message
		Expect(organization.GetStatus().GetState()).To(Equal(privatev1.OrganizationState_ORGANIZATION_STATE_FAILED))
		Expect(organization.GetStatus().GetMessage()).ToNot(BeEmpty())
		Expect(organization.GetStatus().GetMessage()).To(ContainSubstring("IDP sync failed"))
		Expect(organization.GetStatus().GetMessage()).To(ContainSubstring("IDP connection timeout"))
		Expect(organization.GetStatus().GetIdpOrganizationName()).To(BeEmpty())
		Expect(organization.GetStatus().GetBreakGlassUserId()).To(BeEmpty())
	})

	It("should use organization ID as fallback when name is empty", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       "", // Empty name
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State: privatev1.OrganizationState_ORGANIZATION_STATE_PENDING,
			}.Build(),
		}.Build()

		mockIdpMgr.EXPECT().
			CreateOrganization(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, config *idp.OrganizationConfig) (*idp.BreakGlassCredentials, error) {
				Expect(config.Name).To(Equal(orgID)) // Should use ID as fallback
				return &idp.BreakGlassCredentials{
					UserID: breakGlassUser,
				}, nil
			})

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.syncToIDP(ctx)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("update flow", func() {
	const (
		orgID      = "test-org-id"
		orgName    = "test-org"
		tenantName = "my-tenant"
	)

	var (
		ctx        context.Context
		ctrl       *gomock.Controller
		mockIdpMgr *idp.MockOrganizationManagerInterface
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockIdpMgr = idp.NewMockOrganizationManagerInterface(ctrl)
		DeferCleanup(ctrl.Finish)
	})

	It("should add finalizer and return on first reconcile", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       orgName,
				Finalizers: []string{}, // No finalizers yet
				Tenants:    []string{tenantName},
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Verify finalizer was added
		Expect(organization.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))

		// No IDP calls should have been made
		// (gomock will fail if unexpected calls are made)
	})

	It("should skip sync when organization is already SYNCED", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       orgName,
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State:               privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
				IdpOrganizationName: orgName,
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		// No IDP calls should have been made
	})

	It("should skip sync when organization is FAILED", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       orgName,
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State: privatev1.OrganizationState_ORGANIZATION_STATE_FAILED,
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		// No IDP calls should have been made
	})
})

var _ = Describe("delete flow", func() {
	const (
		orgID      = "test-org-id"
		orgName    = "test-org"
		tenantName = "my-tenant"
	)

	var (
		ctx        context.Context
		ctrl       *gomock.Controller
		mockIdpMgr *idp.MockOrganizationManagerInterface
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockIdpMgr = idp.NewMockOrganizationManagerInterface(ctrl)
		DeferCleanup(ctrl.Finish)
	})

	It("should delete organization from IDP and remove finalizer", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       orgName,
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State:               privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
				IdpOrganizationName: orgName,
			}.Build(),
		}.Build()

		mockIdpMgr.EXPECT().
			DeleteOrganizationRealm(gomock.Any(), orgName).
			Return(nil)

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Verify finalizer removed
		Expect(organization.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should skip IDP deletion when organization not synced", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       orgName,
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State: privatev1.OrganizationState_ORGANIZATION_STATE_PENDING,
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Verify finalizer still removed even though no IDP deletion
		Expect(organization.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))

		// No IDP calls should have been made
	})

	It("should handle IDP deletion failure", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       orgName,
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State:               privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
				IdpOrganizationName: orgName,
			}.Build(),
		}.Build()

		mockIdpMgr.EXPECT().
			DeleteOrganizationRealm(gomock.Any(), orgName).
			Return(errors.New("IDP connection timeout"))

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.delete(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to delete IDP organization"))

		// Finalizer should NOT be removed on error
		Expect(organization.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))
	})

	It("should skip IDP deletion when organization name is empty", func() {
		organization := privatev1.Organization_builder{
			Id: orgID,
			Metadata: privatev1.Metadata_builder{
				Name:       orgName,
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{tenantName},
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State:               privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
				IdpOrganizationName: "", // Empty name
			}.Build(),
		}.Build()

		t := &task{
			r: &function{
				logger:     logger,
				idpManager: mockIdpMgr,
			},
			organization: organization,
		}

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Finalizer should be removed
		Expect(organization.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))

		// No IDP calls should have been made
	})
})
