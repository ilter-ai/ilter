package config

// ─────────────────────────────────────────────────────────────────────
// Resolve — scope-chain config resolution
//
// Resolve traverses the scope chain [perKey → team → org → global]
// and returns the first value whose Option.IsSet() is true.
// Team and org are stubbed (always return unset) pending table creation.
//
// Each scope step is backed by an injectable function variable,
// allowing tests to verify chain ordering without database fixtures.
// ─────────────────────────────────────────────────────────────────────

// Resolve traverses the config scope chain and returns the first set value.
// The scope order is:
//
//  1. perKey   — per-API-key override (from database, requires keyID)
//  2. team     — team-level override (stub)
//  3. org      — org-level override (stub)
//  4. global   — global default (from config file or DB)
//
// Returns nil when no scope provides a value.
func Resolve(keyID string, field string) any {
	teamID, orgID := ResolveOwnerFn(keyID)

	steps := []func() optionAny{
		func() optionAny { return ResolvePerKeyFn(keyID, field) },
		func() optionAny { return ResolveTeamFn(teamID, field) },
		func() optionAny { return ResolveOrgFn(orgID, field) },
		func() optionAny { return ResolveGlobalFn(field) },
	}
	for _, step := range steps {
		if v := step(); v.isSet() {
			return v.get()
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────
// Injectable resolver function variables
//
// Default implementations are stubs — all return "not set".
// Tests replace these to verify chain ordering and precedence.
// ─────────────────────────────────────────────────────────────────────

var (
	// ResolveOwnerFn resolves a keyID to its team and org ownership.
	// Default: stub (returns empty strings).
	ResolveOwnerFn = func(_ string) (teamID, orgID string) {
		return "", ""
	}

	// ResolvePerKeyFn resolves a field at the per-API-key scope.
	// The keyID parameter identifies the API key whose overrides to check.
	// Default: stub (returns unset).
	ResolvePerKeyFn = defaultResolvePerKey

	// ResolveTeamFn resolves a field at the team scope.
	// Default: stub (returns unset).
	ResolveTeamFn = defaultResolveWithID

	// ResolveOrgFn resolves a field at the org scope.
	// Default: stub (returns unset).
	ResolveOrgFn = defaultResolveWithID

	// ResolveGlobalFn resolves a field at the global scope (config file default).
	// Default: stub (returns unset).
	ResolveGlobalFn = defaultResolveNone
)

// defaultResolveNone returns an unset option for any field.
func defaultResolveNone(_ string) optionAny {
	return optionNone{}
}

// defaultResolveWithID returns an unset option for any ID/field.
func defaultResolveWithID(_, _ string) optionAny {
	return optionNone{}
}

// defaultResolvePerKey returns an unset option for any key/field.
func defaultResolvePerKey(_, _ string) optionAny {
	return optionNone{}
}

// ResolverFunc resolves a field at the given scope and scopeID.
type ResolverFunc func(scope, scopeID, field string) (any, bool)

// WireResolvers connects the database/runtime config to the resolve chain.
func WireResolvers(
	ownerFn func(keyID string) (teamID, orgID string),
	resolveFn ResolverFunc,
) {
	ResolveOwnerFn = ownerFn

	ResolvePerKeyFn = func(keyID, field string) optionAny {
		val, set := resolveFn("key", keyID, field)
		if !set {
			return optionNone{}
		}
		return wrapValue(val)
	}

	ResolveTeamFn = func(teamID, field string) optionAny {
		val, set := resolveFn("team", teamID, field)
		if !set {
			return optionNone{}
		}
		return wrapValue(val)
	}

	ResolveOrgFn = func(orgID, field string) optionAny {
		val, set := resolveFn("org", orgID, field)
		if !set {
			return optionNone{}
		}
		return wrapValue(val)
	}

	ResolveGlobalFn = func(field string) optionAny {
		val, set := resolveFn("global", "", field)
		if !set {
			return optionNone{}
		}
		return wrapValue(val)
	}
}

func wrapValue(val any) optionAny {
	switch v := val.(type) {
	case bool:
		var opt OptionBool
		_ = opt.Scan(v)
		return opt
	case int64:
		var opt OptionInt64
		_ = opt.Scan(v)
		return opt
	case int:
		var opt OptionInt64
		_ = opt.Scan(int64(v))
		return opt
	case float64:
		var opt OptionFloat64
		_ = opt.Scan(v)
		return opt
	case string:
		var opt OptionString
		_ = opt.Scan(v)
		return opt
	default:
		var opt OptionString
		_ = opt.Scan(v)
		return opt
	}
}
