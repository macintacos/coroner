package help

import (
	"io"

	"github.com/spf13/cobra"
)

func WriteReference(w io.Writer, root *cobra.Command, topics []Topic) error {
	return writeReference(w, root, topics)
}
