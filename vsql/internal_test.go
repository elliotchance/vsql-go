package vsql

// These tests use the driver's internals for the paths that database/sql
// doesn't reach (such as a canceled context, which it checks first) and for
// the errors of libvsql routines that can't fail with valid handles, which
// are given invalid ones.

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

func open(t *testing.T) *conn {
	t.Helper()
	c, err := connect(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	return c
}

func TestDriverMethods(t *testing.T) {
	c, err := (&Driver{}).Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := NewConnector("").Driver().(*Driver); !ok {
		t.Error("Driver isn't a *Driver")
	}

	tx, err := c.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.(*conn).BeginTx(context.Background(), driver.TxOptions{}); err == nil {
		t.Error("a second transaction was started")
	}
	s, err := c.Prepare("VALUES CAST(? AS INTEGER)")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Query([]driver.Value{int64(1)})
	if err != nil {
		t.Fatal(err)
	}
	// The rows are still open, so these use new handles.
	if _, err := s.Exec([]driver.Value{int64(2)}); err != nil {
		t.Error(err)
	}
	other, err := s.(*stmt).QueryContext(context.Background(), namedValues([]driver.Value{int64(3)}))
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Error(err)
	}
	if err := rows.Close(); err != nil {
		t.Error(err)
	}
	if err := rows.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Error(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Error(err)
	}

	// Closing a connection rolls back its transaction.
	if _, err := c.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := c.(*conn).Ping(context.Background()); err != nil {
		t.Error(err)
	}
	if err := c.Close(); err != nil {
		t.Error(err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := c.(*conn).Ping(context.Background()); !errors.Is(err, driver.ErrBadConn) {
		t.Errorf("Ping after Close: %v", err)
	}
}

func TestCanceledContext(t *testing.T) {
	c := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s, err := c.Prepare("VALUES 1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for name, err := range map[string]error{
		"Connect":           func() error { _, err := NewConnector(":memory:").Connect(ctx); return err }(),
		"BeginTx":           func() error { _, err := c.BeginTx(ctx, driver.TxOptions{}); return err }(),
		"PrepareContext":    func() error { _, err := c.PrepareContext(ctx, "VALUES 1"); return err }(),
		"ExecContext":       func() error { _, err := c.ExecContext(ctx, "VALUES 1", nil); return err }(),
		"QueryContext":      func() error { _, err := c.QueryContext(ctx, "VALUES 1", nil); return err }(),
		"stmt.ExecContext":  func() error { _, err := s.(*stmt).ExecContext(ctx, nil); return err }(),
		"stmt.QueryContext": func() error { _, err := s.(*stmt).QueryContext(ctx, nil); return err }(),
	} {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestInvalidHandles(t *testing.T) {
	var s stmtHandle
	var d dbcHandle
	var e envHandle
	var b = newBuffer()
	defer b.free()
	errs := map[string]error{
		"allocDbc":            func() error { _, err := e.allocDbc(); return err }(),
		"allocStmt":           func() error { _, err := d.allocStmt(); return err }(),
		"getInfoString":       func() error { _, err := d.getInfoString(infoBuildVersion); return err }(),
		"rollbackToSavepoint": d.rollbackToSavepoint("a"),
		"paramTypes":          func() error { _, err := s.paramTypes(); return err }(),
		"execute":             s.execute(context.Background(), []param{{null: true}}),
		"rowCount":            func() error { _, err := s.rowCount(); return err }(),
		"numResultCols":       func() error { _, err := s.numResultCols(); return err }(),
		"describeCol":         func() error { _, err := s.describeCol(1); return err }(),
		"getInt64":            func() error { _, _, err := s.getInt64(1, b); return err }(),
		"getFloat64":          func() error { _, _, err := s.getFloat64(1, false, b); return err }(),
		"getString":           func() error { _, _, err := s.getString(1, b); return err }(),
		"getBytes":            func() error { _, _, err := s.getBytes(1, b); return err }(),
		"result":              func() error { _, err := result(s); return err }(),
		"prepare":             func() error { _, err := (&conn{}).prepare("VALUES 1"); return err }(),
		"run":                 func() error { _, err := (&conn{}).run(context.Background(), "VALUES 1", nil); return err }(),
		"newRows":             func() error { _, err := newRows(context.Background(), s, nil); return err }(),
		"count":               func() error { _, err := descHandle(0).count(); return err }(),
		"smallint": func() error {
			if _, ok := descHandle(0).smallint(1, 1); ok {
				return nil
			}
			return errInvalidHandle
		}(),
	}
	for name, err := range errs {
		if !errors.Is(err, errInvalidHandle) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := allocEnvError(-1); err == nil {
		t.Error("allocEnvError(SQL_ERROR) is nil")
	}
	// SQL_HANDLE_STMT, an invalid handle and SQL_DIAG_CONSTRAINT_NAME.
	if s := diagString(3, 0, 2); s != "" {
		t.Errorf("diagString is %q", s)
	}
}

func TestNoDiagnostics(t *testing.T) {
	c := open(t)
	h, err := c.dbc.allocStmt()
	if err != nil {
		t.Fatal(err)
	}
	defer h.free()

	// A failure without a diagnostic record.
	var e *Error
	if err := h.check(-1); !errors.As(err, &e) || e.SQLState != CLISpecificConditionError {
		t.Errorf("got %v", err)
	}
}

func TestCursorErrors(t *testing.T) {
	c := open(t)
	h, err := c.run(context.Background(), "VALUES 'x'", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.free()

	// A value that can't be parsed as its type.
	r := &rows{ctx: context.Background(), h: h, columns: []column{{name: "C1", dataType: typeBoolean}}, buf: newBuffer()}
	defer r.buf.free()
	if err := r.Next(make([]driver.Value, 1)); err == nil {
		t.Error("'x' is a BOOLEAN")
	}

	// The cursor is closed.
	if err := h.closeCursor(); err != nil {
		t.Fatal(err)
	}
	if err := r.Next(make([]driver.Value, 1)); err == nil || err == io.EOF {
		t.Errorf("Next after the cursor is closed: %v", err)
	}
	if _, err := result(h); err == nil {
		t.Error("result closed a closed cursor")
	}

	if _, err := toParam(struct{}{}, 0); err == nil {
		t.Error("struct{} is a parameter")
	}
	if s := unquoteDatetime("x"); s != "x" {
		t.Errorf("got %q", s)
	}
	if n, ok := (&rows{columns: []column{{nullable: 2}}}).ColumnTypeNullable(0); n || ok {
		t.Errorf("unknown nullability is %v, %v", n, ok)
	}
}

var errFault = errors.New("fault")

// fail replaces *f with failing until the test ends. The variables are global,
// so a test that uses it can't be parallel.
func fail[F any](t *testing.T, f *F, failing F) {
	t.Helper()
	old := *f
	*f = failing
	t.Cleanup(func() { *f = old })
}

func TestConnectFaults(t *testing.T) {
	for name, inject := range map[string]func(t *testing.T){
		"allocEnv": func(t *testing.T) {
			fail(t, &allocEnv, func() (envHandle, error) { return 0, errFault })
		},
		"allocDbc": func(t *testing.T) {
			fail(t, &allocDbc, func(envHandle) (dbcHandle, error) { return 0, errFault })
		},
		"setAutocommit": func(t *testing.T) {
			fail(t, &setAutocommit, func(dbcHandle, bool) error { return errFault })
		},
	} {
		t.Run(name, func(t *testing.T) {
			inject(t)
			if _, err := connect(":memory:"); err != errFault {
				t.Errorf("connect: %v", err)
			}
			if _, err := readBuild(); err != errFault {
				t.Errorf("readBuild: %v", err)
			}
		})
	}

	t.Run("getInfoString", func(t *testing.T) {
		fail(t, &getInfoString, func(dbcHandle, int) (string, error) { return "", errFault })
		if _, err := readBuild(); !errors.Is(err, errFault) {
			t.Errorf("got %v", err)
		}
	})
}

func TestStatementFaults(t *testing.T) {
	c := open(t)
	ctx := context.Background()
	arg := []driver.NamedValue{{Ordinal: 1, Value: int64(1)}}
	s, err := c.Prepare("VALUES CAST(? AS INTEGER)")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Each routine fails after the statement was prepared (or executed).
	for name, test := range map[string]struct {
		inject func(t *testing.T)
		run    func() error
	}{
		"paramTypes in PrepareContext": {
			func(t *testing.T) {
				fail(t, &paramTypes, func(stmtHandle) ([]int, error) { return nil, errFault })
			},
			func() error { _, err := c.PrepareContext(ctx, "VALUES 1"); return err },
		},
		"paramTypes in ExecContext": {
			func(t *testing.T) {
				fail(t, &paramTypes, func(stmtHandle) ([]int, error) { return nil, errFault })
			},
			func() error { _, err := c.ExecContext(ctx, "VALUES CAST(? AS INTEGER)", arg); return err },
		},
		"descCount": {
			func(t *testing.T) {
				fail(t, &descCount, func(descHandle) (int, error) { return 0, errFault })
			},
			func() error { _, err := c.PrepareContext(ctx, "VALUES 1"); return err },
		},
		"numResultCols in ExecContext": {
			func(t *testing.T) {
				fail(t, &numResultCols, func(stmtHandle) (int, error) { return 0, errFault })
			},
			func() error { _, err := c.ExecContext(ctx, "VALUES 1", nil); return err },
		},
		"numResultCols in QueryContext": {
			func(t *testing.T) {
				fail(t, &numResultCols, func(stmtHandle) (int, error) { return 0, errFault })
			},
			func() error { _, err := c.QueryContext(ctx, "VALUES 1", nil); return err },
		},
		"describeCol in QueryContext": {
			func(t *testing.T) {
				fail(t, &describeCol, func(stmtHandle, int) (column, error) { return column{}, errFault })
			},
			func() error { _, err := c.QueryContext(ctx, "VALUES 1", nil); return err },
		},
		"describeCol in stmt.QueryContext": {
			func(t *testing.T) {
				fail(t, &describeCol, func(stmtHandle, int) (column, error) { return column{}, errFault })
			},
			func() error { _, err := s.(*stmt).QueryContext(ctx, arg); return err },
		},
	} {
		t.Run(name, func(t *testing.T) {
			test.inject(t)
			if err := test.run(); err != errFault {
				t.Errorf("got %v", err)
			}
		})
	}

	// The statement can still be used after its query failed.
	rows, err := s.(*stmt).QueryContext(ctx, arg)
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Error(err)
	}
}

func TestUnknownParameterType(t *testing.T) {
	c := open(t)

	// A parameter whose type can't be read is an unknown type (0), and its
	// value is still sent.
	fail(t, &descSmallint, func(descHandle, int, int) (int, bool) { return 0, false })
	s, err := c.Prepare("VALUES CAST(? AS INTEGER)")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if types := s.(*stmt).types; len(types) != 1 || types[0] != 0 {
		t.Errorf("types are %v", types)
	}
	if _, err := s.Exec([]driver.Value{int64(1)}); err != nil {
		t.Error(err)
	}
}
