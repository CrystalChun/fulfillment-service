/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package keycloak

import "fmt"

// Helper functions for building organization group paths following the recommended naming convention.
// Organization groups are scoped per organization, enabling clean hierarchical paths.
//
// Recommended naming convention (Option 1: Simple Hierarchical):
//   - Top-level project: "/Projects/{project-name}/{Viewers|Managers}"
//   - Nested project: "/Projects/{parent-name}/{project-name}/{Viewers|Managers}"
//
// Example usage:
//
//	// For a top-level project "web-app" in organization "acme-corp":
//	viewersPath := BuildProjectGroupPath("web-app", "", "Viewers")
//	// Returns: "/Projects/web-app/Viewers"
//
//	// For a nested project "frontend" under "web-app":
//	managersPath := BuildProjectGroupPath("frontend", "web-app", "Managers")
//	// Returns: "/Projects/web-app/frontend/Managers"

// BuildProjectGroupPath constructs an organization group path for a project.
// Parameters:
//   - projectName: The name of the project
//   - parentProjectName: The name of the parent project (empty string for top-level projects)
//   - role: The role name (typically "Viewers" or "Managers")
//
// Returns a path string suitable for use with CreateAuthorizationGroup.
func BuildProjectGroupPath(projectName, parentProjectName, role string) string {
	if parentProjectName == "" {
		// Top-level project
		return fmt.Sprintf("/Projects/%s/%s", projectName, role)
	}
	// Nested project
	return fmt.Sprintf("/Projects/%s/%s/%s", parentProjectName, projectName, role)
}

// BuildProjectGroupName constructs a group name for display purposes.
// The group name is typically just the role, but this helper exists for consistency.
func BuildProjectGroupName(role string) string {
	return role
}
