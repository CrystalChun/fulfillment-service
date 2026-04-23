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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type PrivateAccessKeysServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
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

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateAccessKeysServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateAccessKeysServerBuilder {
	b.metricsRegisterer = value
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
		SetMetricsRegisterer(b.metricsRegisterer).
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
	if err != nil {
		return
	}

	// Zero out secret hashes in all returned items
	for _, item := range response.Items {
		if item != nil && item.Spec != nil {
			item.Spec.SecretHash = ""
		}
	}
	return
}

func (s *PrivateAccessKeysServer) Get(ctx context.Context,
	request *privatev1.AccessKeysGetRequest) (response *privatev1.AccessKeysGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	if err != nil {
		return
	}

	// Zero out the secret hash before returning
	if response.Object != nil && response.Object.Spec != nil {
		response.Object.Spec.SecretHash = ""
	}
	return
}

func (s *PrivateAccessKeysServer) Create(ctx context.Context,
	request *privatev1.AccessKeysCreateRequest) (response *privatev1.AccessKeysCreateResponse, err error) {
	// Validate request
	if request.Object == nil {
		return nil, grpcstatus.Error(grpccodes.InvalidArgument, "object is required")
	}

	// Ensure spec exists and validate required identity fields
	if request.Object.Spec == nil {
		request.Object.Spec = &privatev1.AccessKeySpec{}
	}
	if request.Object.Spec.UserId == "" {
		return nil, grpcstatus.Error(grpccodes.InvalidArgument, "spec.user_id is required")
	}
	if request.Object.Spec.OrganizationId == "" {
		return nil, grpcstatus.Error(grpccodes.InvalidArgument, "spec.organization_id is required")
	}

	// Generate the access key ID and secret
	accessKeyID, err := generateAccessKeyID()
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to generate access key ID: %v", err)
	}

	secretAccessKey, err := generateSecretAccessKey()
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to generate secret access key: %v", err)
	}

	// Hash the secret access key before storing
	hashedSecret := hashSecret(secretAccessKey)

	// Set up the spec with the access key ID and hashed secret
	request.Object.Spec.AccessKeyId = accessKeyID
	request.Object.Spec.SecretHash = hashedSecret
	request.Object.Spec.Enabled = true

	// Create the access key
	createResponse := &privatev1.AccessKeysCreateResponse{}
	err = s.generic.Create(ctx, request, &createResponse)
	if err != nil {
		return nil, err
	}

	// Zero out the secret hash before returning to ensure it's never sent over the wire.
	// The hash is persisted in the database but should not be exposed in API responses.
	if createResponse.Object != nil && createResponse.Object.Spec != nil {
		createResponse.Object.Spec.SecretHash = ""
	}

	// Return both the access key object (without secret hash) and the credentials
	// The secret is only returned here on creation and never again
	response = &privatev1.AccessKeysCreateResponse{
		Object: createResponse.Object,
		Credentials: &privatev1.AccessKeyCredentials{
			AccessKeyId:     accessKeyID,
			SecretAccessKey: secretAccessKey,
		},
	}
	return
}

func (s *PrivateAccessKeysServer) Update(ctx context.Context,
	request *privatev1.AccessKeysUpdateRequest) (response *privatev1.AccessKeysUpdateResponse, err error) {
	// Prevent modification of access_key_id and secret_hash as they are immutable after creation.
	// These fields are critical for authentication and the access_keys_by_access_key_id index.
	// TODO: Implement a separate Rotate RPC for credential rotation.

	if request.UpdateMask != nil {
		// If an update mask is provided, reject if it includes the immutable fields
		for _, path := range request.UpdateMask.Paths {
			if path == "spec.access_key_id" || path == "spec.secret_hash" {
				return nil, grpcstatus.Errorf(
					grpccodes.InvalidArgument,
					"field '%s' is immutable and cannot be updated; use a rotate operation to change credentials",
					path,
				)
			}
		}
	} else {
		// If no update mask is provided (full replacement), preserve the immutable fields
		// from the existing object
		getResponse := &privatev1.AccessKeysGetResponse{}
		err = s.generic.Get(ctx, &privatev1.AccessKeysGetRequest{
			Id: request.Object.Id,
		}, &getResponse)
		if err != nil {
			return nil, err
		}

		// Preserve the original access_key_id and secret_hash
		if request.Object.Spec == nil {
			request.Object.Spec = &privatev1.AccessKeySpec{}
		}
		request.Object.Spec.AccessKeyId = getResponse.Object.Spec.AccessKeyId
		request.Object.Spec.SecretHash = getResponse.Object.Spec.SecretHash
	}

	err = s.generic.Update(ctx, request, &response)
	if err != nil {
		return nil, err
	}

	// Zero out the secret hash before returning
	if response.Object != nil && response.Object.Spec != nil {
		response.Object.Spec.SecretHash = ""
	}
	return
}

