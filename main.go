// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: AGPL-3.0

package main

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	dump "github.com/UNO-SOFT/dokuwiki-go/dump"
	dokuwiki "github.com/UNO-SOFT/dokuwiki-go/rest"
	"github.com/UNO-SOFT/zlog/v2"
	"github.com/go-json-experiment/json"
	"github.com/google/renameio/v2"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

//go:embed htmldoc-data.zip
var htmldocDataZip []byte

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
					err = dokuwiki.CheckResponse(history)
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
					err = dokuwiki.CheckResponse(html)
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
					err = dokuwiki.CheckResponse(links)
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
					err = dokuwiki.CheckResponse(info)
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
	flagDumpSeparateImages := flags.Bool(0, "separate-images", "do not embed images")
	flagDumpMaxWidth := flags.Int(0, "max-width", 0, "page max width in pixels")
	flagDumpHtmldoc := flags.Bool(0, "htmldoc", "use htmldoc to generate a PDF")
	flagDumpEncoding := flags.String(0, "encoding", "utf-8", "charset to use")
	flagDumpKeepTemp := flags.Bool('x', "keep-tmp", "keep temporary directories")
	flagDumpFlat := flags.Bool(0, "flat", "flat, no directory hierarchy")
	dumpCmd := ff.Command{Name: "dump", Flags: flags,
		Exec: func(ctx context.Context, args []string) error {
			destDir := *flagDumpDest
			var htmldoc, dataDir string
			deferRemove := func(dir string) {
				if *flagDumpKeepTemp {
					logger.Warn("keep", "dir", dir)
				} else {
					os.RemoveAll(dir)
				}
			}

			if !*flagDumpHtmldoc && strings.HasSuffix(*flagDumpDest, ".pdf") {
				*flagDumpHtmldoc = true
			}

			if *flagDumpHtmldoc {
				*flagDumpSeparateImages = true
				if *flagDumpMaxWidth == 0 {
					*flagDumpMaxWidth = 1024
				}

				var err error
				if destDir, err = os.MkdirTemp("", "dokuwiki-go-*"); err != nil {
					return err
				}
				defer deferRemove(destDir)
				if htmldoc, _ = exec.LookPath("htmldoc"); htmldoc != "" {
					const defaultDataDir = "/usr/local/share/htmldoc"
					if _, err := os.Stat(filepath.Join(defaultDataDir, "fonts")); err == nil {
						dataDir = defaultDataDir
					}
				}
				if htmldoc == "" || dataDir == "" {
					if dataDir, err = os.MkdirTemp("", "htmldoc-datadir-*"); err != nil {
						return err
					}
					defer deferRemove(dataDir)
					zr, err := zip.NewReader(bytes.NewReader(htmldocDataZip), int64(len(htmldocDataZip)))
					if err != nil {
						return err
					}
					if err = os.CopyFS(dataDir, zr); err != nil {
						return err
					}
					if htmldoc == "" {
						htmldoc = filepath.Join(dataDir, "htmldoc")
					}
				}
			}

			d, err := dump.NewWithClient(cl, wikiURL, destDir, *flagDumpForce, !*flagDumpSeparateImages)
			if err != nil {
				return err
			}
			d.MaxWidth(*flagDumpMaxWidth)
			d.Encoding(*flagDumpEncoding)
			d.Flat(*flagDumpFlat)

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
			if err = tmpl.Execute(&buf, []dump.Element{
				// Csak minta amivel mennie kell
				{ID: "unosoft:alfa:kezikonyv:bruno3", Title: "BRUNO3 Kézikönyv"},
			}); err != nil {
				return err
			}
			// fmt.Println(buf.String())
			if s := buf.String(); strings.Contains(s, "ZgotmplZ") {
				return fmt.Errorf("bad template:\n%s", s)
			}

			var elts []dump.Element
			for _, a := range args {
				ee, err := d.Dump(ctx, a)
				for _, e := range ee {
					if i, ok := slices.BinarySearchFunc(elts, e, func(a, b dump.Element) int { return strings.Compare(a.ID, b.ID) }); !ok {
						elts = slices.Insert(elts, i, e)
					}
				}
				if err != nil && !errors.Is(err, dokuwiki.ErrNotFound) {
					return err
				}
			}
			fh, err := renameio.NewPendingFile(filepath.Join(
				destDir, "index.html",
			), renameio.WithPermissions(0644))
			if err != nil {
				return err
			}
			defer fh.Cleanup()
			logger.Debug("exec", "template", tmpl, "elts", elts, "length", len(elts))
			if err = tmpl.Execute(fh, elts); err != nil {
				return err
			}
			err = fh.CloseAtomicallyReplace()
			if err != nil || !*flagDumpHtmldoc {
				return err
			}

			args = append(make([]string, 0, 10+len(elts)),
				"--charset", *flagDumpEncoding,
				"--browserwidth", strconv.Itoa(*flagDumpMaxWidth),
				"--datadir", dataDir,
				"-t", "pdf14",
				"-f", *flagDumpDest,
			)
			for _, e := range elts {
				args = append(args, filepath.Join(destDir, e.FileName()))
			}
			if *flagDumpDest == "" {
				*flagDumpDest = "-"
			}
			cmd := exec.CommandContext(ctx, htmldoc, args...)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			return cmd.Run()
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

	var err error
	if cl, err = dump.NewClient(*flagRPC, os.Getenv(*flagApiEnvKeyName)); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return app.Run(zlog.NewSContext(ctx, logger))
}
