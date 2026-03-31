/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/pflag"
)

func TestKeycloakAdminClient_CreateRealm(t *testing.T) {
	t.Parallel()

	var sawAuth string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/realms" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		sawAuth = r.Header.Get("Authorization")
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	tokenSource, err := NewStaticTokenSource().
		SetLogger(slog.Default()).
		SetToken(&Token{Access: "test-access-token"}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	client, err := NewKeycloakAdminClient().
		SetLogger(slog.Default()).
		SetBaseURL(srv.URL).
		SetTokenSource(tokenSource).
		SetHttpClient(srv.Client()).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	realmName := "org-123"
	enabled := true
	err = client.CreateRealm(context.Background(), &KeycloakRealmRepresentation{
		Realm:   &realmName,
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}

	if sawAuth != "Bearer test-access-token" {
		t.Errorf("Authorization %q, want Bearer test-access-token", sawAuth)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["realm"] != realmName {
		t.Errorf("realm field %v", decoded["realm"])
	}
}

func TestKeycloakAdminClient_AdminRequest(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/realms" {
			t.Errorf("path %q", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization header")
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	tokenSource, err := NewStaticTokenSource().
		SetLogger(slog.Default()).
		SetToken(&Token{Access: "tok"}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	client, err := NewKeycloakAdminClient().
		SetLogger(slog.Default()).
		SetBaseURL(srv.URL).
		SetTokenSource(tokenSource).
		SetHttpClient(srv.Client()).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.AdminRequest(context.Background(), http.MethodGet, "/admin/realms", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestKeycloakAdminClient_AdminRequest_rejectsPath(t *testing.T) {
	t.Parallel()

	tokenSource, err := NewStaticTokenSource().
		SetLogger(slog.Default()).
		SetToken(&Token{Access: "x"}).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewKeycloakAdminClient().
		SetLogger(slog.Default()).
		SetBaseURL("https://example.com").
		SetTokenSource(tokenSource).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.AdminRequest(context.Background(), http.MethodGet, "/realms/master", nil)
	if err == nil {
		t.Fatal("expected error for non-admin path")
	}
}

func TestKeycloakAdminConfigFromFlags_EnvFallback(t *testing.T) {
	t.Setenv(EnvKeycloakAdminBaseURL, "https://kc.example")
	t.Setenv(EnvKeycloakAdminTokenIssuer, "https://kc.example/realms/master")
	t.Setenv(EnvKeycloakAdminClientID, "admin-cli")
	t.Setenv(EnvKeycloakAdminClientSecret, "secret")
	t.Setenv(EnvKeycloakAdminScopes, "openid, profile")
	t.Setenv(EnvKeycloakAdminInsecureTLS, "true")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddKeycloakAdminFlags(flags)
	cfg, err := KeycloakAdminConfigFromFlags(flags)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.FullyConfigured() {
		t.Fatal("expected fully configured")
	}
	if cfg.BaseURL != "https://kc.example" {
		t.Errorf("BaseURL %q", cfg.BaseURL)
	}
	if len(cfg.Scopes) != 2 || cfg.Scopes[0] != "openid" || cfg.Scopes[1] != "profile" {
		t.Errorf("scopes %v", cfg.Scopes)
	}
	if !cfg.InsecureTLS {
		t.Error("expected insecure TLS from env")
	}
}
