// Package gtfparser is an interpreter for the TinyFugue scripting language,
// written from TinyFugue's published documentation and its observable
// behaviour. It is meant to be embedded (squatterd, squatclient) and to back
// a standalone terminal client (cmd/gotf).
package gtfparser

import (
	"math"
	"strconv"
	"strings"
)

type kind uint8

const (
	kInt kind = iota
	kReal
	kStr
)

// Value is a TF value: integer, real, or string. Reals also stand for
// dtimes and atimes, as in TF.
type Value struct {
	k  kind
	i  int64
	f  float64
	s  string
	dt bool // a dtime literal: prints with a fractional part
}

func Int(n int64) Value    { return Value{k: kInt, i: n} }
func Real(f float64) Value { return Value{k: kReal, f: f} }
func Str(s string) Value   { return Value{k: kStr, s: s} }
func Bool(b bool) Value {
	if b {
		return Int(1)
	}
	return Int(0)
}

// SigFigs is the number of significant figures used when formatting reals.
var SigFigs = 15

func (v Value) String() string {
	switch v.k {
	case kInt:
		return strconv.FormatInt(v.i, 10)
	case kReal:
		if v.dt {
			if r := formatReal0(v.f); !strings.ContainsAny(r, ".en") {
				return r + ".0"
			}
			return formatReal0(v.f)
		}
		return formatReal(v.f)
	}
	return v.s
}

func formatReal(f float64) string {
	s := formatReal0(f)
	if !strings.ContainsAny(s, ".eEn") { // integral reals keep a decimal point, as TF prints them
		s += "."
	}
	return s
}

// formatReal0 mimics how TF prints reals: plain decimal notation where it
// reads well, %g-style exponent form otherwise (checked against real tf).
func formatReal0(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	if f == 0 {
		return "0"
	}
	e := int(math.Floor(math.Log10(math.Abs(f))))
	dec := strconv.FormatFloat(f, 'f', -1, 64)
	sig := len(strings.TrimLeft(strings.Replace(strings.TrimPrefix(dec, "-"), ".", "", 1), "0"))
	switch {
	case f == math.Trunc(f):
		if e < SigFigs {
			return dec
		}
	case e >= 17:
	case e < -4:
		if e >= -6 && sig <= SigFigs {
			return dec
		}
	default:
		if sig > SigFigs { // e.g. 0.1+0.2: round to the significant figures
			if r := strconv.FormatFloat(f, 'g', SigFigs, 64); !strings.Contains(r, "e") {
				return r
			}
		}
		return dec
	}
	return strconv.FormatFloat(f, 'g', SigFigs, 64)
}

// Num converts to a number (int or real); strings are parsed by prefix.
func (v Value) Num() Value {
	if v.k != kStr {
		return v
	}
	return parseNumPrefix(v.s)
}

func (v Value) Float() float64 {
	n := v.Num()
	if n.k == kInt {
		return float64(n.i)
	}
	return n.f
}

func (v Value) Int() int64 {
	n := v.Num()
	if n.k == kInt {
		return n.i
	}
	return int64(n.f)
}

func (v Value) Truth() bool {
	n := v.Num()
	if n.k == kInt {
		return n.i != 0
	}
	return n.f != 0
}

// parseNumPrefix reads a leading integer, real or dtime (h:m[:s]) from s.
// Anything else is 0.
func parseNumPrefix(s string) Value {
	s = strings.TrimLeft(s, " \t")
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	ds := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	intEnd := i
	if intEnd == ds && !(i < len(s) && s[i] == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9') {
		return Int(0)
	}
	// dtime: digits ':' digits [':' digits[.frac]]
	if i < len(s) && s[i] == ':' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
		neg := strings.HasPrefix(s, "-")
		total := 0.0
		parts := 0
		j := ds
		for parts < 3 {
			k := j
			for k < len(s) && s[k] >= '0' && s[k] <= '9' {
				k++
			}
			if parts == 2 && k < len(s) && s[k] == '.' {
				k++
				for k < len(s) && s[k] >= '0' && s[k] <= '9' {
					k++
				}
			}
			f, _ := strconv.ParseFloat(s[j:k], 64)
			total = total*60 + f
			parts++
			j = k
			if j < len(s) && s[j] == ':' && j+1 < len(s) && s[j+1] >= '0' && s[j+1] <= '9' && parts < 3 {
				j++
				continue
			}
			break
		}
		if parts == 2 { // h:m, accumulated as minutes
			total *= 60
		}
		if neg {
			total = -total
		}
		v := Real(total)
		v.dt = true
		return v
	}
	isReal := false
	if i < len(s) && s[i] == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
		isReal = true
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	} else if i < len(s) && s[i] == '.' {
		isReal = true
		i++
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && s[j] >= '0' && s[j] <= '9' {
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			isReal = true
			i = j
		}
	}
	if isReal {
		f, _ := strconv.ParseFloat(s[:i], 64)
		return Real(f)
	}
	n, err := strconv.ParseInt(s[:intEnd], 10, 64)
	if err != nil {
		f, _ := strconv.ParseFloat(s[:intEnd], 64)
		return Real(f)
	}
	return Int(n)
}
