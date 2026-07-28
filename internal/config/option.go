// Package config provides configuration types and resolution for the ilter proxy.
//
// This file defines the Option[T] generic wrapper and concrete types for
// database/sql scanning, enabling distinction between "unset" and "zero value"
// across the config resolution chain.
package config

import (
	"database/sql"
)

// Option[T] is a generic optional value wrapper that distinguishes
// "unset" from "zero value". Useful for config resolution chains where
// each scope (perKey, team, org, global) may or may not define a value.
type Option[T any] interface {
	// IsSet reports whether the value has been explicitly set.
	IsSet() bool

	// Value returns the underlying value and whether it was set.
	// When IsSet() is false, the returned T is the zero value.
	Value() (T, bool)
}

// Some wraps a set value into an Option[T]. Useful for constructing
// non-nil optionals in scope resolvers.
func Some[T any](val T) Option[T] {
	return some[T]{val: val}
}

// some is the concrete "set" implementation of Option[T].
type some[T any] struct {
	val T
}

func (s some[T]) IsSet() bool      { return true }
func (s some[T]) Value() (T, bool) { return s.val, true }

// None returns an unset Option[T]. The returned value's IsSet() is always false.
func None[T any]() Option[T] {
	return none[T]{}
}

// none is the concrete "unset" implementation of Option[T].
type none[T any] struct{}

func (n none[T]) IsSet() bool      { return false }
func (n none[T]) Value() (T, bool) { var zero T; return zero, false }

// ─────────────────────────────────────────────────────────────────────
// optionAny — non-generic interface for runtime resolution chains.
// All concrete Option types below implement this interface privately,
// allowing the Resolve function to iterate heterogeneous scopes
// without generic boilerplate.
// ─────────────────────────────────────────────────────────────────────

type optionAny interface {
	isSet() bool
	get() any
}

// optionNone is a singleton "not set" value implementing optionAny.
// Used by stub resolvers.
type optionNone struct{}

func (optionNone) isSet() bool { return false }
func (optionNone) get() any    { return nil }

// ─────────────────────────────────────────────────────────────────────
// optionCore provides the shared embedding base for all concrete
// DB-scannable Option types. It implements IsSet, isSet, get, and Value
// so that each concrete type only needs to supply Scan.
// ─────────────────────────────────────────────────────────────────────

type optionCore[T any] struct {
	val     T
	present bool // true when explicitly set (distinct from zero value)
}

func (o optionCore[T]) IsSet() bool      { return o.present }
func (o optionCore[T]) Value() (T, bool) { return o.val, o.present }
func (o optionCore[T]) isSet() bool      { return o.present }
func (o optionCore[T]) get() any         { return o.val }

// ─────────────────────────────────────────────────────────────────────
// Concrete Option types — each embeds optionCore[T] and implements
// database/sql.Scanner for direct DB row scanning.
// ─────────────────────────────────────────────────────────────────────

// OptionBool is a scannable optional bool.
type OptionBool struct {
	optionCore[bool]
}

// Scan implements database/sql.Scanner. A NULL value leaves the option unset.
func (o *OptionBool) Scan(src any) error {
	o.present = false
	if src == nil {
		return nil
	}
	var nb sql.NullBool
	if err := nb.Scan(src); err != nil {
		return err
	}
	o.val = nb.Bool
	o.present = nb.Valid
	return nil
}

// OptionFloat64 is a scannable optional float64.
// Zero value (0.0) after a Scan from a valid source is distinguishable
// from "unset" via IsSet().
type OptionFloat64 struct {
	optionCore[float64]
}

// Scan implements database/sql.Scanner. A NULL value leaves the option unset.
func (o *OptionFloat64) Scan(src any) error {
	o.present = false
	if src == nil {
		return nil
	}
	var nf sql.NullFloat64
	if err := nf.Scan(src); err != nil {
		return err
	}
	o.val = nf.Float64
	o.present = nf.Valid
	return nil
}

// OptionString is a scannable optional string.
type OptionString struct {
	optionCore[string]
}

// Scan implements database/sql.Scanner. A NULL value leaves the option unset.
func (o *OptionString) Scan(src any) error {
	o.present = false
	if src == nil {
		return nil
	}
	var ns sql.NullString
	if err := ns.Scan(src); err != nil {
		return err
	}
	o.val = ns.String
	o.present = ns.Valid
	return nil
}

// OptionInt64 is a scannable optional int64.
type OptionInt64 struct {
	optionCore[int64]
}

// Scan implements database/sql.Scanner. A NULL value leaves the option unset.
func (o *OptionInt64) Scan(src any) error {
	o.present = false
	if src == nil {
		return nil
	}
	var ni sql.NullInt64
	if err := ni.Scan(src); err != nil {
		return err
	}
	o.val = ni.Int64
	o.present = ni.Valid
	return nil
}

// compile-time interface checks
var (
	_ Option[bool]    = OptionBool{}
	_ Option[float64] = OptionFloat64{}
	_ Option[string]  = OptionString{}
	_ Option[int64]   = OptionInt64{}
	_ optionAny       = OptionBool{}
	_ optionAny       = OptionFloat64{}
	_ optionAny       = OptionString{}
	_ optionAny       = OptionInt64{}
	_ optionAny       = optionNone{}
)
