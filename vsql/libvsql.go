package vsql

// libvsql.go is the only file that calls libvsql (see libvsql.h), which is
// compiled into the package from the amalgamation by libvsql.c.
//
// Values that libvsql reads after a routine returns (such as the parameters of
// SQLBindParameter, which are read by SQLExecute) are allocated with C.malloc,
// since C can't keep a Go pointer.
//
// The amalgamation's Makefile compiles libvsql.c with -std=gnu11 -fwrapv -w,
// but cgo doesn't allow -fwrapv in #cgo CFLAGS, so it's left out (as it always
// has been here). The C that V generates for Linux passes a pthread_t as a
// void*, which is an error from GCC 14 unless -Wno-int-conversion is given.

// #cgo CFLAGS: -I${SRCDIR}/../internal/amalgamation -std=gnu11 -w -Wno-int-conversion
// #include <stdlib.h>
// #include "libvsql.h"
// void vsql_gc_after_go_init(void);
import "C"

import (
	"context"
	"errors"
	"unsafe"
)

// The handles of an environment, connection and statement.
type (
	envHandle  C.SQLHENV
	dbcHandle  C.SQLHDBC
	stmtHandle C.SQLHSTMT
)

var errInvalidHandle = errors.New("vsql: invalid handle")

// Package initialization runs on the main thread, after the Go runtime has
// installed its signal handlers (see libvsql.c).
func init() {
	C.vsql_gc_after_go_init()
}

// check is the error for a return code, with the diagnostics of the handle.
// SQL_NO_DATA isn't an error.
func check(rc C.SQLRETURN, handleType C.SQLSMALLINT, handle C.SQLINTEGER) error {
	switch rc {
	case C.SQL_SUCCESS, C.SQL_SUCCESS_WITH_INFO, C.SQL_NO_DATA:
		return nil
	case C.SQL_INVALID_HANDLE:
		return errInvalidHandle
	}

	if err := diagnostics(handleType, handle); err != nil {
		return err
	}

	return &Error{SQLState: CLISpecificConditionError, Message: "unknown error"}
}

// diagnostics is the first diagnostic record of the last routine called with
// a handle, or nil if there isn't one.
func diagnostics(handleType C.SQLSMALLINT, handle C.SQLINTEGER) error {
	var sqlstate [6]C.SQLCHAR
	message := make([]C.SQLCHAR, 512)
	var length C.SQLSMALLINT
	for {
		rc := C.SQLGetDiagRec(handleType, handle, 1, &sqlstate[0], nil,
			&message[0], C.SQLSMALLINT(len(message)), &length)
		if rc != C.SQL_SUCCESS && rc != C.SQL_SUCCESS_WITH_INFO {
			return nil
		}

		// The message was truncated.
		if int(length) >= len(message) {
			message = make([]C.SQLCHAR, int(length)+1)
			continue
		}

		return &Error{
			SQLState:          SQLState(C.GoStringN((*C.char)(unsafe.Pointer(&sqlstate[0])), 5)),
			Message:           C.GoStringN((*C.char)(unsafe.Pointer(&message[0])), C.int(length)),
			ConstraintCatalog: diagString(handleType, handle, C.SQL_DIAG_CONSTRAINT_CATALOG),
			ConstraintSchema:  diagString(handleType, handle, C.SQL_DIAG_CONSTRAINT_SCHEMA),
			ConstraintName:    diagString(handleType, handle, C.SQL_DIAG_CONSTRAINT_NAME),
			CatalogName:       diagString(handleType, handle, C.SQL_DIAG_CATALOG_NAME),
			SchemaName:        diagString(handleType, handle, C.SQL_DIAG_SCHEMA_NAME),
			TableName:         diagString(handleType, handle, C.SQL_DIAG_TABLE_NAME),
			ColumnName:        diagString(handleType, handle, C.SQL_DIAG_COLUMN_NAME),
			CursorName:        diagString(handleType, handle, C.SQL_DIAG_CURSOR_NAME),
		}
	}
}

