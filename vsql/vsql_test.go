package vsql_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elliotchance/vsql-go/vsql"
)

// openMemory is a single connection to a new in-memory database.
func openMemory(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("vsql", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	return db
}

// openFile is a new database file, which can have many connections.
func openFile(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("vsql", filepath.Join(t.TempDir(), "test.vsql"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func sqlState(err error) vsql.SQLState {
	var e *vsql.Error
	if errors.As(err, &e) {
		return e.SQLState
	}

	return ""
}

func TestColumnValues(t *testing.T) {
	db := openMemory(t)
	plus0530 := time.FixedZone("", 5*3600+1800)

	for _, test := range []struct {
		expr     string
		want     any
		typeName string
		scanType any
	}{
		{"CAST(12 AS SMALLINT)", int64(12), "SMALLINT", sql.NullInt16{}},
		{"CAST(-12 AS INTEGER)", int64(-12), "INTEGER", sql.NullInt32{}},
		{"CAST(9223372036854775807 AS BIGINT)", int64(9223372036854775807), "BIGINT", sql.NullInt64{}},
		{"CAST(1.5 AS REAL)", float64(1.5), "REAL", sql.Null[float32]{}},
		{"CAST(1.25 AS DOUBLE PRECISION)", 1.25, "DOUBLE PRECISION", sql.NullFloat64{}},
		{"CAST(1.5 AS DECIMAL(10, 3))", "1.500", "DECIMAL", sql.NullString{}},
		{"CAST(12 AS NUMERIC(10, 2))", "12.00", "NUMERIC", sql.NullString{}},
		{"CAST('ab' AS CHARACTER(4))", "ab  ", "CHARACTER", sql.NullString{}},
		{"CAST('héllo' AS VARCHAR(10))", "héllo", "CHARACTER VARYING", sql.NullString{}},
		{"TRUE", true, "BOOLEAN", sql.NullBool{}},
		{"FALSE", false, "BOOLEAN", sql.NullBool{}},
		{"DATE '2022-01-02'", time.Date(2022, 1, 2, 0, 0, 0, 0, time.UTC), "DATE", sql.NullTime{}},
		{"TIME '15:04:05.123456'", time.Date(0, 1, 1, 15, 4, 5, 123456000, time.UTC), "TIME", sql.NullTime{}},
		{"TIME '15:04:05+05:30'", time.Date(0, 1, 1, 15, 4, 5, 0, plus0530), "TIME WITH TIME ZONE", sql.NullTime{}},
		{"TIMESTAMP '2022-01-02 15:04:05.5'", time.Date(2022, 1, 2, 15, 4, 5, 500000000, time.UTC), "TIMESTAMP", sql.NullTime{}},
		{"TIMESTAMP '2022-01-02 15:04:05.123+05:30'", time.Date(2022, 1, 2, 15, 4, 5, 123000000, plus0530), "TIMESTAMP WITH TIME ZONE", sql.NullTime{}},
		{"CAST(NULL AS INTEGER)", nil, "INTEGER", sql.NullInt32{}},
		{"CAST(NULL AS BOOLEAN)", nil, "BOOLEAN", sql.NullBool{}},
		{"CAST(NULL AS DATE)", nil, "DATE", sql.NullTime{}},
		{"CAST(1.25 AS DECFLOAT)", "1.25", "DECFLOAT", sql.NullString{}},
		{"CAST('large' AS CHARACTER LARGE OBJECT)", "large", "CHARACTER LARGE OBJECT", sql.NullString{}},
		{"CAST(X'0102' AS BINARY(3))", []byte{1, 2, 0}, "BINARY", []byte(nil)},
		{"CAST(X'00ff' AS VARBINARY(10))", []byte{0, 0xff}, "BINARY VARYING", []byte(nil)},
		{"CAST(X'' AS VARBINARY(10))", []byte{}, "BINARY VARYING", []byte(nil)},
		{"CAST(X'abcdef' AS BLOB)", []byte{0xab, 0xcd, 0xef}, "BINARY LARGE OBJECT", []byte(nil)},
		{"CAST(NULL AS BLOB)", nil, "BINARY LARGE OBJECT", []byte(nil)},
		{"INTERVAL '1-2' YEAR TO MONTH", "1-02", "INTERVAL YEAR TO MONTH", sql.NullString{}},
		{"INTERVAL '-3' DAY", "-3", "INTERVAL DAY", sql.NullString{}},
		{"INTERVAL '3 04:05:06.5' DAY TO SECOND(3)", "3 04:05:06.500", "INTERVAL DAY TO SECOND", sql.NullString{}},
		{"INTERVAL '90' MINUTE", "90", "INTERVAL MINUTE", sql.NullString{}},
	} {
		t.Run(test.expr, func(t *testing.T) {
			rows, err := db.Query("VALUES " + test.expr)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()

			types, err := rows.ColumnTypes()
			if err != nil {
				t.Fatal(err)
			}
			if got := types[0].DatabaseTypeName(); got != test.typeName {
				t.Errorf("DatabaseTypeName is %s, want %s", got, test.typeName)
			}
			if got, want := types[0].ScanType(), reflect.TypeOf(test.scanType); got != want {
				t.Errorf("ScanType is %s, want %s", got, want)
			}

			if !rows.Next() {
				t.Fatal(rows.Err())
			}
			var got any
			if err := rows.Scan(&got); err != nil {
				t.Fatal(err)
			}
			if gotTime, ok := got.(time.Time); ok {
				want, _ := test.want.(time.Time)
				_, gotOffset := gotTime.Zone()
				_, wantOffset := want.Zone()
				if !gotTime.Equal(want) || gotOffset != wantOffset {
					t.Errorf("got %v, want %v", gotTime, want)
				}
			} else if !reflect.DeepEqual(got, test.want) {
				t.Errorf("got %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestColumnTypes(t *testing.T) {
	db := openMemory(t)
	exec(t, db, `CREATE TABLE foo (a INTEGER NOT NULL, b VARCHAR(20), c NUMERIC(10, 3),
		d VARBINARY(16), e BINARY(4), f BLOB(1000), g BLOB, h CLOB(100), i CLOB)`)

	rows, err := db.Query("SELECT * FROM foo")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}

	if names := fmt.Sprint(rows.Columns()); names != "[A B C D E F G H I] <nil>" {
		t.Errorf("columns are %s", names)
	}
	if nullable, ok := types[0].Nullable(); nullable || !ok {
		t.Errorf("A is nullable (%v, %v)", nullable, ok)
	}
	if scanType := types[0].ScanType(); scanType != reflect.TypeOf(int32(0)) {
		t.Errorf("A has ScanType %s", scanType)
	}
	if nullable, ok := types[1].Nullable(); !nullable || !ok {
		t.Errorf("B isn't nullable (%v, %v)", nullable, ok)
	}
	if length, ok := types[1].Length(); length != 20 || !ok {
		t.Errorf("B has length %d (%v)", length, ok)
	}
	if precision, scale, ok := types[2].DecimalSize(); precision != 10 || scale != 3 || !ok {
		t.Errorf("C has precision %d and scale %d (%v)", precision, scale, ok)
	}
	for i, want := range []int64{16, 4, 1000, math.MaxInt64, 100, math.MaxInt64} {
		if length, ok := types[3+i].Length(); length != want || !ok {
			t.Errorf("%s has length %d (%v), want %d", types[3+i].Name(), length, ok, want)
		}
	}
	if scanType := types[3].ScanType(); scanType != reflect.TypeOf([]byte(nil)) {
		t.Errorf("D has ScanType %s", scanType)
	}
}

func TestParameters(t *testing.T) {
	db := openMemory(t)
	exec(t, db, `CREATE TABLE foo (
		s SMALLINT, i INTEGER, b BIGINT, r REAL, d DOUBLE PRECISION,
		n NUMERIC(20, 5), c VARCHAR(100), bo BOOLEAN,
		ts TIMESTAMP(6), tz TIMESTAMP(6) WITH TIME ZONE, dt DATE,
		vb VARBINARY(100), bl BLOB, cl CLOB, df DECFLOAT,
		ym INTERVAL YEAR TO MONTH, ds INTERVAL DAY TO SECOND(6))`)
	ts := time.Date(2022, 1, 2, 15, 4, 5, 123456000, time.FixedZone("", -4*3600))

	for _, test := range []struct {
		column string
		arg    any
		want   any
	}{
		{"s", 12, int64(12)},
		{"i", int32(-12), int64(-12)},
		{"b", int64(9223372036854775807), int64(9223372036854775807)},
		{"r", float32(1.5), float64(1.5)},
		{"d", 1.25, 1.25},
		{"n", 7, "7.00000"},
		{"n", 1.5, "1.50000"},
		{"n", big.NewInt(123), "123.00000"},
		{"n", big.NewRat(-5, 4), "-1.25000"},
		{"c", "héllo", "héllo"},
		{"c", "", ""},
		{"c", []byte("bytes"), "bytes"},
		{"bo", true, true},
		{"bo", false, false},
		{"ts", ts, time.Date(2022, 1, 2, 19, 4, 5, 123456000, time.UTC)},
		{"dt", ts, time.Date(2022, 1, 2, 0, 0, 0, 0, time.UTC)},
		{"tz", ts, ts},
		{"i", nil, nil},
		{"c", nil, nil},
		{"bo", nil, nil},
		{"dt", nil, nil},
		{"vb", []byte{0, 1, 0xff}, []byte{0, 1, 0xff}},
		{"vb", []byte{}, []byte{}},
		{"vb", []byte(nil), nil},
		{"bl", []byte("blob"), []byte("blob")},
		{"cl", "clob", "clob"},
		{"cl", []byte("clob"), "clob"},
		{"df", "1.25", "1.25"},
		{"df", big.NewRat(-5, 4), "-1.25"},
		{"ym", "1-2", "1-02"},
		{"ds", "3 04:05:06.5", "3 04:05:06.500000"},
	} {
		t.Run(fmt.Sprintf("%s %#v", test.column, test.arg), func(t *testing.T) {
			exec(t, db, "DELETE FROM foo")
			exec(t, db, fmt.Sprintf("INSERT INTO foo (%s) VALUES (?)", test.column), test.arg)

			var got any
			if err := db.QueryRow("SELECT " + test.column + " FROM foo").Scan(&got); err != nil {
				t.Fatal(err)
			}
			if gotTime, ok := got.(time.Time); ok {
				want, _ := test.want.(time.Time)
				if !gotTime.Equal(want) {
					t.Errorf("got %v, want %v", gotTime, want)
				}
			} else if !reflect.DeepEqual(got, test.want) {
				t.Errorf("got %#v, want %#v", got, test.want)
			}
		})
	}

	t.Run("TIME", func(t *testing.T) {
		exec(t, db, "CREATE TABLE times (t TIME(6))")
		exec(t, db, "INSERT INTO times (t) VALUES (?)", ts)
		var got time.Time
		if err := db.QueryRow("SELECT t FROM times").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := time.Date(0, 1, 1, 19, 4, 5, 123456000, time.UTC); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("CAST(? AS DATE)", func(t *testing.T) {
		exec(t, db, "DELETE FROM foo")
		exec(t, db, "INSERT INTO foo (dt) VALUES (CAST(? AS DATE))", ts)
		var got time.Time
		if err := db.QueryRow("SELECT dt FROM foo").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := time.Date(2022, 1, 2, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("WHERE", func(t *testing.T) {
		exec(t, db, "DELETE FROM foo")
		exec(t, db, "INSERT INTO foo (i, c) VALUES (?, ?)", 1, "a")
		exec(t, db, "INSERT INTO foo (i, c) VALUES (?, ?)", 2, "b")
		var c string
		if err := db.QueryRow("SELECT c FROM foo WHERE i = ?", 2).Scan(&c); err != nil {
			t.Fatal(err)
		}
		if c != "b" {
			t.Errorf("got %q", c)
		}
	})

	t.Run("WHERE binary", func(t *testing.T) {
		exec(t, db, "DELETE FROM foo")
		exec(t, db, "INSERT INTO foo (i, vb, c) VALUES (?, ?, ?)", 1, []byte{1}, "a")
		exec(t, db, "INSERT INTO foo (i, vb, c) VALUES (?, ?, ?)", 2, []byte{2}, "b")
		var i int
		if err := db.QueryRow("SELECT i FROM foo WHERE vb = ?", []byte{2}).Scan(&i); err != nil {
			t.Fatal(err)
		}
		if i != 2 {
			t.Errorf("got %d", i)
		}
		// []byte is a string for a character parameter.
		if err := db.QueryRow("SELECT i FROM foo WHERE c = ?", []byte("a")).Scan(&i); err != nil {
			t.Fatal(err)
		}
		if i != 1 {
			t.Errorf("got %d", i)
		}
	})

	t.Run("binary type mismatch", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO foo (vb) VALUES (?)", "not bytes")
		if state := sqlState(err); state != vsql.RestrictedDataTypeAttributeViolationError {
			t.Errorf("got %v", err)
		}
	})

	t.Run("number of parameters", func(t *testing.T) {
		stmt, err := db.Prepare("INSERT INTO foo (i, c) VALUES (?, ?)")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		_, err = stmt.Exec(1)
		if err == nil || !strings.Contains(err.Error(), "expected 2 arguments, got 1") {
			t.Errorf("got %v", err)
		}
		if _, err := stmt.Exec(1, "a"); err != nil {
			t.Error(err)
		}
	})

	t.Run("named", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO foo (i) VALUES (?)", sql.Named("i", 1))
		if err == nil || !strings.Contains(err.Error(), "named parameters") {
			t.Errorf("got %v", err)
		}
	})

	t.Run("inexact *big.Rat", func(t *testing.T) {
		_, err := db.Exec("INSERT INTO foo (n) VALUES (?)", big.NewRat(1, 3))
		if err == nil || !strings.Contains(err.Error(), "finite decimal") {
			t.Errorf("got %v", err)
		}
	})
}

func TestScanTypes(t *testing.T) {
	db := openMemory(t)

	var i16 int16
	var f32 float32
	var f64 float64
	var s string
	err := db.QueryRow("VALUES ROW(CAST(12 AS SMALLINT), CAST(1.23 AS REAL), CAST(1.5 AS NUMERIC(5, 2)), CAST(1.5 AS NUMERIC(5, 2)))").
		Scan(&i16, &f32, &f64, &s)
	if err != nil {
		t.Fatal(err)
	}
	if i16 != 12 || f32 != 1.23 || f64 != 1.5 || s != "1.50" {
		t.Errorf("got %v, %v, %v, %q", i16, f32, f64, s)
	}

	var big int16
	err = db.QueryRow("VALUES CAST(100000 AS INTEGER)").Scan(&big)
	if err == nil {
		t.Errorf("100000 was scanned into an int16 (%d)", big)
	}
}

func TestLongStrings(t *testing.T) {
	db := openMemory(t)
	exec(t, db, "CREATE TABLE foo (s VARCHAR(10000))")

	for _, n := range []int{255, 256, 257, 1000, 9999} {
		// Multi-byte characters cross the boundary of each piece that
		// SQLGetData reads.
		want := strings.Repeat("é", n/2) + strings.Repeat("x", n%2)
		exec(t, db, "DELETE FROM foo")
		exec(t, db, "INSERT INTO foo (s) VALUES (?)", want)

		var got string
		if err := db.QueryRow("SELECT s FROM foo").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%d: got %d octets, want %d", n, len(got), len(want))
		}
	}
}

func TestLongStringsWithNUL(t *testing.T) {
	db := openMemory(t)

	// A NUL in the first piece that SQLGetData reads, and in a later one.
	for _, want := range []string{
		strings.Repeat("x", 100) + "\x00" + strings.Repeat("y", 300),
		strings.Repeat("x", 300) + "\x00" + strings.Repeat("y", 300),
		"\x00" + strings.Repeat("é", 200) + "\x00",
	} {
		var got string
		if err := db.QueryRow("VALUES CAST(? AS VARCHAR(1000))", want).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestLongBinary(t *testing.T) {
	db := openMemory(t)
	exec(t, db, "CREATE TABLE foo (b BLOB)")

	for _, n := range []int{255, 256, 257, 1000, 100000} {
		want := make([]byte, n)
		for i := range want {
			want[i] = byte(i * 7)
		}
		exec(t, db, "DELETE FROM foo")
		exec(t, db, "INSERT INTO foo (b) VALUES (?)", want)

		var got []byte
		if err := db.QueryRow("SELECT b FROM foo").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%d: got %d octets", n, len(got))
		}
	}
}

func TestErrorFields(t *testing.T) {
	db := openMemory(t)
	exec(t, db, `CREATE TABLE foo (id INTEGER PRIMARY KEY,
		x INTEGER CONSTRAINT x_positive CHECK (x > 0))`)
	exec(t, db, "INSERT INTO foo (id, x) VALUES (1, 1)")

	vsqlError := func(err error) *vsql.Error {
		t.Helper()
		var e *vsql.Error
		if !errors.As(err, &e) {
			t.Fatalf("got %v", err)
		}
		return e
	}

	_, err := db.Exec("INSERT INTO foo (id, x) VALUES (1, 2)")
	e := vsqlError(err)
	if e.SQLState != vsql.UniqueViolationError || e.ConstraintSchema != "PUBLIC" ||
		e.ConstraintName != "FOO_PRIMARY_KEY" || e.SchemaName != "PUBLIC" || e.TableName != "FOO" {
		t.Errorf("got %#v", e)
	}

	_, err = db.Exec("INSERT INTO foo (id, x) VALUES (2, -1)")
	e = vsqlError(err)
	if e.SQLState != vsql.CheckViolationError || e.ConstraintName != "X_POSITIVE" || e.TableName != "FOO" {
		t.Errorf("got %#v", e)
	}

	_, err = db.Exec("SELECT * FROM bar")
	e = vsqlError(err)
	if e.TableName != "BAR" || e.ConstraintName != "" {
		t.Errorf("got %#v", e)
	}

	_, err = db.Exec("SELECT y FROM foo")
	e = vsqlError(err)
	if e.ColumnName != "Y" || e.TableName != "" {
		t.Errorf("got %#v", e)
	}
}

func TestBuild(t *testing.T) {
	info, err := vsql.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Version, "v1.") || len(info.Commit) != 40 || len(info.Date) != 10 ||
		info.VVersion == "" || info.CCompiler == "" || info.OS == "" || info.Machine == "" {
		t.Errorf("got %#v", info)
	}
}

func TestErrors(t *testing.T) {
	db := openMemory(t)

	_, err := db.Exec("SELECT * FROM nope")
	if state := sqlState(err); state != vsql.UndefinedTableError {
		t.Errorf("got %v", err)
	}

	_, err = db.Query("SELEC 1")
	if state := sqlState(err); state != vsql.SyntaxError {
		t.Errorf("got %v", err)
	}

	_, err = db.Prepare("SELEC 1")
	if state := sqlState(err); state != vsql.SyntaxError {
		t.Errorf("got %v", err)
	}

	// Transactions must use BeginTx.
	_, err = db.Exec("COMMIT")
	if state := sqlState(err); state != vsql.SyntaxErrorOrAccessRuleViolationError {
		t.Errorf("got %v", err)
	}

	// The connection still works after an error.
	exec(t, db, "VALUES 1")
}

func TestExecResult(t *testing.T) {
	db := openMemory(t)
	exec(t, db, "CREATE TABLE foo (x INTEGER)")
	exec(t, db, "INSERT INTO foo (x) VALUES (1)")
	exec(t, db, "INSERT INTO foo (x) VALUES (2)")

	result, err := db.Exec("UPDATE foo SET x = x + 1")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); n != 2 || err != nil {
		t.Errorf("RowsAffected is %d (%v)", n, err)
	}
	if _, err := result.LastInsertId(); err == nil {
		t.Error("LastInsertId isn't an error")
	}

	// Exec with a query closes its cursor.
	exec(t, db, "SELECT * FROM foo")
	exec(t, db, "SELECT * FROM foo WHERE x = ?", 2)
	exec(t, db, "DROP TABLE foo")
}

func TestPreparedStatements(t *testing.T) {
	db := openMemory(t)
	exec(t, db, "CREATE TABLE foo (x INTEGER)")

	insert, err := db.Prepare("INSERT INTO foo (x) VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10; i++ {
		if _, err := insert.Exec(i); err != nil {
			t.Fatal(err)
		}
	}
	if err := insert.Close(); err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	query, err := tx.Prepare("SELECT x FROM foo WHERE x > ?")
	if err != nil {
		t.Fatal(err)
	}

	// The statement is executed again while the rows of the first execution
	// are still open, and then closed before them.
	rows1, err := query.Query(5)
	if err != nil {
		t.Fatal(err)
	}
	rows2, err := query.Query(8)
	if err != nil {
		t.Fatal(err)
	}
	if err := query.Close(); err != nil {
		t.Fatal(err)
	}

	count := func(rows *sql.Rows) int {
		n := 0
		for rows.Next() {
			n++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(rows2); n != 2 {
		t.Errorf("got %d rows, want 2", n)
	}
	if n := count(rows1); n != 5 {
		t.Errorf("got %d rows, want 5", n)
	}
}

func TestTransactions(t *testing.T) {
	db := openFile(t)
	exec(t, db, "CREATE TABLE foo (x INTEGER)")

	count := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM foo").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("autocommit", func(t *testing.T) {
		exec(t, db, "INSERT INTO foo (x) VALUES (1)")
		// Another connection sees it.
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var n int
		if err := conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM foo").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("got %d rows", n)
		}
	})

	t.Run("commit", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("INSERT INTO foo (x) VALUES (2)"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if n := count(); n != 2 {
			t.Errorf("got %d rows", n)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("INSERT INTO foo (x) VALUES (3)"); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := tx.QueryRow("SELECT COUNT(*) FROM foo").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Errorf("got %d rows in the transaction", n)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := count(); n != 2 {
			t.Errorf("got %d rows", n)
		}
	})

	t.Run("isolation levels", func(t *testing.T) {
		for _, opts := range []sql.TxOptions{
			{Isolation: sql.LevelDefault},
			{Isolation: sql.LevelReadUncommitted},
			{Isolation: sql.LevelReadCommitted},
			{Isolation: sql.LevelRepeatableRead},
			{Isolation: sql.LevelSerializable},
			{ReadOnly: true},
		} {
			tx, err := db.BeginTx(context.Background(), &opts)
			if err != nil {
				t.Errorf("%+v: %v", opts, err)
				continue
			}
			_, err = tx.Exec("INSERT INTO foo (x) VALUES (1)")
			// READ UNCOMMITTED is always READ ONLY.
			readOnly := opts.ReadOnly || opts.Isolation == sql.LevelReadUncommitted
			if state := sqlState(err); readOnly && state != vsql.ReadOnlyTransactionError {
				t.Errorf("%+v: got %v", opts, err)
			} else if !readOnly && err != nil {
				t.Errorf("%+v: %v", opts, err)
			}
			tx.Rollback()
		}
		_, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSnapshot})
		if err == nil {
			t.Error("Snapshot isn't an error")
		}
	})

	t.Run("default is SERIALIZABLE", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.Exec("INSERT INTO foo (x) VALUES (5)"); err != nil {
			t.Fatal(err)
		}
		// Another connection can't change the database until the
		// transaction ends, so it times out (after 5 seconds).
		_, err = db.Exec("INSERT INTO foo (x) VALUES (6)")
		if state := sqlState(err); state != vsql.SerializationFailureError {
			t.Errorf("got %v", err)
		}
	})

	t.Run("read only", func(t *testing.T) {
		tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var n int
		if err := tx.QueryRow("SELECT COUNT(*) FROM foo").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("DELETE FROM foo"); sqlState(err) != vsql.ReadOnlyTransactionError {
			t.Errorf("got %v", err)
		}
	})

	t.Run("savepoints", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		txCount := func() int {
			t.Helper()
			var n int
			if err := tx.QueryRow("SELECT COUNT(*) FROM foo").Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		txExec := func(query string) {
			t.Helper()
			if _, err := tx.Exec(query); err != nil {
				t.Fatalf("%s: %v", query, err)
			}
		}

		before := txCount()
		txExec("INSERT INTO foo (x) VALUES (10)")
		txExec("SAVEPOINT a")
		txExec("INSERT INTO foo (x) VALUES (11)")
		txExec(`SAVEPOINT "b c"`)
		txExec("INSERT INTO foo (x) VALUES (12)")
		txExec(`ROLLBACK TO SAVEPOINT "b c"`)
		if n := txCount(); n != before+2 {
			t.Errorf("got %d rows, want %d", n, before+2)
		}
		txExec("rollback work to savepoint A;")
		if n := txCount(); n != before+1 {
			t.Errorf("got %d rows, want %d", n, before+1)
		}
		txExec("RELEASE SAVEPOINT a")
		if _, err := tx.Exec("ROLLBACK TO SAVEPOINT a"); sqlState(err) != vsql.InvalidSavepointSpecificationError {
			t.Errorf("got %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if n := count(); n != before+1 {
			t.Errorf("got %d rows, want %d", n, before+1)
		}
		exec(t, db, "DELETE FROM foo WHERE x = 10")
	})

	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := db.ExecContext(ctx, "INSERT INTO foo (x) VALUES (4)"); !errors.Is(err, context.Canceled) {
			t.Errorf("got %v", err)
		}
	})
}

func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.vsql")
	db, err := sql.Open("vsql", path)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, db, "CREATE TABLE foo (x VARCHAR(10))")
	exec(t, db, "INSERT INTO foo (x) VALUES ('saved')")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = sql.Open("vsql", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var x string
	if err := db.QueryRow("SELECT x FROM foo").Scan(&x); err != nil {
		t.Fatal(err)
	}
	if x != "saved" {
		t.Errorf("got %q", x)
	}
}

// TestConcurrency uses many connections from many goroutines (and so from
// many threads), with enough work for the garbage collector to run.
func TestConcurrency(t *testing.T) {
	db := openFile(t)
	db.SetMaxOpenConns(8)
	exec(t, db, "CREATE TABLE foo (g INTEGER, x INTEGER)")

	const goroutines, inserts = 8, 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < inserts; i++ {
				if _, err := db.Exec("INSERT INTO foo (g, x) VALUES (?, ?)", g, i); err != nil {
					errs <- err
					return
				}
				var n int
				if err := db.QueryRow("SELECT COUNT(*) FROM foo WHERE g = ?", g).Scan(&n); err != nil {
					errs <- err
					return
				}
				if n != i+1 {
					errs <- fmt.Errorf("goroutine %d: got %d rows, want %d", g, n, i+1)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM foo").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != goroutines*inserts {
		t.Errorf("got %d rows, want %d", n, goroutines*inserts)
	}
}

func TestSQLState(t *testing.T) {
	for _, test := range []struct {
		state   vsql.SQLState
		class   vsql.SQLStateClass
		isError bool
	}{
		{vsql.SuccessfulCompletion, vsql.SuccessfulCompletionClass, false},
		{vsql.StringDataRightTruncationWarning, vsql.WarningClass, false},
		{vsql.NoData, vsql.NoDataClass, false},
		{vsql.UniqueViolationError, vsql.IntegrityConstraintViolationClass, true},
		{vsql.UndefinedTableError, vsql.SyntaxErrorOrAccessRuleViolationClass, true},
		{vsql.FunctionSequenceError, vsql.CLISpecificConditionClass, true},
		{vsql.SQLState("4"), vsql.SQLStateClass("4"), true},
	} {
		if class := test.state.Class(); class != test.class {
			t.Errorf("%s: class is %s, want %s", test.state, class, test.class)
		}
		if isError := test.state.IsError(); isError != test.isError {
			t.Errorf("%s: IsError is %v", test.state, isError)
		}
	}
}

func TestSQLStateClassOfError(t *testing.T) {
	db := openMemory(t)
	exec(t, db, "CREATE TABLE foo (x INTEGER NOT NULL)")

	_, err := db.Exec("INSERT INTO foo (x) VALUES (NULL)")
	if state := sqlState(err); state.Class() != vsql.IntegrityConstraintViolationClass || !state.IsError() {
		t.Errorf("got %v", err)
	}
}
