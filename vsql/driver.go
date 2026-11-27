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
// # Transactions
//
// sql.LevelDefault is SERIALIZABLE, which is vsql's default (and the SQL
// standard's), so it's the same as a statement outside a transaction. Only
// one READ WRITE SERIALIZABLE transaction can be active at a time, so another
// connection that changes the database waits for it to end (for up to 5
// seconds, then it's a 40001 serialization failure, or until its context is
// done). REPEATABLE READ
// runs as SERIALIZABLE, READ UNCOMMITTED is always READ ONLY, and
// sql.TxOptions.ReadOnly starts a READ ONLY transaction. See
// https://vsql.readthedocs.io/en/latest/transactions.html
//
// A transaction can have savepoints, which are SQL statements:
//
//	tx, err := db.Begin()
//	_, err = tx.Exec("SAVEPOINT a")
//	_, err = tx.Exec("INSERT INTO foo (x) VALUES (1)")
//	_, err = tx.Exec("ROLLBACK TO SAVEPOINT a") // or RELEASE SAVEPOINT a
//	err = tx.Commit()
//
// ROLLBACK TO SAVEPOINT must be executed with Exec (or a prepared statement's
// Exec), without parameters, and by itself, since the driver does it with
// SQLEndTran.
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
//	string               CHARACTER VARYING
//	[]byte               BINARY VARYING (or CHARACTER VARYING, see below)
//	time.Time            TIMESTAMP(6) WITH TIME ZONE
//	*big.Int             NUMERIC
//	*big.Rat             NUMERIC (it must be a finite decimal)
//	time.Duration        INTERVAL (see below)
//
// A type that implements driver.Valuer (such as a decimal type) is sent as the
// value it returns. A time.Time for a TIMESTAMP or TIME (without a time zone)
// is converted to UTC, since that's how it's read back, so the instant is
// kept. For a DATE, it's the date in the time.Time's location.
//
// vsql determines the type of each parameter when the statement is prepared
// (such as the type of x in "x = ?"), so a prepared statement checks the
// number of arguments, and a parameter whose type can't be determined (such as
// in "? + ?") is a syntax error. A binary string can't be cast to (or from)
// anything else, so a []byte is sent as a CHARACTER VARYING for a parameter
// that isn't a binary string, and a nil []byte is NULL. A string can't be
// used for a binary string parameter (07006).
//
// DECFLOAT and INTERVAL parameters are strings, such as "1.25" or "1-2" (for
// INTERVAL YEAR TO MONTH) and "3 04:05:06.5" (for INTERVAL DAY TO SECOND). A
// CHARACTER LARGE OBJECT is a string.
//
// A time.Duration is a day-time interval, in the fields of its parameter's
// type (or INTERVAL DAY TO SECOND for any other type), such as "1:30:00" for
// 90 minutes as an INTERVAL HOUR TO SECOND. It's an error if it has a part
// that's smaller than the last field, such as 90 seconds for an INTERVAL HOUR
// TO MINUTE.
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
//	DECFLOAT                   string    string
//	CHARACTER, VARCHAR         string    string
//	CHARACTER LARGE OBJECT     string    string
//	BINARY, BINARY VARYING     []byte    []byte
//	BINARY LARGE OBJECT        []byte    []byte
//	BOOLEAN                    bool      bool
//	DATE                       time.Time time.Time
//	TIME (WITH TIME ZONE)      time.Time time.Time
//	TIMESTAMP (WITH TIME ZONE) time.Time time.Time
//	INTERVAL ...               string    string
//
// The ScanType of a nullable column is the sql.Null type (such as
// sql.NullInt32), except for a binary string, which is a nil []byte when it's
// NULL. DECIMAL, NUMERIC and DECFLOAT are exact, so they're a string that can
// be scanned into a decimal type (or a float64). A DATE, TIME or TIMESTAMP is
// in UTC (WITH TIME ZONE has the offset as its location), and a TIME is on
// January 1, year 0. An INTERVAL is the string in its literal, such as "1-02"
// for INTERVAL '1-02' YEAR(2) TO MONTH, and its DatabaseTypeName (such as
// "INTERVAL YEAR TO MONTH") has its fields.
//
// The Length of a column type is the maximum number of characters of a
// character string, or the maximum number of octets of a binary string
// (math.MaxInt64 for no maximum). DecimalSize is the precision and scale of a
// DECIMAL or NUMERIC.
//
// # Errors
//
// An error from vsql is an *Error, with the SQLSTATE and, when they apply, the
// constraint, table, column or cursor of the condition (see Error).
//
// # Contexts
//
// A statement stops soon after its context is canceled or reaches its
// deadline, even while it's waiting for another connection's transaction, and
// the error is ctx.Err(). The canceled statement has no effect, and the
// transaction it's in continues. Fetching each row of a query is canceled by
// the context of the query.
//
// # Build
//
// Build describes the version of libvsql that's compiled into the package.
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
	return NewConnector(name).Connect(context.Background())
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
