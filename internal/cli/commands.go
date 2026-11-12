package cli

import "slices"

func init() {
	commands = slices.Concat(
		basicCommands(),
		deviceCommands(),
		relayCommands(),
		alertCommands(),
		eventCommands(),
		checkCommands(),
		dockerCommands(),
		fileCommands(),
		editorCommands(),
		aiCommands(),
		adminCommands(),
		tokenCommands(),
		completionCommands(),
		mcpCommands(),
	)
}
