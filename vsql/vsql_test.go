package vsql_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
			} else if got != test.want {
				t.Errorf("got %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestColumnTypes(t *testing.T) {
	db := openMemory(t)
	exec(t, db, "CREATE TABLE foo (a INTEGER NOT NULL, b VARCHAR(20), c NUMERIC(10, 3))")

	rows, err := db.Query("SELECT * FROM foo")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}

	if names := fmt.Sprint(rows.Columns()); names != "[A B C] <nil>" {
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
}

func TestParameters(t *testing.T) {
	db := openMemory(t)
	exec(t, db, `CREATE TABLE foo (
		s SMALLINT, i INTEGER, b BIGINT, r REAL, d DOUBLE PRECISION,
		n NUMERIC(20, 5), c VARCHAR(100), bo BOOLEAN,
		ts TIMESTAMP(6), tz TIMESTAMP(6) WITH TIME ZONE, dt DATE)`)
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
		{"ts", ts, time.Date(2022, 1, 2, 15, 4, 5, 123456000, time.UTC)},
		{"tz", ts, ts},
		{"i", nil, nil},
		{"c", nil, nil},
		{"bo", nil, nil},
		{"dt", nil, nil},
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
			} else if got != test.want {
				t.Errorf("got %#v, want %#v", got, test.want)
			}
		})
	}

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
		for _, level := range []sql.IsolationLevel{sql.LevelDefault, sql.LevelReadUncommitted, sql.LevelReadCommitted} {
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: level})
			if err != nil {
				t.Errorf("%s: %v", level, err)
				continue
			}
			tx.Rollback()
		}
		for _, opts := range []sql.TxOptions{
			{Isolation: sql.LevelSerializable},
			{Isolation: sql.LevelRepeatableRead},
			{ReadOnly: true},
		} {
			_, err := db.BeginTx(context.Background(), &opts)
			if state := sqlState(err); state != vsql.OptionalFeatureNotImplementedError {
				t.Errorf("%+v: got %v", opts, err)
			}
		}
		_, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSnapshot})
		if err == nil {
			t.Error("Snapshot isn't an error")
		}
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
