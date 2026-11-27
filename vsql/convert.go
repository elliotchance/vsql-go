package vsql

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"
)

// layoutParameter is a time.Time parameter, as text for a TIMESTAMP(6) WITH
// TIME ZONE.
const layoutParameter = "2006-01-02 15:04:05.000000-07:00"

// CheckNamedValue allows *big.Int, *big.Rat and time.Duration, along with the
// types that database/sql converts to a driver.Value.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	switch nv.Value.(type) {
	case *big.Int, *big.Rat, time.Duration:
		return nil
	}

	return driver.ErrSkip
}

// toParams are the parameters for a statement, with the types of its dynamic
// parameters from paramTypes (which may be nil).
func toParams(args []driver.NamedValue, types []int) ([]param, error) {
	params := make([]param, len(args))
	for i, arg := range args {
		if arg.Name != "" {
			return nil, fmt.Errorf("vsql: named parameters are not supported (%s)", arg.Name)
		}

		var err error
		dataType := 0
		if i < len(types) {
			dataType = types[i]
		}
		if params[i], err = toParam(arg.Value, dataType); err != nil {
			return nil, fmt.Errorf("vsql: parameter %d: %w", arg.Ordinal, err)
		}
	}

	return params, nil
}

// toParam is a value for a dynamic parameter of dataType (0 if it's not
// known).
func toParam(v driver.Value, dataType int) (param, error) {
	switch v := v.(type) {
	case nil:
		// The type doesn't matter for a NULL, since it's cast to the type it's
		// used as.
		return param{null: true, valueType: valueChar, parameterType: typeVarchar, columnSize: 1}, nil
	case int64:
		return param{valueType: valueBigint, parameterType: typeBigint, i: v}, nil
	case float64:
		return param{valueType: valueDouble, parameterType: typeDouble, f: v}, nil
	case bool:
		s := "FALSE"
		if v {
			s = "TRUE"
		}
		return param{valueType: valueChar, parameterType: typeBoolean, s: s}, nil
	case string:
		return stringParam(v), nil
	case []byte:
		if v == nil {
			return toParam(nil, dataType)
		}
		// A binary string can only be cast to (or from) another binary string,
		// so []byte is a string for anything else, such as a CHARACTER VARYING
		// column.
		switch dataType {
		case typeBinary, typeVarbinary, typeBlob, 0:
			return param{valueType: valueBinary, parameterType: typeVarbinary, columnSize: max(len(v), 1), b: v}, nil
		}
		return stringParam(string(v)), nil
	case time.Time:
		// A TIMESTAMP or TIME without a time zone keeps the wall clock and
		// drops the offset, and it's read back as UTC. So it's sent in UTC,
		// which keeps the instant. A DATE is the date in the value's own
		// location.
		//
		// A time zone displacement is only hours and minutes, so a value with
		// seconds in its offset (such as local mean time before 1900) is also
		// sent in UTC, unless it's a DATE.
		_, offset := v.Zone()
		if dataType == typeTimestamp || dataType == typeTime ||
			(offset%60 != 0 && dataType != typeDate) {
			v = v.UTC()
		}
		s := v.Format(layoutParameter)
		return param{
			valueType:     valueChar,
			parameterType: typeTimestampWithTimezone,
			columnSize:    len(s),
			decimalDigits: 6,
			s:             s,
		}, nil
	case time.Duration:
		s, err := intervalString(v, dataType)
		if err != nil {
			return param{}, err
		}
		return stringParam(s), nil
	case *big.Int:
		if v == nil {
			return toParam(nil, dataType)
		}
		return numericParam(v.String(), 0), nil
	case *big.Rat:
		if v == nil {
			return toParam(nil, dataType)
		}
		s, scale, err := ratString(v)
		if err != nil {
			return param{}, err
		}
		return numericParam(s, scale), nil
	}

	return param{}, fmt.Errorf("unsupported type %T", v)
}

// stringParam is a CHARACTER VARYING as long as the string (which can't be 0).
func stringParam(s string) param {
	return param{
		valueType:     valueChar,
		parameterType: typeVarchar,
		columnSize:    max(utf8.RuneCountInString(s), 1),
		s:             s,
	}
}

