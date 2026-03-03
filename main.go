// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"

	dokuwiki "github.com/UNO-SOFT/dokuwiki-go/rest"
	"github.com/UNO-SOFT/zlog/v2"
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
	var cl dokuwiki.ClientInterface

	FS := ff.NewFlagSet("history")
	flagFirst := FS.IntLong("first", 0, "skip first n items")
	pageHistoryCmd := ff.Command{Name: "history", Flags: FS,
		Exec: func(ctx context.Context, args []string) error {
			req := dokuwiki.CoreGetPageHistoryJSONRequestBody{First: flagFirst}
			for _, a := range args {
				req.Page = a
				resp, err := cl.CoreGetPageHistory(ctx, req)
				if err != nil {
					return err
				}
				if _, err = io.Copy(os.Stdout, resp.Body); err != nil {
					return err
				}
			}
			return nil
		},
	}

	FS = ff.NewFlagSet("dokuwiki")
	FS.Value('v', "verbose", &verbose, "verbose logging")
	flagServer := FS.StringLong("server", "https://wiki.unosoft.hu/dokuwiki/lib/exe/openapi.php", "DokuWiki URL")
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
	var err error
	if cl, err = dokuwiki.NewClient(*flagServer); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return app.Run(ctx)
}
