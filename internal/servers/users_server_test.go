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

var _ = Describe("Users Server (Public)", func() {
	var (
		ctx          context.Context
		tx           database.Tx
		idpClient    *mockClient
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
		idpClient = &mockClient{
			organizations: make(map[string]*idp.Organization),
			users:         make(map[string][]*idp.User),
		}

		// Create public server:
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

		It("Fails if logger is missing", func() {
			_, err := NewUsersServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetIdpClient(idpClient).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
		})
	})

	Describe("Create", func() {
		It("Creates a user successfully", func() {
			request := &publicv1.UsersCreateRequest{
				Object: &publicv1.User{
					Metadata: &publicv1.Metadata{
						Name: "test-user",
					},
					Spec: &publicv1.UserSpec{
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

			response, err := publicServer.Create(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.Object.Id).ToNot(BeEmpty())
			Expect(response.Object.Spec.Username).To(Equal("testuser"))

			// Verify password was cleared
			Expect(response.Object.Spec.Password).To(BeEmpty())
		})
	})

	Describe("Get", func() {
		It("Retrieves a user", func() {
			// Create a user first
			createReq := &publicv1.UsersCreateRequest{
				Object: &publicv1.User{
					Metadata: &publicv1.Metadata{
						Name: "test-user",
					},
					Spec: &publicv1.UserSpec{
						Username:       "testuser",
						Email:          "test@example.com",
						Enabled:        true,
						OrganizationId: "test-org",
					},
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Get the user
			getReq := &publicv1.UsersGetRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			getResp, err := publicServer.Get(ctx, getReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
			Expect(getResp.Object.Spec.Username).To(Equal("testuser"))
		})
	})

	Describe("List", func() {
		It("Lists users", func() {
			// Create multiple users
			for i := 0; i < 3; i++ {
				createReq := &publicv1.UsersCreateRequest{
					Object: &publicv1.User{
						Metadata: &publicv1.Metadata{
							Name: "test-user-" + string(rune('0'+i)),
						},
						Spec: &publicv1.UserSpec{
							Username:       "testuser" + string(rune('0'+i)),
							Email:          "test" + string(rune('0'+i)) + "@example.com",
							Enabled:        true,
							OrganizationId: "test-org",
						},
					},
				}
				_, err := publicServer.Create(ctx, createReq)
				Expect(err).ToNot(HaveOccurred())
			}

			// List users
			listReq := &publicv1.UsersListRequest{
				OrganizationId: "test-org",
			}
			listResp, err := publicServer.List(ctx, listReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.Total).To(BeNumerically(">=", 3))
		})
	})

	Describe("Delete", func() {
		It("Deletes a user", func() {
			// Create a user
			createReq := &publicv1.UsersCreateRequest{
				Object: &publicv1.User{
					Metadata: &publicv1.Metadata{
						Name: "test-user",
					},
					Spec: &publicv1.UserSpec{
						Username:       "testuser",
						Email:          "test@example.com",
						Enabled:        true,
						OrganizationId: "test-org",
					},
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Delete the user
			deleteReq := &publicv1.UsersDeleteRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			_, err = publicServer.Delete(ctx, deleteReq)
			Expect(err).ToNot(HaveOccurred())

			// Verify user is deleted
			getReq := &publicv1.UsersGetRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			_, err = publicServer.Get(ctx, getReq)
			Expect(err).To(HaveOccurred())
		})
	})
})
