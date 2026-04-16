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

var _ = Describe("Access Keys Server (Private)", func() {
	var (
		ctx           context.Context
		tx            database.Tx
		privateServer *PrivateAccessKeysServer
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

		// Create private server:
		privateServer, err = NewPrivateAccessKeysServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all required parameters are set", func() {
			srv, err := NewPrivateAccessKeysServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(srv).ToNot(BeNil())
		})

		It("Fails if logger is missing", func() {
			_, err := NewPrivateAccessKeysServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
		})
	})

	Describe("Create", func() {
		It("Creates an access key successfully", func() {
			request := &privatev1.AccessKeysCreateRequest{
				Object: &privatev1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-access-key",
					},
					Spec: &privatev1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
						Enabled:        true,
					},
				},
			}

			response, err := privateServer.Create(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.Object.Id).ToNot(BeEmpty())

			// Verify credentials are returned
			Expect(response.Credentials).ToNot(BeNil())
			Expect(response.Credentials.AccessKeyId).To(HavePrefix(AccessKeyIDPrefix))
			Expect(response.Credentials.SecretAccessKey).ToNot(BeEmpty())
			Expect(len(response.Credentials.SecretAccessKey)).To(Equal(40))

			// Verify credentials are stored in annotations
			annotations := response.Object.Metadata.Annotations
			Expect(annotations).ToNot(BeNil())
			Expect(annotations[AnnotationAccessKeyID]).To(Equal(response.Credentials.AccessKeyId))
			Expect(annotations[AnnotationSecretHash]).ToNot(BeEmpty())

			// Verify secret hash is valid
			hash := annotations[AnnotationSecretHash]
			isValid := VerifySecretAccessKey(response.Credentials.SecretAccessKey, hash)
			Expect(isValid).To(BeTrue())
		})

		It("Fails if user_id is missing", func() {
			request := &privatev1.AccessKeysCreateRequest{
				Object: &privatev1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-access-key",
					},
					Spec: &privatev1.AccessKeySpec{
						OrganizationId: "test-org",
					},
				},
			}

			_, err := privateServer.Create(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("user_id"))
		})

		It("Fails if organization_id is missing", func() {
			request := &privatev1.AccessKeysCreateRequest{
				Object: &privatev1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-access-key",
					},
					Spec: &privatev1.AccessKeySpec{
						UserId: "user-123",
					},
				},
			}

			_, err := privateServer.Create(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("organization_id"))
		})

		It("Generates unique credentials for each key", func() {
			// Create first key
			request1 := &privatev1.AccessKeysCreateRequest{
				Object: &privatev1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-key-1",
					},
					Spec: &privatev1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
					},
				},
			}
			response1, err := privateServer.Create(ctx, request1)
			Expect(err).ToNot(HaveOccurred())

			// Create second key
			request2 := &privatev1.AccessKeysCreateRequest{
				Object: &privatev1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-key-2",
					},
					Spec: &privatev1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
					},
				},
			}
			response2, err := privateServer.Create(ctx, request2)
			Expect(err).ToNot(HaveOccurred())

			// Verify credentials are different
			Expect(response1.Credentials.AccessKeyId).ToNot(Equal(response2.Credentials.AccessKeyId))
			Expect(response1.Credentials.SecretAccessKey).ToNot(Equal(response2.Credentials.SecretAccessKey))
		})
	})

	Describe("Get", func() {
		It("Retrieves an access key", func() {
			// Create an access key first
			createReq := &privatev1.AccessKeysCreateRequest{
				Object: &privatev1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-key",
					},
					Spec: &privatev1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
					},
				},
			}
			createResp, err := privateServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Get the access key
			getReq := &privatev1.AccessKeysGetRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			getResp, err := privateServer.Get(ctx, getReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))

			// Verify secret is NOT returned on Get
			// (it's only in annotations as a hash)
			annotations := getResp.Object.Metadata.Annotations
			Expect(annotations[AnnotationSecretHash]).ToNot(BeEmpty())
		})
	})

	Describe("List", func() {
		It("Lists access keys", func() {
			// Create multiple access keys
			for i := 0; i < 3; i++ {
				createReq := &privatev1.AccessKeysCreateRequest{
					Object: &privatev1.AccessKey{
						Metadata: &publicv1.Metadata{
							Name: "test-key-" + string(rune('0'+i)),
						},
						Spec: &privatev1.AccessKeySpec{
							UserId:         "user-123",
							OrganizationId: "test-org",
						},
					},
				}
				_, err := privateServer.Create(ctx, createReq)
				Expect(err).ToNot(HaveOccurred())
			}

			// List access keys
			listReq := &privatev1.AccessKeysListRequest{
				UserId:         "user-123",
				OrganizationId: "test-org",
			}
			listResp, err := privateServer.List(ctx, listReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.Total).To(BeNumerically(">=", 3))
		})
	})

	Describe("Disable", func() {
		It("Disables an access key", func() {
			// Create an access key
			createReq := &privatev1.AccessKeysCreateRequest{
				Object: &privatev1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-key",
					},
					Spec: &privatev1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
						Enabled:        true,
					},
				},
			}
			createResp, err := privateServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(createResp.Object.Spec.Enabled).To(BeTrue())

			// Disable the key
			disableReq := &privatev1.AccessKeysDisableRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			disableResp, err := privateServer.Disable(ctx, disableReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(disableResp.Object.Spec.Enabled).To(BeFalse())
		})
	})

	Describe("Enable", func() {
		It("Enables a disabled access key", func() {
			// Create an access key
			createReq := &privatev1.AccessKeysCreateRequest{
				Object: &privatev1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-key",
					},
					Spec: &privatev1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
					},
				},
			}
			createResp, err := privateServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Disable it first
			disableReq := &privatev1.AccessKeysDisableRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			_, err = privateServer.Disable(ctx, disableReq)
			Expect(err).ToNot(HaveOccurred())

			// Enable it
			enableReq := &privatev1.AccessKeysEnableRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			enableResp, err := privateServer.Enable(ctx, enableReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(enableResp.Object.Spec.Enabled).To(BeTrue())
		})
	})

	Describe("Delete", func() {
		It("Deletes an access key", func() {
			// Create an access key
			createReq := &privatev1.AccessKeysCreateRequest{
				Object: &privatev1.AccessKey{
					Metadata: &publicv1.Metadata{
						Name: "test-key",
					},
					Spec: &privatev1.AccessKeySpec{
						UserId:         "user-123",
						OrganizationId: "test-org",
					},
				},
			}
			createResp, err := privateServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())

			// Delete the key
			deleteReq := &privatev1.AccessKeysDeleteRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			_, err = privateServer.Delete(ctx, deleteReq)
			Expect(err).ToNot(HaveOccurred())

			// Verify key is deleted
			getReq := &privatev1.AccessKeysGetRequest{
				Id:             createResp.Object.Id,
				OrganizationId: "test-org",
			}
			_, err = privateServer.Get(ctx, getReq)
			Expect(err).To(HaveOccurred())
		})
	})
})
