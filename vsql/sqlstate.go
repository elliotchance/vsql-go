package vsql

// SQLState is a five character SQLSTATE, such as "42P01". The first two
// characters are its class (see Class).
//
// The constants are the SQLSTATEs that vsql and libvsql return, see
// https://vsql.readthedocs.io/en/latest/sqlstate.html
//
// Each name ends with the kind of condition, from its class: none for class 00
// (successful completion), Warning for 01, NoData for 02 and Error for any
// other class (an exception condition).
type SQLState string

// Class is the first two characters of the SQLSTATE, such as
// SyntaxErrorOrAccessRuleViolationClass ("42") for 42P01.
func (s SQLState) Class() SQLStateClass {
	if len(s) < 2 {
		return SQLStateClass(s)
	}

	return SQLStateClass(s[:2])
}

// IsError is true for an exception condition, which is any class other than
// successful completion (00), warning (01) and no data (02).
func (s SQLState) IsError() bool {
	switch s.Class() {
	case SuccessfulCompletionClass, WarningClass, NoDataClass:
		return false
	}

	return true
}

// SQLStateClass is the class of an SQLSTATE, its first two characters (see
// SQLState.Class).
type SQLStateClass string

// The class of each SQLSTATE that vsql and libvsql return.
const (
	SuccessfulCompletionClass                    SQLStateClass = "00"
	WarningClass                                 SQLStateClass = "01"
	NoDataClass                                  SQLStateClass = "02"
	DynamicSQLErrorClass                         SQLStateClass = "07"
	ConnectionExceptionClass                     SQLStateClass = "08"
	FeatureNotSupportedClass                     SQLStateClass = "0A"
	InvalidTransactionInitiationClass            SQLStateClass = "0B"
	CardinalityViolationClass                    SQLStateClass = "21"
	DataExceptionClass                           SQLStateClass = "22"
	IntegrityConstraintViolationClass            SQLStateClass = "23"
	InvalidCursorStateClass                      SQLStateClass = "24"
	InvalidTransactionStateClass                 SQLStateClass = "25"
	InvalidAuthorizationSpecificationClass       SQLStateClass = "28"
	DependentPrivilegeDescriptorsStillExistClass SQLStateClass = "2B"
	InvalidCharacterSetNameClass                 SQLStateClass = "2C"
	InvalidTransactionTerminationClass           SQLStateClass = "2D"
	InvalidConnectionNameClass                   SQLStateClass = "2E"
	InvalidCursorNameClass                       SQLStateClass = "34"
	InvalidConditionNumberClass                  SQLStateClass = "35"
	CursorSensitivityExceptionClass              SQLStateClass = "36"
	SavepointExceptionClass                      SQLStateClass = "3B"
	InvalidCatalogNameClass                      SQLStateClass = "3D"
	InvalidSchemaNameClass                       SQLStateClass = "3F"
	TransactionRollbackClass                     SQLStateClass = "40"
	SyntaxErrorOrAccessRuleViolationClass        SQLStateClass = "42"
	ProgramLimitExceededClass                    SQLStateClass = "54"
	SystemErrorClass                             SQLStateClass = "58"
	CLISpecificConditionClass                    SQLStateClass = "HY"
	InternalErrorClass                           SQLStateClass = "XX"
)

