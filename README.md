# vsql-go

A [database/sql](https://pkg.go.dev/database/sql) driver for
[vsql](https://github.com/elliotchance/vsql), a single file or in-memory SQL
database. libvsql (the vsql library) is compiled into the package with cgo, so
there's nothing else to install.

```go
import (
	"database/sql"

	_ "github.com/elliotchance/vsql-go/vsql"
)

func main() {
	db, err := sql.Open("vsql", "test.vsql")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE foo (x INTEGER)")
	// ...
}
```

The data source name is the path of the database file (it's created if it
doesn't exist), or `:memory:` for a database that only exists for the life of
the connection. Since each connection to `:memory:` is a different database,
use `db.SetMaxOpenConns(1)` to keep a single one.

See the [package documentation](https://pkg.go.dev/github.com/elliotchance/vsql-go/vsql)
for transactions, parameters and the Go type of each column type, and
[example/main.go](example/main.go) for a complete program:

```sh
go run ./example
```

What the driver supports beyond the basics:

- Binary strings (`BINARY`, `BINARY VARYING` and `BINARY LARGE OBJECT`) are
  `[]byte`. `CHARACTER LARGE OBJECT`, `DECFLOAT` and `INTERVAL` values are
  strings (an interval such as `'1-02'`).
- Transactions are `SERIALIZABLE` by default (the same as vsql), and can be
  `READ ONLY`. Savepoints are SQL statements, including
  `ROLLBACK TO SAVEPOINT`.
- A prepared statement knows how many parameters it has, and the type of each
  one, so a `[]byte` works for a binary or character parameter.
- An error is a `*vsql.Error`, with the SQLSTATE and the constraint, table or
  column of the condition:

  ```go
  var e *vsql.Error
  if errors.As(err, &e) && e.SQLState == vsql.UniqueViolationError {
  	fmt.Println(e.ConstraintName, e.TableName)
  }
  ```

- `vsql.Build()` is the version of libvsql (and how it was built).

## Requirements

cgo must be enabled (`CGO_ENABLED=1`, the default when building for the same
OS) with a C compiler. The first build compiles libvsql, which takes about 20
seconds, after that it's cached by Go.

macOS, Linux and Windows (each on ARM64 and x86-64) are supported.

On Linux, libvsql's garbage collector uses SIGPWR and SIGXCPU to stop threads,
so don't use `signal.Notify` with them.

## Updating vsql

`internal/amalgamation` is the libvsql C from a vsql release
(`vsql-VERSION-amalgamation.zip`). To update it to the latest release:

```sh
scripts/update-amalgamation.sh
go test ./...
```

Or give a version (`scripts/update-amalgamation.sh v1.2.3`), or a zip that has
already been downloaded.
