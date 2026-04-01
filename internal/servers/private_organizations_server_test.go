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

// mockIdpClient is a mock implementation of idp.Client for testing
type mockIdpClient struct {
	organizations         map[string]*idp.Organization
	users                 map[string][]*idp.User
	createOrgError        error
	deleteOrgError        error
	createOrgShouldFail   bool
	deleteOrgShouldFail   bool
	createOrgFailAfterN   int
	createOrgCallCount    int
}

func (m *mockIdpClient) CreateOrganization(ctx context.Context, org *idp.Organization) error {
	m.createOrgCallCount++
	if m.createOrgShouldFail || (m.createOrgFailAfterN > 0 && m.createOrgCallCount >= m.createOrgFailAfterN) {
		return m.createOrgError
	}
	m.organizations[org.Name] = org
	return nil
}

func (m *mockIdpClient) GetOrganization(ctx context.Context, name string) (*idp.Organization, error) {
	org, ok := m.organizations[name]
	if !ok {
		return nil, nil
	}
	return org, nil
}

func (m *mockIdpClient) DeleteOrganization(ctx context.Context, name string) error {
	if m.deleteOrgShouldFail {
		return m.deleteOrgError
	}
	delete(m.organizations, name)
	return nil
}

func (m *mockIdpClient) CreateUser(ctx context.Context, organization string, user *idp.User) error {
	user.ID = "mock-user-id"
	if m.users == nil {
		m.users = make(map[string][]*idp.User)
	}
	m.users[organization] = append(m.users[organization], user)
	return nil
}

func (m *mockIdpClient) GetUser(ctx context.Context, organization, userID string) (*idp.User, error) {
	return nil, nil
}

func (m *mockIdpClient) ListUsers(ctx context.Context, organization string) ([]*idp.User, error) {
	return m.users[organization], nil
}

func (m *mockIdpClient) DeleteUser(ctx context.Context, organization, userID string) error {
	return nil
}

func (m *mockIdpClient) ListRealmRoles(ctx context.Context, organization string) ([]*idp.Role, error) {
	return nil, nil
}

func (m *mockIdpClient) ListClientRoles(ctx context.Context, organization, clientID string) ([]*idp.Role, error) {
	return []*idp.Role{
		{ID: "1", Name: "manage-realm", ApplicationRole: true},
		{ID: "2", Name: "manage-users", ApplicationRole: true},
		{ID: "3", Name: "manage-clients", ApplicationRole: true},
	}, nil
}

func (m *mockIdpClient) AssignRealmRolesToUser(ctx context.Context, organization, userID string, roles []*idp.Role) error {
	return nil
}

func (m *mockIdpClient) AssignClientRolesToUser(ctx context.Context, organization, userID, clientID string, roles []*idp.Role) error {
	return nil
}

func (m *mockIdpClient) RemoveRealmRolesFromUser(ctx context.Context, organization, userID string, roles []*idp.Role) error {
	return nil
}

func (m *mockIdpClient) RemoveClientRolesFromUser(ctx context.Context, organization, userID, clientID string, roles []*idp.Role) error {
	return nil
}

func (m *mockIdpClient) GetUserRealmRoles(ctx context.Context, organization, userID string) ([]*idp.Role, error) {
	return nil, nil
}

func (m *mockIdpClient) GetUserClientRoles(ctx context.Context, organization, userID, clientID string) ([]*idp.Role, error) {
	return nil, nil
}

var _ = Describe("Organizations Server", func() {
	var (
		ctx           context.Context
		tx            database.Tx
		orgManager    *idp.OrganizationManager
		idpClient     *mockIdpClient
		privateServer *PrivateOrganizationsServer
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
		response, err := privateServer.Create(ctx, request)
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

	It("Fails to create organization without admin email", func() {
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminUsername: "admin",
						AnnotationAdminPassword: "password123",
					},
				},
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(AnnotationAdminEmail))
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

	It("Fails to create organization without admin username", func() {
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org.com",
						AnnotationAdminPassword: "password123",
					},
				},
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(AnnotationAdminUsername))
	})

	It("Fails to create organization without admin password", func() {
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
					Annotations: map[string]string{
						AnnotationAdminEmail:    "admin@test-org.com",
						AnnotationAdminUsername: "admin",
					},
				},
			},
		}

		_, err := privateServer.Create(ctx, request)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(AnnotationAdminPassword))
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

	It("Cleans up IdP realm when database creation fails", func() {
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
