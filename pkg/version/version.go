package version

import (
	"fmt"
	"runtime"
)

// Version is populated at build time via -ldflags. Defaults to "0.1.0-dev".
var Version = "0.1.0-dev"

// GetVersionInfo returns a formatted string containing version and target platform info.
func GetVersionInfo() string {
	return fmt.Sprintf("v%s (%s/%s, %s)", Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
