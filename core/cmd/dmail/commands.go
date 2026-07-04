package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// errNotImplemented marks Anillo-1 stubs. Every command below is part of
// the committed CLI surface (§8 of the spec); bodies land with the MVP.
var errNotImplemented = errors.New("not implemented yet (anillo 1)")

var (
	flagJSON   bool
	flagHidden bool
	flagDetach bool
)

var rootCmd = &cobra.Command{
	Use:   "dmail",
	Short: "Multi-account mail notifier and triage daemon",
	Long: "dankmail: a lightweight mail aggregator daemon with a Quickshell UI.\n" +
		"Triage, not management — anything complex opens the webmail.",
	SilenceUsage: true,
	// Default action: focus/open the window (like `dcal`).
	RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented },
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "machine-readable output")

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon plus the Quickshell UI",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	runCmd.Flags().BoolVar(&flagHidden, "hidden", false, "start with the window hidden")
	runCmd.Flags().BoolVarP(&flagDetach, "detach", "d", false, "detach from the terminal")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List threads (scripting surface)",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	listCmd.Flags().Bool("unread", false, "only unread threads")
	listCmd.Flags().String("account", "", "restrict to one account")

	rootCmd.AddCommand(
		runCmd,
		&cobra.Command{Use: "daemon", Short: "Run headless (IPC + HTTP API, no UI)",
			RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented }},
		&cobra.Command{Use: "show", Short: "Show the triage window",
			RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented }},
		&cobra.Command{Use: "toggle", Short: "Toggle window visibility",
			RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented }},
		&cobra.Command{Use: "restart", Short: "Restart the daemon",
			RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented }},
		&cobra.Command{Use: "kill", Short: "Stop the daemon",
			RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented }},
		&cobra.Command{Use: "sync [account-id]", Short: "Trigger a sync now",
			RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented }},
		&cobra.Command{Use: "status", Short: "Accounts, unread counts, queue, last errors",
			RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented }},
		&cobra.Command{Use: "dnd [on|off|status]", Short: "Do-not-disturb control",
			Args: cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented }},
		listCmd,
		&cobra.Command{Use: "open <thread-id>", Short: "Open a thread in the webmail (deep link)",
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error { return errNotImplemented }},
		&cobra.Command{Use: "version", Short: "Print version information",
			Run: func(cmd *cobra.Command, args []string) {
				fmt.Printf("dmail %s (commit %s, built %s)\n", Version, Commit, BuildTime)
			}},
	)
}
