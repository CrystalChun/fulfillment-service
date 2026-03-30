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
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type OrganizationsServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ publicv1.OrganizationsServer = (*OrganizationsServer)(nil)

type OrganizationsServer struct {
	publicv1.UnimplementedOrganizationsServer

	logger    *slog.Logger
	private   privatev1.OrganizationsServer
	inMapper  *GenericMapper[*publicv1.Organization, *privatev1.Organization]
	outMapper *GenericMapper[*privatev1.Organization, *publicv1.Organization]
}

func NewOrganizationsServer() *OrganizationsServerBuilder {
	return &OrganizationsServerBuilder{}
}

func (b *OrganizationsServerBuilder) SetLogger(value *slog.Logger) *OrganizationsServerBuilder {
	b.logger = value
	return b
}

func (b *OrganizationsServerBuilder) SetNotifier(value *database.Notifier) *OrganizationsServerBuilder {
	b.notifier = value
	return b
}

func (b *OrganizationsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *OrganizationsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *OrganizationsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *OrganizationsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *OrganizationsServerBuilder) Build() (result *OrganizationsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	inMapper, err := NewGenericMapper[*publicv1.Organization, *privatev1.Organization]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.Organization, *publicv1.Organization]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	delegate, err := NewPrivateOrganizationsServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	result = &OrganizationsServer{
		logger:    b.logger,
		private:   delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *OrganizationsServer) List(ctx context.Context,
	request *publicv1.OrganizationsListRequest) (response *publicv1.OrganizationsListResponse, err error) {
	privateRequest := &privatev1.OrganizationsListRequest{}
	privateRequest.SetOffset(request.GetOffset())
	privateRequest.SetLimit(request.GetLimit())
	privateRequest.SetFilter(request.GetFilter())

	privateResponse, err := s.private.List(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	privateItems := privateResponse.GetItems()
	publicItems := make([]*publicv1.Organization, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &publicv1.Organization{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private organization to public",
				slog.Any("error", err),
			)
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process organizations")
		}
		publicItems[i] = publicItem
	}

	response = &publicv1.OrganizationsListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *OrganizationsServer) Get(ctx context.Context,
	request *publicv1.OrganizationsGetRequest) (response *publicv1.OrganizationsGetResponse, err error) {
	privateRequest := &privatev1.OrganizationsGetRequest{}
	privateRequest.SetId(request.GetId())

	privateResponse, err := s.private.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	privateOrganization := privateResponse.GetObject()
	publicOrganization := &publicv1.Organization{}
	err = s.outMapper.Copy(ctx, privateOrganization, publicOrganization)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private organization to public",
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process organization")
	}

	response = &publicv1.OrganizationsGetResponse{}
	response.SetObject(publicOrganization)
	return
}

func (s *OrganizationsServer) Create(ctx context.Context,
	request *publicv1.OrganizationsCreateRequest) (response *publicv1.OrganizationsCreateResponse, err error) {
	publicOrganization := request.GetObject()
	if publicOrganization == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateOrganization := &privatev1.Organization{}
	err = s.inMapper.Copy(ctx, publicOrganization, privateOrganization)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public organization to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process organization")
		return
	}

	privateRequest := &privatev1.OrganizationsCreateRequest{}
	privateRequest.SetObject(privateOrganization)
	privateResponse, err := s.private.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	createdPrivate := privateResponse.GetObject()
	createdPublic := &publicv1.Organization{}
	err = s.outMapper.Copy(ctx, createdPrivate, createdPublic)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private organization to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process organization")
		return
	}

	response = &publicv1.OrganizationsCreateResponse{}
	response.SetObject(createdPublic)
	return
}

func (s *OrganizationsServer) Update(ctx context.Context,
	request *publicv1.OrganizationsUpdateRequest) (response *publicv1.OrganizationsUpdateResponse, err error) {
	publicOrganization := request.GetObject()
	if publicOrganization == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	id := publicOrganization.GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	var privateOrganization *privatev1.Organization
	updateMask := request.GetUpdateMask()
	if len(updateMask.GetPaths()) > 0 {
		privateOrganization = &privatev1.Organization{}
		privateOrganization.SetId(id)
	} else {
		getRequest := &privatev1.OrganizationsGetRequest{}
		getRequest.SetId(id)
		var getResponse *privatev1.OrganizationsGetResponse
		getResponse, err = s.private.Get(ctx, getRequest)
		if err != nil {
			return nil, err
		}
		privateOrganization = getResponse.GetObject()
	}
	err = s.inMapper.Copy(ctx, publicOrganization, privateOrganization)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public organization to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process organization")
		return
	}

	privateRequest := &privatev1.OrganizationsUpdateRequest{}
	privateRequest.SetObject(privateOrganization)
	privateRequest.SetUpdateMask(updateMask)
	privateResponse, err := s.private.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	updatedPrivate := privateResponse.GetObject()
	updatedPublic := &publicv1.Organization{}
	err = s.outMapper.Copy(ctx, updatedPrivate, updatedPublic)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private organization to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process organization")
		return
	}

	response = &publicv1.OrganizationsUpdateResponse{}
	response.SetObject(updatedPublic)
	return
}

func (s *OrganizationsServer) Delete(ctx context.Context,
	request *publicv1.OrganizationsDeleteRequest) (response *publicv1.OrganizationsDeleteResponse, err error) {
	privateRequest := &privatev1.OrganizationsDeleteRequest{}
	privateRequest.SetId(request.GetId())

	_, err = s.private.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	response = &publicv1.OrganizationsDeleteResponse{}
	return
}
