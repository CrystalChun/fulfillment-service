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

// KeycloakRealmRepresentation is a subset of the Keycloak Admin API RealmRepresentation JSON (realm lifecycle and settings).
// See: https://www.keycloak.org/docs-api/latest/rest-api/index.html#RealmRepresentation
type KeycloakRealmRepresentation struct {
	Realm                   *string `json:"realm,omitempty"`
	Enabled                 *bool   `json:"enabled,omitempty"`
	DisplayName             *string `json:"displayName,omitempty"`
	DisplayNameHTML         *string `json:"displayNameHtml,omitempty"`
	RegistrationAllowed     *bool   `json:"registrationAllowed,omitempty"`
	LoginWithEmailAllowed   *bool   `json:"loginWithEmailAllowed,omitempty"`
	DuplicateEmailsAllowed  *bool   `json:"duplicateEmailsAllowed,omitempty"`
	ResetPasswordAllowed    *bool   `json:"resetPasswordAllowed,omitempty"`
	EditUsernameAllowed     *bool   `json:"editUsernameAllowed,omitempty"`
	BruteForceProtected     *bool   `json:"bruteForceProtected,omitempty"`
	PermanentLockout        *bool   `json:"permanentLockout,omitempty"`
	SSLRequired             *string `json:"sslRequired,omitempty"`
}
