// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: AGPL-3.0

package dump

import (
	"context"
	"crypto/sha256"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	dokuwiki "github.com/UNO-SOFT/dokuwiki-go/rest"
	"github.com/UNO-SOFT/zlog/v2"
	"github.com/google/renameio/v2"

	"golang.org/x/sync/errgroup"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
)

type (
	visitor struct {
		cl       dokuwiki.ClientWithResponsesInterface
		base     *url.URL
		maxWidth int
	}
	dumper struct {
		visitor
		destDir     string
		encoding    encoding.Encoding
		embedImages bool
	}

	Element struct {
		Children  []string
		HTML      string
		ID, Title string
		Revision  int
		visitor   *visitor
	}
)

// MaxWidth sets the maximum image width.
func (v *visitor) MaxWidth(maxWidth int) *visitor {
	v.maxWidth = maxWidth
	return v
}

// MaxWidth sets the maximum image width.
func (d *dumper) MaxWidth(maxWidth int) *dumper {
	d.visitor.MaxWidth(maxWidth)
	return d
}

// DestDir sets the destination directory.
func (d *dumper) DestDir(destDir string) *dumper {
	d.destDir = destDir
	return d
}

// Client sets the client.
func (d *dumper) Client(cl dokuwiki.ClientWithResponsesInterface) *dumper {
	d.cl = cl
	return d
}

// EmbedImages sets whether we should embed the images.
func (d *dumper) EmbedImages(embed bool) *dumper {
	d.embedImages = embed
	return d
}

// Encoding sets the encoding of which the dumped HTML files are written.
func (d *dumper) Encoding(enc string) *dumper {
	enc = strings.ToLower(strings.TrimSpace(enc))
	if enc == "" || enc == "utf8" || enc == "utf-8" {
		d.encoding = nil
	} else {
		var err error
		if d.encoding, err = htmlindex.Get(enc); err != nil {
			panic(err)
		}
	}
	return d
}

func New(wikiURL, destDir, token string, force, embedImages bool) (*dumper, error) {
	wikiURL = strings.TrimSuffix(wikiURL, "/doku.php")
	cl, err := NewClient(wikiURL, token)
	if err != nil {
		return nil, err
	}
	v, err := NewVisitor(cl, wikiURL+"/doku.php")
	if err != nil {
		return nil, err
	}
	return NewDumper(v, destDir, force, embedImages)
}

