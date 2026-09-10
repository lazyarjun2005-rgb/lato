package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"lato/internal/session"
	"lato/internal/tui"
	"lato/internal/version"
)

var resumeID string

var rootCmd = &cobra.Command{
	Use:   "lato",
	Short: "A local-first agent harness.",
	Long: `Lato is a local-first agent harness and runtime.

It lets you chat with local models, execute tools, and build
AI-powered workflows without requiring cloud services.`,

	// Version wiring: release builds inject the value through
	// -ldflags "-X lato/internal/version.Version=1.0.9", and both
	// `lato --version` and the /version command read the same variable.
	Version: version.Version,

	// Running `lato` with no subcommand drops straight into the interactive
	// chat session, the same one `lato chat` starts explicitly.
	RunE: func(cmd *cobra.Command, args []string) error {
		var sess *session.Session

		if resumeID != "" {
			loaded, err := session.Load(resumeID)
			if err != nil {
				return err
			}
			sess = loaded
		} else {
			sess = session.New()
		}

		return tui.Start(sess)
	},
}

func init() {
	// "lato v1.0.9" instead of Cobra's default "lato version v1.0.9"
	// wording for `lato --version` and `lato -v`.
	rootCmd.SetVersionTemplate("lato {{.Version}}\n")

	rootCmd.Flags().StringVar(
		&resumeID,
		"resume",
		"",
		"resume an existing session",
	)
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
