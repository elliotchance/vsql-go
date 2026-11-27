package vsql

import (
	"context"
	"database/sql/driver"
	"errors"
)

// stmt is a prepared statement. Its handle can only have one cursor open, so
// executing it while the rows of a query are still open uses a new handle.
//
// A ROLLBACK TO SAVEPOINT statement isn't prepared by libvsql (see
// rollbackToSavepoint), so it has no handle, only the name of the savepoint.
type stmt struct {
	c         *conn
	h         stmtHandle
	query     string
	types     []int
	rows      *rows
	closed    bool
	savepoint string
}

var (
	_ driver.Stmt             = (*stmt)(nil)
	_ driver.StmtExecContext  = (*stmt)(nil)
	_ driver.StmtQueryContext = (*stmt)(nil)
)

// Close frees the handle, or the rows free it when they're closed.
func (s *stmt) Close() error {
	s.closed = true
	if s.rows != nil || s.savepoint != "" {
		return nil
	}

	return s.h.free()
}

// NumInput is the number of dynamic parameters, which libvsql describes when
// the statement is prepared.
func (s *stmt) NumInput() int {
	return len(s.types)
}

func (s *stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), namedValues(args))
}

func (s *stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), namedValues(args))
}

func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if s.rows != nil || s.savepoint != "" {
		return s.c.ExecContext(ctx, s.query, args)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	params, err := toParams(args, s.types)
	if err != nil {
		return nil, err
	}
	if err := s.h.execute(ctx, params); err != nil {
		return nil, err
	}

	return result(s.h)
}

func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if s.savepoint != "" {
		return nil, errors.New("vsql: ROLLBACK TO SAVEPOINT must be executed with Exec")
	}
	if s.rows != nil {
		return s.c.QueryContext(ctx, s.query, args)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	params, err := toParams(args, s.types)
	if err != nil {
		return nil, err
	}
	if err := s.h.execute(ctx, params); err != nil {
		return nil, err
	}

	r, err := newRows(ctx, s.h, s)
	if err != nil {
		_ = s.h.closeCursor()
		return nil, err
	}
	s.rows = r

	return r, nil
}

func namedValues(args []driver.Value) []driver.NamedValue {
	named := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}

	return named
}
