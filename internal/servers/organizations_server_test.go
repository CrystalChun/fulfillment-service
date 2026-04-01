/*
Copyright (c) 2025 Red Hat Inc.

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

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

var _ = Describe("Organizations Server (Public)", func() {
	var (
		ctx          context.Context
		tx           database.Tx
		orgManager   *idp.OrganizationManager
		idpClient    *mockIdpClient
		publicServer *OrganizationsServer
	)

	BeforeEach(func() {
		var err error

		// Create context:
		ctx = context.Background()

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

		// Create mock IdP client:
		idpClient = &mockIdpClient{
			organizations: make(map[string]*idp.Organization),
		}

		// Create organization manager with mock client:
		orgManager, err = idp.NewOrganizationManager().
			SetLogger(logger).
			SetClient(idpClient).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create public server (without notifier for testing):
		publicServer, err = NewOrganizationsServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetOrganizationManager(orgManager).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all required parameters are set", func() {
			srv, err := NewOrganizationsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetOrganizationManager(orgManager).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(srv).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			srv, err := NewOrganizationsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetOrganizationManager(orgManager).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(srv).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			srv, err := NewOrganizationsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetOrganizationManager(orgManager).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(srv).To(BeNil())
		})

		It("Fails if organization manager is not set", func() {
			srv, err := NewOrganizationsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("organization manager is mandatory"))
			Expect(srv).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		It("Creates an organization in both database and IdP", func() {
			// Create request:
			request := &publicv1.OrganizationsCreateRequest{
				Object: &publicv1.Organization{
					Metadata: &publicv1.Metadata{
						Name: "test-org",
						Annotations: map[string]string{
							AnnotationAdminEmail:            "admin@test-org.com",
							AnnotationAdminUsername:         "admin",
							AnnotationAdminPassword:         "password123",
							AnnotationAssignRealmManagement: "true",
						},
					},
					Description: "Test Organization",
				},
			}

			// Create organization:
			response, err := publicServer.Create(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.Object).ToNot(BeNil())
			Expect(response.Object.Id).ToNot(BeEmpty())
			Expect(response.Object.Metadata.Name).To(Equal("test-org"))

			// Verify password annotation was removed from response:
			Expect(response.Object.Metadata.Annotations).ToNot(HaveKey(AnnotationAdminPassword))

			// Verify organization was created in IdP:
			Expect(idpClient.organizations).To(HaveKey("test-org"))
			Expect(idpClient.organizations["test-org"].Name).To(Equal("test-org"))
		})

		It("Lists organizations", func() {
			// Create an organization first:
			createReq := &publicv1.OrganizationsCreateRequest{
				Object: &publicv1.Organization{
					Metadata: &publicv1.Metadata{
						Name: "test-org",
						Annotations: map[string]string{
							AnnotationAdminEmail:    "admin@test-org.com",
							AnnotationAdminUsername: "admin",
							AnnotationAdminPassword: "password123",
						},
					},
				},
			}
			_, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// List organizations:
			listResp, err := publicServer.List(ctx, &publicv1.OrganizationsListRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.Size).To(Equal(int32(1)))
			Expect(listResp.Items).To(HaveLen(1))
			Expect(listResp.Items[0].Metadata.Name).To(Equal("test-org"))
		})

		It("Gets an organization by ID", func() {
			// Create an organization:
			createReq := &publicv1.OrganizationsCreateRequest{
				Object: &publicv1.Organization{
					Metadata: &publicv1.Metadata{
						Name: "test-org",
						Annotations: map[string]string{
							AnnotationAdminEmail:    "admin@test-org.com",
							AnnotationAdminUsername: "admin",
							AnnotationAdminPassword: "password123",
						},
					},
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Get the organization:
			getResp, err := publicServer.Get(ctx, &publicv1.OrganizationsGetRequest{
				Id: createResp.Object.Id,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
			Expect(getResp.Object.Metadata.Name).To(Equal("test-org"))
		})

		It("Updates an organization", func() {
			// Create an organization:
			createReq := &publicv1.OrganizationsCreateRequest{
				Object: &publicv1.Organization{
					Metadata: &publicv1.Metadata{
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
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Update the organization:
			updateReq := &publicv1.OrganizationsUpdateRequest{
				Object: &publicv1.Organization{
					Id:          createResp.Object.Id,
					Description: "Updated description",
				},
			}
			updateResp, err := publicServer.Update(ctx, updateReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResp.Object.Description).To(Equal("Updated description"))
		})

		It("Deletes an organization from both database and IdP", func() {
			// Create an organization:
			createReq := &publicv1.OrganizationsCreateRequest{
				Object: &publicv1.Organization{
					Metadata: &publicv1.Metadata{
						Name: "test-org",
						Annotations: map[string]string{
							AnnotationAdminEmail:    "admin@test-org.com",
							AnnotationAdminUsername: "admin",
							AnnotationAdminPassword: "password123",
						},
					},
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Verify it exists in IdP:
			Expect(idpClient.organizations).To(HaveKey("test-org"))

			// Delete the organization:
			_, err = publicServer.Delete(ctx, &publicv1.OrganizationsDeleteRequest{
				Id: createResp.Object.Id,
			})
			Expect(err).ToNot(HaveOccurred())

			// Verify it was deleted from IdP:
			Expect(idpClient.organizations).ToNot(HaveKey("test-org"))
		})

		It("Properly maps public to private and back", func() {
			// Create with public API:
			createReq := &publicv1.OrganizationsCreateRequest{
				Object: &publicv1.Organization{
					Metadata: &publicv1.Metadata{
						Name: "test-org",
						Annotations: map[string]string{
							AnnotationAdminEmail:    "admin@test-org.com",
							AnnotationAdminUsername: "admin",
							AnnotationAdminPassword: "password123",
						},
						Labels: map[string]string{
							"env": "test",
						},
					},
					Description: "Test mapping",
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Verify response has public type:
			Expect(createResp.Object).To(BeAssignableToTypeOf(&publicv1.Organization{}))
			Expect(createResp.Object.Description).To(Equal("Test mapping"))
			Expect(createResp.Object.Metadata.Labels).To(HaveKeyWithValue("env", "test"))

			// Get and verify mapping:
			getResp, err := publicServer.Get(ctx, &publicv1.OrganizationsGetRequest{
				Id: createResp.Object.Id,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(getResp.Object).To(BeAssignableToTypeOf(&publicv1.Organization{}))
			Expect(getResp.Object.Description).To(Equal("Test mapping"))
		})
	})
})
