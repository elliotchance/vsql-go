package vsql_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// canceledIn checks that run fails with context.DeadlineExceeded soon after
// its context's deadline.
func canceledIn(t *testing.T, name string, run func(ctx context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("%s: got %v", name, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("%s: took %v", name, elapsed)
	}
}

func TestCancelLongQuery(t *testing.T) {
	db := openMemory(t)
	exec(t, db, "CREATE TABLE foo (x INTEGER)")
	values := make([]string, 100)
	for i := range values {
		values[i] = fmt.Sprintf("(%d)", i)
	}
	exec(t, db, "INSERT INTO foo (x) VALUES "+strings.Join(values, ", "))

	// 100 million rows.
	query := "SELECT COUNT(*) FROM foo a, foo b, foo c, foo d"
	canceledIn(t, "QueryRowContext", func(ctx context.Context) error {
		var n int
		return db.QueryRowContext(ctx, query).Scan(&n)
	})
	canceledIn(t, "with a parameter", func(ctx context.Context) error {
		var n int
		return db.QueryRowContext(ctx, query+" WHERE a.x < ?", 1000).Scan(&n)
	})

	// The connection can still be used.
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM foo").Scan(&n); err != nil || n != 100 {
		t.Errorf("got %d (%v)", n, err)
	}
}

func TestCancelLockWait(t *testing.T) {
	db := openFile(t)
	exec(t, db, "CREATE TABLE foo (x INTEGER)")
	stmt, err := db.Prepare("INSERT INTO foo (x) VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO foo (x) VALUES (1)"); err != nil {
		t.Fatal(err)
	}

	// Another connection waits for the SERIALIZABLE transaction to end (for
	// up to 5 seconds), unless its context is done first.
	canceledIn(t, "ExecContext", func(ctx context.Context) error {
		_, err := db.ExecContext(ctx, "INSERT INTO foo (x) VALUES (2)")
		return err
	})
	canceledIn(t, "prepared ExecContext", func(ctx context.Context) error {
		_, err := stmt.ExecContext(ctx, 3)
		return err
	})
	canceledIn(t, "QueryContext", func(ctx context.Context) error {
		rows, err := db.QueryContext(ctx, "INSERT INTO foo (x) VALUES (4)")
		if err == nil {
			err = rows.Close()
		}
		return err
	})

	// The canceled statements had no effect.
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := stmt.Exec(5); err != nil {
		t.Fatal(err)
	}
	var got []int
	rows, err := db.Query("SELECT x FROM foo ORDER BY x")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var x int
		if err := rows.Scan(&x); err != nil {
			t.Fatal(err)
		}
		got = append(got, x)
	}
	if fmt.Sprint(got) != "[1 5]" {
		t.Errorf("got %v", got)
	}
}
