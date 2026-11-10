package cli

import "slices"

func init() {
	commands = slices.Concat(
		basicCommands(),
		deviceCommands(),
		alertCommands(),
		eventCommands(),
		checkCommands(),
		dockerCommands(),
		fileCommands(),
		adminCommands(),
		completionCommands(),
		mcpCommands(),
	)
}
