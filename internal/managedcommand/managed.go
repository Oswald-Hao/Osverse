// Package managedcommand safely activates per-user command shims owned by
// Osverse. It never replaces an entry that cannot be proven to belong to the
// requested component.
package managedcommand

import "errors"

var ErrExternalCommand = errors.New("command entry is owned by another program")

type Paths struct {
	ToolRoot    string
	CurrentPath string
	BinRoot     string
	ShimPath    string
	WrapperPath string
}
