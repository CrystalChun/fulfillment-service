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

	"github.com/prometheus/client_golang/prometheus"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/events"
)

// PrivateOrganizationsServerBuilder is superseded by PrivateTenantsServerBuilder.
type PrivateOrganizationsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.OrganizationsServer = (*PrivateOrganizationsServer)(nil)

// PrivateOrganizationsServer is superseded by PrivateTenantsServer.
type PrivateOrganizationsServer struct {
	privatev1.UnimplementedOrganizationsServer
	logger    *slog.Logger
	tenants   privatev1.TenantsServer
	inMapper  *GenericMapper[*privatev1.Organization, *privatev1.Tenant]
	outMapper *GenericMapper[*privatev1.Tenant, *privatev1.Organization]
}

// NewPrivateOrganizationsServer is superseded by NewPrivateTenantsServer.
func NewPrivateOrganizationsServer() *PrivateOrganizationsServerBuilder {
	return &PrivateOrganizationsServerBuilder{}
}

func (b *PrivateOrganizationsServerBuilder) SetLogger(value *slog.Logger) *PrivateOrganizationsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) SetNotifier(value events.Notifier) *PrivateOrganizationsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateOrganizationsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateOrganizationsServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateOrganizationsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateOrganizationsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) Build() (result *PrivateOrganizationsServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the mappers between Organization and Tenant types:
	inMapper, err := NewGenericMapper[*privatev1.Organization, *privatev1.Tenant]().
		SetLogger(b.logger).
		SetStrict(true).
		AddIgnoredFields("idp_organization_name").
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.Tenant, *privatev1.Organization]().
		SetLogger(b.logger).
		SetStrict(false).
		AddIgnoredFields("idp_tenant_name").
		Build()
	if err != nil {
		return
	}

	// Create the tenant server to delegate to:
	delegate, err := NewPrivateTenantsServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateOrganizationsServer{
		logger:    b.logger,
		tenants:   delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *PrivateOrganizationsServer) copyRenamedFields(tenant *privatev1.Tenant, org *privatev1.Organization) {
	if status := tenant.GetStatus(); status != nil {
		org.GetStatus().IdpOrganizationName = status.GetIdpTenantName()
	}
}

func (s *PrivateOrganizationsServer) List(ctx context.Context,
	request *privatev1.OrganizationsListRequest) (response *privatev1.OrganizationsListResponse, err error) {
	// Map to tenant request:
	tenantRequest := &privatev1.TenantsListRequest{}
	tenantRequest.SetOffset(request.GetOffset())
	tenantRequest.SetLimit(request.GetLimit())
	tenantRequest.SetFilter(request.GetFilter())

	// Delegate to tenant server:
	tenantResponse, err := s.tenants.List(ctx, tenantRequest)
	if err != nil {
		return nil, err
	}

	// Map tenant response to organization format:
	tenantItems := tenantResponse.GetItems()
	orgItems := make([]*privatev1.Organization, len(tenantItems))
	for i, tenantItem := range tenantItems {
		orgItem := &privatev1.Organization{}
		err = s.outMapper.Copy(ctx, tenantItem, orgItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map tenant to organization",
				slog.Any("error", err),
			)
			return nil, err
		}
		s.copyRenamedFields(tenantItem, orgItem)
		orgItems[i] = orgItem
	}

	// Build and return the response:
	response = &privatev1.OrganizationsListResponse{}
	response.SetSize(tenantResponse.GetSize())
	response.SetTotal(tenantResponse.GetTotal())
	response.SetItems(orgItems)
	return
}

