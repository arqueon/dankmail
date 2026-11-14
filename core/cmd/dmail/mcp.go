package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/arqueon/dux-mail/core/internal/mcp"
)

func mcpCmd() *cobra.Command {
	var (
		modeFlag   string
		socketFlag string
		noAuto     bool
	)

	defaultMode := os.Getenv("DMAIL_MCP_MODE")
	if defaultMode == "" {
		defaultMode = string(mcp.ModeFull)
	}

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the Model Context Protocol (MCP) server over stdio for AI agents",
		Long: "Expose Dux Mail over MCP (stdio JSON-RPC 2.0) so AI agents can read, search,\n" +
			"inspect attachments, manage drafts, triage threads, and send mail without opening a browser.\n\n" +
			"Permission modes (--mode):\n" +
			"  full          All tools enabled, including send_email, reply_thread, forward_thread, and trash_threads (default)\n" +
			"  draft         Read, search, silent attachments, drafts, and reversible triage (blocks outbound sending and trash)\n" +
			"  draft-strict  Read, search, silent attachments, and drafts only (no triage or sending)\n" +
			"  readonly      Read, search, sync, and silent attachment caching only",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Ensure slog never writes to stdout during an MCP stdio session.
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

			mode, err := mcp.ParseMode(modeFlag)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			dialer := &mcp.DaemonDialer{
				SocketPath: socketFlag,
				AutoStart:  !noAuto && socketFlag == "",
			}
			srv := mcp.NewServer(mcp.Options{
				Invoker: dialer,
				Mode:    mode,
				Version: Version,
			})
			return srv.Serve(ctx, os.Stdin, os.Stdout)
		},
	}

	cmd.Flags().StringVar(&modeFlag, "mode", defaultMode, "permission mode: full|draft|draft-strict|readonly")
	cmd.Flags().StringVar(&socketFlag, "socket", "", "override daemon unix socket path")
	cmd.Flags().BoolVar(&noAuto, "no-autostart", false, "do not auto-start the dmail daemon when offline")
	return cmd
}
