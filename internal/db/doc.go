// Package db provides the SQLite database layer — schema migrations, connection
// management, and CRUD operations for the application's entities (api_keys,
// prompts, providers, groups, users, runtime_config). This is the
// schema-authoritative persistence layer; there is no separate store/ package.
//
// db/migrations/ holds the schema (embedded via go:embed and applied by goose).
// db/queries/ holds the SQL sqlc compiles into db/sqlc/ (typed Queries, run via
// `go generate` — see migrations.go). SQLiteStore wraps *sqlc.Queries and exposes
// the domain-level methods the rest of the app calls (sqlite_*.go, one file per
// entity); a few mutations with a dynamic SET clause (e.g. SetKeyBudget) are
// hand-written because sqlc requires static SQL. db/seed/ applies demo/init data
// and db/audit/ logs config mutations; both operate on the same schema via
// SQLiteStore/sqlc rather than a separate layer.
//
// When adding a new entity: put its migration in db/migrations/, its queries in
// db/queries/ (regenerate sqlc), and its domain methods in a new sqlite_*.go file.
package db