func (s *PrivateOrganizationsServer) Get(ctx context.Context,
	request *privatev1.OrganizationsGetRequest) (response *privatev1.OrganizationsGetResponse, err error) {
	// Map to tenant request:
	tenantRequest := &privatev1.TenantsGetRequest{}
	tenantRequest.SetId(request.GetId())

	// Delegate to tenant server:
	tenantResponse, err := s.tenants.Get(ctx, tenantRequest)
	if err != nil {
		return nil, err
	}

	// Map tenant response to organization format:
	orgObject := &privatev1.Organization{}
	err = s.outMapper.Copy(ctx, tenantResponse.GetObject(), orgObject)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map tenant to organization",
			slog.Any("error", err),
		)
		return nil, err
	}
	s.copyRenamedFields(tenantResponse.GetObject(), orgObject)

	// Build and return the response:
	response = &privatev1.OrganizationsGetResponse{}
	response.SetObject(orgObject)
	return
}

func (s *PrivateOrganizationsServer) Create(ctx context.Context,
	request *privatev1.OrganizationsCreateRequest) (response *privatev1.OrganizationsCreateResponse, err error) {
	// Map organization to tenant:
	tenantObject := &privatev1.Tenant{}
	err = s.inMapper.Copy(ctx, request.GetObject(), tenantObject)
	if err != nil {
		return nil, err
	}

	// Delegate to tenant server (validation happens there):
	tenantRequest := &privatev1.TenantsCreateRequest{}
	tenantRequest.SetObject(tenantObject)
	tenantResponse, err := s.tenants.Create(ctx, tenantRequest)
	if err != nil {
		return nil, err
	}

	// Map tenant response back to organization:
	orgObject := &privatev1.Organization{}
	err = s.outMapper.Copy(ctx, tenantResponse.GetObject(), orgObject)
	if err != nil {
		return nil, err
	}
	s.copyRenamedFields(tenantResponse.GetObject(), orgObject)

	response = &privatev1.OrganizationsCreateResponse{}
	response.SetObject(orgObject)
	return
}

func (s *PrivateOrganizationsServer) Update(ctx context.Context,
	request *privatev1.OrganizationsUpdateRequest) (response *privatev1.OrganizationsUpdateResponse, err error) {
	// Map organization to tenant:
	tenantObject := &privatev1.Tenant{}
	err = s.inMapper.Copy(ctx, request.GetObject(), tenantObject)
	if err != nil {
		return nil, err
	}

	// Delegate to tenant server:
	tenantRequest := &privatev1.TenantsUpdateRequest{}
	tenantRequest.SetObject(tenantObject)
	tenantRequest.SetUpdateMask(request.GetUpdateMask())
	tenantRequest.SetLock(request.GetLock())
	tenantResponse, err := s.tenants.Update(ctx, tenantRequest)
	if err != nil {
		return nil, err
	}

	// Map tenant response back to organization:
	orgObject := &privatev1.Organization{}
	err = s.outMapper.Copy(ctx, tenantResponse.GetObject(), orgObject)
	if err != nil {
		return nil, err
	}
	s.copyRenamedFields(tenantResponse.GetObject(), orgObject)

	response = &privatev1.OrganizationsUpdateResponse{}
	response.SetObject(orgObject)
	return
}

func (s *PrivateOrganizationsServer) Delete(ctx context.Context,
	request *privatev1.OrganizationsDeleteRequest) (response *privatev1.OrganizationsDeleteResponse, err error) {
	// Delegate to tenant server:
	tenantRequest := &privatev1.TenantsDeleteRequest{}
	tenantRequest.SetId(request.GetId())
	_, err = s.tenants.Delete(ctx, tenantRequest)
	if err != nil {
		return nil, err
	}

	response = &privatev1.OrganizationsDeleteResponse{}
	return
}

func (s *PrivateOrganizationsServer) Signal(ctx context.Context,
	request *privatev1.OrganizationsSignalRequest) (response *privatev1.OrganizationsSignalResponse, err error) {
	// Delegate to tenant server:
	tenantRequest := &privatev1.TenantsSignalRequest{}
	tenantRequest.SetId(request.GetId())
	_, err = s.tenants.Signal(ctx, tenantRequest)
	if err != nil {
		return nil, err
	}

	response = &privatev1.OrganizationsSignalResponse{}
	return
}
