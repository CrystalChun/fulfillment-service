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
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

var _ = Describe("Users Server (Private)", func() {
	var (
		ctx           context.Context
		tx            database.Tx
		idpClient     *mockIdpClient
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
		idpClient = &mockIdpClient{
			organizations: make(map[string]*idp.Organization),
		}

		// Create server (without notifier for testing):
		privateServer, err = NewPrivateUsersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetIdpClient(idpClient).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Creates a user in both database and IdP", func() {
		// Create request:
		request := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name: "testuser",
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
		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object).ToNot(BeNil())
		Expect(response.Object.Id).ToNot(BeEmpty())
		Expect(response.Object.Username).To(Equal("testuser"))
		Expect(response.Object.Email).To(Equal("testuser@example.com"))

		// Verify password annotation was removed from response:
		Expect(response.Object.Metadata.Annotations).ToNot(HaveKey(AnnotationUserPassword))

		// Verify IdP user ID was added to annotations:
		Expect(response.Object.Metadata.Annotations).To(HaveKey("idp.osac.io/user-id"))

		// Verify user was created in IdP:
		Expect(idpClient.users).To(HaveKey("test-org"))
		Expect(idpClient.users["test-org"]).To(HaveLen(1))
		Expect(idpClient.users["test-org"][0].Username).To(Equal("testuser"))
	})

	It("Fails to create user without email", func() {
		request := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name:    "testuser",
					Tenants: []string{"test-org"},
					Annotations: map[string]string{
						AnnotationUserPassword: "SecurePass123!",
					},
				},
				Username: "testuser",
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("email is required"))
	})

	It("Fails to create user without username", func() {
		request := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name:    "testuser",
					Tenants: []string{"test-org"},
					Annotations: map[string]string{
						AnnotationUserPassword: "SecurePass123!",
					},
				},
				Email: "testuser@example.com",
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("username is required"))
	})

	It("Fails to create user without password", func() {
		request := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name:    "testuser",
					Tenants: []string{"test-org"},
				},
				Email:    "testuser@example.com",
				Username: "testuser",
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(AnnotationUserPassword))
	})

	It("Fails to create user without organization (tenant)", func() {
		request := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name: "testuser",
					Annotations: map[string]string{
						AnnotationUserPassword: "SecurePass123!",
					},
				},
				Email:    "testuser@example.com",
				Username: "testuser",
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("metadata.tenants is required"))
	})

	It("Fails to create user when IdP creation fails", func() {
		// Configure mock to fail IdP creation:
		idpClient.createOrgShouldFail = true
		idpClient.createOrgError = errors.New("IdP service unavailable")

		request := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
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

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create user in IdP"))

		// Verify user was NOT created in IdP:
		Expect(idpClient.users).ToNot(HaveKey("test-org"))

		// Verify user was NOT created in database:
		listResp, err := privateServer.List(ctx, &privatev1.UsersListRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(0)))
	})

	It("Lists users", func() {
		// Create a user first:
		createReq := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
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
		_, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// List users:
		listResp, err := privateServer.List(ctx, &privatev1.UsersListRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(1)))
		Expect(listResp.Items).To(HaveLen(1))
		Expect(listResp.Items[0].Username).To(Equal("testuser"))
	})

	It("Gets a user by ID", func() {
		// Create a user:
		createReq := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
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
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Get the user:
		getResp, err := privateServer.Get(ctx, &privatev1.UsersGetRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
		Expect(getResp.Object.Username).To(Equal("testuser"))
	})

	It("Deletes a user from both database and IdP", func() {
		// Create a user:
		createReq := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
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
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Verify it exists in IdP:
		Expect(idpClient.users).To(HaveKey("test-org"))
		Expect(idpClient.users["test-org"]).To(HaveLen(1))

		// Delete the user:
		_, err = privateServer.Delete(ctx, &privatev1.UsersDeleteRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())

		// Verify it was deleted from IdP:
		Expect(idpClient.users["test-org"]).To(HaveLen(0))
	})

	It("Fails to delete user when IdP deletion fails", func() {
		// Create a user first:
		createReq := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
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
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Configure mock to fail IdP deletion:
		idpClient.deleteOrgShouldFail = true
		idpClient.deleteOrgError = errors.New("IdP service unavailable")

		// Attempt to delete the user:
		_, err = privateServer.Delete(ctx, &privatev1.UsersDeleteRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to delete user from IdP"))

		// Verify user still exists in IdP:
		Expect(idpClient.users["test-org"]).To(HaveLen(1))

		// Verify user still exists in database:
		getResp, err := privateServer.Get(ctx, &privatev1.UsersGetRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Username).To(Equal("testuser"))
	})

	It("Updates a user in the database only", func() {
		// Create a user:
		createReq := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
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
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Update the user:
		updateReq := &privatev1.UsersUpdateRequest{
			Object: &privatev1.User{
				Id:        createResp.Object.Id,
				FirstName: "Updated",
				LastName:  "Name",
			},
		}
		updateResp, err := privateServer.Update(ctx, updateReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResp.Object.FirstName).To(Equal("Updated"))
		Expect(updateResp.Object.LastName).To(Equal("Name"))

		// Note: Update does not sync to IdP currently
	})

	It("Creates a user with temporary password", func() {
		request := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name:    "tempuser",
					Tenants: []string{"test-org"},
					Annotations: map[string]string{
						AnnotationUserPassword:          "TempPass123!",
						AnnotationUserTemporaryPassword: "true",
					},
				},
				Email:    "tempuser@example.com",
				Username: "tempuser",
				Enabled:  true,
			},
		}

		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(response.Object.Username).To(Equal("tempuser"))

		// Verify user was created in IdP with temporary password:
		Expect(idpClient.users["test-org"]).To(HaveLen(1))
		idpUser := idpClient.users["test-org"][0]
		Expect(idpUser.Credentials).To(HaveLen(1))
		Expect(idpUser.Credentials[0].Temporary).To(BeTrue())
	})
})
