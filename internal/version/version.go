// Package version holds ILTER's single source-of-truth application version.
//
// The canonical version string lives in the repo-root VERSION file. Real
// builds (make build, make dev, Dockerfile) inject it into Version via
// -ldflags "-X github.com/ilter-ai/ilter/internal/version.Version=$(cat VERSION)".
// A plain `go build`/`go run`/`go test` without that flag keeps the "dev"
// fallback below.
package version

var Version = "dev"
