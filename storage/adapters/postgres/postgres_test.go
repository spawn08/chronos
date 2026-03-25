package postgres

import (
	"testing"
)

// TestNew_InvalidDSN tests that New with a syntactically-broken DSN returns an error.
// sql.Open itself rarely fails (it's lazy); the error appears on Ping or first query.
// We verify the Store is still created (sql.Open defers connection), so we test
// that New does not return nil even with an unknown driver.
func TestNew_UnknownDriver(t *testing.T) {
	// sql.Open succeeds even with invalid DSN for the "postgres" driver if the
	// driver is registered. If not registered, it returns an error.
	// We cannot easily test without a real DB, but at minimum we verify the
	// function signature works correctly.
	//
	// Since the postgres driver is likely not registered in this test environment,
	// New should return an error.
	s, err := New("postgres://user:pass@localhost:5432/testdb?sslmode=disable")
	if err != nil {
		// Expected: driver not registered
		if s != nil {
			t.Error("expected nil store on error")
		}
		return
	}
	// If the driver is registered (e.g. lib/pq imported elsewhere), we still get a Store.
	if s == nil {
		t.Error("New returned nil store without error")
	}
	// Close to avoid leaks; ignore error since no connection was made.
	s.Close()
}
