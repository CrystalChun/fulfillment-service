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

var _ = Describe("Users Server (Public)", func() {
	var (
		ctx          context.Context
		tx           database.Tx
		idpClient    *mockIdpClient
		publicServer *UsersServer
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
		err = dao.CreateTables[*privatev1.User](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create mock IdP client:
		idpClient = &mockIdpClient{
			organizations: make(map[string]*idp.Organization),
		}

		// Create public server (without notifier for testing):
		publicServer, err = NewUsersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetIdpClient(idpClient).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all required parameters are set", func() {
			srv, err := NewUsersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetIdpClient(idpClient).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(srv).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			srv, err := NewUsersServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetIdpClient(idpClient).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(srv).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			srv, err := NewUsersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetIdpClient(idpClient).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(srv).To(BeNil())
		})

		It("Fails if idp client is not set", func() {
			srv, err := NewUsersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("idp client is mandatory"))
			Expect(srv).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		It("Creates a user in both database and IdP", func() {
			// Create request:
			request := &publicv1.UsersCreateRequest{
				Object: &publicv1.User{
					Metadata: &publicv1.Metadata{
						Name:    "testuser",
						Tenants: []string{"test-org"},
						Annotations: map[string]string{
							AnnotationUserPassword: "SecurePass123!",
						},
					},
					Email:     "testuser@example.com",
					Username:  "testuser",
					FirstName: "Test",
					LastName:  "User",
					Enabled:   true,
				},
			}

			// Create user:
			response, err := publicServer.Create(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.Object).ToNot(BeNil())
			Expect(response.Object.Id).ToNot(BeEmpty())
			Expect(response.Object.Username).To(Equal("testuser"))

			// Verify password annotation was removed from response:
			Expect(response.Object.Metadata.Annotations).ToNot(HaveKey(AnnotationUserPassword))

			// Verify user was created in IdP:
			Expect(idpClient.users).To(HaveKey("test-org"))
			Expect(idpClient.users["test-org"]).To(HaveLen(1))
			Expect(idpClient.users["test-org"][0].Username).To(Equal("testuser"))
		})

		It("Lists users", func() {
			// Create a user first:
			createReq := &publicv1.UsersCreateRequest{
				Object: &publicv1.User{
					Metadata: &publicv1.Metadata{
						Name:    "testuser",
						Tenants: []string{"test-org"},
						Annotations: map[string]string{
							AnnotationUserPassword: "SecurePass123!",
						},
					},
					Email:    "testuser@example.com",
					Username: "testuser",
					Enabled:  true,
				},
			}
			_, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// List users:
			listResp, err := publicServer.List(ctx, &publicv1.UsersListRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.Size).To(Equal(int32(1)))
			Expect(listResp.Items).To(HaveLen(1))
			Expect(listResp.Items[0].Username).To(Equal("testuser"))
		})

		It("Gets a user by ID", func() {
			// Create a user:
			createReq := &publicv1.UsersCreateRequest{
				Object: &publicv1.User{
					Metadata: &publicv1.Metadata{
						Name:    "testuser",
						Tenants: []string{"test-org"},
						Annotations: map[string]string{
							AnnotationUserPassword: "SecurePass123!",
						},
					},
					Email:    "testuser@example.com",
					Username: "testuser",
					Enabled:  true,
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Get the user:
			getResp, err := publicServer.Get(ctx, &publicv1.UsersGetRequest{
				Id: createResp.Object.Id,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
			Expect(getResp.Object.Username).To(Equal("testuser"))
		})

		It("Updates a user", func() {
			// Create a user:
			createReq := &publicv1.UsersCreateRequest{
				Object: &publicv1.User{
					Metadata: &publicv1.Metadata{
						Name:    "testuser",
						Tenants: []string{"test-org"},
						Annotations: map[string]string{
							AnnotationUserPassword: "SecurePass123!",
						},
					},
					Email:     "testuser@example.com",
					Username:  "testuser",
					FirstName: "Test",
					LastName:  "User",
					Enabled:   true,
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Update the user:
			updateReq := &publicv1.UsersUpdateRequest{
				Object: &publicv1.User{
					Id:        createResp.Object.Id,
					FirstName: "Updated",
					LastName:  "Name",
				},
			}
			updateResp, err := publicServer.Update(ctx, updateReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResp.Object.FirstName).To(Equal("Updated"))
			Expect(updateResp.Object.LastName).To(Equal("Name"))
		})

		It("Deletes a user from both database and IdP", func() {
			// Create a user:
			createReq := &publicv1.UsersCreateRequest{
				Object: &publicv1.User{
					Metadata: &publicv1.Metadata{
						Name:    "testuser",
						Tenants: []string{"test-org"},
						Annotations: map[string]string{
							AnnotationUserPassword: "SecurePass123!",
						},
					},
					Email:    "testuser@example.com",
					Username: "testuser",
					Enabled:  true,
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Verify it exists in IdP:
			Expect(idpClient.users).To(HaveKey("test-org"))
			Expect(idpClient.users["test-org"]).To(HaveLen(1))

			// Delete the user:
			_, err = publicServer.Delete(ctx, &publicv1.UsersDeleteRequest{
				Id: createResp.Object.Id,
			})
			Expect(err).ToNot(HaveOccurred())

			// Verify it was deleted from IdP:
			Expect(idpClient.users["test-org"]).To(HaveLen(0))
		})

		It("Properly maps public to private and back", func() {
			// Create with public API:
			createReq := &publicv1.UsersCreateRequest{
				Object: &publicv1.User{
					Metadata: &publicv1.Metadata{
						Name:    "testuser",
						Tenants: []string{"test-org"},
						Annotations: map[string]string{
							AnnotationUserPassword: "SecurePass123!",
						},
						Labels: map[string]string{
							"role": "admin",
						},
					},
					Email:     "testuser@example.com",
					Username:  "testuser",
					FirstName: "Test",
					LastName:  "User",
					Enabled:   true,
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Verify response has public type:
			Expect(createResp.Object).To(BeAssignableToTypeOf(&publicv1.User{}))
			Expect(createResp.Object.FirstName).To(Equal("Test"))
			Expect(createResp.Object.LastName).To(Equal("User"))
			Expect(createResp.Object.Metadata.Labels).To(HaveKeyWithValue("role", "admin"))

			// Get and verify mapping:
			getResp, err := publicServer.Get(ctx, &publicv1.UsersGetRequest{
				Id: createResp.Object.Id,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(getResp.Object).To(BeAssignableToTypeOf(&publicv1.User{}))
			Expect(getResp.Object.FirstName).To(Equal("Test"))
			Expect(getResp.Object.LastName).To(Equal("User"))
		})
	})
})
