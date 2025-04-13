package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCountryCode_ValidCodes(t *testing.T) {
	// Define a list of valid country codes to test
	validCodes := []string{"US", "GB", "CA", "FR", "DE", "IN", "AU", "JP", "CN", "BR"}

	// Assert that each of these codes is valid
	for _, code := range validCodes {
		assert.True(t, ValidateCountryCode(code), "Expected %s to be a valid country code", code)
	}
}

func TestValidateCountryCode_InvalidCodes(t *testing.T) {
	// Define a list of invalid country codes to test
	invalidCodes := []string{"ZZ", "XX", "AAA", "123", "", "U"} // Invalid codes, overly long codes, empty strings, etc.

	// Assert that each of these codes is NOT valid
	for _, code := range invalidCodes {
		assert.False(t, ValidateCountryCode(code), "Expected %s to be an invalid country code", code)
	}
}
