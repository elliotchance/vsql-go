// Package vsql is a database/sql driver for vsql
// (https://github.com/elliotchance/vsql), a single file or in-memory SQL
// database. libvsql (the vsql library) is compiled into the package with cgo,
// so nothing else needs to be installed.
//
//	db, err := sql.Open("vsql", "test.vsql")
//
// The data source name is the path of the database file, which is created if
// it doesn't exist, or ":memory:" for a database that only exists for the
// life of the connection. Since each connection to ":memory:" is a different
// database, use db.SetMaxOpenConns(1) to keep a single one.
//
// Each statement is committed when it finishes (or when the rows of a query
// are closed), unless it's in a transaction started with db.Begin or
// db.BeginTx. START TRANSACTION, COMMIT and ROLLBACK can't be executed as
// statements.
//
// # Parameters
//
// Parameters are "?" (named parameters are not supported). Each Go type is
// sent as an SQL type, which is cast to the type it's used as:
//
//	nil                  NULL
//	int64 (and int, ...) BIGINT
//	float64, float32     DOUBLE PRECISION
//	bool                 BOOLEAN
//	string, []byte       CHARACTER VARYING
//	time.Time            TIMESTAMP(6) WITH TIME ZONE
//	*big.Int             NUMERIC
//	*big.Rat             NUMERIC (it must be a finite decimal)
//
// A type that implements driver.Valuer (such as a decimal type) is sent as the
// value it returns. A TIMESTAMP WITH TIME ZONE can't be assigned to a DATE or
// TIME column, so use CAST(? AS DATE) or CAST(? AS TIME).
//
// # Columns
//
// The value of each column type, and its ScanType (see sql.ColumnType):
//
//	SMALLINT                   int64     int16
//	INTEGER                    int64     int32
//	BIGINT                     int64     int64
//	REAL                       float64   float32
//	DOUBLE PRECISION           float64   float64
//	DECIMAL, NUMERIC           string    string
//	CHARACTER, VARCHAR         string    string
//	BOOLEAN                    bool      bool
//	DATE                       time.Time time.Time
//	TIME (WITH TIME ZONE)      time.Time time.Time
//	TIMESTAMP (WITH TIME ZONE) time.Time time.Time
//
// The ScanType of a nullable column is the sql.Null type (such as
// sql.NullInt32). DECIMAL and NUMERIC are exact, so they're a string that can
// be scanned into a decimal type (or a float64). A DATE, TIME or TIMESTAMP is
// in UTC (WITH TIME ZONE has the offset as its location), and a TIME is on
// January 1, year 0.
package vsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
)

func init() {
	sql.Register("vsql", &Driver{})
}

// Driver is the vsql driver, which is registered as "vsql".
type Driver struct{}

var (
	_ driver.Driver        = (*Driver)(nil)
	_ driver.DriverContext = (*Driver)(nil)
	_ driver.Connector     = (*Connector)(nil)
)

// Open opens a connection, see OpenConnector.
func (d *Driver) Open(name string) (driver.Conn, error) {
	c, err := d.OpenConnector(name)
	if err != nil {
		return nil, err
	}

	return c.Connect(context.Background())
}

// OpenConnector returns a connector for a database, see the package
// documentation for name.
func (d *Driver) OpenConnector(name string) (driver.Connector, error) {
	return NewConnector(name), nil
}

// Connector connects to a database. It can be used with sql.OpenDB.
type Connector struct {
	name string
}

// NewConnector is a connector for a database file, or ":memory:".
func NewConnector(name string) *Connector {
	return &Connector{name: name}
}

// Connect opens a new connection to the database.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return connect(c.name)
}

// Driver is the vsql driver.
func (c *Connector) Driver() driver.Driver {
	return &Driver{}
}