// diagString is a string field of the first diagnostic record, or "" if it
// can't be read.
func diagString(handleType C.SQLSMALLINT, handle C.SQLINTEGER, field C.SQLSMALLINT) string {
	value := make([]C.SQLCHAR, 129)
	var length C.SQLSMALLINT
	for {
		rc := C.SQLGetDiagField(handleType, handle, 1, field, C.SQLPOINTER(unsafe.Pointer(&value[0])),
			C.SQLSMALLINT(len(value)), &length)
		if rc != C.SQL_SUCCESS && rc != C.SQL_SUCCESS_WITH_INFO {
			return ""
		}

		// The value was truncated.
		if int(length) >= len(value) {
			value = make([]C.SQLCHAR, int(length)+1)
			continue
		}

		return C.GoStringN((*C.char)(unsafe.Pointer(&value[0])), C.int(length))
	}
}

func (h envHandle) check(rc C.SQLRETURN) error {
	return check(rc, C.SQL_HANDLE_ENV, C.SQLINTEGER(h))
}

func (h dbcHandle) check(rc C.SQLRETURN) error {
	return check(rc, C.SQL_HANDLE_DBC, C.SQLINTEGER(h))
}

func (h stmtHandle) check(rc C.SQLRETURN) error {
	return check(rc, C.SQL_HANDLE_STMT, C.SQLINTEGER(h))
}

func allocEnv() (envHandle, error) {
	var env C.SQLHENV
	rc := C.SQLAllocHandle(C.SQL_HANDLE_ENV, C.SQL_NULL_HANDLE, &env)
	if rc != C.SQL_SUCCESS {
		return 0, &Error{SQLState: MemoryAllocationError, Message: "can't allocate an environment"}
	}

	return envHandle(env), nil
}

func (h envHandle) free() error {
	return h.check(C.SQLFreeHandle(C.SQL_HANDLE_ENV, C.SQLINTEGER(h)))
}

func (h envHandle) allocDbc() (dbcHandle, error) {
	var dbc C.SQLHDBC
	if err := h.check(C.SQLAllocHandle(C.SQL_HANDLE_DBC, C.SQLINTEGER(h), &dbc)); err != nil {
		return 0, err
	}

	return dbcHandle(dbc), nil
}

func (h dbcHandle) free() error {
	return h.check(C.SQLFreeHandle(C.SQL_HANDLE_DBC, C.SQLINTEGER(h)))
}

func (h dbcHandle) connect(name string) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	return h.check(C.SQLConnect(C.SQLHDBC(h), (*C.SQLCHAR)(unsafe.Pointer(cname)),
		C.SQL_NTS, nil, 0, nil, 0))
}

func (h dbcHandle) disconnect() error {
	return h.check(C.SQLDisconnect(C.SQLHDBC(h)))
}

func (h dbcHandle) setAutocommit(on bool) error {
	value := (*C.SQLINTEGER)(C.malloc(C.sizeof_SQLINTEGER))
	defer C.free(unsafe.Pointer(value))
	*value = C.SQL_AUTOCOMMIT_OFF
	if on {
		*value = C.SQL_AUTOCOMMIT_ON
	}

	return h.check(C.SQLSetConnectAttr(C.SQLHDBC(h), C.SQL_ATTR_AUTOCOMMIT,
		C.SQLPOINTER(value), 0))
}

// Access modes and isolation levels for startTran.
const (
	transactionReadOnly        = C.SQL_TRANSACTION_READ_ONLY
	transactionReadWrite       = C.SQL_TRANSACTION_READ_WRITE
	transactionReadUncommitted = C.SQL_TRANSACTION_READ_UNCOMMITTED
	transactionReadCommitted   = C.SQL_TRANSACTION_READ_COMMITTED
	transactionRepeatableRead  = C.SQL_TRANSACTION_REPEATABLE_READ
	transactionSerializable    = C.SQL_TRANSACTION_SERIALIZABLE
)

func (h dbcHandle) startTran(accessMode, isolationLevel int) error {
	return h.check(C.SQLStartTran(C.SQL_HANDLE_DBC, C.SQLINTEGER(h),
		C.SQLINTEGER(accessMode), C.SQLINTEGER(isolationLevel)))
}

