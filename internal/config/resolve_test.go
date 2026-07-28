package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ─────────────────────────────────────────────────────────────────────
// Unit tests — Option types (generic + concrete)
// ─────────────────────────────────────────────────────────────────────

func TestOption_Some(t *testing.T) {
	opt := Some("hello")
	assert.True(t, opt.IsSet())
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.Equal(t, "hello", v)
}

func TestOption_None(t *testing.T) {
	opt := None[string]()
	assert.False(t, opt.IsSet())
	v, ok := opt.Value()
	assert.False(t, ok)
	assert.Empty(t, v)
}

func TestOption_SomeInt(t *testing.T) {
	opt := Some(42)
	assert.True(t, opt.IsSet())
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.Equal(t, 42, v)
}

func TestOptionBool_Scan_FromNil(t *testing.T) {
	var opt OptionBool
	err := opt.Scan(nil)
	assert.NoError(t, err)
	assert.False(t, opt.IsSet())
}

func TestOptionBool_Scan_FromTrue(t *testing.T) {
	var opt OptionBool
	err := opt.Scan(true)
	assert.NoError(t, err)
	assert.True(t, opt.IsSet())
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.True(t, v)
}

func TestOptionBool_Scan_FromFalse(t *testing.T) {
	var opt OptionBool
	err := opt.Scan(false)
	assert.NoError(t, err)
	assert.True(t, opt.IsSet())
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.False(t, v)
}

func TestOptionFloat64_Scan_FromNil(t *testing.T) {
	var opt OptionFloat64
	err := opt.Scan(nil)
	assert.NoError(t, err)
	assert.False(t, opt.IsSet())
}

func TestOptionFloat64_Scan_FromValue(t *testing.T) {
	var opt OptionFloat64
	err := opt.Scan(3.14)
	assert.NoError(t, err)
	assert.True(t, opt.IsSet())
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.InDelta(t, 3.14, v, 0.001)
}

func TestOptionFloat64_Scan_ZeroIsSet(t *testing.T) {
	// Critical edge case: 0.0 must be distinguishable from unset.
	var opt OptionFloat64
	err := opt.Scan(0.0)
	assert.NoError(t, err)
	assert.True(t, opt.IsSet(), "0.0 after Scan must be IsSet()")
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.Equal(t, 0.0, v)
}

func TestOptionString_Scan_FromNil(t *testing.T) {
	var opt OptionString
	err := opt.Scan(nil)
	assert.NoError(t, err)
	assert.False(t, opt.IsSet())
}

func TestOptionString_Scan_FromValue(t *testing.T) {
	var opt OptionString
	err := opt.Scan("hello")
	assert.NoError(t, err)
	assert.True(t, opt.IsSet())
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.Equal(t, "hello", v)
}

func TestOptionString_Scan_FromEmptyString(t *testing.T) {
	var opt OptionString
	err := opt.Scan("")
	assert.NoError(t, err)
	assert.True(t, opt.IsSet(), "empty string after Scan must be IsSet()")
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.Empty(t, v)
}

func TestOptionInt64_Scan_FromNil(t *testing.T) {
	var opt OptionInt64
	err := opt.Scan(nil)
	assert.NoError(t, err)
	assert.False(t, opt.IsSet())
}

func TestOptionInt64_Scan_FromValue(t *testing.T) {
	var opt OptionInt64
	err := opt.Scan(int64(42))
	assert.NoError(t, err)
	assert.True(t, opt.IsSet())
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.Equal(t, int64(42), v)
}

func TestOptionInt64_Scan_FromZero(t *testing.T) {
	var opt OptionInt64
	err := opt.Scan(int64(0))
	assert.NoError(t, err)
	assert.True(t, opt.IsSet(), "zero int64 after Scan must be IsSet()")
	v, ok := opt.Value()
	assert.True(t, ok)
	assert.Equal(t, int64(0), v)
}

// ─────────────────────────────────────────────────────────────────────
// optionAny interface tests
// ─────────────────────────────────────────────────────────────────────

func TestOptionNone_ImplementsOptionAny(t *testing.T) {
	var v optionAny = optionNone{}
	assert.False(t, v.isSet())
	assert.Nil(t, v.get())
}

func TestOptionBool_ImplementsOptionAny(t *testing.T) {
	var v optionAny = OptionBool{optionCore: optionCore[bool]{val: true, present: true}}
	assert.True(t, v.isSet())
	assert.Equal(t, true, v.get())
}

