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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// KeycloakAdminClient performs authenticated calls to the Keycloak Admin REST API using a bearer token
// (typically a client-credentials / service-account access token).
type KeycloakAdminClient struct {
	logger     *slog.Logger
	baseURL    *url.URL
	httpClient *http.Client
	tokens     TokenSource
	userAgent  string
}

// KeycloakAdminClientBuilder builds a [KeycloakAdminClient].
type KeycloakAdminClientBuilder struct {
	logger     *slog.Logger
	baseURL    string
	httpClient *http.Client
	tokens     TokenSource
	caPool     *x509.CertPool
	insecure   bool
	userAgent  string
}

// NewKeycloakAdminClient returns a new [KeycloakAdminClientBuilder].
func NewKeycloakAdminClient() *KeycloakAdminClientBuilder {
	return &KeycloakAdminClientBuilder{}
}

// SetLogger sets the logger (required).
func (b *KeycloakAdminClientBuilder) SetLogger(value *slog.Logger) *KeycloakAdminClientBuilder {
	b.logger = value
	return b
}

// SetBaseURL sets the Keycloak base URL without a path, for example https://keycloak.example.com:8443 (required).
func (b *KeycloakAdminClientBuilder) SetBaseURL(value string) *KeycloakAdminClientBuilder {
	b.baseURL = value
	return b
}

// SetTokenSource sets the OAuth bearer token source (required).
func (b *KeycloakAdminClientBuilder) SetTokenSource(value TokenSource) *KeycloakAdminClientBuilder {
	b.tokens = value
	return b
}

// SetUserAgent sets the HTTP User-Agent header. Optional.
func (b *KeycloakAdminClientBuilder) SetUserAgent(value string) *KeycloakAdminClientBuilder {
	b.userAgent = value
	return b
}

// SetHttpClient sets a custom HTTP client. Optional; when unset, a client with TLS from SetCaPool / SetInsecureTLS is used.
func (b *KeycloakAdminClientBuilder) SetHttpClient(value *http.Client) *KeycloakAdminClientBuilder {
	b.httpClient = value
	return b
}

// SetCaPool sets trusted CAs for HTTPS. Optional when using SetHttpClient.
func (b *KeycloakAdminClientBuilder) SetCaPool(value *x509.CertPool) *KeycloakAdminClientBuilder {
	b.caPool = value
	return b
}

// SetInsecureTLS skips TLS certificate verification. Optional; not for production use.
func (b *KeycloakAdminClientBuilder) SetInsecureTLS(value bool) *KeycloakAdminClientBuilder {
	b.insecure = value
	return b
}

// Build creates a new [KeycloakAdminClient].
func (b *KeycloakAdminClientBuilder) Build() (result *KeycloakAdminClient, err error) {
	if b.logger == nil {
		return nil, fmt.Errorf("logger is mandatory")
	}
	if strings.TrimSpace(b.baseURL) == "" {
		return nil, fmt.Errorf("base URL is mandatory")
	}
	if b.tokens == nil {
		return nil, fmt.Errorf("token source is mandatory")
	}
	parsed, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(b.baseURL), "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid Keycloak base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("Keycloak base URL must include scheme and host")
	}

	httpClient := b.httpClient
	if httpClient == nil {
		tlsConfig := &tls.Config{}
		if b.insecure {
			tlsConfig.InsecureSkipVerify = true
		} else if b.caPool != nil {
			tlsConfig.RootCAs = b.caPool
		}
		httpClient = &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		}
	}

	result = &KeycloakAdminClient{
		logger:     b.logger,
		baseURL:    parsed,
		httpClient: httpClient,
		tokens:     b.tokens,
		userAgent:  b.userAgent,
	}
	return result, nil
}

// AdminRequest issues an authenticated HTTP request to the Keycloak Admin REST API.
// path must begin with "/admin/" (for example "/admin/realms" or "/admin/realms/{realm}/clients").
// The caller must close resp.Body when err is nil.
func (c *KeycloakAdminClient) AdminRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/admin/") {
		return nil, fmt.Errorf("Keycloak admin path must start with /admin/, got %q", path)
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain access token for Keycloak admin: %w", err)
	}

	endpoint := c.baseURL.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Access)
	if body != nil && methodUsesJSONBody(method) {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Keycloak admin request failed: %w", err)
	}
	return resp, nil
}

func methodUsesJSONBody(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

// CreateRealm calls POST /admin/realms with a JSON [KeycloakRealmRepresentation].
func (c *KeycloakAdminClient) CreateRealm(ctx context.Context, realm *KeycloakRealmRepresentation) error {
	if realm == nil || realm.Realm == nil || strings.TrimSpace(*realm.Realm) == "" {
		return fmt.Errorf("realm representation must include a non-empty realm name")
	}

	payload, err := json.Marshal(realm)
	if err != nil {
		return fmt.Errorf("failed to marshal realm: %w", err)
	}

	resp, err := c.AdminRequest(ctx, http.MethodPost, "/admin/realms", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusNoContent {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return fmt.Errorf("Keycloak admin returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
}