func (h dbcHandle) endTran(commit bool) error {
	completion := C.SQLSMALLINT(C.SQL_ROLLBACK)
	if commit {
		completion = C.SQL_COMMIT
	}

	return h.check(C.SQLEndTran(C.SQL_HANDLE_DBC, C.SQLINTEGER(h), completion))
}

// rollbackToSavepoint rolls back to a savepoint of the active transaction.
// The name is an identifier, as it's written in SQL (so a delimited identifier
// is in double quotes).
func (h dbcHandle) rollbackToSavepoint(name string) error {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	err := h.check(C.SQLSetConnectAttr(C.SQLHDBC(h), C.SQL_ATTR_SAVEPOINT_NAME,
		C.SQLPOINTER(unsafe.Pointer(cname)), C.SQL_NTS))
	if err != nil {
		return err
	}

	return h.check(C.SQLEndTran(C.SQL_HANDLE_DBC, C.SQLINTEGER(h), C.SQL_SAVEPOINT_NAME_ROLLBACK))
}

// Build information for getInfoString.
const (
	infoBuildVersion    = C.SQL_VSQL_BUILD_VERSION
	infoBuildCommit     = C.SQL_VSQL_BUILD_COMMIT
	infoBuildDate       = C.SQL_VSQL_BUILD_DATE
	infoBuildVVersion   = C.SQL_VSQL_BUILD_V_VERSION
	infoBuildVCommit    = C.SQL_VSQL_BUILD_V_COMMIT
	infoBuildCCompiler  = C.SQL_VSQL_BUILD_C_COMPILER
	infoOperatingSystem = C.SQL_VSQL_OPERATING_SYSTEM
	infoMachine         = C.SQL_VSQL_MACHINE
)

// getInfoString reads a string with SQLGetInfo.
func (h dbcHandle) getInfoString(infoType int) (string, error) {
	value := make([]C.SQLCHAR, 129)
	var length C.SQLSMALLINT
	for {
		err := h.check(C.SQLGetInfo(C.SQLHDBC(h), C.SQLSMALLINT(infoType),
			C.SQLPOINTER(unsafe.Pointer(&value[0])), C.SQLSMALLINT(len(value)), &length))
		if err != nil {
			return "", err
		}

		// The value was truncated.
		if int(length) >= len(value) {
			value = make([]C.SQLCHAR, int(length)+1)
			continue
		}

		return C.GoStringN((*C.char)(unsafe.Pointer(&value[0])), C.int(length)), nil
	}
}

func (h dbcHandle) allocStmt() (stmtHandle, error) {
	var stmt C.SQLHSTMT
	if err := h.check(C.SQLAllocHandle(C.SQL_HANDLE_STMT, C.SQLINTEGER(h), &stmt)); err != nil {
		return 0, err
	}

	return stmtHandle(stmt), nil
}

func (h stmtHandle) free() error {
	return h.check(C.SQLFreeHandle(C.SQL_HANDLE_STMT, C.SQLINTEGER(h)))
}

// operationCanceled is the SQLSTATE of a routine that was canceled by
// SQLCancel.
const operationCanceled SQLState = "HY008"

// cancelable calls a routine that runs on the statement (such as SQLExecute),
// and cancels it with SQLCancel (from another goroutine) if ctx is done before
// it returns. The statement stops soon after, even if it's waiting for
// another connection's transaction, and the error is ctx.Err(). The canceled
// statement has no effect, and its transaction continues.
//
// If ctx is done just before the routine starts, SQLCancel has nothing to
// cancel, so the routine runs as usual.
func (h stmtHandle) cancelable(ctx context.Context, routine func() error) error {
	canceled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(canceled)
		C.SQLCancel(C.SQLHSTMT(h))
	})
	err := routine()
	if !stop() {
		// SQLCancel was called (or is being called), and it has to return
		// before anything else uses the handle.
		<-canceled
		var e *Error
		if errors.As(err, &e) && e.SQLState == operationCanceled {
			return ctx.Err()
		}
	}

	return err
}

