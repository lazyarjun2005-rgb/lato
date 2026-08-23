package builtin

import (
	"fmt"
	"runtime"

	"lato/internal/command"
	"lato/internal/version"
)

// Version is the /version command: report which Lato build is running.
// The value comes from internal/version, the same source `lato
// --version` uses, so the two can never disagree. Release builds set it
// at link time; a plain `go build` reports "dev".
type Version struct{}

// NewVersion returns a ready-to-register /version command.
func NewVersion() *Version { return &Version{} }

func (Version) Name() string        { return "version" }
func (Version) Aliases() []string   { return nil }
func (Version) Description() string { return "Show the running Lato version." }
func (Version) Usage() string       { return "/version" }

func (Version) Execute(ctx command.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: %s", Version{}.Usage())
	}
	ctx.Println("Lato %s (%s, %s/%s)", version.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}
