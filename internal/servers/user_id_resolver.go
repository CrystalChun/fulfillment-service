/*
Copyright (c) 2025 Red Hat Inc.

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
	"fmt"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
)

// privateServerUserIDResolver is an adapter that implements auth.UserIDResolver by delegating
// to the private users server. This follows the same pattern as privateServerHubLookup and
// privateServerCILookup for cross-server lookups.
type privateServerUserIDResolver struct {
	usersServer privatev1.UsersServer
}

// NewPrivateServerUserIDResolver creates a UserIDResolver adapter that delegates to the given
// private users server.
func NewPrivateServerUserIDResolver(usersServer privatev1.UsersServer) auth.UserIDResolver {
	return &privateServerUserIDResolver{usersServer: usersServer}
}

// GetID resolves a username to a user ID by querying the private users server.
func (r *privateServerUserIDResolver) GetID(ctx context.Context, username string) (string, error) {
	filter := fmt.Sprintf("this.spec.username==%q", username)
	limit := int32(1)
	listResponse, err := r.usersServer.List(ctx, privatev1.UsersListRequest_builder{
		Filter: &filter,
		Limit:  &limit,
	}.Build())
	if err != nil {
		return "", fmt.Errorf("failed to list users: %w", err)
	}

	if listResponse.GetSize() == 0 {
		return "", nil
	}

	user := listResponse.GetItems()[0]
	return user.GetId(), nil
}