// numericParam is a NUMERIC with the precision of the decimal digits in s.
func numericParam(s string, scale int) param {
	digits := len(strings.TrimLeft(strings.NewReplacer("-", "", ".", "").Replace(s), "0"))

	return param{
		valueType:     valueChar,
		parameterType: typeNumeric,
		columnSize:    max(digits, scale, 1),
		decimalDigits: scale,
		s:             s,
	}
}

var errNotDecimal = errors.New("*big.Rat isn't a finite decimal")

// ratString is the exact decimal of a *big.Rat, and its scale. It's an error
// if the denominator has a prime factor other than 2 and 5.
func ratString(r *big.Rat) (string, int, error) {
	// The scale is the number of times the denominator can be divided by 2 or
	// 5, whichever is greater.
	d := new(big.Int).Set(r.Denom())
	scale := 0
	two, five := 0, 0
	m := new(big.Int)
	for _, p := range []struct {
		n     int64
		count *int
	}{{2, &two}, {5, &five}} {
		divisor := big.NewInt(p.n)
		for {
			q, rem := new(big.Int).QuoRem(d, divisor, m)
			if rem.Sign() != 0 {
				break
			}
			d = q
			*p.count++
		}
	}
	if d.Cmp(big.NewInt(1)) != 0 {
		return "", 0, errNotDecimal
	}
	scale = max(two, five)

	return r.FloatString(scale), scale, nil
}

// intervalString is a time.Duration as the string in an interval literal of
// a day-time interval type, such as "1:30:00" for an INTERVAL HOUR TO SECOND.
// It's an INTERVAL DAY TO SECOND if dataType isn't a day-time interval. The
// leading field has every unit above it, so 25 hours is "25" for an INTERVAL
// HOUR. It's an error if the duration has a part that's smaller than the
// last field.
func intervalString(d time.Duration, dataType int) (string, error) {
	sign := ""
	n := uint64(d)
	if d < 0 {
		sign = "-"
		n = -n
	}
	ns := n % uint64(time.Second)
	seconds := n / uint64(time.Second)
	minutes := seconds / 60
	hours := minutes / 60
	days := hours / 24

	fraction := ""
	if ns != 0 {
		fraction = strings.TrimRight(fmt.Sprintf(".%09d", ns), "0")
	}
	exact := func(unit string, rest uint64) error {
		if rest != 0 {
			return fmt.Errorf("%s isn't a whole number of %s for %s", d, unit, typeNames[dataType])
		}
		return nil
	}

	var s string
	var err error
	switch dataType {
	case typeIntervalDay:
		s, err = fmt.Sprint(days), exact("days", n%uint64(24*time.Hour))
	case typeIntervalHour:
		s, err = fmt.Sprint(hours), exact("hours", n%uint64(time.Hour))
	case typeIntervalMinute:
		s, err = fmt.Sprint(minutes), exact("minutes", n%uint64(time.Minute))
	case typeIntervalSecond:
		s = fmt.Sprintf("%d%s", seconds, fraction)
	case typeIntervalDayToHour:
		s, err = fmt.Sprintf("%d %02d", days, hours%24), exact("hours", n%uint64(time.Hour))
	case typeIntervalDayToMinute:
		s, err = fmt.Sprintf("%d %02d:%02d", days, hours%24, minutes%60), exact("minutes", n%uint64(time.Minute))
	case typeIntervalHourToMinute:
		s, err = fmt.Sprintf("%d:%02d", hours, minutes%60), exact("minutes", n%uint64(time.Minute))
	case typeIntervalHourToSecond:
		s = fmt.Sprintf("%d:%02d:%02d%s", hours, minutes%60, seconds%60, fraction)
	case typeIntervalMinuteToSecond:
		s = fmt.Sprintf("%d:%02d%s", minutes, seconds%60, fraction)
	default:
		s = fmt.Sprintf("%d %02d:%02d:%02d%s", days, hours%24, minutes%60, seconds%60, fraction)
	}

	return sign + s, err
}
