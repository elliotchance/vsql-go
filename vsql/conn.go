package vsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
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

// BeginTx starts a transaction. vsql transactions are always READ COMMITTED,
// so a higher isolation level or a read only transaction is an error.
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
	case sql.LevelDefault, sql.LevelReadCommitted:
		isolationLevel = transactionReadCommitted
	case sql.LevelReadUncommitted:
		isolationLevel = transactionReadUncommitted
	case sql.LevelRepeatableRead:
		isolationLevel = transactionRepeatableRead
	case sql.LevelSerializable:
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

	h, err := c.prepare(query)
	if err != nil {
		return nil, err
	}

	return &stmt{c: c, h: h, query: query}, nil
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

	params, err := toParams(args)
	if err != nil {
		return nil, err
	}

	h, err := c.run(query, params)
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

	params, err := toParams(args)
	if err != nil {
		return nil, err
	}

	h, err := c.run(query, params)
	if err != nil {
		return nil, err
	}

	r, err := newRows(h, nil)
	if err != nil {
		_ = h.free()
		return nil, err
	}

	return r, nil
}

// run executes a statement on a new statement handle, which must be freed.
func (c *conn) run(query string, params []param) (stmtHandle, error) {
	if len(params) == 0 {
		h, err := c.dbc.allocStmt()
		if err != nil {
			return 0, err
		}
		if err := h.execDirect(query); err != nil {
			_ = h.free()
			return 0, err
		}

		return h, nil
	}

	h, err := c.prepare(query)
	if err != nil {
		return 0, err
	}
	if err := h.execute(params); err != nil {
		_ = h.free()
		return 0, err
	}

	return h, nil
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
