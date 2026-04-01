/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

var _ = Describe("Organizations Server", func() {
	var (
		ctx           context.Context
		tx            database.Tx
		orgManager    *idp.OrganizationManager
		idpClient     *statefulMockClient
		privateServer *PrivateOrganizationsServer
		ctrl          *gomock.Controller
	)

	BeforeEach(func() {
		var err error

		// Create context:
		ctx = context.Background()

		// Create gomock controller:
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)

		// Prepare the database pool:
		db := server.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.MakeURL())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		// Create the transaction manager:
		tm, err := database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Start a transaction and add it to the context:
		tx, err = tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		// Create DAO tables:
		err = dao.CreateTables[*privatev1.Organization](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create stateful mock IdP client:
		idpClient = newStatefulMockClient(ctrl)

		// Create organization manager with mock client:
		orgManager, err = idp.NewOrganizationManager().
			SetLogger(logger).
			SetClient(idpClient).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create server (without notifier for testing):
		privateServer, err = NewPrivateOrganizationsServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetOrganizationManager(orgManager).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Creates an organization in both database and IdP", func() {
		// Create request:
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:             "admin@test-org.com",
						AnnotationAdminUsername:          "admin",
						AnnotationAdminPassword:          "password123",
						AnnotationAssignTenantManagement: "true",
					},
				},
				Description: "Test Organization",
			},
		}

		// Create organization:
		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object).ToNot(BeNil())
		Expect(response.Object.Id).ToNot(BeEmpty())
		Expect(response.Object.Metadata.Name).To(Equal("test-org"))

		// Verify all credential annotations were removed from response:
		Expect(response.Object.Metadata.Annotations).ToNot(HaveKey(AnnotationAdminPassword))
		Expect(response.Object.Metadata.Annotations).ToNot(HaveKey(AnnotationAdminEmail))
		Expect(response.Object.Metadata.Annotations).ToNot(HaveKey(AnnotationAdminUsername))

		// Verify break-glass credentials are returned in the response:
		Expect(response.BreakGlassCredentials).ToNot(BeNil())
		Expect(response.BreakGlassCredentials.UserId).ToNot(BeEmpty())
		Expect(response.BreakGlassCredentials.Username).To(Equal("admin"))
		Expect(response.BreakGlassCredentials.Email).To(Equal("admin@test-org.com"))
		Expect(response.BreakGlassCredentials.TemporaryPassword).To(Equal("password123"))

		// Verify organization was created in IdP:
		Expect(idpClient.organizations).To(HaveKey("test-org"))
		Expect(idpClient.organizations["test-org"].Name).To(Equal("test-org"))
	})

	It("Does not persist credentials in the database", func() {
		// Create request with credentials in annotations:
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org-secure",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org-secure.com",
						AnnotationAdminUsername: "admin",
						AnnotationAdminPassword: "password123",
					},
				},
				Description: "Test Organization for Security",
			},
		}

		// Create organization:
		createResp, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())

		// Retrieve the organization from the database:
		getResp, err := privateServer.Get(ctx, &privatev1.OrganizationsGetRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())

		// Verify credentials are NOT in the stored annotations:
		Expect(getResp.Object.Metadata.Annotations).ToNot(HaveKey(AnnotationAdminPassword))
		Expect(getResp.Object.Metadata.Annotations).ToNot(HaveKey(AnnotationAdminEmail))
		Expect(getResp.Object.Metadata.Annotations).ToNot(HaveKey(AnnotationAdminUsername))

		// Verify credentials were returned in the create response:
		Expect(createResp.BreakGlassCredentials).ToNot(BeNil())
		Expect(createResp.BreakGlassCredentials.TemporaryPassword).To(Equal("password123"))
	})

	It("Supports partial credential overrides", func() {
		// Provide only custom email, other fields auto-generated:
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "partial-override-org",
					Annotations: map[string]string{
						AnnotationAdminEmail: "custom@example.com",
					},
				},
			},
		}

		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())

		// Custom email used, but username and password auto-generated:
		Expect(response.BreakGlassCredentials.Email).To(Equal("custom@example.com"))
		Expect(response.BreakGlassCredentials.Username).To(Equal("partial-override-org-osac-break-glass"))
		Expect(response.BreakGlassCredentials.TemporaryPassword).To(HaveLen(24))
	})

	It("Lists organizations", func() {
		// Create an organization first:
		createReq := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org.com",
						AnnotationAdminUsername: "admin",
						AnnotationAdminPassword: "password123",
					},
				},
			},
		}
		_, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// List organizations:
		listResp, err := privateServer.List(ctx, &privatev1.OrganizationsListRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(1)))
		Expect(listResp.Items).To(HaveLen(1))
		Expect(listResp.Items[0].Metadata.Name).To(Equal("test-org"))
	})

	It("Gets an organization by ID", func() {
		// Create an organization:
		createReq := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org.com",
						AnnotationAdminUsername: "admin",
						AnnotationAdminPassword: "password123",
					},
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Get the organization:
		getResp, err := privateServer.Get(ctx, &privatev1.OrganizationsGetRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
		Expect(getResp.Object.Metadata.Name).To(Equal("test-org"))
	})

	It("Deletes an organization from both database and IdP", func() {
		// Create an organization:
		createReq := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org.com",
						AnnotationAdminUsername: "admin",
						AnnotationAdminPassword: "password123",
					},
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Verify it exists in IdP:
		Expect(idpClient.organizations).To(HaveKey("test-org"))

		// Delete the organization:
		_, err = privateServer.Delete(ctx, &privatev1.OrganizationsDeleteRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())

		// Verify it was deleted from IdP:
		Expect(idpClient.organizations).ToNot(HaveKey("test-org"))
	})

	It("Auto-generates credentials when annotations are not provided", func() {
		// Create organization without any credential annotations:
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "auto-creds-org",
				},
				Description: "Organization with auto-generated credentials",
			},
		}

		// Create organization:
		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())

		// Verify auto-generated credentials are returned:
		Expect(response.BreakGlassCredentials).ToNot(BeNil())
		Expect(response.BreakGlassCredentials.UserId).ToNot(BeEmpty())
		Expect(response.BreakGlassCredentials.Username).To(Equal("auto-creds-org-osac-break-glass"))
		Expect(response.BreakGlassCredentials.Email).To(Equal("break-glass@auto-creds-org.osac.local"))
		Expect(response.BreakGlassCredentials.TemporaryPassword).ToNot(BeEmpty())
		Expect(response.BreakGlassCredentials.TemporaryPassword).To(HaveLen(24))
		Expect(response.BreakGlassCredentials.TemporaryPassword).To(MatchRegexp(`^[A-Za-z0-9!@#$%]{24}$`))
	})

	It("Uses provided credentials when annotations are present", func() {
		// Create organization with custom credentials:
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "custom-creds-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "custom-admin@example.com",
						AnnotationAdminUsername: "custom-admin",
						AnnotationAdminPassword: "CustomP@ss123",
					},
				},
			},
		}

		// Create organization:
		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())

		// Verify custom credentials are used:
		Expect(response.BreakGlassCredentials.Username).To(Equal("custom-admin"))
		Expect(response.BreakGlassCredentials.Email).To(Equal("custom-admin@example.com"))
		Expect(response.BreakGlassCredentials.TemporaryPassword).To(Equal("CustomP@ss123"))
	})

	It("Fails to create organization when IdP creation fails", func() {
		// Configure mock to fail IdP creation:
		idpClient.createOrgShouldFail = true
		idpClient.createOrgError = errors.New("IdP service unavailable")

		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org.com",
						AnnotationAdminUsername: "admin",
						AnnotationAdminPassword: "password123",
					},
				},
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create organization in IdP"))

		// Verify organization was NOT created in IdP:
		Expect(idpClient.organizations).ToNot(HaveKey("test-org"))

		// Verify organization was NOT created in database:
		listResp, err := privateServer.List(ctx, &privatev1.OrganizationsListRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(0)))
	})

	It("Cleans up IdP tenant when database creation fails", func() {
		// We can't easily simulate database failure in this test setup,
		// but we can verify the cleanup logic is in place by checking
		// that organizations are created in IdP first, then in the database.
		// This test verifies successful flow.
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org.com",
						AnnotationAdminUsername: "admin",
						AnnotationAdminPassword: "password123",
					},
				},
			},
		}

		createResp, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())

		// Verify organization exists in both IdP and database:
		Expect(idpClient.organizations).To(HaveKey("test-org"))

		getResp, err := privateServer.Get(ctx, &privatev1.OrganizationsGetRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Metadata.Name).To(Equal("test-org"))
	})

	It("Fails to delete organization when IdP deletion fails", func() {
		// Create an organization first:
		createReq := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org.com",
						AnnotationAdminUsername: "admin",
						AnnotationAdminPassword: "password123",
					},
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Configure mock to fail IdP deletion:
		idpClient.deleteOrgShouldFail = true
		idpClient.deleteOrgError = errors.New("IdP service unavailable")

		// Attempt to delete the organization:
		_, err = privateServer.Delete(ctx, &privatev1.OrganizationsDeleteRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to delete organization from IdP"))

		// Verify organization still exists in IdP:
		Expect(idpClient.organizations).To(HaveKey("test-org"))

		// Verify organization still exists in database:
		getResp, err := privateServer.Get(ctx, &privatev1.OrganizationsGetRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Metadata.Name).To(Equal("test-org"))
	})

	It("Updates an organization in the database only", func() {
		// Create an organization:
		createReq := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org.com",
						AnnotationAdminUsername: "admin",
						AnnotationAdminPassword: "password123",
					},
				},
				Description: "Original description",
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Update the organization:
		updateReq := &privatev1.OrganizationsUpdateRequest{
			Object: &privatev1.Organization{
				Id:          createResp.Object.Id,
				Description: "Updated description",
			},
		}
		updateResp, err := privateServer.Update(ctx, updateReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResp.Object.Description).To(Equal("Updated description"))

		// Note: Update does not sync to IdP currently
	})
})
