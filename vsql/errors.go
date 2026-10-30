package vsql

import "fmt"

// Error is an error from vsql, with its SQLSTATE (such as UndefinedTableError).
type Error struct {
	SQLState SQLState
	Message  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("vsql: %s (SQLSTATE %s)", e.Message, e.SQLState)
}
