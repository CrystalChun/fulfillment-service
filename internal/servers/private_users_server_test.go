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

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

var _ = Describe("Users Server (Private)", func() {
	var (
		ctx           context.Context
		tx            database.Tx
		idpClient     *mockClient
		privateServer *PrivateUsersServer
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
		idpClient = &mockClient{
			organizations: make(map[string]*idp.Organization),
			users:         make(map[string][]*idp.User),
		}

		// Create private server:
		privateServer, err = NewPrivateUsersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetIdpClient(idpClient).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all required parameters are set", func() {
			srv, err := NewPrivateUsersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetIdpClient(idpClient).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(srv).ToNot(BeNil())
		})

		It("Fails if logger is missing", func() {
			_, err := NewPrivateUsersServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetIdpClient(idpClient).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
		})

		It("Fails if IdP client is missing", func() {
			_, err := NewPrivateUsersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("idp client"))
		})
	})

	Describe("Create", func() {
		It("Creates a user successfully", func() {
			request := &privatev1.UsersCreateRequest{
				Object: &privatev1.User{
					Metadata: &publicv1.Metadata{
						Name: "test-user",
					},
					Spec: &privatev1.UserSpec{
						Username:       "testuser",
						Email:          "test@example.com",
						EmailVerified:  true,
						Enabled:        true,
						FirstName:      "Test",
						LastName:       "User",
						OrganizationId: "test-org",
						Password:       "InitialPassword123!",
					},
				},
			}

			response, err := privateServer.Create(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.Object.Id).ToNot(BeEmpty())
			Expect(response.Object.Spec.Username).To(Equal("testuser"))

			// Verify password was cleared
			Expect(response.Object.Spec.Password).To(BeEmpty())

			// Verify user was created in IdP
			idpUsers := idpClient.users["test-org"]
			Expect(idpUsers).To(HaveLen(1))
			Expect(idpUsers[0].Username).To(Equal("testuser"))
		})

		It("Fails if username is missing", func() {
			request := &privatev1.UsersCreateRequest{
				Object: &privatev1.User{
					Metadata: &publicv1.Metadata{
						Name: "test-user",
					},
					Spec: &privatev1.UserSpec{
						OrganizationId: "test-org",
					},
				},
			}

			_, err := privateServer.Create(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("username"))
		})

		It("Fails if organization_id is missing", func() {
			request := &privatev1.UsersCreateRequest{
				Object: &privatev1.User{
					Metadata: &publicv1.Metadata{
						Name: "test-user",
					},
					Spec: &privatev1.UserSpec{
						Username: "testuser",
					},
				},
			}

			_, err := privateServer.Create(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("organization_id"))
		})
	})

	Describe("Get", func() {
		It("Retrieves a user", func() {
			// Create a user first
			createReq := &privatev1.UsersCreateRequest{
				Object: &privatev1.User{
					Metadata: &publicv1.Metadata{
						Name: "test-user",
					},
					Spec: &privatev1.UserSpec{
						Username:       "testuser",
						Email:          "test@example.com",
						Enabled:        true,
						OrganizationId: "test-org",
					},
				},
			}
			createResp, err := privateServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Get the user
			getReq := &privatev1.UsersGetRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			getResp, err := privateServer.Get(ctx, getReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
			Expect(getResp.Object.Spec.Username).To(Equal("testuser"))
		})
	})

	Describe("List", func() {
		It("Lists users", func() {
			// Create multiple users
			for i := 0; i < 3; i++ {
				createReq := &privatev1.UsersCreateRequest{
					Object: &privatev1.User{
						Metadata: &publicv1.Metadata{
							Name: "test-user-" + string(rune('0'+i)),
						},
						Spec: &privatev1.UserSpec{
							Username:       "testuser" + string(rune('0'+i)),
							Email:          "test" + string(rune('0'+i)) + "@example.com",
							Enabled:        true,
							OrganizationId: "test-org",
						},
					},
				}
				_, err := privateServer.Create(ctx, createReq)
				Expect(err).ToNot(HaveOccurred())
			}

			// List users
			listReq := &privatev1.UsersListRequest{
				OrganizationId: "test-org",
			}
			listResp, err := privateServer.List(ctx, listReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.Total).To(BeNumerically(">=", 3))
		})
	})

	Describe("Delete", func() {
		It("Deletes a user", func() {
			// Create a user
			createReq := &privatev1.UsersCreateRequest{
				Object: &privatev1.User{
					Metadata: &publicv1.Metadata{
						Name: "test-user",
					},
					Spec: &privatev1.UserSpec{
						Username:       "testuser",
						Email:          "test@example.com",
						Enabled:        true,
						OrganizationId: "test-org",
					},
				},
			}
			createResp, err := privateServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Delete the user
			deleteReq := &privatev1.UsersDeleteRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			_, err = privateServer.Delete(ctx, deleteReq)
			Expect(err).ToNot(HaveOccurred())

			// Verify user is deleted
			getReq := &privatev1.UsersGetRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			_, err = privateServer.Get(ctx, getReq)
			Expect(err).To(HaveOccurred())
		})
	})
})