func NewClient(wikiURL, token string) (*dokuwiki.ClientWithResponses, error) {
	return dokuwiki.NewClientWithResponses(wikiURL,
		dokuwiki.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-DokuWiki-Token", token) // Alternative if Auth header is stripped somewhere
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

func NewWithClient(cl dokuwiki.ClientWithResponsesInterface, wikiURL, destDir string, force, embedImages bool) (*dumper, error) {
	v, err := NewVisitor(cl, wikiURL)
	if err != nil {
		return nil, err
	}
	return NewDumper(v, destDir, force, embedImages)
}

func NewVisitor(cl dokuwiki.ClientWithResponsesInterface, wikiURL string) (*visitor, error) {
	base, err := url.Parse(wikiURL)
	if err != nil {
		return nil, err
	}
	return &visitor{cl: cl, base: base}, nil
}

func NewDumper(v *visitor, destDir string, force, embedImages bool) (*dumper, error) {
	if force {
		os.RemoveAll(destDir)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}
	return &dumper{
		visitor: *v, destDir: destDir, embedImages: embedImages,
	}, nil
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
	err = elt.ParseHTML(ctx, r, "")
	return elt, err
}

// curl -X POST "https://wiki.unosoft.hu/lib/exe/jsonrpc.php/core.getPageInfo" -H "content-type: application/json" -d '{ "page": "unosoft:alfa:kezikonyv:bruno3"}' -H authorization:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJkb2t1d2lraSIsInN1YiI6Imtlemlrb255diIsImlhdCI6MTc4NTc2OTE4OH0=.XvvkVitAGeoHoeAzrVoN11fVPNYL2nALDtz33sRfS8A="

func (v *visitor) GetInfo(ctx context.Context, a string) (Element, error) {
	resp, err := v.cl.CoreGetPageInfoWithResponse(ctx, dokuwiki.CoreGetPageInfoJSONRequestBody{
		Page: a,
	})
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

func (elt *Element) ParseHTML(ctx context.Context, r io.Reader, imagesDir string) error {
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
		sel.SetAttr("href", elt.RelHRef(a))
	})
	dir := filepath.Join(imagesDir, path.Dir(elt.FileName()))
	os.MkdirAll(dir, 0755)
	var cwd string
	maxWidth := elt.visitor.maxWidth
	var buf, sty strings.Builder
	hsh := sha256.New()
	doc.Find("img.media").Each(func(_ int, sel *goquery.Selection) {
		if err := func() error {
			src, ok := sel.Attr("src")
			if !ok {
				logger.Warn("no src", "at", sel)
				return nil
			} else if strings.HasPrefix(src, "data:") {
				return nil
			}
			logger.Debug("download", "src", src)
			ref, err := url.Parse(src)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", src, err)
			}

			// width, height to style
			var width int64
			if _, ok := sel.Attr("style"); !ok {
				q := ref.Query()
				sty.Reset()
				for _, k := range []string{"width", "height"} {
					if s := q.Get(k[:1]); s != "" {
						sty.WriteString(k)
						sty.WriteString(": ")
						sty.WriteString(s)
						sty.WriteString("; ")
						if maxWidth > 0 && k == "width" {
							width, _ = strconv.ParseInt(s, 10, 32)
						}
					}
				}
				sel.SetAttr("style", sty.String())
			}

			// Limit WIDTH for HTMLDOC
			if maxWidth > 0 { //HTMLDOC
				if width == 0 {
					if s, ok := sel.Attr("width"); ok {
						width, _ = strconv.ParseInt(s, 10, 32)
					}
				}
				if width > int64(maxWidth) {
					sel.SetAttr("WIDTH", "100%")
				}
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
			if imagesDir == "" {
				buf.Reset()
				if err = dataURL(&buf, resp.Body, ct); err != nil {
					return err
				}
				sel.SetAttr("src", buf.String())
			} else {
				_, ext, _ := strings.Cut(ct, "/")
				fh, err := os.CreateTemp(dir, "img-*."+ext)
				if err != nil {
					if cwd == "" {
						cwd, _ = os.Getwd()
					}
					return fmt.Errorf("CreateTemp@%s(%s): %w", cwd, dir, err)
				}
				// logger.Warn("tmp", "file", fh.Name())
				hsh.Reset()
				if _, err = io.Copy(io.MultiWriter(fh, hsh), resp.Body); err != nil {
					err = fmt.Errorf("Copy to %s: %w", fh.Name(), err)
				}
				fh.Close()
				if err != nil {
					os.Remove(fh.Name())
					return err
				}
				bn := base64.URLEncoding.EncodeToString(hsh.Sum(nil)) + "." + ext
				src, dst := fh.Name(), filepath.Join(filepath.Dir(fh.Name()), bn)
				// logger.Warn("mv", "src", src, "dst", dst)
				if err = os.Rename(src, dst); err != nil {
					return fmt.Errorf("mv %s %s: %w", src, dst, err)
				}
				sel.SetAttr("src", "./"+bn)
			}
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
	transform := func(s string) []byte { return []byte(s) }
	if d.encoding != nil {
		enc := encoding.HTMLEscapeUnsupported(d.encoding.NewEncoder())
		transform = func(s string) []byte {
			b, err := enc.Bytes([]byte(s))
			if err != nil {
				panic(fmt.Errorf("%q: %w", s, err))
			}
			return b
		}
	}
	err := d.Walk(ctx, func(ctx context.Context, elt Element, err error) error {
		if err != nil {
			logger.Warn("walk", "error", err)
			return nil
		}
		logger.Info("Walk", "id", elt.ID)
		fn := filepath.Join(d.destDir, elt.FileName())
		os.MkdirAll(filepath.Dir(fn), 0775)

		err = renameio.WriteFile(fn, transform(elt.HTML), 0644)
		elt.HTML = ""
		elts = append(elts, elt)
		return err
	}, a)
	return elts, err
}

func (e *Element) HRef() string     { return "./" + id2ref(e.ID) }
func (e *Element) FileName() string { return filepath.FromSlash(e.HRef()) }
func (e *Element) RelHRef(targetID string) string {
	me := strings.Split(e.ID, ":") // ["unosoft","alfa","kezikonyv","bruno3"]
	if len(me) > 1 {
		me = me[:len(me)-1]
	}
	ot := strings.Split(targetID, ":") // ["unosoft","alfa","kezikonyv","bruno3","09_giro"]
	for i := 0; i < len(me); i++ {
		if len(ot) <= i || ot[i] != me[i] {
			break
		}
		me, ot = me[1:], ot[1:]
		i--
	}
	if len(me) == 0 {
		return "./" + id2ref(strings.Join(ot, ":"))
	}
	pre := make([]string, 0, len(me))
	for range len(me) {
		pre = append(pre, "..")
	}
	// []
	// ["09_giro"]
	return id2ref(strings.Join(append(pre, ot...), ":"))
}

func id2ref(id string) string {
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
		var dd string
		if !d.embedImages {
			dd = d.destDir
		}

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

				fn := filepath.Join(d.destDir, elt.FileName())
				var body io.Reader
				if elt.Revision != 0 {
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
				if err = elt.ParseHTML(ctx, body, dd); err != nil {
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