func (h stmtHandle) execDirect(ctx context.Context, query string) error {
	cquery := C.CString(query)
	defer C.free(unsafe.Pointer(cquery))

	return h.cancelable(ctx, func() error {
		return h.check(C.SQLExecDirect(C.SQLHSTMT(h), (*C.SQLCHAR)(unsafe.Pointer(cquery)),
			C.SQLINTEGER(len(query))))
	})
}

func (h stmtHandle) prepare(query string) error {
	cquery := C.CString(query)
	defer C.free(unsafe.Pointer(cquery))

	return h.check(C.SQLPrepare(C.SQLHSTMT(h), (*C.SQLCHAR)(unsafe.Pointer(cquery)),
		C.SQLINTEGER(len(query))))
}

// paramTypes is the (concise) data type of each dynamic parameter of a prepared
// statement, from the IPD (which libvsql fills in when it's prepared). A type
// that isn't known until the statement is executed is 0.
func (h stmtHandle) paramTypes() ([]int, error) {
	var ipd C.SQLHDESC
	err := h.check(C.SQLGetStmtAttr(C.SQLHSTMT(h), C.SQL_ATTR_IMP_PARAM_DESC,
		C.SQLPOINTER(unsafe.Pointer(&ipd)), 0, nil))
	if err != nil {
		return nil, err
	}

	checkDesc := func(rc C.SQLRETURN) error {
		return check(rc, C.SQL_HANDLE_DESC, C.SQLINTEGER(ipd))
	}
	var count C.SQLSMALLINT
	if err := checkDesc(C.SQLGetDescField(ipd, 0, C.SQL_DESC_COUNT,
		C.SQLPOINTER(unsafe.Pointer(&count)), 0, nil)); err != nil {
		return nil, err
	}

	types := make([]int, count)
	for i := range types {
		var dataType C.SQLSMALLINT
		rc := C.SQLGetDescField(ipd, C.SQLSMALLINT(i+1), C.SQL_DESC_TYPE,
			C.SQLPOINTER(unsafe.Pointer(&dataType)), 0, nil)
		// A field that was never set (HY091) is an unknown type.
		if rc != C.SQL_SUCCESS && rc != C.SQL_SUCCESS_WITH_INFO {
			continue
		}
		types[i] = int(dataType)

		// SQL_DESC_TYPE is SQL_DATETIME for any datetime, so the concise type
		// (such as SQL_TYPE_TIMESTAMP) is from its datetime interval code.
		if dataType == C.SQL_DATETIME {
			var code C.SQLSMALLINT
			rc := C.SQLGetDescField(ipd, C.SQLSMALLINT(i+1), C.SQL_DESC_DATETIME_INTERVAL_CODE,
				C.SQLPOINTER(unsafe.Pointer(&code)), 0, nil)
			if rc == C.SQL_SUCCESS || rc == C.SQL_SUCCESS_WITH_INFO {
				types[i] = datetimeTypes[code]
			}
		}
	}

	return types, nil
}

