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
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// AccessKeyIDPrefix is the prefix for all access key IDs
	AccessKeyIDPrefix = "OSAC"

	// AccessKeyIDRandomLength is the number of random characters after the prefix
	AccessKeyIDRandomLength = 16

	// SecretAccessKeyLength is the number of random bytes for the secret (will be base64 encoded)
	SecretAccessKeyLength = 30 // Results in 40 base64 characters
)

// GenerateAccessKeyID generates a new access key ID.
// Format: OSAC{16 random alphanumeric characters}
// Example: OSACAK7X2P9Q4M5N8R1T
func GenerateAccessKeyID() (string, error) {
	// Generate random bytes
	randomBytes := make([]byte, AccessKeyIDRandomLength)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Convert to alphanumeric characters (A-Z, 0-9)
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	randomChars := make([]byte, AccessKeyIDRandomLength)
	for i, b := range randomBytes {
		randomChars[i] = chars[int(b)%len(chars)]
	}

	return AccessKeyIDPrefix + string(randomChars), nil
}

// GenerateSecretAccessKey generates a new secret access key.
// Returns 40 base64url-encoded random characters.
func GenerateSecretAccessKey() (string, error) {
	// Generate random bytes
	randomBytes := make([]byte, SecretAccessKeyLength)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Encode to base64url (no padding, URL-safe)
	secret := base64.RawURLEncoding.EncodeToString(randomBytes)
	return secret, nil
}

// HashSecretAccessKey hashes a secret access key using bcrypt.
// The hash is stored in the database instead of the plaintext secret.
func HashSecretAccessKey(secret string) (string, error) {
	// Use bcrypt cost 10 (default)
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash secret: %w", err)
	}
	return string(hash), nil
}

// VerifySecretAccessKey verifies a secret access key against its hash.
// Returns true if the secret matches the hash.
func VerifySecretAccessKey(secret, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret))
	return err == nil
}

// GenerateAccessKeyCredentials generates both an access key ID and secret.
// Returns: (accessKeyID, secretAccessKey, secretHash, error)
func GenerateAccessKeyCredentials() (string, string, string, error) {
	// Generate access key ID
	accessKeyID, err := GenerateAccessKeyID()
	if err != nil {
		return "", "", "", err
	}

	// Generate secret access key
	secretAccessKey, err := GenerateSecretAccessKey()
	if err != nil {
		return "", "", "", err
	}

	// Hash the secret for storage
	secretHash, err := HashSecretAccessKey(secretAccessKey)
	if err != nil {
		return "", "", "", err
	}

	return accessKeyID, secretAccessKey, secretHash, nil
}
