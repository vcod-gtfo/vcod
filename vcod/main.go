// vcod — start, stop and test Call of Duty (2003) servers and clients in this workspace.
//
// One static binary. It drives the cod_rea Linux port and the four flavor installs:
// dedicated servers in podman (cod_rea/server/run.sh), the native cod_client against
// them, cod_rea's builds and its ctest and live test scripts. Run with no arguments in
// a terminal it opens an interactive shell with tabs; every command also runs on its
// own, which is how scripts and agents use it.
package main

import (
	"os"

	"vcod/internal/cli"

	// Commands register themselves from init, so the dispatcher, the help listing and
	// the generated shell completion all derive from one table.
	_ "vcod/cmd/build"
	_ "vcod/cmd/client"
	_ "vcod/cmd/completion"
	_ "vcod/cmd/config"
	_ "vcod/cmd/display"
	_ "vcod/cmd/doctor"
	_ "vcod/cmd/flavors"
	_ "vcod/cmd/get"
	_ "vcod/cmd/install"
	_ "vcod/cmd/lan"
	_ "vcod/cmd/logs"
	_ "vcod/cmd/play"
	_ "vcod/cmd/rcon"
	_ "vcod/cmd/screens"
	_ "vcod/cmd/server"
	_ "vcod/cmd/setup"
	_ "vcod/cmd/shell"
	_ "vcod/cmd/status"
	_ "vcod/cmd/stop"
	_ "vcod/cmd/test"
	_ "vcod/cmd/version"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
