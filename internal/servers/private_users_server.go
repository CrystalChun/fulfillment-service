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
	"errors"
	"log/slog"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

type PrivateUsersServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
	idpClient        idp.Client
}

var _ privatev1.UsersServer = (*PrivateUsersServer)(nil)

type PrivateUsersServer struct {
	privatev1.UnimplementedUsersServer
	logger    *slog.Logger
	idpClient idp.Client
	generic   *GenericServer[*privatev1.User]
}

func NewPrivateUsersServer() *PrivateUsersServerBuilder {
	return &PrivateUsersServerBuilder{}
}

func (b *PrivateUsersServerBuilder) SetLogger(value *slog.Logger) *PrivateUsersServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateUsersServerBuilder) SetNotifier(value *database.Notifier) *PrivateUsersServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateUsersServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateUsersServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateUsersServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateUsersServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateUsersServerBuilder) SetIdpClient(value idp.Client) *PrivateUsersServerBuilder {
	b.idpClient = value
	return b
}

func (b *PrivateUsersServerBuilder) Build() (result *PrivateUsersServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}
	if b.idpClient == nil {
		err = errors.New("idp client is mandatory")
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.User]().
		SetLogger(b.logger).
		SetService(privatev1.Users_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateUsersServer{
		logger:    b.logger,
		idpClient: b.idpClient,
		generic:   generic,
	}
	return
}

func (s *PrivateUsersServer) List(ctx context.Context,
	request *privatev1.UsersListRequest) (response *privatev1.UsersListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateUsersServer) Get(ctx context.Context,
	request *privatev1.UsersGetRequest) (response *privatev1.UsersGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateUsersServer) Create(ctx context.Context,
	request *privatev1.UsersCreateRequest) (response *privatev1.UsersCreateResponse, err error) {
	// Validate that required fields are present
	obj := request.GetObject()
	if obj == nil {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "object is required")
		return
	}

	spec := obj.GetSpec()
	if spec == nil {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "spec is required")
		return
	}

	if spec.GetUsername() == "" {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "spec.username is required")
		return
	}

	if spec.GetOrganizationId() == "" {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "spec.organization_id is required")
		return
	}

	// Create user in the IdP first
	s.logger.InfoContext(ctx, "Creating user in IdP",
		slog.String("username", spec.GetUsername()),
		slog.String("organization", spec.GetOrganizationId()),
	)

	// Build IdP user object
	idpUser := &idp.User{
		Username:      spec.GetUsername(),
		Email:         spec.GetEmail(),
		EmailVerified: spec.GetEmailVerified(),
		Enabled:       spec.GetEnabled(),
		FirstName:     spec.GetFirstName(),
		LastName:      spec.GetLastName(),
	}

	// Add password if provided
	if spec.GetPassword() != "" {
		idpUser.Credentials = []*idp.Credential{
			{
				Type:      "password",
				Value:     spec.GetPassword(),
				Temporary: spec.GetTemporaryPassword(),
			},
		}
	}

	// Create user in IdP
	createdUser, err := s.idpClient.CreateUser(ctx, spec.GetOrganizationId(), idpUser)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to create user in IdP",
			slog.String("username", spec.GetUsername()),
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to create user in IdP: %v", err)
		return
	}

	s.logger.InfoContext(ctx, "User created successfully in IdP",
		slog.String("username", spec.GetUsername()),
		slog.String("user_id", createdUser.ID),
	)

	// Set the IdP user ID in the object
	obj.Id = createdUser.ID

	// Clear the password from spec before storing in database
	spec.Password = ""

	// Create user in database using generic server
	err = s.generic.Create(ctx, request, &response)
	if err != nil {
		// Attempt to clean up the IdP user if database creation fails
		deleteErr := s.idpClient.DeleteUser(ctx, spec.GetOrganizationId(), createdUser.ID)
		if deleteErr != nil {
			s.logger.ErrorContext(ctx, "Failed to clean up IdP user after database error",
				slog.String("user_id", createdUser.ID),
				slog.Any("error", deleteErr),
			)
		}
		return
	}

	return
}

func (s *PrivateUsersServer) Update(ctx context.Context,
	request *privatev1.UsersUpdateRequest) (response *privatev1.UsersUpdateResponse, err error) {
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateUsersServer) Delete(ctx context.Context,
	request *privatev1.UsersDeleteRequest) (response *privatev1.UsersDeleteResponse, err error) {
	// Get the user first to get the organization ID
	getReq := &privatev1.UsersGetRequest{
		Id:             request.GetId(),
		OrganizationId: request.GetOrganizationId(),
	}
	getResp, err := s.Get(ctx, getReq)
	if err != nil {
		return
	}

	user := getResp.GetObject()
	if user == nil {
		err = grpcstatus.Error(grpccodes.NotFound, "user not found")
		return
	}

	// Delete from IdP first
	s.logger.InfoContext(ctx, "Deleting user from IdP",
		slog.String("user_id", request.GetId()),
		slog.String("organization", request.GetOrganizationId()),
	)

	err = s.idpClient.DeleteUser(ctx, request.GetOrganizationId(), request.GetId())
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete user from IdP",
			slog.String("user_id", request.GetId()),
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to delete user from IdP: %v", err)
		return
	}

	// Delete from database
	err = s.generic.Delete(ctx, request, &response)
	return
}
