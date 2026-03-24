package pgvector

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spawn08/chronos/storage"
)

const (
	mockPGVecExecFail   = "pgvector_iter6_exec_fail"
	mockPGVecQueryFail  = "pgvector_iter6_query_fail"
	mockPGVecScanFail   = "pgvector_iter6_scan_fail"
)

func init() {
	sql.Register(mockPGVecExecFail, &vecExecFailDriver{})
	sql.Register(mockPGVecQueryFail, &vecQueryFailDriver{})
	sql.Register(mockPGVecScanFail, &vecScanFailDriver{})
}

type vecExecFailDriver struct{}

func (d *vecExecFailDriver) Open(string) (driver.Conn, error) { return &vecExecFailConn{}, nil }

type vecExecFailConn struct{}

func (c *vecExecFailConn) Prepare(string) (driver.Stmt, error) { return &vecExecFailStmt{}, nil }
func (c *vecExecFailConn) Close() error                         { return nil }
func (c *vecExecFailConn) Begin() (driver.Tx, error)            { return &mockTx{}, nil }

type vecExecFailStmt struct{}

func (s *vecExecFailStmt) Close() error                               { return nil }
func (s *vecExecFailStmt) NumInput() int                              { return -1 }
func (s *vecExecFailStmt) Exec([]driver.Value) (driver.Result, error) { return nil, errors.New("exec fail") }
func (s *vecExecFailStmt) Query([]driver.Value) (driver.Rows, error)  { return &mockRows{}, nil }

type vecQueryFailDriver struct{}

func (d *vecQueryFailDriver) Open(string) (driver.Conn, error) { return &vecQueryFailConn{}, nil }

type vecQueryFailConn struct{}

func (c *vecQueryFailConn) Prepare(string) (driver.Stmt, error) { return &vecQueryFailStmt{}, nil }
func (c *vecQueryFailConn) Close() error                        { return nil }
func (c *vecQueryFailConn) Begin() (driver.Tx, error)           { return &mockTx{}, nil }

type vecQueryFailStmt struct{}

func (s *vecQueryFailStmt) Close() error                               { return nil }
func (s *vecQueryFailStmt) NumInput() int                              { return -1 }
func (s *vecQueryFailStmt) Exec([]driver.Value) (driver.Result, error) { return &mockResult{}, nil }
func (s *vecQueryFailStmt) Query([]driver.Value) (driver.Rows, error)  { return nil, errors.New("query fail") }

type vecScanFailDriver struct{}

func (d *vecScanFailDriver) Open(string) (driver.Conn, error) { return &vecScanFailConn{}, nil }

type vecScanFailConn struct{}

func (c *vecScanFailConn) Prepare(string) (driver.Stmt, error) { return &vecScanFailStmt{}, nil }
func (c *vecScanFailConn) Close() error                        { return nil }
func (c *vecScanFailConn) Begin() (driver.Tx, error)           { return &mockTx{}, nil }

type vecScanFailStmt struct{}

func (s *vecScanFailStmt) Close() error                               { return nil }
func (s *vecScanFailStmt) NumInput() int                              { return -1 }
func (s *vecScanFailStmt) Exec([]driver.Value) (driver.Result, error) { return &mockResult{}, nil }
func (s *vecScanFailStmt) Query([]driver.Value) (driver.Rows, error) {
	return &badScanRows{}, nil
}

type badScanRows struct{ done bool }

func (r *badScanRows) Columns() []string {
	return []string{"id", "embedding", "content", "metadata", "score"}
}
func (r *badScanRows) Close() error { return nil }
func (r *badScanRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = "doc1"
	dest[1] = "[0.1]"
	dest[2] = "c"
	dest[3] = "{}"
	dest[4] = "not-a-float" // cannot scan into *float64
	return nil
}

type boomMeta struct{}

func (boomMeta) MarshalJSON() ([]byte, error) {
	return nil, errors.New("metadata marshal boom")
}

func TestCreateCollection_ExecError_ITER6(t *testing.T) {
	db, err := sql.Open(mockPGVecExecFail, "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := New(db)
	err = st.CreateCollection(context.Background(), "c", 3)
	if err == nil {
		t.Fatal("expected CreateCollection error")
	}
}

func TestSearch_QueryError_ITER6(t *testing.T) {
	db, err := sql.Open(mockPGVecQueryFail, "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := New(db)
	_, err = st.Search(context.Background(), "c", []float32{1, 2}, 5)
	if err == nil {
		t.Fatal("expected Search error")
	}
}

func TestSearch_ScanError_ITER6(t *testing.T) {
	db, err := sql.Open(mockPGVecScanFail, "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := New(db)
	_, err = st.Search(context.Background(), "c", []float32{1}, 5)
	if err == nil {
		t.Fatal("expected Search scan error")
	}
}

func TestUpsert_MetadataMarshalError_ITER6(t *testing.T) {
	db, err := sql.Open(mockDriverName, "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := New(db)
	err = st.Upsert(context.Background(), "c", []storage.Embedding{{
		ID: "e1", Vector: []float32{1},
		Metadata: map[string]any{"x": boomMeta{}},
	}})
	if err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestDelete_ExecError_ITER6(t *testing.T) {
	db, err := sql.Open(mockPGVecExecFail, "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := New(db)
	err = st.Delete(context.Background(), "c", []string{"a", "b"})
	if err == nil {
		t.Fatal("expected Delete error")
	}
}
