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

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/osac-project/fulfillment-service/internal/idp"
)

// statefulMockClient wraps idp.MockClient to provide stateful behavior for tests.
// This maintains in-memory maps of organizations and users to simulate IDP behavior.
type statefulMockClient struct {
	*idp.MockClient
	organizations       map[string]*idp.Organization
	users               map[string][]*idp.User
	createOrgError      error
	deleteOrgError      error
	createOrgShouldFail bool
	deleteOrgShouldFail bool
	createOrgFailAfterN int
	createOrgCallCount  int
}

// newStatefulMockClient creates a stateful mock client with expectations configured.
func newStatefulMockClient(ctrl *gomock.Controller) *statefulMockClient {
	mockClient := idp.NewMockClient(ctrl)
	s := &statefulMockClient{
		MockClient:    mockClient,
		organizations: make(map[string]*idp.Organization),
		users:         make(map[string][]*idp.User),
	}

	// Configure expectations with stateful behavior
	mockClient.EXPECT().
		CreateOrganization(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, org *idp.Organization) (*idp.Organization, error) {
			org.ID = "mock-org-id"
			s.createOrgCallCount++
			if s.createOrgShouldFail || (s.createOrgFailAfterN > 0 && s.createOrgCallCount >= s.createOrgFailAfterN) {
				return nil, s.createOrgError
			}
			s.organizations[org.Name] = org
			return org, nil
		}).
		AnyTimes()

	mockClient.EXPECT().
		GetOrganization(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, name string) (*idp.Organization, error) {
			org, ok := s.organizations[name]
			if !ok {
				return nil, nil
			}
			return org, nil
		}).
		AnyTimes()

	mockClient.EXPECT().
		DeleteOrganization(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, name string) error {
			if s.deleteOrgShouldFail {
				return s.deleteOrgError
			}
			delete(s.organizations, name)
			return nil
		}).
		AnyTimes()

	mockClient.EXPECT().
		CreateUser(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, organization string, user *idp.User) (*idp.User, error) {
			user.ID = uuid.New().String()
			if s.users == nil {
				s.users = make(map[string][]*idp.User)
			}
			s.users[organization] = append(s.users[organization], user)
			return user, nil
		}).
		AnyTimes()

	mockClient.EXPECT().
		ListUsers(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, organization string) ([]*idp.User, error) {
			return s.users[organization], nil
		}).
		AnyTimes()

	mockClient.EXPECT().
		ListClientRoles(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, organization, clientID string) ([]*idp.Role, error) {
			return []*idp.Role{
				{ID: "1", Name: "manage-realm", ClientRole: true},
				{ID: "2", Name: "manage-users", ClientRole: true},
				{ID: "3", Name: "manage-clients", ClientRole: true},
			}, nil
		}).
		AnyTimes()

	mockClient.EXPECT().AssignOrganizationAdminPermissions(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockClient.EXPECT().AssignIdpManagerPermissions(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockClient.EXPECT().AssignClientRolesToUser(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	return s
}
