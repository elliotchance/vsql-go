package vsql_test

import (
	"database/sql"
	"errors"
	"math/big"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/elliotchance/vsql-go/vsql"
)

func TestOpenError(t *testing.T) {
	db, err := sql.Open("vsql", filepath.Join(t.TempDir(), "missing", "test.vsql"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var e *vsql.Error
	if err := db.Ping(); !errors.As(err, &e) || !strings.Contains(e.Error(), "SQLSTATE") {
		t.Errorf("got %v", err)
	}
}

func TestStatementErrors(t *testing.T) {
	db := openMemory(t)

	if _, err := db.Exec("SELEC ?", 1); err == nil {
		t.Error("a syntax error with a parameter worked")
	}

	stmt, err := db.Prepare("VALUES CAST(? AS INTEGER)")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if _, err := stmt.Exec(sql.Named("a", 1)); err == nil {
		t.Error("Exec with a named parameter worked")
	}
	if _, err := stmt.Query(sql.Named("a", 1)); err == nil {
		t.Error("Query with a named parameter worked")
	}
	if _, err := stmt.Exec("x"); err == nil {
		t.Error("Exec cast 'x' to an INTEGER")
	}
	if _, err := stmt.Query("x"); err == nil {
		t.Error("Query cast 'x' to an INTEGER")
	}
}

func TestNilBigNumbers(t *testing.T) {
	db := openMemory(t)
	for _, arg := range []any{(*big.Int)(nil), (*big.Rat)(nil)} {
		var got sql.NullString
		if err := db.QueryRow("VALUES CAST(? AS NUMERIC(5, 2))", arg).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got.Valid {
			t.Errorf("%T: got %q", arg, got.String)
		}
	}
}

func TestLongNames(t *testing.T) {
	db := openMemory(t)
	// An identifier is at most 128 characters, but these are 256 octets,
	// which is longer than the buffers that are first used to read them.
	name := strings.Repeat("é", 128)

	_, err := db.Exec(`SELECT * FROM "` + name + `"`)
	var e *vsql.Error
	if !errors.As(err, &e) || !strings.Contains(e.Message, name) || e.TableName != name {
		t.Errorf("got %v", err)
	}

	// A message that's longer than its buffer.
	long := strings.Repeat("X", 600)
	if _, err := db.Exec("SET SCHEMA '" + long + "'"); err == nil || !strings.Contains(err.Error(), long) {
		t.Errorf("got %v", err)
	}

	rows, err := db.Query(`SELECT 1 AS "` + name + `" FROM (VALUES 1) AS t (x)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if columns, err := rows.Columns(); err != nil || len(columns) != 1 || columns[0] != name {
		t.Errorf("got %v (%v)", columns, err)
	}
}

func TestNotNullScanTypes(t *testing.T) {
	db := openMemory(t)
	exec(t, db, "CREATE TABLE foo (t TIMESTAMP NOT NULL, s VARCHAR(10) NOT NULL)")
	rows, err := db.Query("SELECT t, s FROM foo")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if got := types[0].ScanType(); got != reflect.TypeOf(time.Time{}) {
		t.Errorf("TIMESTAMP NOT NULL: %v", got)
	}
	if got := types[1].ScanType(); got != reflect.TypeOf("") {
		t.Errorf("VARCHAR NOT NULL: %v", got)
	}
}
