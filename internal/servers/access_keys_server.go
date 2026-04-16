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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type AccessKeysServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ publicv1.AccessKeysServer = (*AccessKeysServer)(nil)

type AccessKeysServer struct {
	publicv1.UnimplementedAccessKeysServer

	logger    *slog.Logger
	private   privatev1.AccessKeysServer
	inMapper  *GenericMapper[*publicv1.AccessKey, *privatev1.AccessKey]
	outMapper *GenericMapper[*privatev1.AccessKey, *publicv1.AccessKey]
}

func NewAccessKeysServer() *AccessKeysServerBuilder {
	return &AccessKeysServerBuilder{}
}

func (b *AccessKeysServerBuilder) SetLogger(value *slog.Logger) *AccessKeysServerBuilder {
	b.logger = value
	return b
}

func (b *AccessKeysServerBuilder) SetNotifier(value *database.Notifier) *AccessKeysServerBuilder {
	b.notifier = value
	return b
}

func (b *AccessKeysServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *AccessKeysServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *AccessKeysServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *AccessKeysServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *AccessKeysServerBuilder) Build() (result *AccessKeysServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the mappers:
	inMapper, err := NewGenericMapper[*publicv1.AccessKey, *privatev1.AccessKey]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.AccessKey, *publicv1.AccessKey]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivateAccessKeysServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &AccessKeysServer{
		logger:    b.logger,
		private:   delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *AccessKeysServer) List(ctx context.Context,
	request *publicv1.AccessKeysListRequest) (response *publicv1.AccessKeysListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.AccessKeysListRequest{}
	privateRequest.SetOffset(request.GetOffset())
	privateRequest.SetLimit(request.GetLimit())
	privateRequest.SetFilter(request.GetFilter())
	privateRequest.UserId = request.UserId
	privateRequest.OrganizationId = request.OrganizationId

	// Delegate to private server:
	privateResponse, err := s.private.List(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateItems := privateResponse.GetItems()
	publicItems := make([]*publicv1.AccessKey, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &publicv1.AccessKey{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map access key to public format", slog.Any("error", err))
			return nil, err
		}
		publicItems[i] = publicItem
	}

	response = &publicv1.AccessKeysListResponse{
		Size:  privateResponse.GetSize(),
		Total: privateResponse.GetTotal(),
		Items: publicItems,
	}
	return
}

func (s *AccessKeysServer) Get(ctx context.Context,
	request *publicv1.AccessKeysGetRequest) (response *publicv1.AccessKeysGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.AccessKeysGetRequest{
		Id:             request.GetId(),
		OrganizationId: request.GetOrganizationId(),
	}

	// Delegate to private server:
	privateResponse, err := s.private.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map to public format:
	publicAccessKey := &publicv1.AccessKey{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), publicAccessKey)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map access key to public format", slog.Any("error", err))
		return nil, err
	}

	response = &publicv1.AccessKeysGetResponse{
		Object: publicAccessKey,
	}
	return
}

func (s *AccessKeysServer) Create(ctx context.Context,
	request *publicv1.AccessKeysCreateRequest) (response *publicv1.AccessKeysCreateResponse, err error) {
	// Map public request to private:
	privateAccessKey := &privatev1.AccessKey{}
	err = s.inMapper.Copy(ctx, request.GetObject(), privateAccessKey)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map access key to private format", slog.Any("error", err))
		return nil, err
	}

	privateRequest := &privatev1.AccessKeysCreateRequest{
		Object: privateAccessKey,
	}

	// Delegate to private server:
	privateResponse, err := s.private.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map to public format:
	publicAccessKey := &publicv1.AccessKey{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), publicAccessKey)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map access key to public format", slog.Any("error", err))
		return nil, err
	}

	// Map credentials (which are only returned on creation)
	var publicCredentials *publicv1.AccessKeyCredentials
	if privateResponse.GetCredentials() != nil {
		publicCredentials = &publicv1.AccessKeyCredentials{
			AccessKeyId:     privateResponse.GetCredentials().GetAccessKeyId(),
			SecretAccessKey: privateResponse.GetCredentials().GetSecretAccessKey(),
		}
	}

	response = &publicv1.AccessKeysCreateResponse{
		Object:      publicAccessKey,
		Credentials: publicCredentials,
	}
	return
}

func (s *AccessKeysServer) Disable(ctx context.Context,
	request *publicv1.AccessKeysDisableRequest) (response *publicv1.AccessKeysDisableResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.AccessKeysDisableRequest{
		Id:             request.GetId(),
		OrganizationId: request.GetOrganizationId(),
	}

	// Delegate to private server:
	privateResponse, err := s.private.Disable(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map to public format:
	publicAccessKey := &publicv1.AccessKey{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), publicAccessKey)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map access key to public format", slog.Any("error", err))
		return nil, err
	}

	response = &publicv1.AccessKeysDisableResponse{
		Object: publicAccessKey,
	}
	return
}

func (s *AccessKeysServer) Enable(ctx context.Context,
	request *publicv1.AccessKeysEnableRequest) (response *publicv1.AccessKeysEnableResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.AccessKeysEnableRequest{
		Id:             request.GetId(),
		OrganizationId: request.GetOrganizationId(),
	}

	// Delegate to private server:
	privateResponse, err := s.private.Enable(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map to public format:
	publicAccessKey := &publicv1.AccessKey{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), publicAccessKey)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map access key to public format", slog.Any("error", err))
		return nil, err
	}

	response = &publicv1.AccessKeysEnableResponse{
		Object: publicAccessKey,
	}
	return
}

func (s *AccessKeysServer) Delete(ctx context.Context,
	request *publicv1.AccessKeysDeleteRequest) (response *publicv1.AccessKeysDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.AccessKeysDeleteRequest{
		Id:             request.GetId(),
		OrganizationId: request.GetOrganizationId(),
	}

	// Delegate to private server:
	_, err = s.private.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	response = &publicv1.AccessKeysDeleteResponse{}
	return
}
