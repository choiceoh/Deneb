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
