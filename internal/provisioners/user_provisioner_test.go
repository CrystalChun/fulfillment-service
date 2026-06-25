/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package provisioners

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/logging"
)

func TestUserProvisioner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "User Provisioner Suite")
}

var (
	ctx      context.Context
	ctrl     *gomock.Controller
	logger   *slog.Logger
	server   *database.Container
	usersDAO *dao.GenericDAO[*privatev1.User]
	tenancy  *auth.MockTenancyLogic
	prov     *UserProvisioner
)

var _ = BeforeSuite(func() {
	var err error

	// Create the mock controller:
	ctrl = gomock.NewController(GinkgoT())
	DeferCleanup(ctrl.Finish)

	// Create logger:
	logger, err = logging.NewLogger().
		SetLevel(slog.LevelDebug.String()).
		SetWriter(GinkgoWriter).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Create the tenancy logic:
	tenancy = auth.NewMockTenancyLogic(ctrl)
	tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
		Return(auth.AllTenants, nil).
		AnyTimes()
	tenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
		Return(auth.SystemTenant, nil).
		AnyTimes()
	tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
		Return(auth.AllTenants, nil).
		AnyTimes()

	// Create the database server:
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	DeferCleanup(cancel)
	server, err = database.NewContainer().
		SetLogger(logger).
		Build()
	Expect(err).ToNot(HaveOccurred())
	err = server.Start(ctx)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err = server.Stop(ctx)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = BeforeEach(func() {
	var err error

	// Create a context:
	ctx = context.Background()

	// Prepare the database pool:
	db, err := server.NewInstance().Build()
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(db.Close)
	pool, err := db.Pool(ctx)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(pool.Close)

	// Create the transaction manager:
	tm, err := database.NewTxManager().
		SetLogger(logger).
		SetPool(pool).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Start a transaction and add it to the context:
	tx, err := tm.Begin(ctx)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		err := tx.End(ctx)
		Expect(err).ToNot(HaveOccurred())
	})
	ctx = database.TxIntoContext(ctx, tx)

	// Create users DAO:
	usersDAO, err = dao.NewGenericDAO[*privatev1.User]().
		SetLogger(logger).
		SetTenancyLogic(tenancy).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Create provisioner:
	prov, err = NewUserProvisioner().
		SetUsersDAO(usersDAO).
		Build()
	Expect(err).ToNot(HaveOccurred())
})

