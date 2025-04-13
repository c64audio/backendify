package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseArgs_ValidKeyValuePairs(t *testing.T) {
	// Save original args to restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Arrange: Set up test args
	os.Args = []string{"cmd", "us=http://localhost:9001", "uk=http://localhost:9002"}

	// Act: Call ParseArgs
	argsMap := ParseArgs()

	// Assert: Check that the map contains the expected key-value pairs
	assert.Equal(t, "http://localhost:9001", argsMap["us"], "Expected name to be 'http://localhost:9001'")
	assert.Equal(t, "http://localhost:9002", argsMap["uk"], "Expected port to be 'http://localhost:9002'")
	assert.Len(t, argsMap, 2, "Expected map to contain exactly 2 entries")
}

func TestParseArgs_EmptyArgs(t *testing.T) {
	// Save original args to restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Arrange: Set up empty args (just the program name)
	os.Args = []string{"cmd"}

	// Act: Call ParseArgs
	argsMap := ParseArgs()

	// Assert: Check that the map is empty
	assert.Empty(t, argsMap, "Expected empty map when no args provided")
}

func TestParseArgs_MixedValidAndInvalidArgs(t *testing.T) {
	// Save original args to restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Arrange: Set up args with both valid and invalid formats
	os.Args = []string{"cmd", "name=TestApp", "invalid-arg", "port=8080", "=empty-key", "no-value="}

	// Act: Call ParseArgs
	argsMap := ParseArgs()

	// Assert: Check that only valid key-value pairs are in the map
	assert.Equal(t, "TestApp", argsMap["name"], "Expected name to be 'TestApp'")
	assert.Equal(t, "8080", argsMap["port"], "Expected port to be '8080'")
	assert.Equal(t, "", argsMap["no-value"], "Expected 'no-value' to be empty string")
	assert.NotContains(t, argsMap, "invalid-arg", "Invalid arg should be excluded")
	assert.Len(t, argsMap, 2, "Expected map to contain exactly 2 entries")
}

func TestParseArgs_DuplicateKeys(t *testing.T) {
	// Save original args to restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Arrange: Set up args with duplicate keys
	os.Args = []string{"cmd", "name=TestApp", "name=OverwrittenApp"}

	// Act: Call ParseArgs
	argsMap := ParseArgs()

	// Assert: Check that later values overwrite earlier ones for the same key
	assert.Equal(t, "OverwrittenApp", argsMap["name"], "Expected name to be 'OverwrittenApp' (last value)")
	assert.Len(t, argsMap, 1, "Expected map to contain exactly 1 entry")
}

func TestParseArgs_ValuesWithEquals(t *testing.T) {
	// Save original args to restore after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Arrange: Set up args with equals signs in the values
	os.Args = []string{"cmd", "equation=1+1=2", "url=http://example.com?key=value"}

	// Act: Call ParseArgs
	argsMap := ParseArgs()

	// Assert: Check that values with equals are correctly parsed (only first = is used as separator)
	assert.Equal(t, "1+1=2", argsMap["equation"], "Expected equation to be '1+1=2'")
	assert.Equal(t, "http://example.com?key=value", argsMap["url"], "Expected URL to be preserved")
	assert.Len(t, argsMap, 2, "Expected map to contain exactly 2 entries")
}
