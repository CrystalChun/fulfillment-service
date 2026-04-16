/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Access Key Crypto", func() {
	Describe("GenerateAccessKeyID", func() {
		It("Generates a valid access key ID", func() {
			keyID, err := GenerateAccessKeyID()
			Expect(err).ToNot(HaveOccurred())
			Expect(keyID).ToNot(BeEmpty())

			// Check format: OSAC + 16 random chars
			Expect(keyID).To(HavePrefix(AccessKeyIDPrefix))
			Expect(len(keyID)).To(Equal(len(AccessKeyIDPrefix) + AccessKeyIDRandomLength))
		})

		It("Generates unique IDs", func() {
			keyID1, err := GenerateAccessKeyID()
			Expect(err).ToNot(HaveOccurred())

			keyID2, err := GenerateAccessKeyID()
			Expect(err).ToNot(HaveOccurred())

			Expect(keyID1).ToNot(Equal(keyID2))
		})

		It("Only contains alphanumeric characters", func() {
			keyID, err := GenerateAccessKeyID()
			Expect(err).ToNot(HaveOccurred())

			// Remove prefix and check remaining chars
			randomPart := strings.TrimPrefix(keyID, AccessKeyIDPrefix)
			for _, char := range randomPart {
				Expect(char).To(SatisfyAny(
					BeNumerically(">=", 'A'),
					BeNumerically("<=", 'Z'),
					BeNumerically(">=", '0'),
					BeNumerically("<=", '9'),
				))
			}
		})
	})

	Describe("GenerateSecretAccessKey", func() {
		It("Generates a valid secret", func() {
			secret, err := GenerateSecretAccessKey()
			Expect(err).ToNot(HaveOccurred())
			Expect(secret).ToNot(BeEmpty())

			// Should be 40 characters (30 bytes base64url encoded)
			Expect(len(secret)).To(Equal(40))
		})

		It("Generates unique secrets", func() {
			secret1, err := GenerateSecretAccessKey()
			Expect(err).ToNot(HaveOccurred())

			secret2, err := GenerateSecretAccessKey()
			Expect(err).ToNot(HaveOccurred())

			Expect(secret1).ToNot(Equal(secret2))
		})

		It("Only contains base64url characters", func() {
			secret, err := GenerateSecretAccessKey()
			Expect(err).ToNot(HaveOccurred())

			// base64url chars: A-Z, a-z, 0-9, -, _
			for _, char := range secret {
				Expect(char).To(SatisfyAny(
					BeNumerically(">=", 'A'),
					BeNumerically("<=", 'Z'),
					BeNumerically(">=", 'a'),
					BeNumerically("<=", 'z'),
					BeNumerically(">=", '0'),
					BeNumerically("<=", '9'),
					Equal('-'),
					Equal('_'),
				))
			}
		})
	})

	Describe("HashSecretAccessKey", func() {
		It("Hashes a secret successfully", func() {
			secret := "test-secret-123"
			hash, err := HashSecretAccessKey(secret)
			Expect(err).ToNot(HaveOccurred())
			Expect(hash).ToNot(BeEmpty())

			// Bcrypt hashes start with $2a$ or $2b$
			Expect(hash).To(MatchRegexp(`^\$2[ab]\$`))
		})

		It("Generates different hashes for the same secret", func() {
			secret := "test-secret-123"
			hash1, err := HashSecretAccessKey(secret)
			Expect(err).ToNot(HaveOccurred())

			hash2, err := HashSecretAccessKey(secret)
			Expect(err).ToNot(HaveOccurred())

			// Bcrypt includes random salt, so hashes differ
			Expect(hash1).ToNot(Equal(hash2))
		})
	})

	Describe("VerifySecretAccessKey", func() {
		It("Verifies a correct secret", func() {
			secret := "test-secret-456"
			hash, err := HashSecretAccessKey(secret)
			Expect(err).ToNot(HaveOccurred())

			isValid := VerifySecretAccessKey(secret, hash)
			Expect(isValid).To(BeTrue())
		})

		It("Rejects an incorrect secret", func() {
			secret := "test-secret-456"
			hash, err := HashSecretAccessKey(secret)
			Expect(err).ToNot(HaveOccurred())

			isValid := VerifySecretAccessKey("wrong-secret", hash)
			Expect(isValid).To(BeFalse())
		})

		It("Rejects an invalid hash", func() {
			isValid := VerifySecretAccessKey("test-secret", "invalid-hash")
			Expect(isValid).To(BeFalse())
		})
	})

	Describe("GenerateAccessKeyCredentials", func() {
		It("Generates complete credentials", func() {
			accessKeyID, secret, hash, err := GenerateAccessKeyCredentials()
			Expect(err).ToNot(HaveOccurred())

			// Check access key ID format
			Expect(accessKeyID).To(HavePrefix(AccessKeyIDPrefix))
			Expect(len(accessKeyID)).To(Equal(len(AccessKeyIDPrefix) + AccessKeyIDRandomLength))

			// Check secret length
			Expect(len(secret)).To(Equal(40))

			// Check hash is valid
			Expect(hash).To(MatchRegexp(`^\$2[ab]\$`))

			// Verify secret matches hash
			isValid := VerifySecretAccessKey(secret, hash)
			Expect(isValid).To(BeTrue())
		})

		It("Generates unique credentials each time", func() {
			keyID1, secret1, hash1, err := GenerateAccessKeyCredentials()
			Expect(err).ToNot(HaveOccurred())

			keyID2, secret2, hash2, err := GenerateAccessKeyCredentials()
			Expect(err).ToNot(HaveOccurred())

			Expect(keyID1).ToNot(Equal(keyID2))
			Expect(secret1).ToNot(Equal(secret2))
			Expect(hash1).ToNot(Equal(hash2))
		})
	})
})
