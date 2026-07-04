package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/arqueon/dankmail/core/internal/ipc"
	"github.com/arqueon/dankmail/core/internal/paths"
	"github.com/arqueon/dankmail/core/models"
)

var (
	flagJSON   bool
	flagHidden bool
	flagShell  string
)

var rootCmd = &cobra.Command{
	Use:   "dmail",
	Short: "Multi-account mail notifier and triage daemon",
	Long: "dankmail: a lightweight mail aggregator daemon with a Quickshell UI.\n" +
		"Triage, not management — anything complex opens the webmail.",
	SilenceUsage: true,
	// Default action: focus/open the window (like `dcal`).
	RunE: func(cmd *cobra.Command, args []string) error {
		return callSimple("ui.show", nil)
	},
}

// dial connects to the running daemon or fails with a friendly hint.
func dial() (*ipc.Client, error) {
	c, err := ipc.Dial(paths.SocketPath())
	if err != nil {
		return nil, fmt.Errorf("daemon not running (start it with 'dmail daemon' or systemctl --user start dmail): %w", err)
	}
	return c, nil
}

func callSimple(method string, params map[string]any) error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	res, err := c.Call(method, params)
	if err != nil {
		return err
	}
	if flagJSON {
		fmt.Println(string(res))
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "machine-readable output")

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon plus the Quickshell UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := flagShell
			if shell == "" {
				shell = defaultShellDir()
			}
			return runDaemon(shell, flagHidden)
		},
	}
	runCmd.Flags().BoolVar(&flagHidden, "hidden", false, "start with the window hidden")
	runCmd.Flags().StringVarP(&flagShell, "shell-config", "c", "", "quickshell config directory (default: installed dankmail shell)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List threads (scripting surface)",
		RunE:  runList,
	}
	listCmd.Flags().Bool("unread", false, "only unread threads")
	listCmd.Flags().String("account", "", "restrict to one account UUID")
	listCmd.Flags().Int("limit", 50, "max threads")

	rootCmd.AddCommand(
		runCmd,
		&cobra.Command{Use: "daemon", Short: "Run headless (IPC + HTTP API, no UI)",
			RunE: func(cmd *cobra.Command, args []string) error { return runDaemon("", false) }},
		&cobra.Command{Use: "show", Short: "Show the triage window",
			RunE: func(cmd *cobra.Command, args []string) error { return callSimple("ui.show", nil) }},
		&cobra.Command{Use: "toggle", Short: "Toggle window visibility",
			RunE: func(cmd *cobra.Command, args []string) error { return callSimple("ui.toggle", nil) }},
		&cobra.Command{Use: "restart", Short: "Restart the daemon",
			RunE: runRestart},
		&cobra.Command{Use: "kill", Short: "Stop the daemon",
			RunE: func(cmd *cobra.Command, args []string) error { return callSimple("system.exit", nil) }},
		&cobra.Command{Use: "sync [account-id]", Short: "Trigger a sync now", Args: cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				params := map[string]any{}
				if len(args) == 1 {
					params["account"] = args[0]
				}
				return callSimple("system.sync", params)
			}},
		&cobra.Command{Use: "status", Short: "Accounts, unread counts, queue, last errors",
			RunE: runStatus},
		&cobra.Command{Use: "dnd [on|off|status]", Short: "Do-not-disturb control",
			Args: cobra.MaximumNArgs(1), RunE: runDND},
		listCmd,
		&cobra.Command{Use: "open <thread-id>", Short: "Open a thread in the webmail (deep link)",
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				var id int
				if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
					return fmt.Errorf("thread-id must be the numeric local id from 'dmail list'")
				}
				return callSimple("ui.openLink", map[string]any{"id": id})
			}},
		accountCmd(),
		&cobra.Command{Use: "version", Short: "Print version information",
			Run: func(cmd *cobra.Command, args []string) {
				fmt.Printf("dmail %s (commit %s, built %s)\n", Version, Commit, BuildTime)
			}},
	)
}

func defaultShellDir() string {
	for _, dir := range []string{
		"/usr/local/share/quickshell/dankmail",
		"/usr/share/quickshell/dankmail",
	} {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	return ""
}

func runRestart(cmd *cobra.Command, args []string) error {
	if err := callSimple("system.exit", nil); err == nil {
		time.Sleep(500 * time.Millisecond)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	child := exec.Command(self, "daemon")
	if err := child.Start(); err != nil {
		return err
	}
	fmt.Printf("daemon restarted (pid %d)\n", child.Process.Pid)
	return child.Process.Release()
}

func runStatus(cmd *cobra.Command, args []string) error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	raw, err := c.Call("system.status", nil)
	if err != nil {
		return err
	}
	if flagJSON {
		fmt.Println(string(raw))
		return nil
	}
	var st models.DaemonStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return err
	}
	fmt.Printf("dankmail %s — %d unread", st.Version, st.Unread)
	if st.DND {
		fmt.Print(" [DND]")
	}
	fmt.Printf("  queue: %d pending / %d inflight / %d failed\n",
		st.Queue.Pending, st.Queue.Inflight, st.Queue.Failed)
	for _, a := range st.Accounts {
		fmt.Printf("  %-30s %-6s unread:%-4d status:%s", a.Email, a.Type, a.Unread, a.Status)
		if a.LastError != "" {
			fmt.Printf("  err: %s", a.LastError)
		}
		fmt.Println()
	}
	return nil
}

func runDND(cmd *cobra.Command, args []string) error {
	method := "dnd.status"
	if len(args) == 1 {
		switch args[0] {
		case "on":
			method = "dnd.on"
		case "off":
			method = "dnd.off"
		case "status":
		default:
			return fmt.Errorf("dnd takes on|off|status")
		}
	}
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	raw, err := c.Call(method, nil)
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()

	unread, _ := cmd.Flags().GetBool("unread")
	acct, _ := cmd.Flags().GetString("account")
	limit, _ := cmd.Flags().GetInt("limit")
	raw, err := c.Call("threads.list", map[string]any{
		"unread": unread, "account": acct, "limit": limit, "inbox": true,
	})
	if err != nil {
		return err
	}
	if flagJSON {
		fmt.Println(string(raw))
		return nil
	}
	var threads []models.ThreadSummary
	if err := json.Unmarshal(raw, &threads); err != nil {
		return err
	}
	for _, t := range threads {
		mark := " "
		if t.Unread {
			mark = "●"
		}
		star := " "
		if t.Starred {
			star = "★"
		}
		from := ""
		if len(t.Participants) > 0 {
			from = t.Participants[0]
		}
		fmt.Printf("%s%s %5d  %-16s  %-28.28s  %s\n",
			mark, star, t.ID, t.LastMessageAt.Local().Format("Jan 02 15:04"), from, t.Subject)
	}
	return nil
}
