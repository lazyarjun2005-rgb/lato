package clipboard

import (
	"encoding/base64"
	"fmt"
	"io"
)

// writeOSC52To emits the terminal clipboard protocol using only the standard
// library. A single write prevents a partial escape sequence from leaking.
func writeOSC52To(w io.Writer, text string) error {
	payload := base64.StdEncoding.EncodeToString([]byte(text))
	_, err := fmt.Fprintf(w, "\x1b]52;c;%s\x07", payload)
	return err
}