func (s *PrivateAccessKeysServer) Disable(ctx context.Context,
	request *privatev1.AccessKeysDisableRequest) (response *privatev1.AccessKeysDisableResponse, err error) {
	// Get the access key
	getResponse := &privatev1.AccessKeysGetResponse{}
	err = s.generic.Get(ctx, &privatev1.AccessKeysGetRequest{
		Id:             request.Id,
		OrganizationId: request.OrganizationId,
		UserId:         request.UserId,
	}, &getResponse)
	if err != nil {
		return nil, err
	}

	// Set enabled to false
	getResponse.Object.Spec.Enabled = false

	// Update the access key
	updateResponse := &privatev1.AccessKeysUpdateResponse{}
	err = s.generic.Update(ctx, &privatev1.AccessKeysUpdateRequest{
		Object: getResponse.Object,
	}, &updateResponse)
	if err != nil {
		return nil, err
	}

	// Zero out the secret hash before returning
	if updateResponse.Object != nil && updateResponse.Object.Spec != nil {
		updateResponse.Object.Spec.SecretHash = ""
	}

	response = &privatev1.AccessKeysDisableResponse{
		Object: updateResponse.Object,
	}
	return
}

func (s *PrivateAccessKeysServer) Enable(ctx context.Context,
	request *privatev1.AccessKeysEnableRequest) (response *privatev1.AccessKeysEnableResponse, err error) {
	// Get the access key
	getResponse := &privatev1.AccessKeysGetResponse{}
	err = s.generic.Get(ctx, &privatev1.AccessKeysGetRequest{
		Id:             request.Id,
		OrganizationId: request.OrganizationId,
		UserId:         request.UserId,
	}, &getResponse)
	if err != nil {
		return nil, err
	}

	// Set enabled to true
	getResponse.Object.Spec.Enabled = true

	// Update the access key
	updateResponse := &privatev1.AccessKeysUpdateResponse{}
	err = s.generic.Update(ctx, &privatev1.AccessKeysUpdateRequest{
		Object: getResponse.Object,
	}, &updateResponse)
	if err != nil {
		return nil, err
	}

	// Zero out the secret hash before returning
	if updateResponse.Object != nil && updateResponse.Object.Spec != nil {
		updateResponse.Object.Spec.SecretHash = ""
	}

	response = &privatev1.AccessKeysEnableResponse{
		Object: updateResponse.Object,
	}
	return
}

func (s *PrivateAccessKeysServer) Delete(ctx context.Context,
	request *privatev1.AccessKeysDeleteRequest) (response *privatev1.AccessKeysDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateAccessKeysServer) Signal(ctx context.Context,
	request *privatev1.AccessKeysSignalRequest) (response *privatev1.AccessKeysSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// generateAccessKeyID generates a random access key ID
func generateAccessKeyID() (string, error) {
	bytes := make([]byte, 20)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("AK%s", base64.RawURLEncoding.EncodeToString(bytes)), nil
}

// generateSecretAccessKey generates a random secret access key
func generateSecretAccessKey() (string, error) {
	bytes := make([]byte, 40)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// hashSecret hashes the secret using SHA-256
func hashSecret(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return base64.StdEncoding.EncodeToString(hash[:])
}
