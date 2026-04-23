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
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Access Keys Server", func() {
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

		// Create server (without notifier for testing):
		privateServer, err = NewPrivateAccessKeysServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Creates an access key", func() {
		// Create request:
		request := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}

		// Create access key:
		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object).ToNot(BeNil())
		Expect(response.Object.Id).ToNot(BeEmpty())
		Expect(response.Object.Metadata.Name).To(Equal("test-access-key"))
		Expect(response.Object.Spec.Enabled).To(BeTrue())
		Expect(response.Object.Spec.AccessKeyId).ToNot(BeEmpty())

		// Verify the secret hash is NOT returned in the response (security requirement)
		Expect(response.Object.Spec.SecretHash).To(BeEmpty())

		// Verify credentials are returned
		Expect(response.Credentials).ToNot(BeNil())
		Expect(response.Credentials.AccessKeyId).ToNot(BeEmpty())
		Expect(response.Credentials.SecretAccessKey).ToNot(BeEmpty())
	})

	It("Rejects create with nil object", func() {
		request := &privatev1.AccessKeysCreateRequest{
			Object: nil,
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("object is required"))
	})

	It("Rejects create with missing user_id", func() {
		request := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					OrganizationId: "org-123",
				},
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.user_id is required"))
	})

	It("Rejects create with missing organization_id", func() {
		request := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId: "user-123",
				},
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.organization_id is required"))
	})

	It("Lists access keys", func() {
		// Create an access key first:
		createReq := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}
		_, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// List access keys:
		listResp, err := privateServer.List(ctx, &privatev1.AccessKeysListRequest{
			UserId:         "user-123",
			OrganizationId: "org-123",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(1)))
		Expect(listResp.Items).To(HaveLen(1))
		Expect(listResp.Items[0].Metadata.Name).To(Equal("test-access-key"))
	})

	It("Gets an access key by ID", func() {
		// Create an access key:
		createReq := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Get the access key:
		getResp, err := privateServer.Get(ctx, &privatev1.AccessKeysGetRequest{
			Id:             createResp.Object.Id,
			UserId:         "user-123",
			OrganizationId: "org-123",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
		Expect(getResp.Object.Metadata.Name).To(Equal("test-access-key"))
	})

	It("Disables an access key", func() {
		// Create an access key:
		createReq := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(createResp.Object.Spec.Enabled).To(BeTrue())

		// Disable the access key:
		disableResp, err := privateServer.Disable(ctx, &privatev1.AccessKeysDisableRequest{
			Id:             createResp.Object.Id,
			UserId:         "user-123",
			OrganizationId: "org-123",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(disableResp.Object.Spec.Enabled).To(BeFalse())
	})

	It("Enables an access key", func() {
		// Create and disable an access key:
		createReq := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		_, err = privateServer.Disable(ctx, &privatev1.AccessKeysDisableRequest{
			Id:             createResp.Object.Id,
			UserId:         "user-123",
			OrganizationId: "org-123",
		})
		Expect(err).ToNot(HaveOccurred())

		// Enable the access key:
		enableResp, err := privateServer.Enable(ctx, &privatev1.AccessKeysEnableRequest{
			Id:             createResp.Object.Id,
			UserId:         "user-123",
			OrganizationId: "org-123",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(enableResp.Object.Spec.Enabled).To(BeTrue())
	})

	It("Deletes an access key", func() {
		// Create an access key:
		createReq := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Delete the access key:
		_, err = privateServer.Delete(ctx, &privatev1.AccessKeysDeleteRequest{
			Id:             createResp.Object.Id,
			UserId:         "user-123",
			OrganizationId: "org-123",
		})
		Expect(err).ToNot(HaveOccurred())
	})

	It("Updates an access key", func() {
		// Create an access key:
		createReq := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Update the access key:
		createResp.Object.Metadata.Name = "updated-access-key"
		updateReq := &privatev1.AccessKeysUpdateRequest{
			Object: createResp.Object,
		}
		updateResp, err := privateServer.Update(ctx, updateReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResp.Object.Metadata.Name).To(Equal("updated-access-key"))
	})

	It("Preserves immutable fields when updating without a mask", func() {
		// Create an access key:
		createReq := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		originalAccessKeyID := createResp.Object.Spec.AccessKeyId
		// Note: SecretHash is not returned in the response, so we can't check it directly

		// Attempt to update with different access_key_id and secret_hash (no mask)
		createResp.Object.Metadata.Name = "updated-name"
		createResp.Object.Spec.AccessKeyId = "different-id"
		createResp.Object.Spec.SecretHash = "different-hash"
		updateReq := &privatev1.AccessKeysUpdateRequest{
			Object: createResp.Object,
		}
		updateResp, err := privateServer.Update(ctx, updateReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResp.Object.Metadata.Name).To(Equal("updated-name"))

		// Verify the access_key_id was preserved
		Expect(updateResp.Object.Spec.AccessKeyId).To(Equal(originalAccessKeyID))
		// SecretHash is not returned in responses, so we can't verify it here,
		// but the Update method guards against modification
	})

	It("Rejects updates with update_mask containing spec.access_key_id", func() {
		// Create an access key:
		createReq := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Attempt to update with a mask that includes spec.access_key_id
		createResp.Object.Spec.AccessKeyId = "different-id"
		updateReq := &privatev1.AccessKeysUpdateRequest{
			Object: createResp.Object,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"spec.access_key_id"},
			},
		}
		_, err = privateServer.Update(ctx, updateReq)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("immutable"))
	})

	It("Rejects updates with update_mask containing spec.secret_hash", func() {
		// Create an access key:
		createReq := &privatev1.AccessKeysCreateRequest{
			Object: &privatev1.AccessKey{
				Metadata: &privatev1.Metadata{
					Name: "test-access-key",
				},
				Spec: &privatev1.AccessKeySpec{
					UserId:         "user-123",
					OrganizationId: "org-123",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Attempt to update with a mask that includes spec.secret_hash
		createResp.Object.Spec.SecretHash = "different-hash"
		updateReq := &privatev1.AccessKeysUpdateRequest{
			Object: createResp.Object,
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{"spec.secret_hash"},
			},
		}
		_, err = privateServer.Update(ctx, updateReq)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("immutable"))
	})
})
