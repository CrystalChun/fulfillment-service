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
	"slices"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

// Annotation keys for organization admin credentials
const (
	// AnnotationAdminEmail is the email for the organization admin user in the IdP
	AnnotationAdminEmail = "idp.osac.io/admin-email"
	// AnnotationAdminUsername is the username for the organization admin user in the IdP
	AnnotationAdminUsername = "idp.osac.io/admin-username"
	// AnnotationAdminPassword is the password for the organization admin user in the IdP
	AnnotationAdminPassword = "idp.osac.io/admin-password"
	// AnnotationAssignTenantManagement indicates whether to assign tenant management roles to the admin
	AnnotationAssignTenantManagement = "idp.osac.io/assign-tenant-management"
)

// Finalizer keys
const (
	// FinalizerIdpTenant ensures IdP tenant is deleted before the organization is removed from the database
	FinalizerIdpTenant = "idp.osac.io/tenant"
)

type PrivateOrganizationsServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	orgManager        *idp.OrganizationManager
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.OrganizationsServer = (*PrivateOrganizationsServer)(nil)

type PrivateOrganizationsServer struct {
	privatev1.UnimplementedOrganizationsServer
	logger     *slog.Logger
	orgManager *idp.OrganizationManager
	generic    *GenericServer[*privatev1.Organization]
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

func (b *PrivateOrganizationsServerBuilder) SetOrganizationManager(value *idp.OrganizationManager) *PrivateOrganizationsServerBuilder {
	b.orgManager = value
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
	if b.orgManager == nil {
		err = errors.New("organization manager is mandatory")
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.Organization]().
		SetLogger(b.logger).
		SetService(privatev1.Organizations_ServiceDesc.ServiceName).
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
		logger:     b.logger,
		orgManager: b.orgManager,
		generic:    generic,
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

	orgName := metadata.GetName()
	if orgName == "" {
		err = grpcstatus.Error(grpccodes.InvalidArgument, "metadata.name is required")
		return
	}

	// Extract optional admin credentials from annotations.
	// If not provided, the IdP manager will auto-generate them:
	// - Username: {org-name}-osac-break-glass
	// - Email: break-glass@{org-name}.osac.local
	// - Password: 24-character cryptographically random string
	annotations := metadata.GetAnnotations()
	adminEmail := annotations[AnnotationAdminEmail]
	adminUsername := annotations[AnnotationAdminUsername]
	adminPassword := annotations[AnnotationAdminPassword]

	// Create organization in the IdP first
	s.logger.InfoContext(ctx, "Creating organization in IdP",
		slog.String("organization", orgName),
	)

	config := &idp.OrganizationConfig{
		Name:               orgName,
		DisplayName:        orgName, // Use name as display name
		BreakGlassUsername: adminUsername,
		BreakGlassEmail:    adminEmail,
		BreakGlassPassword: adminPassword,
	}

	breakGlassCredentials, err := s.orgManager.CreateOrganization(ctx, config)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to create organization in IdP",
			slog.String("organization", orgName),
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal,
			"failed to create organization in IdP: %v", err)
		return
	}

	s.logger.InfoContext(ctx, "Organization created successfully in IdP",
		slog.String("organization", orgName),
		slog.Bool("break_glass_created", true),
	)

	// Add finalizer to ensure IdP cleanup happens before database deletion
	if !slices.Contains(metadata.Finalizers, FinalizerIdpTenant) {
		metadata.Finalizers = append(metadata.Finalizers, FinalizerIdpTenant)
	}

	// Remove sensitive credentials from annotations before storing in database
	// This prevents passwords from being persisted in the database, logs, or backups
	// The credentials will be returned in the response instead.
	if annotations != nil {
		delete(annotations, AnnotationAdminPassword)
		delete(annotations, AnnotationAdminEmail)
		delete(annotations, AnnotationAdminUsername)
	}

	// Create organization in the database (with finalizer and without credentials)
	err = s.generic.Create(ctx, request, &response)
	if err != nil {
		// IdP tenant was created but database insertion failed - attempt cleanup
		s.logger.ErrorContext(ctx, "Failed to create organization in database, attempting IdP cleanup",
			slog.String("organization", orgName),
			slog.Any("error", err),
		)
		cleanupErr := s.orgManager.DeleteOrganizationRealm(ctx, orgName)
		if cleanupErr != nil {
			s.logger.ErrorContext(ctx, "Failed to cleanup IdP tenant after database failure",
				slog.String("organization", orgName),
				slog.Any("error", cleanupErr),
			)
		}
		return
	}

	// Attach the break-glass credentials to the response.
	// These are only returned on creation and must be stored securely by the caller.
	response.BreakGlassCredentials = breakGlassCredentials

	return
}

func (s *PrivateOrganizationsServer) Update(ctx context.Context,
	request *privatev1.OrganizationsUpdateRequest) (response *privatev1.OrganizationsUpdateResponse, err error) {
	// For now, we don't sync updates to the IdP
	// You could extend this to update organization display name, etc.
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Delete(ctx context.Context,
	request *privatev1.OrganizationsDeleteRequest) (response *privatev1.OrganizationsDeleteResponse, err error) {
	// Get the organization to check for finalizers
	var getResp *privatev1.OrganizationsGetResponse
	err = s.generic.Get(ctx, &privatev1.OrganizationsGetRequest{Id: request.GetId()}, &getResp)
	if err != nil {
		return
	}

	obj := getResp.GetObject()
	metadata := obj.GetMetadata()
	orgName := metadata.GetName()
	finalizers := metadata.GetFinalizers()

	// Check if the IdP finalizer is present
	if slices.Contains(finalizers, FinalizerIdpTenant) {
		// Delete from IdP first (cleanup phase)
		s.logger.InfoContext(ctx, "Deleting organization from IdP",
			slog.String("organization", orgName),
		)

		err = s.orgManager.DeleteOrganizationRealm(ctx, orgName)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to delete organization from IdP",
				slog.String("organization", orgName),
				slog.Any("error", err),
			)
			err = grpcstatus.Errorf(grpccodes.Internal,
				"failed to delete organization from IdP: %v", err)
			return
		}

		s.logger.InfoContext(ctx, "Organization deleted successfully from IdP",
			slog.String("organization", orgName),
		)

		// Remove the finalizer and update the object
		updatedFinalizers := make([]string, 0, len(finalizers)-1)
		for _, f := range finalizers {
			if f != FinalizerIdpTenant {
				updatedFinalizers = append(updatedFinalizers, f)
			}
		}
		metadata.Finalizers = updatedFinalizers

		// Persist the finalizer removal
		var updateResp *privatev1.OrganizationsUpdateResponse
		err = s.generic.Update(ctx, &privatev1.OrganizationsUpdateRequest{
			Object: obj,
		}, &updateResp)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to remove finalizer after IdP cleanup",
				slog.String("organization", orgName),
				slog.Any("error", err),
			)
			err = grpcstatus.Errorf(grpccodes.Internal,
				"failed to remove finalizer after IdP cleanup: %v", err)
			return
		}

		s.logger.InfoContext(ctx, "Finalizer removed after IdP cleanup",
			slog.String("organization", orgName),
		)
	}

	// Delete from database (now safe because IdP cleanup is done and finalizer is removed)
	err = s.generic.Delete(ctx, request, &response)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete organization from database",
			slog.String("organization", orgName),
			slog.Any("error", err),
		)
		return
	}

	return
}

func (s *PrivateOrganizationsServer) Signal(ctx context.Context,
	request *privatev1.OrganizationsSignalRequest) (response *privatev1.OrganizationsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}
