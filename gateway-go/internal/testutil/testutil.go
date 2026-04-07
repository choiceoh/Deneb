// Package testutil provides shared test helpers.
package testutil

import "testing"

// NoError fails the test immediately if err is non-nil.
func NoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Must returns val if err is nil, otherwise panics.
// Follows the template.Must pattern from the standard library.
// Panics are caught by the test runner and reported as test failures.
func Must[T any](val T, err error) T {
	if err != nil {
		panic("testutil.Must: " + err.Error())
	}
	return val
}