func TestOptionFloat64_ImplementsOptionAny(t *testing.T) {
	var v optionAny = OptionFloat64{optionCore: optionCore[float64]{val: 3.14, present: true}}
	assert.True(t, v.isSet())
	assert.InDelta(t, 3.14, v.get().(float64), 0.001)
}

func TestOptionFloat64_UnsetImplementsOptionAny(t *testing.T) {
	var v optionAny = OptionFloat64{}
	assert.False(t, v.isSet())
	assert.Equal(t, 0.0, v.get())
}

// ─────────────────────────────────────────────────────────────────────
// Resolve — scope chain precedence tests
//
// These tests temporarily replace the package-level resolver function
// variables to verify chain ordering without database fixtures.
// Each test case sets up a different combination of scope values and
// asserts the expected winner.
// ─────────────────────────────────────────────────────────────────────

type resolveTestCase struct {
	name  string
	keyID string
	field string
	// scope resolver overrides (nil = leave as default stub)
	perKey func(string, string) optionAny
	team   func(string, string) optionAny
	org    func(string, string) optionAny
	global func(string) optionAny
	want   any
}

func runResolveTest(t *testing.T, tc resolveTestCase) {
	t.Helper()

	// Save originals and schedule cleanup.
	origPerKey := ResolvePerKeyFn
	origTeam := ResolveTeamFn
	origOrg := ResolveOrgFn
	origGlobal := ResolveGlobalFn
	t.Cleanup(func() {
		ResolvePerKeyFn = origPerKey
		ResolveTeamFn = origTeam
		ResolveOrgFn = origOrg
		ResolveGlobalFn = origGlobal
	})

	if tc.perKey != nil {
		ResolvePerKeyFn = tc.perKey
	}
	if tc.team != nil {
		ResolveTeamFn = tc.team
	}
	if tc.org != nil {
		ResolveOrgFn = tc.org
	}
	if tc.global != nil {
		ResolveGlobalFn = tc.global
	}

	got := Resolve(tc.keyID, tc.field)
	assert.Equal(t, tc.want, got)
}

func TestResolve_AllUnset_ReturnsNil(t *testing.T) {
	runResolveTest(t, resolveTestCase{
		name:  "all unset",
		keyID: "key-1",
		field: "rate_limit.default_rpm",
		want:  nil,
	})
}

func TestResolve_PerKeySet_Wins(t *testing.T) {
	runResolveTest(t, resolveTestCase{
		name:  "perKey set, rest unset",
		keyID: "key-1",
		field: "rate_limit.default_rpm",
		perKey: func(_, _ string) optionAny {
			return OptionInt64{optionCore: optionCore[int64]{val: 100, present: true}}
		},
		want: int64(100),
	})
}

func TestResolve_GlobalSet_Fallback(t *testing.T) {
	runResolveTest(t, resolveTestCase{
		name:   "only global set",
		keyID:  "key-1",
		field:  "rate_limit.default_rpm",
		global: func(_ string) optionAny { return OptionInt64{optionCore: optionCore[int64]{val: 50, present: true}} },
		want:   int64(50),
	})
}

func TestResolve_PerKeyOverridesGlobal(t *testing.T) {
	runResolveTest(t, resolveTestCase{
		name:  "perKey and global both set — perKey wins",
		keyID: "key-1",
		field: "rate_limit.default_rpm",
		perKey: func(_, _ string) optionAny {
			return OptionInt64{optionCore: optionCore[int64]{val: 200, present: true}}
		},
		global: func(_ string) optionAny { return OptionInt64{optionCore: optionCore[int64]{val: 50, present: true}} },
		want:   int64(200),
	})
}

func TestResolve_TeamStubFallsThrough(t *testing.T) {
	runResolveTest(t, resolveTestCase{
		name:   "only team returns value — should still be found",
		keyID:  "key-1",
		field:  "some.field",
		perKey: func(_, _ string) optionAny { return optionNone{} },
		team: func(_, _ string) optionAny {
			return OptionString{optionCore: optionCore[string]{val: "team-val", present: true}}
		},
		want: "team-val",
	})
}

func TestResolve_OrgStubFallsThrough(t *testing.T) {
	runResolveTest(t, resolveTestCase{
		name:   "only org returns value — should still be found",
		keyID:  "key-1",
		field:  "some.field",
		perKey: func(_, _ string) optionAny { return optionNone{} },
		team:   func(_, _ string) optionAny { return optionNone{} },
		org: func(_, _ string) optionAny {
			return OptionString{optionCore: optionCore[string]{val: "org-val", present: true}}
		},
		want: "org-val",
	})
}

