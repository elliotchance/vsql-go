package vsql

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"math"
	"reflect"
	"time"
)

// rows is the cursor of a query. If it's from a prepared statement, the
// statement's handle is only freed when both are closed, otherwise the rows
// free their own handle.
type rows struct {
	h       stmtHandle
	stmt    *stmt
	columns []column
	buf     *buffer
	closed  bool
}

var (
	_ driver.Rows                           = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
	_ driver.RowsColumnTypeLength           = (*rows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*rows)(nil)
)

func newRows(h stmtHandle, s *stmt) (*rows, error) {
	n, err := h.numResultCols()
	if err != nil {
		return nil, err
	}

	r := &rows{h: h, stmt: s, columns: make([]column, n)}
	for i := range r.columns {
		if r.columns[i], err = h.describeCol(i + 1); err != nil {
			return nil, err
		}
	}
	r.buf = newBuffer()

	return r, nil
}

func (r *rows) Columns() []string {
	names := make([]string, len(r.columns))
	for i, c := range r.columns {
		names[i] = c.name
	}

	return names
}

// Close closes the cursor, which commits the statement's transaction (unless
// it's in a transaction started with BeginTx).
func (r *rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	r.buf.free()

	err := r.h.closeCursor()
	if r.stmt != nil {
		r.stmt.rows = nil
		if !r.stmt.closed {
			return err
		}
	}
	if freeErr := r.h.free(); err == nil {
		err = freeErr
	}

	return err
}

func (r *rows) Next(dest []driver.Value) error {
	ok, err := r.h.fetch()
	if err != nil {
		return err
	}
	if !ok {
		return io.EOF
	}

	for i, c := range r.columns {
		if dest[i], err = r.value(i+1, c); err != nil {
			return fmt.Errorf("vsql: column %s: %w", c.name, err)
		}
	}

	return nil
}

// Layouts of the datetimes from SQLGetData.
const (
	layoutDate                  = "2006-01-02"
	layoutTime                  = "15:04:05.999999999"
	layoutTimeWithTimezone      = "15:04:05.999999999-0700"
	layoutTimestamp             = "2006-01-02T15:04:05.999999999"
	layoutTimestampWithTimezone = "2006-01-02T15:04:05.999999999-0700"
)

func (r *rows) value(n int, c column) (driver.Value, error) {
	var v driver.Value
	var ok bool
	var err error
	switch c.dataType {
	case typeSmallint, typeInteger, typeBigint:
		v, ok, err = r.h.getInt64(n, r.buf)
	case typeReal, typeFloat, typeDouble:
		v, ok, err = r.h.getFloat64(n, c.dataType == typeReal, r.buf)
	default:
		var s string
		s, ok, err = r.h.getString(n, r.buf)
		if err == nil && ok {
			v, err = parse(s, c.dataType)
		}
	}
	if err != nil || !ok {
		return nil, err
	}

	return v, nil
}

// parse is the value of a column that's read as a string.
func parse(s string, dataType int) (driver.Value, error) {
	switch dataType {
	case typeBoolean:
		switch s {
		case "TRUE":
			return true, nil
		case "FALSE":
			return false, nil
		}
		return nil, fmt.Errorf("invalid BOOLEAN: %q", s)
	case typeDate:
		return time.Parse(layoutDate, s)
	case typeTime:
		return time.Parse(layoutTime, s)
	case typeTimeWithTimezone:
		return time.Parse(layoutTimeWithTimezone, s)
	case typeTimestamp:
		return time.Parse(layoutTimestamp, s)
	case typeTimestampWithTimezone:
		return time.Parse(layoutTimestampWithTimezone, s)
	}

	// CHARACTER, CHARACTER VARYING, DECIMAL and NUMERIC.
	return s, nil
}

var typeNames = map[int]string{
	typeSmallint:              "SMALLINT",
	typeInteger:               "INTEGER",
	typeBigint:                "BIGINT",
	typeReal:                  "REAL",
	typeFloat:                 "DOUBLE PRECISION",
	typeDouble:                "DOUBLE PRECISION",
	typeDecimal:               "DECIMAL",
	typeNumeric:               "NUMERIC",
	typeChar:                  "CHARACTER",
	typeVarchar:               "CHARACTER VARYING",
	typeBoolean:               "BOOLEAN",
	typeDate:                  "DATE",
	typeTime:                  "TIME",
	typeTimeWithTimezone:      "TIME WITH TIME ZONE",
	typeTimestamp:             "TIMESTAMP",
	typeTimestampWithTimezone: "TIMESTAMP WITH TIME ZONE",
}

func (r *rows) ColumnTypeDatabaseTypeName(index int) string {
	return typeNames[r.columns[index].dataType]
}

var (
	scanTypes = map[int]reflect.Type{
		typeSmallint: reflect.TypeOf(int16(0)),
		typeInteger:  reflect.TypeOf(int32(0)),
		typeBigint:   reflect.TypeOf(int64(0)),
		typeReal:     reflect.TypeOf(float32(0)),
		typeFloat:    reflect.TypeOf(float64(0)),
		typeDouble:   reflect.TypeOf(float64(0)),
		typeBoolean:  reflect.TypeOf(false),
	}
	nullScanTypes = map[int]reflect.Type{
		typeSmallint: reflect.TypeOf(sql.NullInt16{}),
		typeInteger:  reflect.TypeOf(sql.NullInt32{}),
		typeBigint:   reflect.TypeOf(sql.NullInt64{}),
		typeReal:     reflect.TypeOf(sql.Null[float32]{}),
		typeFloat:    reflect.TypeOf(sql.NullFloat64{}),
		typeDouble:   reflect.TypeOf(sql.NullFloat64{}),
		typeBoolean:  reflect.TypeOf(sql.NullBool{}),
	}
)

func (r *rows) ColumnTypeScanType(index int) reflect.Type {
	c := r.columns[index]
	null := c.nullable != noNulls
	if t, ok := scanTypes[c.dataType]; ok {
		if null {
			return nullScanTypes[c.dataType]
		}
		return t
	}

	switch c.dataType {
	case typeDate, typeTime, typeTimeWithTimezone, typeTimestamp, typeTimestampWithTimezone:
		if null {
			return reflect.TypeOf(sql.NullTime{})
		}
		return reflect.TypeOf(time.Time{})
	}

	if null {
		return reflect.TypeOf(sql.NullString{})
	}
	return reflect.TypeOf("")
}

func (r *rows) ColumnTypeNullable(index int) (nullable, ok bool) {
	switch r.columns[index].nullable {
	case noNulls:
		return false, true
	case nullableColumn:
		return true, true
	}

	return false, false
}

// ColumnTypeLength is the maximum number of characters of a CHARACTER or
// CHARACTER VARYING.
func (r *rows) ColumnTypeLength(index int) (length int64, ok bool) {
	c := r.columns[index]
	if c.dataType != typeChar && c.dataType != typeVarchar {
		return 0, false
	}

	// The size is in octets, with 4 for each character.
	if c.columnSize <= 0 {
		return math.MaxInt64, true
	}
	return int64(c.columnSize / 4), true
}

// ColumnTypePrecisionScale is the precision and scale of a DECIMAL or NUMERIC.
func (r *rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	c := r.columns[index]
	if c.dataType != typeDecimal && c.dataType != typeNumeric {
		return 0, 0, false
	}

	return int64(c.columnSize), int64(c.decimalDigits), true
}