// execute executes the prepared statement with the parameters, which are
// bound for this execution only. It's canceled if ctx is done (see
// cancelable).
func (h stmtHandle) execute(ctx context.Context, params []param) error {
	// The values must stay allocated until SQLExecute has read them.
	var allocated []unsafe.Pointer
	defer func() {
		for _, p := range allocated {
			C.free(p)
		}
	}()

	for i, p := range params {
		ind := (*C.SQLINTEGER)(C.malloc(C.sizeof_SQLINTEGER))
		allocated = append(allocated, unsafe.Pointer(ind))

		var value unsafe.Pointer
		var bufferLength C.SQLINTEGER
		switch {
		case p.null:
			*ind = C.SQL_NULL_DATA
		case p.valueType == C.SQL_BIGINT:
			value = C.malloc(C.sizeof_SQLBIGINT)
			*(*C.SQLBIGINT)(value) = C.SQLBIGINT(p.i)
			bufferLength = C.sizeof_SQLBIGINT
		case p.valueType == C.SQL_DOUBLE:
			value = C.malloc(C.sizeof_SQLDOUBLE)
			*(*C.SQLDOUBLE)(value) = C.SQLDOUBLE(p.f)
			bufferLength = C.sizeof_SQLDOUBLE
		case p.valueType == C.SQL_BINARY:
			// A binary string isn't null-terminated, and its length can be 0
			// (but it still needs a buffer).
			value = C.malloc(C.size_t(max(len(p.b), 1)))
			copy(unsafe.Slice((*byte)(value), len(p.b)), p.b)
			bufferLength = C.SQLINTEGER(len(p.b))
			*ind = C.SQLINTEGER(len(p.b))
		default:
			value = C.CBytes([]byte(p.s + "\x00"))
			bufferLength = C.SQLINTEGER(len(p.s) + 1)
			*ind = C.SQLINTEGER(len(p.s))
			// A length of 0 isn't allowed, so an empty string is
			// null-terminated instead.
			if len(p.s) == 0 {
				*ind = C.SQL_NTS
			}
		}
		if value != nil {
			allocated = append(allocated, value)
			if p.valueType != C.SQL_CHAR && p.valueType != C.SQL_BINARY {
				*ind = 0
			}
		}

		err := h.check(C.SQLBindParameter(C.SQLHSTMT(h), C.SQLSMALLINT(i+1),
			C.SQL_PARAM_MODE_IN, C.SQLSMALLINT(p.valueType), C.SQLSMALLINT(p.parameterType),
			C.SQLINTEGER(p.columnSize), C.SQLSMALLINT(p.decimalDigits), C.SQLPOINTER(value),
			bufferLength, ind))
		if err != nil {
			return err
		}
	}

	return h.cancelable(ctx, func() error {
		return h.check(C.SQLExecute(C.SQLHSTMT(h)))
	})
}

func (h stmtHandle) rowCount() (int64, error) {
	var n C.SQLINTEGER
	if err := h.check(C.SQLRowCount(C.SQLHSTMT(h), &n)); err != nil {
		return 0, err
	}

	return int64(n), nil
}

func (h stmtHandle) numResultCols() (int, error) {
	var n C.SQLSMALLINT
	if err := h.check(C.SQLNumResultCols(C.SQLHSTMT(h), &n)); err != nil {
		return 0, err
	}

	return int(n), nil
}

// column is a column of a cursor, from SQLDescribeCol.
type column struct {
	name          string
	dataType      int
	columnSize    int
	decimalDigits int
	nullable      int
}

// Data types, from SQLDescribeCol.
const (
	typeChar                  = C.SQL_CHAR
	typeNumeric               = C.SQL_NUMERIC
	typeDecimal               = C.SQL_DECIMAL
	typeInteger               = C.SQL_INTEGER
	typeSmallint              = C.SQL_SMALLINT
	typeFloat                 = C.SQL_FLOAT
	typeReal                  = C.SQL_REAL
	typeDouble                = C.SQL_DOUBLE
	typeVarchar               = C.SQL_VARCHAR
	typeBoolean               = C.SQL_BOOLEAN
	typeBigint                = C.SQL_BIGINT
	typeDate                  = C.SQL_TYPE_DATE
	typeTime                  = C.SQL_TYPE_TIME
	typeTimestamp             = C.SQL_TYPE_TIMESTAMP
	typeTimeWithTimezone      = C.SQL_TYPE_TIME_WITH_TIMEZONE
	typeTimestampWithTimezone = C.SQL_TYPE_TIMESTAMP_WITH_TIMEZONE
	typeDecfloat              = C.SQL_DECFLOAT
	typeBinary                = C.SQL_BINARY
	typeVarbinary             = C.SQL_VARBINARY
	typeBlob                  = C.SQL_BLOB
	typeClob                  = C.SQL_CLOB

	// The concise codes of the intervals (SQL_INTERVAL is the verbose type of
	// any of them).
	typeInterval               = C.SQL_INTERVAL
	typeIntervalYear           = C.SQL_INTERVAL_YEAR
	typeIntervalMonth          = C.SQL_INTERVAL_MONTH
	typeIntervalDay            = C.SQL_INTERVAL_DAY
	typeIntervalHour           = C.SQL_INTERVAL_HOUR
	typeIntervalMinute         = C.SQL_INTERVAL_MINUTE
	typeIntervalSecond         = C.SQL_INTERVAL_SECOND
	typeIntervalYearToMonth    = C.SQL_INTERVAL_YEAR_TO_MONTH
	typeIntervalDayToHour      = C.SQL_INTERVAL_DAY_TO_HOUR
	typeIntervalDayToMinute    = C.SQL_INTERVAL_DAY_TO_MINUTE
	typeIntervalDayToSecond    = C.SQL_INTERVAL_DAY_TO_SECOND
	typeIntervalHourToMinute   = C.SQL_INTERVAL_HOUR_TO_MINUTE
	typeIntervalHourToSecond   = C.SQL_INTERVAL_HOUR_TO_SECOND
	typeIntervalMinuteToSecond = C.SQL_INTERVAL_MINUTE_TO_SECOND
)

