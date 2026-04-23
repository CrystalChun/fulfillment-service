/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package organization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/idp"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

// FunctionBuilder contains the data and logic needed to build a function that reconciles organizations.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	idpManager idp.OrganizationManagerInterface
}

type function struct {
	logger               *slog.Logger
	organizationsClient  privatev1.OrganizationsClient
	idpManager           idp.OrganizationManagerInterface
	maskCalculator       *masks.Calculator
}

type task struct {
	r            *function
	organization *privatev1.Organization
}

// NewFunction creates a new builder that can then be used to create a new organization reconciler function.
func NewFunction() *FunctionBuilder {
	return &FunctionBuilder{}
}

// SetLogger sets the logger. This is mandatory.
func (b *FunctionBuilder) SetLogger(value *slog.Logger) *FunctionBuilder {
	b.logger = value
	return b
}

// SetConnection sets the gRPC client connection. This is mandatory.
func (b *FunctionBuilder) SetConnection(value *grpc.ClientConn) *FunctionBuilder {
	b.connection = value
	return b
}

// SetIdpManager sets the identity provider organization manager. This is mandatory.
func (b *FunctionBuilder) SetIdpManager(value idp.OrganizationManagerInterface) *FunctionBuilder {
	b.idpManager = value
	return b
}

// Build uses the information stored in the builder to create a new organization reconciler.
func (b *FunctionBuilder) Build() (result controllers.ReconcilerFunction[*privatev1.Organization], err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.connection == nil {
		err = errors.New("connection is mandatory")
		return
	}
	if b.idpManager == nil {
		err = errors.New("IDP manager is mandatory")
		return
	}

	// Create and populate the object:
	object := &function{
		logger:              b.logger,
		organizationsClient: privatev1.NewOrganizationsClient(b.connection),
		idpManager:          b.idpManager,
		maskCalculator:      masks.NewCalculator().Build(),
	}
	result = object.run
	return
}

func (r *function) run(ctx context.Context, organization *privatev1.Organization) error {
	oldOrganization := proto.Clone(organization).(*privatev1.Organization)
	t := task{
		r:            r,
		organization: organization,
	}
	var err error
	if organization.GetMetadata().HasDeletionTimestamp() {
		err = t.delete(ctx)
	} else {
		err = t.update(ctx)
	}
	if err != nil {
		return err
	}

	// Calculate which fields the reconciler actually modified and use a field mask
	// to update only those fields. This prevents overwriting concurrent user changes.
	updateMask := r.maskCalculator.Calculate(oldOrganization, organization)

	// Only send an update if there are actual changes
	if len(updateMask.GetPaths()) > 0 {
		_, err = r.organizationsClient.Update(ctx, privatev1.OrganizationsUpdateRequest_builder{
			Object:     organization,
			UpdateMask: updateMask,
		}.Build())
	}
	return err
}

func (t *task) update(ctx context.Context) error {
	// Add the finalizer and return immediately if it was added. This ensures the finalizer is persisted before any
	// other work is done, reducing the chance of the object being deleted before the finalizer is saved.
	if t.addFinalizer() {
		return nil
	}

	// Set the default values:
	t.setDefaults()

	// Validate that exactly one tenant is assigned:
	if err := t.validateTenant(); err != nil {
		return err
	}

	// Skip if already synced to IDP
	state := t.organization.GetStatus().GetState()
	if state == privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED {
		return nil
	}

	// Skip if in terminal failure state
	if state == privatev1.OrganizationState_ORGANIZATION_STATE_FAILED {
		return nil
	}

	// Sync to IDP
	return t.syncToIDP(ctx)
}

