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
	"strings"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

type PrivateOrganizationsServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
	keycloakAdmin    *auth.KeycloakAdminClient
}

var _ privatev1.OrganizationsServer = (*PrivateOrganizationsServer)(nil)

type PrivateOrganizationsServer struct {
	privatev1.UnimplementedOrganizationsServer
	logger        *slog.Logger
	generic       *GenericServer[*privatev1.Organization]
	keycloakAdmin *auth.KeycloakAdminClient
}

func NewPrivateOrganizationsServer() *PrivateOrganizationsServerBuilder {
	return &PrivateOrganizationsServerBuilder{}
}

func (b *PrivateOrganizationsServerBuilder) SetLogger(value *slog.Logger) *PrivateOrganizationsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) SetNotifier(value *database.Notifier) *PrivateOrganizationsServerBuilder {
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

// SetKeycloakAdminClient sets an optional Keycloak Admin API client used to create a realm per organization on Create.
// When nil, no realm is provisioned (useful when Keycloak admin integration is not configured).
func (b *PrivateOrganizationsServerBuilder) SetKeycloakAdminClient(value *auth.KeycloakAdminClient) *PrivateOrganizationsServerBuilder {
	b.keycloakAdmin = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) Build() (result *PrivateOrganizationsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	generic, err := NewGenericServer[*privatev1.Organization]().
		SetLogger(b.logger).
		SetService(privatev1.Organizations_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	result = &PrivateOrganizationsServer{
		logger:        b.logger,
		generic:       generic,
		keycloakAdmin: b.keycloakAdmin,
	}
	return
}

func (s *PrivateOrganizationsServer) List(ctx context.Context,
	request *privatev1.OrganizationsListRequest) (response *privatev1.OrganizationsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Get(ctx context.Context,
	request *privatev1.OrganizationsGetRequest) (response *privatev1.OrganizationsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Create(ctx context.Context,
	request *privatev1.OrganizationsCreateRequest) (response *privatev1.OrganizationsCreateResponse, err error) {
	if request.GetObject() != nil {
		alignOrganizationTenant(request.GetObject())
	}
	// Provision the IdP realm before persisting the organization so we never commit without a matching realm
	// when Keycloak integration is enabled (fail fast on IdP errors; no DB rollback needed).
	if s.keycloakAdmin != nil && request.GetObject() != nil {
		err = s.createKeycloakRealm(ctx, request.GetObject())
		if err != nil {
			return nil, err
		}
	}
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) createKeycloakRealm(ctx context.Context, org *privatev1.Organization) error {
	id := org.GetId()
	if id == "" {
		return grpcstatus.Errorf(grpccodes.Internal, "organization identifier is empty")
	}
	enabled := true
	rep := &auth.KeycloakRealmRepresentation{
		Realm:   &id,
		Enabled: &enabled,
	}
	if meta := org.GetMetadata(); meta != nil {
		if name := strings.TrimSpace(meta.GetName()); name != "" {
			rep.DisplayName = &name
		}
	}
	err := s.keycloakAdmin.CreateRealm(ctx, rep)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to create Keycloak realm for organization",
			slog.String("organizationId", id),
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(grpccodes.Unavailable, "failed to create identity realm: %v", err)
	}
	return nil
}

func (s *PrivateOrganizationsServer) Update(ctx context.Context,
	request *privatev1.OrganizationsUpdateRequest) (response *privatev1.OrganizationsUpdateResponse, err error) {
	if request.GetObject() != nil {
		alignOrganizationTenant(request.GetObject())
	}
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Delete(ctx context.Context,
	request *privatev1.OrganizationsDeleteRequest) (response *privatev1.OrganizationsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Signal(ctx context.Context,
	request *privatev1.OrganizationsSignalRequest) (response *privatev1.OrganizationsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// alignOrganizationTenant enforces one tenant per organization: the tenant identifier is the organization id.
// This matches a model where a tenant and an organization are the same boundary for access control.
func alignOrganizationTenant(org *privatev1.Organization) {
	if org.GetId() == "" {
		org.SetId(uuid.New())
	}
	id := org.GetId()
	if !org.HasMetadata() {
		org.SetMetadata(privatev1.Metadata_builder{}.Build())
	}
	org.GetMetadata().SetTenants([]string{id})
}
