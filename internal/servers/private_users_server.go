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
	"log/slog"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

// Annotation keys for user credentials
const (
	// AnnotationUserPassword is the password for the user in the IdP
	AnnotationUserPassword = "idp.osac.io/password"
	// AnnotationUserTemporaryPassword indicates whether the password is temporary and must be changed on first login
	AnnotationUserTemporaryPassword = "idp.osac.io/temporary-password"
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

	metadata := obj.GetMetadata()
	if metadata == nil {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "metadata is required")
		return
	}

	// Get organization from tenants (first tenant is the organization)
	tenants := metadata.GetTenants()
	if len(tenants) == 0 {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "metadata.tenants is required and must contain the organization name")
		return
	}
	organization := tenants[0]

	username := obj.GetUsername()
	if username == "" {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "username is required")
		return
	}

	email := obj.GetEmail()
	if email == "" {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "email is required")
		return
	}

	// Extract password from annotations
	annotations := metadata.GetAnnotations()
	password := annotations[AnnotationUserPassword]
	temporaryPassword := annotations[AnnotationUserTemporaryPassword] == "true"

	// Validate password
	if password == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument,
			"annotation '%s' is required for user creation", AnnotationUserPassword)
		return
	}

	// Create user in the IdP first
	s.logger.InfoContext(ctx, "Creating user in IdP",
		slog.String("organization", organization),
		slog.String("username", username),
		slog.String("email", email),
	)

	idpUser := &idp.User{
		Username:  username,
		Email:     email,
		Enabled:   obj.GetEnabled(),
		FirstName: obj.GetFirstName(),
		LastName:  obj.GetLastName(),
		Credentials: []*idp.Credential{
			{
				Type:      "password",
				Value:     password,
				Temporary: temporaryPassword,
			},
		},
	}

	err = s.idpClient.CreateUser(ctx, organization, idpUser)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to create user in IdP",
			slog.String("organization", organization),
			slog.String("username", username),
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal,
			"failed to create user in IdP: %v", err)
		return
	}

	s.logger.InfoContext(ctx, "User created successfully in IdP",
		slog.String("organization", organization),
		slog.String("username", username),
		slog.String("idp_user_id", idpUser.ID),
	)

	// Store IdP user ID in annotations for future reference
	if obj.Metadata.Annotations == nil {
		obj.Metadata.Annotations = make(map[string]string)
	}
	obj.Metadata.Annotations["idp.osac.io/user-id"] = idpUser.ID

	// Create user in the database
	err = s.generic.Create(ctx, request, &response)
	if err != nil {
		// IdP user was created but database insertion failed - attempt cleanup
		s.logger.ErrorContext(ctx, "Failed to create user in database, attempting IdP cleanup",
			slog.String("organization", organization),
			slog.String("username", username),
			slog.Any("error", err),
		)
		cleanupErr := s.idpClient.DeleteUser(ctx, organization, idpUser.ID)
		if cleanupErr != nil {
			s.logger.ErrorContext(ctx, "Failed to cleanup IdP user after database failure",
				slog.String("organization", organization),
				slog.String("username", username),
				slog.String("idp_user_id", idpUser.ID),
				slog.Any("error", cleanupErr),
			)
		}
		return
	}

	// Remove sensitive credentials from the response annotations
	if response.Object != nil && response.Object.Metadata != nil && response.Object.Metadata.Annotations != nil {
		delete(response.Object.Metadata.Annotations, AnnotationUserPassword)
	}

	return
}

func (s *PrivateUsersServer) Update(ctx context.Context,
	request *privatev1.UsersUpdateRequest) (response *privatev1.UsersUpdateResponse, err error) {
	// Get the existing user to find its organization
	obj := request.GetObject()
	if obj == nil {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "object is required")
		return
	}

	var getResp *privatev1.UsersGetResponse
	err = s.generic.Get(ctx, &privatev1.UsersGetRequest{Id: obj.GetId()}, &getResp)
	if err != nil {
		return
	}

	// For now, we don't sync updates to the IdP
	// You could extend this to update user properties in IdP
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateUsersServer) Delete(ctx context.Context,
	request *privatev1.UsersDeleteRequest) (response *privatev1.UsersDeleteResponse, err error) {
	// Get the user to find its organization and IdP user ID
	var getResp *privatev1.UsersGetResponse
	err = s.generic.Get(ctx, &privatev1.UsersGetRequest{Id: request.GetId()}, &getResp)
	if err != nil {
		return
	}

	user := getResp.GetObject()
	metadata := user.GetMetadata()
	tenants := metadata.GetTenants()
	if len(tenants) == 0 {
		err = grpcstatus.Error(grpccodes.Internal, "user has no organization (tenant) assigned")
		return
	}
	organization := tenants[0]

	// Get IdP user ID from annotations
	annotations := metadata.GetAnnotations()
	idpUserID := annotations["idp.osac.io/user-id"]
	if idpUserID == "" {
		err = grpcstatus.Error(grpccodes.Internal, "user has no IdP user ID in annotations")
		return
	}

	// Delete from IdP first
	s.logger.InfoContext(ctx, "Deleting user from IdP",
		slog.String("organization", organization),
		slog.String("username", user.GetUsername()),
		slog.String("idp_user_id", idpUserID),
	)

	err = s.idpClient.DeleteUser(ctx, organization, idpUserID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete user from IdP",
			slog.String("organization", organization),
			slog.String("username", user.GetUsername()),
			slog.String("idp_user_id", idpUserID),
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal,
			"failed to delete user from IdP: %v", err)
		return
	}

	s.logger.InfoContext(ctx, "User deleted successfully from IdP",
		slog.String("organization", organization),
		slog.String("username", user.GetUsername()),
	)

	// Delete from database
	err = s.generic.Delete(ctx, request, &response)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete user from database",
			slog.String("organization", organization),
			slog.String("username", user.GetUsername()),
			slog.Any("error", err),
		)
		return
	}

	return
}

func (s *PrivateUsersServer) Signal(ctx context.Context,
	request *privatev1.UsersSignalRequest) (response *privatev1.UsersSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
