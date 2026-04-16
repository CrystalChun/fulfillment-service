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
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

// Annotation keys for access key metadata
const (
	// AnnotationAccessKeyID stores the public access key identifier
	AnnotationAccessKeyID = "access-key.osac.io/key-id"
	// AnnotationSecretHash stores the bcrypt hash of the secret
	AnnotationSecretHash = "access-key.osac.io/secret-hash"
)

type PrivateAccessKeysServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ privatev1.AccessKeysServer = (*PrivateAccessKeysServer)(nil)

type PrivateAccessKeysServer struct {
	privatev1.UnimplementedAccessKeysServer
	logger  *slog.Logger
	generic *GenericServer[*privatev1.AccessKey]
}

func NewPrivateAccessKeysServer() *PrivateAccessKeysServerBuilder {
	return &PrivateAccessKeysServerBuilder{}
}

func (b *PrivateAccessKeysServerBuilder) SetLogger(value *slog.Logger) *PrivateAccessKeysServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateAccessKeysServerBuilder) SetNotifier(value *database.Notifier) *PrivateAccessKeysServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateAccessKeysServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateAccessKeysServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateAccessKeysServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateAccessKeysServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateAccessKeysServerBuilder) Build() (result *PrivateAccessKeysServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.AccessKey]().
		SetLogger(b.logger).
		SetService(privatev1.AccessKeys_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateAccessKeysServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateAccessKeysServer) List(ctx context.Context,
	request *privatev1.AccessKeysListRequest) (response *privatev1.AccessKeysListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateAccessKeysServer) Get(ctx context.Context,
	request *privatev1.AccessKeysGetRequest) (response *privatev1.AccessKeysGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateAccessKeysServer) Create(ctx context.Context,
	request *privatev1.AccessKeysCreateRequest) (response *privatev1.AccessKeysCreateResponse, err error) {
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

	if spec.GetUserId() == "" {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "spec.user_id is required")
		return
	}

	if spec.GetOrganizationId() == "" {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "spec.organization_id is required")
		return
	}

	// Generate access key credentials
	s.logger.InfoContext(ctx, "Generating access key credentials",
		slog.String("user_id", spec.GetUserId()),
		slog.String("organization", spec.GetOrganizationId()),
	)

	accessKeyID, secretAccessKey, secretHash, err := GenerateAccessKeyCredentials()
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to generate access key credentials",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to generate credentials: %v", err)
		return
	}

	// Store the access key ID and secret hash in annotations
	metadata := obj.GetMetadata()
	if metadata == nil {
		obj.Metadata = &publicv1.Metadata{}
		metadata = obj.Metadata
	}
	if metadata.Annotations == nil {
		metadata.Annotations = make(map[string]string)
	}
	metadata.Annotations[AnnotationAccessKeyID] = accessKeyID
	metadata.Annotations[AnnotationSecretHash] = secretHash

	// Set enabled to true by default
	if spec.Enabled == false {
		spec.Enabled = true
	}

	s.logger.InfoContext(ctx, "Access key credentials generated",
		slog.String("access_key_id", accessKeyID),
		slog.String("user_id", spec.GetUserId()),
	)

	// Create access key in database using generic server
	err = s.generic.Create(ctx, request, &response)
	if err != nil {
		return
	}

	// Add the credentials to the response
	// NOTE: The secret is only returned once at creation time!
	response.Credentials = &privatev1.AccessKeyCredentials{
		AccessKeyId:     accessKeyID,
		SecretAccessKey: secretAccessKey,
	}

	return
}

func (s *PrivateAccessKeysServer) Disable(ctx context.Context,
	request *privatev1.AccessKeysDisableRequest) (response *privatev1.AccessKeysDisableResponse, err error) {
	// Get the access key first
	getReq := &privatev1.AccessKeysGetRequest{
		Id:             request.GetId(),
		OrganizationId: request.GetOrganizationId(),
	}
	getResp, err := s.Get(ctx, getReq)
	if err != nil {
		return
	}

	accessKey := getResp.GetObject()
	if accessKey == nil {
		err = grpcstatus.Error(grpccodes.NotFound, "access key not found")
		return
	}

	// Set enabled to false
	spec := accessKey.GetSpec()
	if spec == nil {
		err = grpcstatus.Error(grpccodes.Internal, "access key spec is nil")
		return
	}
	spec.Enabled = false

	s.logger.InfoContext(ctx, "Access key disabled",
		slog.String("access_key_id", request.GetId()),
	)

	response = &privatev1.AccessKeysDisableResponse{
		Object: accessKey,
	}
	return
}

func (s *PrivateAccessKeysServer) Enable(ctx context.Context,
	request *privatev1.AccessKeysEnableRequest) (response *privatev1.AccessKeysEnableResponse, err error) {
	// Get the access key first
	getReq := &privatev1.AccessKeysGetRequest{
		Id:             request.GetId(),
		OrganizationId: request.GetOrganizationId(),
	}
	getResp, err := s.Get(ctx, getReq)
	if err != nil {
		return
	}

	accessKey := getResp.GetObject()
	if accessKey == nil {
		err = grpcstatus.Error(grpccodes.NotFound, "access key not found")
		return
	}

	// Set enabled to true
	spec := accessKey.GetSpec()
	if spec == nil {
		err = grpcstatus.Error(grpccodes.Internal, "access key spec is nil")
		return
	}
	spec.Enabled = true

	s.logger.InfoContext(ctx, "Access key enabled",
		slog.String("access_key_id", request.GetId()),
	)

	response = &privatev1.AccessKeysEnableResponse{
		Object: accessKey,
	}
	return
}

func (s *PrivateAccessKeysServer) Delete(ctx context.Context,
	request *privatev1.AccessKeysDeleteRequest) (response *privatev1.AccessKeysDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}
