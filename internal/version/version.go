// Package version is the single source of truth for Lato's version
// string. The default is a plain "dev"; release builds override it at
// link time without touching source:
//
//	go build -ldflags "-X lato/internal/version.Version=1.2.3" .
//
// Both the /version slash command and `lato --version` read this one
// variable, so they can never disagree.
package version

// Version is overridden at build time through -ldflags -X. It lives in
// a var (not a const) because that is the only form the linker can
// rewrite.
var Version = "dev"
