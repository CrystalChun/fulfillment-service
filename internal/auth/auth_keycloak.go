/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

// Keycloak Admin API support.
//
// [KeycloakAdminClient] is a small HTTP client for the Keycloak Admin REST API (realms, clients, users, groups,
// identity providers, and other resources). Configure it with [KeycloakAdminConfig], [AddKeycloakAdminFlags], and
// oauth.NewKeycloakAdminClientFromConfig in the oauth package.
//
// Use [KeycloakAdminClient.AdminRequest] for arbitrary endpoints. Convenience methods such as [KeycloakAdminClient.CreateRealm]
// wrap common operations; add more as needed following the same pattern.
//
// See: https://www.keycloak.org/docs-api/latest/rest-api/index.html
package auth