// datetimeTypes are the concise types of the SQL_DESC_DATETIME_INTERVAL_CODEs
// of SQL_DATETIME.
var datetimeTypes = map[C.SQLSMALLINT]int{
	C.SQL_CODE_DATE:           typeDate,
	C.SQL_CODE_TIME:           typeTime,
	C.SQL_CODE_TIMESTAMP:      typeTimestamp,
	C.SQL_CODE_TIME_ZONE:      typeTimeWithTimezone,
	C.SQL_CODE_TIMESTAMP_ZONE: typeTimestampWithTimezone,
}

// Nullability, from SQLDescribeCol.
const (
	noNulls        = C.SQL_NO_NULLS
	nullableColumn = C.SQL_NULLABLE
)

func (h stmtHandle) describeCol(n int) (column, error) {
	name := make([]C.SQLCHAR, 129)
	var nameLength, dataType, decimalDigits, null C.SQLSMALLINT
	var columnSize C.SQLINTEGER
	for {
		err := h.check(C.SQLDescribeCol(C.SQLHSTMT(h), C.SQLSMALLINT(n), &name[0],
			C.SQLSMALLINT(len(name)), &nameLength, &dataType, &columnSize, &decimalDigits, &null))
		if err != nil {
			return column{}, err
		}

		// The name was truncated.
		if int(nameLength) >= len(name) {
			name = make([]C.SQLCHAR, int(nameLength)+1)
			continue
		}

		return column{
			name:          C.GoStringN((*C.char)(unsafe.Pointer(&name[0])), C.int(nameLength)),
			dataType:      int(dataType),
			columnSize:    int(columnSize),
			decimalDigits: int(decimalDigits),
			nullable:      int(null),
		}, nil
	}
}

// fetch moves to the next row, it's false after the last row. It's canceled
// if ctx is done (see cancelable).
func (h stmtHandle) fetch(ctx context.Context) (bool, error) {
	ok := true
	err := h.cancelable(ctx, func() error {
		rc := C.SQLFetch(C.SQLHSTMT(h))
		ok = rc != C.SQL_NO_DATA

		return h.check(rc)
	})

	return ok, err
}

func (h stmtHandle) closeCursor() error {
	return h.check(C.SQLCloseCursor(C.SQLHSTMT(h)))
}

// buffer is C memory for reading values with SQLGetData, which is reused for
// each value.
type buffer struct {
	data unsafe.Pointer
	size int
	ind  *C.SQLINTEGER
}

func newBuffer() *buffer {
	return &buffer{
		data: C.malloc(256),
		size: 256,
		ind:  (*C.SQLINTEGER)(C.malloc(C.sizeof_SQLINTEGER)),
	}
}

func (b *buffer) free() {
	C.free(b.data)
	C.free(unsafe.Pointer(b.ind))
}

// getInt64 reads a column as an SQLBIGINT, it's false for a NULL.
func (h stmtHandle) getInt64(n int, b *buffer) (int64, bool, error) {
	err := h.check(C.SQLGetData(C.SQLHSTMT(h), C.SQLSMALLINT(n), C.SQL_BIGINT, C.SQLPOINTER(b.data),
		C.sizeof_SQLBIGINT, b.ind))
	if err != nil || *b.ind == C.SQL_NULL_DATA {
		return 0, false, err
	}

	return int64(*(*C.SQLBIGINT)(b.data)), true, nil
}

