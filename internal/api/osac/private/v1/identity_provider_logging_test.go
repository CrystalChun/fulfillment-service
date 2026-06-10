/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package privatev1

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestIdentityProviderHealth_LogValue(t *testing.T) {
	message := "LDAP connection to ldap://internal-server.example.com:389 failed"

	health := &IdentityProviderHealth{
		Status:      IdentityProviderHealthStatus_IDENTITY_PROVIDER_HEALTH_STATUS_UNHEALTHY,
		Message:     &message,
		LastChecked: timestamppb.New(time.Now()),
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("health check", "health", health)

	logOutput := buf.String()

	// Verify internal network details are NOT in logs
	if strings.Contains(logOutput, "internal-server") {
		t.Errorf("internal network details should be redacted: %s", logOutput)
	}

	// Verify status IS in logs
	if !strings.Contains(logOutput, "UNHEALTHY") {
		t.Errorf("status should be present: %s", logOutput)
	}
	if !strings.Contains(logOutput, "has_message") {
		t.Errorf("has_message indicator should be present: %s", logOutput)
	}
}