const (
	// Class 00: successful completion.
	SuccessfulCompletion SQLState = "00000"

	// Class 01: warning.
	Warning                                 SQLState = "01000"
	NullValueEliminatedInSetFunctionWarning SQLState = "01003"
	StringDataRightTruncationWarning        SQLState = "01004"
	InvalidNumberOfConditionsWarning        SQLState = "01012"

	// Class 02: no data.
	NoData                               SQLState = "02000"
	NoAdditionalResultSetsReturnedNoData SQLState = "02001"

	// Class 07: dynamic SQL error.
	UsingClauseDoesNotMatchDynamicParameterSpecificationsError SQLState = "07001"
	UsingClauseDoesNotMatchTargetSpecificationsError           SQLState = "07002"
	PreparedStatementNotACursorSpecificationError              SQLState = "07005"
	RestrictedDataTypeAttributeViolationError                  SQLState = "07006"
	InvalidDescriptorCountError                                SQLState = "07008"
	InvalidDescriptorIndexError                                SQLState = "07009"
	InvalidLevelValueError                                     SQLState = "0700E"

	// Class 08: connection exception.
	ClientUnableToEstablishConnectionError SQLState = "08001"
	ConnectionNameInUseError               SQLState = "08002"
	ConnectionDoesNotExistError            SQLState = "08003"

	// Class 0A: feature not supported.
	FeatureNotSupportedError        SQLState = "0A000"
	MultipleServerTransactionsError SQLState = "0A001"

	// Class 0B: invalid transaction initiation.
	InvalidTransactionInitiationError SQLState = "0B000"

	// Class 21: cardinality violation.
	CardinalityViolationError SQLState = "21000"

	// Class 22: data exception.
	StringDataRightTruncationError           SQLState = "22001"
	NullValueNoIndicatorParameterError       SQLState = "22002"
	NumericValueOutOfRangeError              SQLState = "22003"
	InvalidDatetimeFormatError               SQLState = "22007"
	EscapeCharacterConflictError             SQLState = "2200B"
	InvalidUseOfEscapeCharacterError         SQLState = "2200C"
	SequenceGeneratorLimitExceededError      SQLState = "2200H"
	SubstringError                           SQLState = "22011"
	DivisionByZeroError                      SQLState = "22012"
	InvalidCharacterValueForCastError        SQLState = "22018"
	InvalidEscapeCharacterError              SQLState = "22019"
	InvalidRegularExpressionError            SQLState = "2201B"
	InvalidArgumentForNaturalLogarithmError  SQLState = "2201E"
	InvalidArgumentForPowerFunctionError     SQLState = "2201F"
	InvalidRowCountInFetchFirstClauseError   SQLState = "2201W"
	InvalidRowCountInResultOffsetClauseError SQLState = "2201X"
	InvalidEscapeSequenceError               SQLState = "22025"
	TrimError                                SQLState = "22027"

	// Class 23: integrity constraint violation.
	NotNullViolationError    SQLState = "23502"
	ForeignKeyViolationError SQLState = "23503"
	UniqueViolationError     SQLState = "23505"
	CheckViolationError      SQLState = "23514"

	// Class 24: invalid cursor state.
	InvalidCursorStateError SQLState = "24000"

	// Class 25: invalid transaction state.
	ActiveTransactionError   SQLState = "25001"
	ReadOnlyTransactionError SQLState = "25006"

	// Class 28: invalid authorization specification.
	InvalidAuthorizationSpecificationError SQLState = "28000"

	// Class 2B: dependent privilege descriptors still exist.
	DependentObjectsStillExistError SQLState = "2BP01"

	// Class 2C: invalid character set name.
	InvalidCharacterSetNameError SQLState = "2C000"

	// Class 2D: invalid transaction termination.
	InvalidTransactionTerminationError SQLState = "2D000"

	// Class 2E: invalid connection name.
	InvalidConnectionNameError SQLState = "2E000"

	// Class 34: invalid cursor name.
	InvalidCursorNameError SQLState = "34000"

	// Class 35: invalid condition number.
	InvalidConditionNumberError SQLState = "35000"

	// Class 36: cursor sensitivity exception.
	CursorSensitivityRequestRejectedError SQLState = "36001"

	// Class 3B: savepoint exception.
	InvalidSavepointSpecificationError SQLState = "3B001"

	// Class 3D: invalid catalog name.
	InvalidCatalogNameError SQLState = "3D000"

	// Class 3F: invalid schema name.
	InvalidSchemaNameError SQLState = "3F000"

	// Class 40: transaction rollback.
	SerializationFailureError                    SQLState = "40001"
	TransactionIntegrityConstraintViolationError SQLState = "40002"

	// Class 42: syntax error or access rule violation.
	SyntaxErrorOrAccessRuleViolationError SQLState = "42000"
	InsufficientPrivilegeError            SQLState = "42501"
	SyntaxError                           SQLState = "42601"
	InvalidNameError                      SQLState = "42602"
	AmbiguousColumnError                  SQLState = "42702"
	UndefinedColumnError                  SQLState = "42703"
	UndefinedObjectError                  SQLState = "42704"
	DuplicateObjectError                  SQLState = "42710"
	DuplicateFunctionError                SQLState = "42723"
	GroupingError                         SQLState = "42803"
	DatatypeMismatchError                 SQLState = "42804"
	InvalidForeignKeyError                SQLState = "42830"
	CannotCoerceError                     SQLState = "42846"
	UndefinedFunctionError                SQLState = "42883"
	GeneratedAlwaysError                  SQLState = "428C9"
	UndefinedTableError                   SQLState = "42P01"
	UndefinedParameterError               SQLState = "42P02"
	DuplicateCatalogError                 SQLState = "42P04"
	DuplicateSchemaError                  SQLState = "42P06"
	DuplicateTableError                   SQLState = "42P07"
	InvalidColumnReferenceError           SQLState = "42P10"

	// Class 54: program limit exceeded.
	ProgramLimitExceededError SQLState = "54000"

	// Class 58: system error.
	IOError SQLState = "58030"

	// Class HY: CLI-specific condition.
	CLISpecificConditionError                               SQLState = "HY000"
	MemoryAllocationError                                   SQLState = "HY001"
	InvalidDataTypeInApplicationDescriptorError             SQLState = "HY003"
	InvalidDataTypeError                                    SQLState = "HY004"
	AssociatedStatementIsNotPreparedError                   SQLState = "HY007"
	InvalidUseOfNullPointerError                            SQLState = "HY009"
	FunctionSequenceError                                   SQLState = "HY010"
	AttributeCannotBeSetNowError                            SQLState = "HY011"
	InvalidTransactionOperationCodeError                    SQLState = "HY012"
	CannotModifyAnImplementationRowDescriptorError          SQLState = "HY016"
	InvalidUseOfAutomaticallyAllocatedDescriptorHandleError SQLState = "HY017"
	NonStringDataCannotBeSentInPiecesError                  SQLState = "HY019"
	AttemptToConcatenateANullValueError                     SQLState = "HY020"
	InconsistentDescriptorInformationError                  SQLState = "HY021"
	InvalidAttributeValueError                              SQLState = "HY024"
	InvalidStringLengthOrBufferLengthError                  SQLState = "HY090"
	InvalidDescriptorFieldIdentifierError                   SQLState = "HY091"
	InvalidAttributeIdentifierError                         SQLState = "HY092"
	InvalidFunctionIDError                                  SQLState = "HY095"
	InvalidInformationTypeError                             SQLState = "HY096"
	ColumnTypeOutOfRangeError                               SQLState = "HY097"
	ScopeOutOfRangeError                                    SQLState = "HY098"
	NullableTypeOutOfRangeError                             SQLState = "HY099"
	InvalidRetrievalCodeError                               SQLState = "HY103"
	InvalidParameterModeError                               SQLState = "HY105"
	InvalidFetchOrientationError                            SQLState = "HY106"
	RowValueOutOfRangeError                                 SQLState = "HY107"
	InvalidCursorPositionError                              SQLState = "HY109"
	OptionalFeatureNotImplementedError                      SQLState = "HYC00"

	// Class XX: internal error.
	InternalError      SQLState = "XX000"
	DataCorruptedError SQLState = "XX001"
)
