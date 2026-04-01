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
	"log/slog"
	"testing"
)

func TestOrganizationManager_AssignRealmManagementRoles(t *testing.T) {
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
		Name:                       "test-org",
		DisplayName:                "Test Organization",
		AdminEmail:                 "admin@test-org.com",
		AdminUsername:              "admin",
		AdminPassword:              "password123",
		AssignRealmManagementRoles: true, // Enable role assignment
	}

	err = manager.CreateOrganizationRealm(context.Background(), config)
	if err != nil {
		t.Fatalf("failed to create organization realm: %v", err)
	}

	// Verify user was created
	if len(mock.createdUsers) != 1 {
		t.Fatalf("expected 1 user, got %d", len(mock.createdUsers))
	}

	// Verify roles were assigned
	userID := mock.createdUsers[0].ID
	if mock.userRoleAssignments == nil || mock.userRoleAssignments[userID] == nil {
		t.Fatal("no role assignments found for user")
	}

	// Check for realm-management application role assignments
	realmMgmtRoles := mock.userRoleAssignments[userID]["realm-management"]
	if len(realmMgmtRoles) == 0 {
		t.Fatal("no realm management roles assigned to user")
	}

	// Verify specific roles were assigned
	roleNames := make([]string, len(realmMgmtRoles))
	for i, role := range realmMgmtRoles {
		roleNames[i] = role.Name
	}

	expectedRoles := []string{"manage-realm", "manage-users", "manage-clients"}
	for _, expectedRole := range expectedRoles {
		if !contains(roleNames, expectedRole) {
			t.Errorf("expected role '%s' to be assigned, got %v", expectedRole, roleNames)
		}
	}

	t.Logf("Successfully assigned %d realm management roles to admin user", len(realmMgmtRoles))
}

func TestOrganizationManager_NoRoleAssignment(t *testing.T) {
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
		Name:                       "test-org",
		DisplayName:                "Test Organization",
		AdminEmail:                 "admin@test-org.com",
		AdminUsername:              "admin",
		AdminPassword:              "password123",
		AssignRealmManagementRoles: false, // Disable role assignment
	}

	err = manager.CreateOrganizationRealm(context.Background(), config)
	if err != nil {
		t.Fatalf("failed to create organization realm: %v", err)
	}

	// Verify user was created
	if len(mock.createdUsers) != 1 {
		t.Fatalf("expected 1 user, got %d", len(mock.createdUsers))
	}

	// Verify NO roles were assigned
	userID := mock.createdUsers[0].ID
	if mock.userRoleAssignments != nil && mock.userRoleAssignments[userID] != nil {
		t.Errorf("expected no role assignments, but found: %v", mock.userRoleAssignments[userID])
	}
}
