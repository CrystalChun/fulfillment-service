/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package idp

import (
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/osac-project/fulfillment-service/internal/apiclient"
	"github.com/osac-project/fulfillment-service/internal/auth"
)

// Client is a Keycloak admin client for managing identity provider resources.
//
// Architecture:
// - One Keycloak realm contains all OSAC (default: "osac" realm)
// - OSAC organizations map to Keycloak Organizations within that realm
// - Identity providers are realm-level resources assigned to organizations
type Client struct {
	logger     *slog.Logger
	httpClient *apiclient.Client

	// realmName is the single Keycloak realm that contains all OSAC organizations
	realmName               string
	realmManagementClientID string
}

// ClientBuilder builds a Keycloak client.
type ClientBuilder struct {
	logger      *slog.Logger
	baseURL     string
	tokenSource auth.TokenSource
	caPool      *x509.CertPool
	httpClient  *http.Client
	realmName   string
}

// NewClient creates a builder for a Keycloak admin client.
func NewClient() *ClientBuilder {
	return &ClientBuilder{}
}

// SetLogger sets the logger.
func (b *ClientBuilder) SetLogger(value *slog.Logger) *ClientBuilder {
	b.logger = value
	return b
}

// SetBaseURL sets the base URL of the Keycloak server.
func (b *ClientBuilder) SetBaseURL(value string) *ClientBuilder {
	b.baseURL = value
	return b
}

// SetTokenSource sets the token source for authentication.
func (b *ClientBuilder) SetTokenSource(value auth.TokenSource) *ClientBuilder {
	b.tokenSource = value
	return b
}

// SetRealmName sets the realm name.
// This is the single Keycloak realm that contains all OSAC organizations.
// If not set, the default realm name is "osac".
func (b *ClientBuilder) SetRealmName(value string) *ClientBuilder {
	b.realmName = value
	return b
}

// SetCaPool sets the CA certificate pool.
func (b *ClientBuilder) SetCaPool(value *x509.CertPool) *ClientBuilder {
	b.caPool = value
	return b
}

// SetHTTPClient sets a custom HTTP client.
func (b *ClientBuilder) SetHTTPClient(value *http.Client) *ClientBuilder {
	b.httpClient = value
	return b
}

// Build creates the Keycloak client.
func (b *ClientBuilder) Build() (result *Client, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.baseURL == "" {
		err = errors.New("base URL is mandatory")
		return
	}
	if b.tokenSource == nil {
		err = errors.New("token source is mandatory")
		return
	}
	if b.realmName == "" {
		b.realmName = "osac"
	}

	// Build the underlying HTTP client
	httpClientBuilder := apiclient.NewClient().
		SetLogger(b.logger).
		SetBaseURL(strings.TrimSuffix(b.baseURL, "/")).
		SetTokenSource(b.tokenSource)

	if b.caPool != nil {
		httpClientBuilder = httpClientBuilder.SetCaPool(b.caPool)
	}
	if b.httpClient != nil {
		httpClientBuilder = httpClientBuilder.SetHTTPClient(b.httpClient)
	}

	httpClient, err := httpClientBuilder.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP client: %w", err)
	}

	result = &Client{
		logger:     b.logger,
		httpClient: httpClient,
		realmName:  b.realmName,
	}
	return
}
