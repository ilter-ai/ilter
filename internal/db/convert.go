package db

// Small type-conversion helpers shared across sqlite_*.go wrappers, bridging
// sqlc's nullable-pointer/int64 types and this package's plain Go types.
// Centralized here instead of duplicated per-file — check here before adding
// a new one.

func strPtr(s string) *string {
	return &s
}

func strDeref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func intToInt64Ptr(v *int) *int64 {
	if v == nil {
		return nil
	}
	r := int64(*v)
	return &r
}

func int64PtrToIntPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	i := int(*v)
	return &i
}

func int64Deref(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
