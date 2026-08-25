// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: AGPL-3.0

package dump

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	dokuwiki "github.com/UNO-SOFT/dokuwiki-go/rest"
	"github.com/UNO-SOFT/zlog/v2"
	"github.com/google/renameio/v2"
	"golang.org/x/sync/errgroup"
)

type (
	visitor struct {
		cl   dokuwiki.ClientWithResponsesInterface
		base *url.URL
	}
	dumper struct {
		visitor
		destDir string
		force   bool
	}

	Element struct {
		Children  []string
		HTML      string
		ID, Title string
		Revision  int
		visitor   *visitor
	}
)

func New(wikiURL, destDir, token string, force bool) (*dumper, error) {
	wikiURL = strings.TrimSuffix(wikiURL, "/doku.php")
	cl, err := NewClient(wikiURL, token)
	if err != nil {
		return nil, err
	}
	v, err := NewVisitor(cl, wikiURL+"/doku.php")
	if err != nil {
		return nil, err
	}
	return NewDumper(v, destDir, force)
}

func NewClient(wikiURL, token string) (*dokuwiki.ClientWithResponses, error) {
	return dokuwiki.NewClientWithResponses(wikiURL+"/lib/exe/jsonrpc.php",
		dokuwiki.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Accept", "application/json")
			logger := zlog.SFromContext(ctx)
			if logger.Enabled(ctx, slog.LevelDebug) {
				b, err := httputil.DumpRequestOut(req, true)
				io.WriteString(os.Stderr, "\nvvvvvv\n")
				os.Stderr.Write(b)
				io.WriteString(os.Stderr, "\n^^^^^^\n")
				return err
			}
			return nil
		}),
	)
}

func NewWithClient(cl dokuwiki.ClientWithResponsesInterface, wikiURL, destDir string, force bool) (*dumper, error) {
	v, err := NewVisitor(cl, wikiURL)
	if err != nil {
		return nil, err
	}
	return NewDumper(v, destDir, force)
}

func NewVisitor(cl dokuwiki.ClientWithResponsesInterface, wikiURL string) (*visitor, error) {
	base, err := url.Parse(wikiURL)
	if err != nil {
		return nil, err
	}
	return &visitor{cl: cl, base: base}, nil
}

func NewDumper(v *visitor, destDir string, force bool) (*dumper, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}
	return &dumper{visitor: *v, destDir: destDir}, nil
}

func (v *visitor) Get(ctx context.Context, a string) (Element, error) {
	elt, err := v.GetInfo(ctx, a)
	if err != nil {
		return elt, err
	}
	r, err := elt.GetHTML(ctx)
	if err != nil {
		return elt, err
	}
	err = elt.ParseHTML(ctx, r)
	return elt, err
}

func (v *visitor) GetInfo(ctx context.Context, a string) (Element, error) {
	resp, err := v.cl.CoreGetPageInfoWithResponse(ctx, dokuwiki.CoreGetPageInfoJSONRequestBody{Page: a})
	if err == nil {
		err = dokuwiki.CheckResponse(resp)
	}
	if err != nil {
		return Element{}, err
	}
	result := resp.GetJSON200().Result
	return Element{visitor: v, ID: result.Id, Title: result.Title, Revision: result.Revision}, nil
}

func (elt *Element) GetHTML(ctx context.Context) (io.Reader, error) {
	logger := zlog.SFromContext(ctx).With("id", elt.ID)
	logger.Info("download", "title", elt.Title)
	resp, err := elt.visitor.cl.CoreGetPageHTMLWithResponse(ctx, dokuwiki.CoreGetPageHTMLJSONRequestBody{Page: elt.ID})
	if err == nil {
		err = dokuwiki.CheckResponse(resp)
	}
	if err != nil {
		return nil, err
	}
	return strings.NewReader(resp.GetJSON200().Result), nil
}