func TestResolve_PerKeyFloat64ZeroIsSet(t *testing.T) {
	// Critical edge: 0.0 at perkey scope must be returned, not treated as unset.
	runResolveTest(t, resolveTestCase{
		name:  "perKey float64 0.0 is distinguishable from unset",
		keyID: "key-1",
		field: "budget.default_monthly_limit",
		perKey: func(_, _ string) optionAny {
			return OptionFloat64{optionCore: optionCore[float64]{val: 0.0, present: true}}
		},
		global: func(_ string) optionAny {
			return OptionFloat64{optionCore: optionCore[float64]{val: 100.0, present: true}}
		},
		want: 0.0,
	})
}

func TestResolve_GlobalFloat64ZeroIsSet(t *testing.T) {
	// Critical edge: 0.0 at global scope must be distinguishable from unset.
	runResolveTest(t, resolveTestCase{
		name:   "global float64 0.0 returned when perKey unset",
		keyID:  "key-1",
		field:  "budget.default_monthly_limit",
		perKey: func(_, _ string) optionAny { return optionNone{} },
		global: func(_ string) optionAny {
			return OptionFloat64{optionCore: optionCore[float64]{val: 0.0, present: true}}
		},
		want: 0.0,
	})
}

func TestResolve_PerKeyStringEmptyIsSet(t *testing.T) {
	// Empty string at perKey scope must be returned, not treated as unset.
	runResolveTest(t, resolveTestCase{
		name:  "perKey empty string distinguishable from unset",
		keyID: "key-1",
		field: "server.host",
		perKey: func(_, _ string) optionAny {
			return OptionString{optionCore: optionCore[string]{val: "", present: true}}
		},
		global: func(_ string) optionAny {
			return OptionString{optionCore: optionCore[string]{val: "default", present: true}}
		},
		want: "",
	})
}

func TestResolve_AllScopesSet_PerKeyWins(t *testing.T) {
	runResolveTest(t, resolveTestCase{
		name:  "all scopes set — perKey wins",
		keyID: "key-1",
		field: "some.field",
		perKey: func(_, _ string) optionAny {
			return OptionString{optionCore: optionCore[string]{val: "perkey", present: true}}
		},
		team: func(_, _ string) optionAny {
			return OptionString{optionCore: optionCore[string]{val: "team", present: true}}
		},
		org: func(_, _ string) optionAny {
			return OptionString{optionCore: optionCore[string]{val: "org", present: true}}
		},
		global: func(_ string) optionAny {
			return OptionString{optionCore: optionCore[string]{val: "global", present: true}}
		},
		want: "perkey",
	})
}

// ─────────────────────────────────────────────────────────────────────
// Table-driven runner — all (YAML × DB × scope) combos
//
// Simulates YAML = global scope, DB = perKey scope.
// ─────────────────────────────────────────────────────────────────────

type comboCase struct {
	name      string
	globalSet bool // global scope has a value
	dbSet     bool // perKey scope has a value
	want      any
}

func TestResolve_ScopeMatrix(t *testing.T) {
	tests := []comboCase{
		{name: "boot_unset_DB_unset", globalSet: false, dbSet: false, want: nil},
		{name: "boot_set_DB_unset", globalSet: true, dbSet: false, want: "global-val"},
		{name: "boot_unset_DB_set", globalSet: false, dbSet: true, want: "perkey-val"},
		{name: "boot_set_DB_set__perKey_wins", globalSet: true, dbSet: true, want: "perkey-val"},
	}

	origPerKey := ResolvePerKeyFn
	origGlobal := ResolveGlobalFn
	t.Cleanup(func() {
		ResolvePerKeyFn = origPerKey
		ResolveGlobalFn = origGlobal
	})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dbSet {
				ResolvePerKeyFn = func(_, _ string) optionAny {
					return OptionString{optionCore: optionCore[string]{val: "perkey-val", present: true}}
				}
			} else {
				ResolvePerKeyFn = defaultResolvePerKey
			}

			if tc.globalSet {
				ResolveGlobalFn = func(_ string) optionAny {
					return OptionString{optionCore: optionCore[string]{val: "global-val", present: true}}
				}
			} else {
				ResolveGlobalFn = defaultResolveNone
			}

			got := Resolve("key-1", "some.field")
			assert.Equal(t, tc.want, got, "scope matrix case: %s", tc.name)
		})
	}
}
