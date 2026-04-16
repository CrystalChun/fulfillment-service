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
)

var _ = Describe("Access Keys Server (Public)", func() {
	var (
		ctx          context.Context
		tx           database.Tx
		publicServer *AccessKeysServer
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
		err = dao.CreateTables[*privatev1.AccessKey](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create public server:
		publicServer, err = NewAccessKeysServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all required parameters are set", func() {
			srv, err := NewAccessKeysServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(srv).ToNot(BeNil())
		})

		It("Fails if logger is missing", func() {
			_, err := NewAccessKeysServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
		})
	})

	Describe("Create", func() {
		It("Creates an access key successfully", func() {
			request := &publicv1.AccessKeysCreateRequest{
				Object: &publicv1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-access-key",
					},
					Spec: &publicv1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
						Enabled:        true,
					},
				},
			}

			response, err := publicServer.Create(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.Object.Id).ToNot(BeEmpty())

			// Verify credentials are returned
			Expect(response.Credentials).ToNot(BeNil())
			Expect(response.Credentials.AccessKeyId).To(HavePrefix(AccessKeyIDPrefix))
			Expect(response.Credentials.SecretAccessKey).ToNot(BeEmpty())
		})
	})

	Describe("Get", func() {
		It("Retrieves an access key", func() {
			// Create an access key first
			createReq := &publicv1.AccessKeysCreateRequest{
				Object: &publicv1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-key",
					},
					Spec: &publicv1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
					},
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Get the access key
			getReq := &publicv1.AccessKeysGetRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			getResp, err := publicServer.Get(ctx, getReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
		})
	})

	Describe("Disable and Enable", func() {
		It("Disables and re-enables an access key", func() {
			// Create an access key
			createReq := &publicv1.AccessKeysCreateRequest{
				Object: &publicv1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-key",
					},
					Spec: &publicv1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
						Enabled:        true,
					},
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(createResp.Object.Spec.Enabled).To(BeTrue())

			// Disable the key
			disableReq := &publicv1.AccessKeysDisableRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			disableResp, err := publicServer.Disable(ctx, disableReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(disableResp.Object.Spec.Enabled).To(BeFalse())

			// Re-enable the key
			enableReq := &publicv1.AccessKeysEnableRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			enableResp, err := publicServer.Enable(ctx, enableReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(enableResp.Object.Spec.Enabled).To(BeTrue())
		})
	})

	Describe("Delete", func() {
		It("Deletes an access key", func() {
			// Create an access key
			createReq := &publicv1.AccessKeysCreateRequest{
				Object: &publicv1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-key",
					},
					Spec: &publicv1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
					},
				},
			}
			createResp, err := publicServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Delete the key
			deleteReq := &publicv1.AccessKeysDeleteRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			_, err = publicServer.Delete(ctx, deleteReq)
			Expect(err).ToNot(HaveOccurred())

			// Verify key is deleted
			getReq := &publicv1.AccessKeysGetRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			_, err = publicServer.Get(ctx, getReq)
			Expect(err).To(HaveOccurred())
		})
	})
})
