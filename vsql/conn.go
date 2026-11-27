package vsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
)

// conn is a connection, which has its own environment so that it can be used
// at the same time as the other connections (see "Threads" in libvsql.h).
type conn struct {
	env    envHandle
	dbc    dbcHandle
	inTx   bool
	closed bool
}

var (
	_ driver.Conn               = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
	_ driver.Validator          = (*conn)(nil)
	_ driver.NamedValueChecker  = (*conn)(nil)
)

func connect(name string) (_ *conn, err error) {
	c := &conn{}
	if c.env, err = allocEnv(); err != nil {
		return nil, err
	}
	if c.dbc, err = c.env.allocDbc(); err != nil {
		_ = c.env.free()
		return nil, err
	}
	if err = c.dbc.connect(name); err != nil {
		_ = c.dbc.free()
		_ = c.env.free()
		return nil, err
	}
	if err = c.dbc.setAutocommit(true); err != nil {
		_ = c.Close()
		return nil, err
	}

	return c, nil
}

func (c *conn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true

	var errs []error
	if c.inTx {
		errs = append(errs, c.dbc.endTran(false))
	}
	errs = append(errs, c.dbc.disconnect(), c.dbc.free(), c.env.free())

	return errors.Join(errs...)
}

func (c *conn) IsValid() bool {
	return !c.closed
}

func (c *conn) Ping(ctx context.Context) error {
	if c.closed {
		return driver.ErrBadConn
	}

	return ctx.Err()
}

func (c *conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx starts a transaction. The default isolation level is SERIALIZABLE,
// the same as vsql's (and the SQL standard's) default for every other
// transaction. REPEATABLE READ is also SERIALIZABLE in vsql, and READ
// UNCOMMITTED transactions are always READ ONLY.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	accessMode := transactionReadWrite
	if opts.ReadOnly {
		accessMode = transactionReadOnly
	}

	var isolationLevel int
	switch sql.IsolationLevel(opts.Isolation) {
	case sql.LevelReadCommitted:
		isolationLevel = transactionReadCommitted
	case sql.LevelReadUncommitted:
		// READ WRITE can't be used with READ UNCOMMITTED.
		accessMode = transactionReadOnly
		isolationLevel = transactionReadUncommitted
	case sql.LevelRepeatableRead:
		isolationLevel = transactionRepeatableRead
	case sql.LevelDefault, sql.LevelSerializable:
		isolationLevel = transactionSerializable
	default:
		return nil, fmt.Errorf("vsql: isolation level %s is not supported",
			sql.IsolationLevel(opts.Isolation))
	}

	if err := c.dbc.startTran(accessMode, isolationLevel); err != nil {
		return nil, err
	}
	c.inTx = true

	return &tx{c: c}, nil
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if name, ok := rollbackToSavepoint(query); ok {
		return &stmt{c: c, query: query, savepoint: name}, nil
	}

	h, err := c.prepare(query)
	if err != nil {
		return nil, err
	}
	types, err := h.paramTypes()
	if err != nil {
		_ = h.free()
		return nil, err
	}

	return &stmt{c: c, h: h, query: query, types: types}, nil
}

func (c *conn) prepare(query string) (stmtHandle, error) {
	h, err := c.dbc.allocStmt()
	if err != nil {
		return 0, err
	}
	if err := h.prepare(query); err != nil {
		_ = h.free()
		return 0, err
	}

	return h, nil
}

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(args) == 0 {
		if name, ok := rollbackToSavepoint(query); ok {
			if err := c.dbc.rollbackToSavepoint(name); err != nil {
				return nil, err
			}
			return driver.RowsAffected(0), nil
		}
	}

	h, err := c.run(ctx, query, args)
	if err != nil {
		return nil, err
	}
	defer h.free()

	return result(h)
}

func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	h, err := c.run(ctx, query, args)
	if err != nil {
		return nil, err
	}

	r, err := newRows(ctx, h, nil)
	if err != nil {
		_ = h.free()
		return nil, err
	}

	return r, nil
}

// run executes a statement on a new statement handle, which must be freed.
// It's canceled if ctx is done.
func (c *conn) run(ctx context.Context, query string, args []driver.NamedValue) (stmtHandle, error) {
	if len(args) == 0 {
		h, err := c.dbc.allocStmt()
		if err != nil {
			return 0, err
		}
		if err := h.execDirect(ctx, query); err != nil {
			_ = h.free()
			return 0, err
		}

		return h, nil
	}

	h, err := c.prepare(query)
	if err != nil {
		return 0, err
	}
	types, err := h.paramTypes()
	if err == nil {
		var params []param
		if params, err = toParams(args, types); err == nil {
			err = h.execute(ctx, params)
		}
	}
	if err != nil {
		_ = h.free()
		return 0, err
	}

	return h, nil
}

// savepointPattern is a ROLLBACK TO SAVEPOINT statement, and the name of the
// savepoint (a regular or delimited identifier). There can be comments before,
// between and after the keywords.
var savepointPattern = func() *regexp.Regexp {
	space := `(?:\s|--[^\n]*(?:\n|$)|/\*.*?\*/)`
	identifier := `[A-Za-z_][A-Za-z0-9_]*|"(?:[^"]|"")+"`

	return regexp.MustCompile(`(?is)^` + space + `*ROLLBACK` + space + `+(?:WORK` + space + `+)?TO` +
		space + `+SAVEPOINT` + space + `+(` + identifier + `)` + space + `*;?` + space + `*$`)
}()

// rollbackToSavepoint is the name of the savepoint if query is a ROLLBACK TO
// SAVEPOINT statement. libvsql doesn't execute it as a statement, since it
// ends (part of) a transaction, which is done with SQLEndTran instead.
func rollbackToSavepoint(query string) (string, bool) {
	m := savepointPattern.FindStringSubmatch(query)
	if m == nil {
		return "", false
	}

	return m[1], true
}

// result is the result of a statement that was executed, which closes the
// cursor if it's a query.
func result(h stmtHandle) (driver.Result, error) {
	n, err := h.rowCount()
	if err != nil {
		return nil, err
	}

	columns, err := h.numResultCols()
	if err != nil {
		return nil, err
	}
	if columns > 0 {
		if err := h.closeCursor(); err != nil {
			return nil, err
		}
	}

	return driver.RowsAffected(n), nil
}

type tx struct {
	c *conn
}

func (t *tx) Commit() error {
	t.c.inTx = false

	return t.c.dbc.endTran(true)
}

func (t *tx) Rollback() error {
	t.c.inTx = false

	return t.c.dbc.endTran(false)
}