// getFloat64 reads a column as an SQLDOUBLE (or SQLREAL, if real is true), it's
// false for a NULL.
func (h stmtHandle) getFloat64(n int, real bool, b *buffer) (float64, bool, error) {
	targetType := C.SQLSMALLINT(C.SQL_DOUBLE)
	if real {
		targetType = C.SQL_REAL
	}
	err := h.check(C.SQLGetData(C.SQLHSTMT(h), C.SQLSMALLINT(n), targetType, C.SQLPOINTER(b.data),
		C.sizeof_SQLDOUBLE, b.ind))
	if err != nil || *b.ind == C.SQL_NULL_DATA {
		return 0, false, err
	}
	if real {
		return float64(*(*C.SQLREAL)(b.data)), true, nil
	}

	return float64(*(*C.SQLDOUBLE)(b.data)), true, nil
}

// getString reads a column as SQL_CHAR, it's false for a NULL. A string that
// doesn't fit in the buffer is read in pieces, and the buffer grows to the
// size of the rest of it.
//
// The string can contain NUL bytes, so the length of a piece isn't found with
// strlen. It's the length that ind had before the piece was read, less the
// length it has after.
func (h stmtHandle) getString(n int, b *buffer) (string, bool, error) {
	var s, piece []byte
	pieceInd := 0
	for {
		rc := C.SQLGetData(C.SQLHSTMT(h), C.SQLSMALLINT(n), C.SQL_CHAR, C.SQLPOINTER(b.data),
			C.SQLINTEGER(b.size), b.ind)
		if err := h.check(rc); err != nil {
			return "", false, err
		}
		if *b.ind == C.SQL_NULL_DATA {
			return "", false, nil
		}
		if piece != nil {
			s = append(s, piece[:pieceInd-int(*b.ind)]...)
			piece = nil
		}

		// The rest of the string (ind) didn't fit, so the buffer has a null
		// terminated piece of it.
		if rc == C.SQL_SUCCESS_WITH_INFO && int(*b.ind) >= b.size {
			piece = C.GoBytes(b.data, C.int(b.size-1))
			pieceInd = int(*b.ind)
			C.free(b.data)
			b.size = pieceInd + 1
			b.data = C.malloc(C.size_t(b.size))
			continue
		}

		if s == nil {
			return C.GoStringN((*C.char)(b.data), C.int(*b.ind)), true, nil
		}

		return string(append(s, C.GoBytes(b.data, C.int(*b.ind))...)), true, nil
	}
}

// getBytes reads a binary string column as SQL_BINARY, it's false for a NULL.
// A value that doesn't fit in the buffer is read in pieces, like getString.
func (h stmtHandle) getBytes(n int, b *buffer) ([]byte, bool, error) {
	s := []byte{}
	for {
		rc := C.SQLGetData(C.SQLHSTMT(h), C.SQLSMALLINT(n), C.SQL_BINARY, C.SQLPOINTER(b.data),
			C.SQLINTEGER(b.size), b.ind)
		if err := h.check(rc); err != nil {
			return nil, false, err
		}
		if *b.ind == C.SQL_NULL_DATA {
			return nil, false, nil
		}

		// ind is the length of the rest of the value, and the buffer has as
		// much of it as fits.
		rest := int(*b.ind)
		s = append(s, C.GoBytes(b.data, C.int(min(rest, b.size)))...)
		if rc != C.SQL_SUCCESS_WITH_INFO || rest <= b.size {
			return s, true, nil
		}

		rest -= b.size
		C.free(b.data)
		b.size = rest
		b.data = C.malloc(C.size_t(b.size))
	}
}

// Value types and parameter types for SQLBindParameter.
const (
	valueChar   = C.SQL_CHAR
	valueBigint = C.SQL_BIGINT
	valueDouble = C.SQL_DOUBLE
	valueBinary = C.SQL_BINARY
)

// param is a value for SQLBindParameter.
type param struct {
	null          bool
	valueType     int
	parameterType int
	columnSize    int
	decimalDigits int

	// One of these is the value, depending on valueType.
	i int64
	f float64
	s string
	b []byte
}
