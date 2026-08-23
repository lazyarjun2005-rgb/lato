package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"lato/internal/doctor"
	"lato/internal/workspace"
)

// doctorCmd helps verify a global `lato` installation and the local
// environment Lato depends on. The report itself lives in
// internal/doctor so the /doctor slash command renders exactly the same
// text inside the chat. It only reads: no setting is changed, no shell
// configuration is touched, and no secret is ever printed.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the installation, configuration, and environment",
	Long: `Doctor reports where the running lato binary lives, whether that
location is on your PATH (so plain "lato" works from any terminal),
where configuration, provider connections, memory, and tasks are
stored, which workspace you are in, and which clipboard helper would
be used. It changes nothing.`,
	Args: cobra.NoArgs,

	RunE: func(cmd *cobra.Command, args []string) error {
		doctor.Report(os.Stdout, workspace.Discover())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