func (elt *Element) ParseHTML(ctx context.Context, r io.Reader) error {
	logger := zlog.SFromContext(ctx).With("id", elt.ID)
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	var errs []error
	doc.Find("a[data-wiki-id]").Each(func(_ int, sel *goquery.Selection) {
		a, ok := sel.Attr("data-wiki-id")
		if !ok {
			errs = append(errs, fmt.Errorf("no data-wiki-id: %v", sel))
		}
		elt.Children = append(elt.Children, a)
		sel.SetAttr("href", elt.HRef())
	})
	var buf, sty strings.Builder
	doc.Find("img.media").Each(func(_ int, sel *goquery.Selection) {
		if err := func() error {
			src, ok := sel.Attr("src")
			if !ok {
				logger.Warn("no src", "at", sel)
				return nil
			} else if strings.HasPrefix(src, "data:") {
				return nil
			}
			logger.Info("download", "src", src)
			ref, err := url.Parse(src)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", src, err)
			}
			if _, ok := sel.Attr("style"); !ok {
				q := ref.Query()
				sty.Reset()
				for _, k := range []string{"width", "height"} {
					if s := q.Get(k[:1]); s != "" {
						sty.WriteString(k)
						sty.WriteString(": ")
						sty.WriteString(s)
						sty.WriteString("; ")
					}
				}
				sel.SetAttr("style", sty.String())
			}
			want := elt.visitor.base.ResolveReference(ref).String()
			req, err := http.NewRequestWithContext(ctx, "GET", want, nil)
			if err != nil {
				return fmt.Errorf("NewRequest(%s): %w", want, err)
			}
			resp, err := elt.visitor.cl.(dokuwiki.HttpRequestDoer).Do(req)
			if err != nil {
				return fmt.Errorf("Do(%s): %w", want, err)
			}
			defer resp.Body.Close()
			ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
			buf.Reset()
			if err = dataURL(&buf, resp.Body, ct); err != nil {
				return err
			}
			sel.SetAttr("src", buf.String())
			return nil
		}(); err != nil {
			logger.Error("sel", "error", err)
			errs = append(errs, err)
		}
	})
	if elt.HTML, err = doc.Html(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Dump pages, recursively.
func (d *dumper) Dump(ctx context.Context, a string) ([]Element, error) {
	logger := zlog.SFromContext(ctx)
	var elts []Element
	err := d.Walk(ctx, func(ctx context.Context, elt Element, err error) error {
		if err != nil {
			logger.Warn("walk", "error", err)
			return nil
		}
		logger.Info("Walk", "id", elt.ID)
		fn := filepath.Join(d.destDir, id2fn(elt.ID))
		os.MkdirAll(filepath.Dir(fn), 0775)
		err = renameio.WriteFile(fn, []byte(elt.HTML), 0644)
		elt.HTML = ""
		elts = append(elts, elt)
		return err
	}, a)
	return elts, err
}

func (e *Element) HRef() string { return "./" + id2fn(e.ID) }

func id2fn(id string) string {
	parts := make([]string, 0, strings.Count(id, ":"))
	for p := range strings.SplitSeq(id+".html", ":") {
		parts = append(parts, template.URLQueryEscaper(p))
	}
	return path.Join(parts...)
}

// https://developer.mozilla.org/en-US/docs/Web/URI/Reference/Schemes/data
// data:[<media-type>][;base64],<data>
func dataURL(w io.Writer, r io.Reader, mediaType string) error {
	fmt.Fprintf(w, "data:%s;base64,", mediaType)
	enc := base64.NewEncoder(base64.StdEncoding, w)
	if _, err := io.Copy(enc, r); err != nil {
		return err
	}
	return enc.Close()
}

var ErrSkip = errors.New("skip")

func (d *dumper) Walk(
	ctx context.Context,
	walk func(context.Context, Element, error) error,
	root ...string,
) error {
	logger := zlog.SFromContext(ctx)
	var todoMu sync.Mutex
	todo := [][]string{root}
	seen := make(map[string]struct{})
	for {
		logger.Debug("todo", "todo", todo, "length", len(todo))
		todoMu.Lock()
		if len(todo) == 0 {
			todoMu.Unlock()
			break
		}
		args := todo[0]
		todo = todo[1:]
		todoMu.Unlock()

		grp, ctx := errgroup.WithContext(ctx)
		grp.SetLimit(runtime.GOMAXPROCS(-1))
		for _, a := range args {
			if _, ok := seen[a]; ok {
				continue
			}
			seen[a] = struct{}{}

			grp.Go(func() error {
				logger := logger.With("id", a)
				elt, err := d.visitor.GetInfo(ctx, a)
				if err != nil {
					if errors.Is(err, dokuwiki.ErrNotFound) {
						logger.Warn("not found", "error", err)
						return nil
					}
					return err
				}

				fn := filepath.Join(d.destDir, id2fn(elt.ID))
				var body io.Reader
				if !d.force && elt.Revision != 0 {
					if fh, err := os.Open(fn); err == nil {
						defer fh.Close()
						if fi, err := fh.Stat(); err == nil && fi.Size() != 0 && fi.ModTime().After(time.Unix(int64(elt.Revision), 0)) {
							logger.Info("skip already fresh", "file", fn)
							body = fh
						} else {
							fh.Close()
						}
					}
				}
				if body == nil {
					if body, err = elt.GetHTML(ctx); err != nil {
						return err
					}
				}
				if err = elt.ParseHTML(ctx, body); err != nil {
					return err
				}
				if err = walk(ctx, elt, err); err != nil {
					if errors.Is(err, ErrSkip) {
						return nil
					}
					return err
				}
				if len(elt.Children) != 0 {
					logger.Info("have", "children", elt.Children)
					todoMu.Lock()
					todo = append(todo, elt.Children)
					todoMu.Unlock()
				}
				return nil
			})
		}
		if err := grp.Wait(); err != nil {
			return err
		}
	}
	return nil
}