func (t *task) syncToIDP(ctx context.Context) error {
	// Set state to PENDING
	t.organization.GetStatus().SetState(privatev1.OrganizationState_ORGANIZATION_STATE_PENDING)

	// Get organization name from metadata
	orgName := t.organization.GetMetadata().GetName()
	if orgName == "" {
		// Fallback to ID if name is empty
		orgName = t.organization.GetId()
	}

	// Create configuration for IDP
	config := &idp.OrganizationConfig{
		Name:        orgName,
		DisplayName: t.organization.GetDescription(),
	}

	t.r.logger.InfoContext(ctx, "Creating organization in IDP",
		slog.String("organization_id", t.organization.GetId()),
		slog.String("organization_name", orgName),
	)

	// Create in IDP
	credentials, err := t.r.idpManager.CreateOrganization(ctx, config)
	if err != nil {
		// Set FAILED state with error message
		msg := fmt.Sprintf("IDP sync failed: %v", err)
		t.organization.GetStatus().SetState(privatev1.OrganizationState_ORGANIZATION_STATE_FAILED)
		t.organization.GetStatus().SetMessage(msg)
		t.r.logger.ErrorContext(ctx, "Failed to create organization in IDP",
			slog.String("organization_id", t.organization.GetId()),
			slog.String("error", err.Error()),
		)
		// Don't return error - state is captured in status
		return nil
	}

	// Set SYNCED state with IDP details
	t.organization.GetStatus().SetState(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED)
	t.organization.GetStatus().SetIdpOrganizationName(config.Name)
	t.organization.GetStatus().SetBreakGlassUserId(credentials.UserID)

	// Clear any previous error message
	t.organization.GetStatus().SetMessage("")

	t.r.logger.InfoContext(ctx, "Successfully synced organization to IDP",
		slog.String("organization_id", t.organization.GetId()),
		slog.String("idp_organization_name", config.Name),
		slog.String("break_glass_user_id", credentials.UserID),
	)

	// TODO: Handle break-glass password - currently lost after this point
	// Options:
	// 1. Store in Secret resource and return secret ID
	// 2. Return in API response (requires API server changes)
	// 3. Log securely for admin retrieval

	return nil
}

func (t *task) delete(ctx context.Context) (err error) {
	// Remember to remove the finalizer if there was no error:
	defer func() {
		if err == nil {
			t.removeFinalizer()
		}
	}()

	// Skip if not synced to IDP yet (nothing to delete)
	if t.organization.GetStatus().GetState() != privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED {
		t.r.logger.DebugContext(ctx, "Organization not synced to IDP, skipping IDP deletion",
			slog.String("organization_id", t.organization.GetId()),
			slog.String("state", t.organization.GetStatus().GetState().String()),
		)
		return nil
	}

	// Get IDP organization name
	orgName := t.organization.GetStatus().GetIdpOrganizationName()
	if orgName == "" {
		t.r.logger.WarnContext(ctx, "Organization has SYNCED state but no IDP organization name",
			slog.String("organization_id", t.organization.GetId()),
		)
		return nil
	}

	t.r.logger.InfoContext(ctx, "Deleting organization from IDP",
		slog.String("organization_id", t.organization.GetId()),
		slog.String("idp_organization_name", orgName),
	)

	// Delete from IDP
	err = t.r.idpManager.DeleteOrganizationRealm(ctx, orgName)
	if err != nil {
		return fmt.Errorf("failed to delete IDP organization: %w", err)
	}

	t.r.logger.InfoContext(ctx, "Successfully deleted organization from IDP",
		slog.String("organization_id", t.organization.GetId()),
		slog.String("idp_organization_name", orgName),
	)

	return nil
}

func (t *task) setDefaults() {
	if !t.organization.HasStatus() {
		t.organization.SetStatus(&privatev1.OrganizationStatus{})
	}
	if t.organization.GetStatus().GetState() == privatev1.OrganizationState_ORGANIZATION_STATE_UNSPECIFIED {
		t.organization.GetStatus().SetState(privatev1.OrganizationState_ORGANIZATION_STATE_PENDING)
	}
}

func (t *task) validateTenant() error {
	if !t.organization.HasMetadata() || len(t.organization.GetMetadata().GetTenants()) != 1 {
		return errors.New("Organization must have exactly one tenant assigned")
	}
	return nil
}

// addFinalizer adds the controller finalizer if it is not already present. Returns true if the finalizer was added,
// false if it was already present.
func (t *task) addFinalizer() bool {
	list := t.organization.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.organization.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

func (t *task) removeFinalizer() {
	list := t.organization.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.organization.GetMetadata().SetFinalizers(list)
	}
}
