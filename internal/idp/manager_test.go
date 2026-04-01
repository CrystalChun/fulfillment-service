/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package idp

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
)

// mockClient is a mock IdP client for testing.
type mockClient struct {
	createdRealm        *Organization
	createdUsers        []*User
	deletedRealm        string
	userRoleAssignments map[string]map[string][]*Role // userID -> clientID -> roles
}

func (m *mockClient) CreateOrganization(ctx context.Context, org *Organization) error {
	m.createdRealm = org
	return nil
}

func (m *mockClient) GetOrganization(ctx context.Context, name string) (*Organization, error) {
	return m.createdRealm, nil
}

func (m *mockClient) DeleteOrganization(ctx context.Context, name string) error {
	m.deletedRealm = name
	return nil
}

func (m *mockClient) CreateUser(ctx context.Context, organization string, user *User) error {
	// Assign an ID to the user
	user.ID = fmt.Sprintf("user-%d", len(m.createdUsers)+1)
	m.createdUsers = append(m.createdUsers, user)
	return nil
}

func (m *mockClient) GetUser(ctx context.Context, organization, userID string) (*User, error) {
	for _, user := range m.createdUsers {
		if user.ID == userID {
			return user, nil
		}
	}
	return nil, nil
}

func (m *mockClient) ListUsers(ctx context.Context, organization string) ([]*User, error) {
	return m.createdUsers, nil
}

func (m *mockClient) DeleteUser(ctx context.Context, organization, userID string) error {
	return nil
}

func (m *mockClient) ListRealmRoles(ctx context.Context, organization string) ([]*Role, error) {
	return nil, nil
}

func (m *mockClient) ListClientRoles(ctx context.Context, organization, clientID string) ([]*Role, error) {
	// Only return roles for realm-management client
	if clientID == "realm-management" {
		return []*Role{
			{ID: "1", Name: "manage-realm", ApplicationRole: true},
			{ID: "2", Name: "manage-users", ApplicationRole: true},
			{ID: "3", Name: "manage-clients", ApplicationRole: true},
		}, nil
	}
	return nil, nil
}

func (m *mockClient) AssignRealmRolesToUser(ctx context.Context, organization, userID string, roles []*Role) error {
	if m.userRoleAssignments == nil {
		m.userRoleAssignments = make(map[string]map[string][]*Role)
	}
	if m.userRoleAssignments[userID] == nil {
		m.userRoleAssignments[userID] = make(map[string][]*Role)
	}
	m.userRoleAssignments[userID]["realm"] = roles
	return nil
}

func (m *mockClient) AssignClientRolesToUser(ctx context.Context, organization, userID, clientID string, roles []*Role) error {
	if m.userRoleAssignments == nil {
		m.userRoleAssignments = make(map[string]map[string][]*Role)
	}
	if m.userRoleAssignments[userID] == nil {
		m.userRoleAssignments[userID] = make(map[string][]*Role)
	}
	m.userRoleAssignments[userID][clientID] = roles
	return nil
}

func (m *mockClient) RemoveRealmRolesFromUser(ctx context.Context, organization, userID string, roles []*Role) error {
	return nil
}

func (m *mockClient) RemoveClientRolesFromUser(ctx context.Context, organization, userID, clientID string, roles []*Role) error {
	return nil
}

func (m *mockClient) GetUserRealmRoles(ctx context.Context, organization, userID string) ([]*Role, error) {
	if m.userRoleAssignments != nil && m.userRoleAssignments[userID] != nil {
		return m.userRoleAssignments[userID]["realm"], nil
	}
	return nil, nil
}

func (m *mockClient) GetUserClientRoles(ctx context.Context, organization, userID, clientID string) ([]*Role, error) {
	if m.userRoleAssignments != nil && m.userRoleAssignments[userID] != nil {
		return m.userRoleAssignments[userID][clientID], nil
	}
	return nil, nil
}

func TestOrganizationManager_CreateOrganizationRealm(t *testing.T) {
	mock := &mockClient{}
	logger := slog.Default()

	manager, err := NewOrganizationManager().
		SetLogger(logger).
		SetClient(mock).
		Build()
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	config := &OrganizationConfig{
		Name:          "test-org",
		DisplayName:   "Test Organization",
		AdminEmail:    "admin@test-org.com",
		AdminUsername: "admin",
		AdminPassword: "password123",
	}

	err = manager.CreateOrganizationRealm(context.Background(), config)
	if err != nil {
		t.Fatalf("failed to create organization realm: %v", err)
	}

	// Verify realm was created
	if mock.createdRealm == nil {
		t.Fatal("realm was not created")
	}
	if mock.createdRealm.Name != "test-org" {
		t.Errorf("expected realm 'test-org', got %s", mock.createdRealm.Name)
	}

	// Verify user was created
	if len(mock.createdUsers) != 1 {
		t.Fatalf("expected 1 user, got %d", len(mock.createdUsers))
	}
	if mock.createdUsers[0].Username != "admin" {
		t.Errorf("expected username 'admin', got %s", mock.createdUsers[0].Username)
	}
}

func TestOrganizationManager_DeleteOrganizationRealm(t *testing.T) {
	mock := &mockClient{}
	logger := slog.Default()

	manager, err := NewOrganizationManager().
		SetLogger(logger).
		SetClient(mock).
		Build()
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	err = manager.DeleteOrganizationRealm(context.Background(), "test-org")
	if err != nil {
		t.Fatalf("failed to delete organization realm: %v", err)
	}

	if mock.deletedRealm != "test-org" {
		t.Errorf("expected to delete realm 'test-org', deleted '%s'", mock.deletedRealm)
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
