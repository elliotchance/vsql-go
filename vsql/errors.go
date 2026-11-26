package vsql

import "fmt"

// Error is an error from vsql, with its SQLSTATE (such as UndefinedTableError).
//
// The other fields are filled in when they apply to the condition, otherwise
// they're "". A constraint violation (class 23) has the constraint that was
// violated, and the table it's on:
//
//	var e *vsql.Error
//	if errors.As(err, &e) && e.SQLState == vsql.UniqueViolationError {
//		fmt.Println(e.ConstraintName, e.TableName) // T_PRIMARY_KEY T
//	}
//
// An undefined table (42P01) has the table's name, an unknown column has the
// column's name, and an invalid cursor state (24000) has the cursor's name.
// Names are as vsql stores them, so a regular identifier is upper case.
type Error struct {
	SQLState SQLState
	Message  string

	// ConstraintCatalog, ConstraintSchema and ConstraintName are the
	// constraint that was violated.
	ConstraintCatalog string
	ConstraintSchema  string
	ConstraintName    string

	// CatalogName, SchemaName and TableName are the table of the condition.
	CatalogName string
	SchemaName  string
	TableName   string

	// ColumnName is the column of the condition.
	ColumnName string

	// CursorName is the cursor of the condition.
	CursorName string
}

func (e *Error) Error() string {
	return fmt.Sprintf("vsql: %s (SQLSTATE %s)", e.Message, e.SQLState)
}
