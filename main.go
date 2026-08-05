// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: AGPL-3.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"

	dokuwiki "github.com/UNO-SOFT/dokuwiki-go/rest"
	"github.com/UNO-SOFT/zlog/v2"
	"github.com/go-json-experiment/json"
	"github.com/google/renameio/v2"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"golang.org/x/sync/errgroup"
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

	flags := ff.NewFlagSet("history")
	flagFirst := flags.IntLong("first", 0, "skip first n items")
	pageHistoryCmd := ff.Command{Name: "history", Flags: flags,
		Exec: func(ctx context.Context, args []string) error {
			req := dokuwiki.CoreGetPageHistoryJSONRequestBody{First: *flagFirst}
			for _, a := range args {
				req.Page = a
				history, err := cl.CoreGetPageHistoryWithResponse(ctx, req)
				if err == nil {
					err = checkResponse(history)
				}
				if err != nil {
					return err
				}
				logger.Info("got", "status", history.Status())
				json.MarshalWrite(os.Stderr, history.JSON200.Result)
				os.Stdout.Write([]byte{'\n'})
			}
			return nil
		},
	}

	pageHTMLCmd := ff.Command{Name: "html",
		Exec: func(ctx context.Context, args []string) error {
			var req dokuwiki.CoreGetPageHTMLJSONRequestBody
			for _, a := range args {
				req.Page = a
				html, err := cl.CoreGetPageHTMLWithResponse(ctx, req)
				if err == nil {
					err = checkResponse(html)
				}
				if err != nil {
					return err
				}
				logger.Info("got", "status", html.Status())
				os.Stdout.Write([]byte(html.GetJSON200().Result + "\n"))
			}
			return nil
		},
	}

	pageLinksCmd := ff.Command{Name: "links",
		Exec: func(ctx context.Context, args []string) error {
			var req dokuwiki.CoreGetPageLinksJSONRequestBody
			for _, a := range args {
				req.Page = a
				links, err := cl.CoreGetPageLinksWithResponse(ctx, req)
				if err == nil {
					err = checkResponse(links)
				}
				if err != nil {
					return err
				}
				logger.Info("got", "status", links.Status())
				json.MarshalWrite(os.Stdout, links.GetJSON200().Result)
				os.Stdout.Write([]byte{'\n'})
			}
			return nil
		},
	}

	pageInfoCmd := ff.Command{Name: "info",
		Exec: func(ctx context.Context, args []string) error {
			var req dokuwiki.CoreGetPageInfoJSONRequestBody
			for _, a := range args {
				req.Page = a
				info, err := cl.CoreGetPageInfoWithResponse(ctx, req)
				if err == nil {
					err = checkResponse(info)
				}
				if err != nil {
					return err
				}
				logger.Info("got", "status", info.Status())
				json.MarshalWrite(os.Stdout, info.GetJSON200().Result)
				os.Stdout.Write([]byte{'\n'})
			}
			return nil
		},
	}

	var wikiURL string

	flags = ff.NewFlagSet("dump")
	flagDumpDest := flags.String('o', "output", "", "destination directory")
	flagDumpForce := flags.Bool('f', "force", "force download")
	dumpCmd := ff.Command{Name: "dump", Flags: flags,
		Exec: func(ctx context.Context, args []string) error {
			d, err := newDumper(cl, wikiURL, *flagDumpDest, *flagDumpForce)
			if err != nil {
				return err
			}
			tmpl, err := template.New("index").Parse(`<!DOCTYPE html>
	<body>
		<ul>
			{{range .}}
			<li><a href="{{.HRef}}">{{.Title}}</a></li>
			{{end}}
		</ul>
	</body>
</html>`)
			if err != nil {
				return err
			}

			var buf strings.Builder
			if err = tmpl.Execute(&buf, []element{
				{ID: "unosoft:alfa:kezikonyv:bruno3", Title: "BRUNO3 Kézikönyv"},
			}); err != nil {
				return err
			}
			// fmt.Println(buf.String())
			if s := buf.String(); strings.Contains(s, "ZgotmplZ") {
				return fmt.Errorf("bad template:\n%s", s)
			}

			var eltsMu sync.Mutex
			var elts []element
			var todoMu sync.Mutex
			todo := [][]string{args}
			for {
				todoMu.Lock()
				logger.Debug("todo", "todo", todo, "length", len(todo))
				if len(todo) == 0 {
					todoMu.Unlock()
					break
				}
				args, todo = todo[0], todo[1:]
				todoMu.Unlock()

				grp, ctx := errgroup.WithContext(ctx)
				grp.SetLimit(runtime.GOMAXPROCS(-1))
				for _, a := range args {
					grp.Go(func() error {
						e, more, err := d.dump(ctx, a)
						if err != nil {
							if errors.Is(err, ErrNotFound) {
								logger.Error("not found", "page", a, "error", err)
								return nil
							}
							return err
						}
						eltsMu.Lock()
						if i, ok := slices.BinarySearchFunc(elts, e, func(a, b element) int { return strings.Compare(a.ID, b.ID) }); !ok {
							elts = slices.Insert(elts, i, e)
						}
						eltsMu.Unlock()
						if len(more) != 0 {
							todoMu.Lock()
							todo = append(todo, more)
							todoMu.Unlock()
						}
						// logger.Info("found", "a", a, "more", more)
						return nil
					})
				}
				if err := grp.Wait(); err != nil {
					logger.Error("ERROR", "error", err)
					return err
				}
			}
			fh, err := renameio.NewPendingFile(filepath.Join(d.destDir, "index.html"), renameio.WithPermissions(0644))
			if err != nil {
				return err
			}
			defer fh.Cleanup()
			logger.Debug("exec", "template", tmpl, "elts", elts, "length", len(elts))
			if err = tmpl.Execute(fh, elts); err != nil {
				return err
			}
			return fh.CloseAtomicallyReplace()
		},
	}

	JP := func(dest *string, baseURL, path string) *string {
		var err error
		if *dest, err = url.JoinPath(baseURL, path); err != nil {
			panic(err)
		}
		return dest
	}

	flags = ff.NewFlagSet("dokuwiki")
	flagApiEnvKeyName := flags.StringLong("api-key-env-name", "DOKUWIKI_API_KEY", "environment variable name")
	flags.Value('v', "verbose", &verbose, "verbose logging")
	flagBase := flags.StringLong("base", "https://wiki.unosoft.hu", "DokuWiki base URL")
	flagRPC := JP(flags.StringLong("rpc", "", "DokuWiki RPC URL"), *flagBase, "/lib/exe/jsonrpc.php")
	flags.StringVar(&wikiURL, 0, "wiki", "", "DokuWiki URL")
	JP(&wikiURL, *flagBase, "/doku.php")
	app := ff.Command{Name: "dokuwiki", Flags: flags,
		Usage:       "May need the @remoteapi group permission!",
		Subcommands: []*ff.Command{&pageHistoryCmd, &pageHTMLCmd, &pageLinksCmd, &pageInfoCmd, &dumpCmd},
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
	if cl, err = dokuwiki.NewClientWithResponses(*flagRPC,
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
		}),
	); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return app.Run(ctx)
}

func checkResponse(resp interface{ GetBody() []byte }) error {
	rv := reflect.ValueOf(resp)
	rm, ok := rv.Type().MethodByName("GetJSON200")
	if !ok {
		return fmt.Errorf("no GetJSON200 on %#v", resp)
	}
	// if resp.GetJSON200() != nil {
	if !rm.Func.Call([]reflect.Value{rv})[0].IsNil() {
		return nil
	}
	if b := resp.GetBody(); bytes.Contains(b, []byte("does not exist")) {
		return fmt.Errorf("%w: %s", ErrNotFound, string(b))
	} else {
		return fmt.Errorf("get %s", string(b))
	}
	return nil
}
