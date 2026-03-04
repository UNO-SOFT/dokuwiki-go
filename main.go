// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"

	dokuwiki "github.com/UNO-SOFT/dokuwiki-go/rest"
	"github.com/UNO-SOFT/zlog/v2"
	"github.com/go-json-experiment/json"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

var (
	verbose = zlog.VerboseVar(1)
	logger  = zlog.NewLogger(zlog.MaybeConsoleHandler(&verbose, os.Stderr)).SLog()
)

func main() {
	if err := Main(); err != nil {
		logger.Error("Main", "error", err)
		os.Exit(1)
	}
}

func Main() error {
	var cl dokuwiki.ClientWithResponsesInterface

	FS := ff.NewFlagSet("history")
	flagFirst := FS.IntLong("first", 0, "skip first n items")
	pageHistoryCmd := ff.Command{Name: "history", Flags: FS,
		Exec: func(ctx context.Context, args []string) error {
			req := dokuwiki.CoreGetPageHistoryJSONRequestBody{First: flagFirst}
			for _, a := range args {
				req.Page = a
				history, err := cl.CoreGetPageHistoryWithResponse(ctx, req)
				if err != nil {
					return err
				}
				json.MarshalWrite(os.Stderr, history.JSON200)
			}
			return nil
		},
	}

	FS = ff.NewFlagSet("dokuwiki")
	flagApiEnvKeyName := FS.StringLong("api-key-env-name", "DOKUWIKI_API_KEY", "environment variable name")
	FS.Value('v', "verbose", &verbose, "verbose logging")
	flagServer := FS.StringLong("server", "https://wiki.unosoft.hu/lib/exe/jsonrpc.php", "DokuWiki URL")
	app := ff.Command{Name: "dokuwiki", Flags: FS,
		Subcommands: []*ff.Command{&pageHistoryCmd},
		Exec: func(ctx context.Context, args []string) error {
			return nil
		},
	}
	if err := app.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, ff.ErrHelp) {
			ffhelp.Command(&app).WriteTo(os.Stderr)
			return nil
		}
		return err
	}
	token := os.Getenv(*flagApiEnvKeyName)
	var err error
	if cl, err = dokuwiki.NewClientWithResponses(*flagServer,
		dokuwiki.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/json")
			if logger.Enabled(ctx, slog.LevelDebug) {
				b, err := httputil.DumpRequestOut(req, true)
				io.WriteString(os.Stderr, "\nvvvvvv\n")
				os.Stderr.Write(b)
				io.WriteString(os.Stderr, "\n^^^^^^\n")
				return err
			}
			return nil
		})); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return app.Run(ctx)
}
