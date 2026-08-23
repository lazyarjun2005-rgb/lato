// Runtime-level access to Lato's editing engine (see internal/edit).
//
// The editing tools must be registered after the Runtime exists because
// they need the discovered workspace root, and they report completed
// writes back to the runtime so the cached repository index can be
// invalidated — otherwise search_repo would keep serving stale content
// for files an edit just changed.
package runtime

import (
	"lato/internal/edit"
	"lato/internal/tools/editing"
)

// noteEdit invalidates the cached index when a file under the workspace
// was created or its content changed, so the next Index() call rebuilds
// from disk instead of serving stale bodies.
func (r *Runtime) noteEdit() {
	r.index = nil
}

// RegisterEditTools wires the editing engine into r's tool manager,
// scoped to the discovered workspace root. Every successful write
// invalidates the runtime's cached repository index lazily: nothing is
// rebuilt until something next reads it.
func (r *Runtime) RegisterEditTools() error {
	ws := edit.NewWorkspace(r.workspace.Root)
	ws.OnChange = r.noteEdit
	return editing.Register(r.manager, ws)
}
