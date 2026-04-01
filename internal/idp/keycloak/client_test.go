/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package keycloak

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

func TestKeycloakClient_CreateOrganization(t *testing.T) {
	var receivedRealm *keycloakRealm
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/admin/realms" {
			t.Errorf("expected path /admin/realms, got %s", r.URL.Path)
		}

		receivedRealm = &keycloakRealm{}
		json.NewDecoder(r.Body).Decode(receivedRealm)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := createTestClient(t, server.URL)

	org := &idp.Organization{
		Name:        "test-org",
		DisplayName: "Test Organization",
		Enabled:     true,
	}
	err := client.CreateOrganization(context.Background(), org)
	if err != nil {
		t.Fatalf("failed to create organization: %v", err)
	}

	if receivedRealm.Realm != "test-org" {
		t.Errorf("expected realm 'test-org', got %s", receivedRealm.Realm)
	}
}

func TestKeycloakClient_GetOrganization(t *testing.T) {
	enabled := true
	testRealm := &keycloakRealm{
		ID:          "org-id",
		Realm:       "test-org",
		DisplayName: "Test Organization",
		Enabled:     &enabled,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(testRealm)
	}))
	defer server.Close()

	client := createTestClient(t, server.URL)

	org, err := client.GetOrganization(context.Background(), "test-org")
	if err != nil {
		t.Fatalf("failed to get organization: %v", err)
	}

	if org.Name != "test-org" {
		t.Errorf("expected organization 'test-org', got %s", org.Name)
	}
	if org.DisplayName != "Test Organization" {
		t.Errorf("expected display name 'Test Organization', got %s", org.DisplayName)
	}
}

func TestKeycloakClient_CreateUser(t *testing.T) {
	var receivedUser *keycloakUser
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUser = &keycloakUser{}
		json.NewDecoder(r.Body).Decode(receivedUser)
		w.Header().Set("Location", "/admin/realms/test-org/users/user-123-abc")
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := createTestClient(t, server.URL)

	user := &idp.User{
		Username:      "testuser",
		Email:         "test@example.com",
		EmailVerified: true,
		Enabled:       true,
		FirstName:     "Test",
		LastName:      "User",
	}
	err := client.CreateUser(context.Background(), "test-org", user)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	if receivedUser.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %s", receivedUser.Username)
	}

	if user.ID != "user-123-abc" {
		t.Errorf("expected user ID 'user-123-abc', got %s", user.ID)
	}
}

func TestKeycloakClient_ListUsers_Pagination(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		// Parse query parameters
		query := r.URL.Query()
		first := query.Get("first")
		max := query.Get("max")

		requestCount++

		// Simulate pagination: first page returns 100 users, second page returns 50
		var users []keycloakUser
		if first == "0" && max == "100" {
			// First page: return 100 users
			for i := 0; i < 100; i++ {
				enabled := true
				users = append(users, keycloakUser{
					ID:       fmt.Sprintf("user-%d", i),
					Username: fmt.Sprintf("user%d", i),
					Enabled:  &enabled,
				})
			}
		} else if first == "100" && max == "100" {
			// Second page: return 50 users (less than max, indicates last page)
			for i := 100; i < 150; i++ {
				enabled := true
				users = append(users, keycloakUser{
					ID:       fmt.Sprintf("user-%d", i),
					Username: fmt.Sprintf("user%d", i),
					Enabled:  &enabled,
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(users)
	}))
	defer server.Close()

	client := createTestClient(t, server.URL)

	users, err := client.ListUsers(context.Background(), "test-org")
	if err != nil {
		t.Fatalf("failed to list users: %v", err)
	}

	// Should have fetched all 150 users across 2 pages
	if len(users) != 150 {
		t.Errorf("expected 150 users, got %d", len(users))
	}

	// Should have made 2 requests (one per page)
	if requestCount != 2 {
		t.Errorf("expected 2 requests, got %d", requestCount)
	}

	// Verify first and last user
	if users[0].ID != "user-0" {
		t.Errorf("expected first user ID 'user-0', got %s", users[0].ID)
	}
	if users[149].ID != "user-149" {
		t.Errorf("expected last user ID 'user-149', got %s", users[149].ID)
	}
}

func createTestClient(t *testing.T, serverURL string) *Client {
	logger := slog.Default()
	tokenSource, err := auth.NewStaticTokenSource().
		SetLogger(logger).
		SetToken(&auth.Token{Access: "test-token"}).
		Build()
	if err != nil {
		t.Fatalf("failed to create token source: %v", err)
	}

	client, err := NewClient().
		SetLogger(logger).
		SetBaseURL(serverURL).
		SetTokenSource(tokenSource).
		Build()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	return client
}
