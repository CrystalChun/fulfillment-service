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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("ResourceManager", func() {
	var (
		ctrl       *gomock.Controller
		mockClient *MockClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = NewMockClient(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("Builder validation", func() {
		It("should fail when logger is not set", func() {
			_, err := NewResourceManager().
				SetClient(mockClient).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger is mandatory"))
		})

		It("should fail when client is not set", func() {
			_, err := NewResourceManager().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("IdP client is mandatory"))
		})
	})
})
