package cli

import "time"

// The tests' installs end at once, so they needn't wait seconds to see it.
func init() { editorPoll = 10 * time.Millisecond }
