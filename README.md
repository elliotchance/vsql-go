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

## Requirements

cgo must be enabled (`CGO_ENABLED=1`, the default when building for the same
OS) with a C compiler. The first build compiles libvsql, which takes about 20
seconds, after that it's cached by Go.

macOS (ARM64 and x86-64), Linux (ARM64 and x86-64) and Windows (x86-64) are
supported.

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