var _ = Describe("UserProvisioner", func() {

	Describe("Builder validation", func() {
		It("Requires users DAO", func() {
			_, err := NewUserProvisioner().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("users DAO is mandatory"))
		})

		It("Builds successfully with all required parameters", func() {
			p, err := NewUserProvisioner().
				SetUsersDAO(usersDAO).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(p).ToNot(BeNil())
		})
	})

	Describe("Provision", func() {
		It("Creates a new user with all fields populated", func() {
			claims := jwt.MapClaims{
				"email":          "alice@example.com",
				"email_verified": true,
				"name":           "Alice Smith",
			}

			err := prov.Provision(ctx, "alice", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			// Verify user was created:
			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='alice'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetMetadata().GetName()).To(Equal("alice"))
			Expect(user.GetMetadata().GetTenant()).To(Equal(auth.SystemTenant))
			Expect(user.GetSpec().GetUsername()).To(Equal("alice"))
			Expect(user.GetSpec().GetEmail()).To(Equal("alice@example.com"))
			Expect(user.GetSpec().GetEmailVerified()).To(BeTrue())
			Expect(user.GetSpec().GetFirstName()).To(Equal("Alice"))
			Expect(user.GetSpec().GetLastName()).To(Equal("Smith"))
			Expect(user.GetSpec().GetEnabled()).To(BeTrue())
		})

		It("Creates a user with minimal claims", func() {
			claims := jwt.MapClaims{}

			err := prov.Provision(ctx, "bob", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			// Verify user was created:
			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='bob'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetMetadata().GetName()).To(Equal("bob"))
			Expect(user.GetMetadata().GetTenant()).To(Equal(auth.SystemTenant))
			Expect(user.GetSpec().GetUsername()).To(Equal("bob"))
			Expect(user.GetSpec().GetEmail()).To(Equal(""))
			Expect(user.GetSpec().GetEmailVerified()).To(BeFalse())
			Expect(user.GetSpec().GetFirstName()).To(Equal(""))
			Expect(user.GetSpec().GetLastName()).To(Equal(""))
			Expect(user.GetSpec().GetEnabled()).To(BeTrue())
		})

		It("Parses first name only from name claim", func() {
			claims := jwt.MapClaims{
				"name": "Charlie",
			}

			err := prov.Provision(ctx, "charlie", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='charlie'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetSpec().GetFirstName()).To(Equal("Charlie"))
			Expect(user.GetSpec().GetLastName()).To(Equal(""))
		})

		It("Parses multi-word last name from name claim", func() {
			claims := jwt.MapClaims{
				"name": "David Van Der Berg",
			}

			err := prov.Provision(ctx, "david", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='david'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetSpec().GetFirstName()).To(Equal("David"))
			Expect(user.GetSpec().GetLastName()).To(Equal("Van Der Berg"))
		})

		It("Does not create duplicate user if user already exists", func() {
			claims := jwt.MapClaims{
				"email": "eve@example.com",
				"name":  "Eve Johnson",
			}

			// Create user first time:
			err := prov.Provision(ctx, "eve", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			// Try to create same user again:
			err = prov.Provision(ctx, "eve", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			// Verify only one user exists:
			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='eve'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))
		})

		It("Handles unverified email", func() {
			claims := jwt.MapClaims{
				"email":          "frank@example.com",
				"email_verified": false,
			}

			err := prov.Provision(ctx, "frank", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='frank'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetSpec().GetEmail()).To(Equal("frank@example.com"))
			Expect(user.GetSpec().GetEmailVerified()).To(BeFalse())
		})

		It("Handles email_verified as non-boolean gracefully", func() {
			claims := jwt.MapClaims{
				"email":          "grace@example.com",
				"email_verified": "true", // String instead of boolean
			}

			err := prov.Provision(ctx, "grace", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='grace'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetSpec().GetEmail()).To(Equal("grace@example.com"))
			Expect(user.GetSpec().GetEmailVerified()).To(BeFalse()) // Defaults to false for non-boolean
		})

		It("Handles email claim as non-string gracefully", func() {
			claims := jwt.MapClaims{
				"email": 12345, // Number instead of string
			}

			err := prov.Provision(ctx, "harry", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='harry'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetSpec().GetEmail()).To(Equal("")) // Defaults to empty string for non-string
		})

		It("Handles name claim as non-string gracefully", func() {
			claims := jwt.MapClaims{
				"name": 12345, // Number instead of string
			}

			err := prov.Provision(ctx, "iris", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='iris'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetSpec().GetFirstName()).To(Equal(""))
			Expect(user.GetSpec().GetLastName()).To(Equal(""))
		})

		It("Handles empty name claim", func() {
			claims := jwt.MapClaims{
				"name": "",
			}

			err := prov.Provision(ctx, "judy", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='judy'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetSpec().GetFirstName()).To(Equal(""))
			Expect(user.GetSpec().GetLastName()).To(Equal(""))
		})

		It("Trims whitespace from name parts", func() {
			claims := jwt.MapClaims{
				"name": "  Karl   Lee  ",
			}

			err := prov.Provision(ctx, "karl", auth.SystemTenant, claims)
			Expect(err).ToNot(HaveOccurred())

			listResp, err := usersDAO.List().
				SetFilter("this.spec.username=='karl'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(listResp.GetSize()).To(Equal(int32(1)))

			user := listResp.GetItems()[0]
			Expect(user.GetSpec().GetFirstName()).To(Equal("Karl"))
			Expect(user.GetSpec().GetLastName()).To(Equal("Lee"))
		})
	})
})
