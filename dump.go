// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: AGPL-3.0

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	dokuwiki "github.com/UNO-SOFT/dokuwiki-go/rest"
	"github.com/google/renameio/v2"
)

var ErrNotFound = errors.New("not found")

type (
	dumper struct {
		seen    map[string]struct{}
		destDir string
		base    *url.URL
		cl      dokuwiki.ClientWithResponsesInterface
		mu      sync.Mutex
	}
	element struct {
		ID, Title string
	}
)

func newDumper(cl dokuwiki.ClientWithResponsesInterface, wikiURL, destDir string) (*dumper, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}
	base, err := url.Parse(wikiURL)
	if err != nil {
		return nil, err
	}
	return &dumper{base: base, cl: cl, destDir: destDir, seen: make(map[string]struct{})}, nil
}

func (d *dumper) dump(ctx context.Context, a string) (element, []string, error) {
	var elt element
	d.mu.Lock()
	_, ok := d.seen[a]
	if !ok {
		d.seen[a] = struct{}{}
	}
	d.mu.Unlock()
	if ok {
		return elt, nil, nil
	}
	{
		resp, err := d.cl.CoreGetPageInfoWithResponse(ctx, dokuwiki.CoreGetPageInfoJSONRequestBody{Page: a})
		if err != nil {
			return elt, nil, err
		}
		if result := resp.GetJSON200(); result == nil {
			if b := resp.GetBody(); bytes.Contains(b, []byte("does not exist")) {
				return elt, nil, fmt.Errorf("%w: %s", ErrNotFound, string(b))
			} else {
				return elt, nil, fmt.Errorf("get %s: %s", a, string(b))
			}
		} else {
			elt.ID, elt.Title = *result.Result.Id, *result.Result.Title
		}
	}
	logger.Info("download", "id", elt.ID, "title", elt.Title)

	var more []string
	err := func() error {
		resp, err := d.cl.CoreGetPageHTMLWithResponse(ctx, dokuwiki.CoreGetPageHTMLJSONRequestBody{Page: elt.ID})
		if err != nil {
			return err
		}
		result := *resp.GetJSON200().Result
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(result))
		if err != nil {
			return fmt.Errorf("parse %s: %w", result, err)
		}
		var errs []error
		doc.Find("a[data-wiki-id]").Each(func(_ int, sel *goquery.Selection) {
			a, ok := sel.Attr("data-wiki-id")
			if !ok {
				errs = append(errs, fmt.Errorf("no data-wiki-id: %v", sel))
			}
			more = append(more, a)
			sel.SetAttr("href", "./"+id2fn(a))
		})
		var buf strings.Builder
		doc.Find("img.media").Each(func(_ int, sel *goquery.Selection) {
			if err := func() error {
				src, ok := sel.Attr("src")
				if !ok {
					logger.Warn("no src", "at", sel)
					return nil
				}
				ref, err := url.Parse(src)
				if err != nil {
					return fmt.Errorf("resolve %s: %w", src, err)
				}
				want := d.base.ResolveReference(ref).String()
				req, err := http.NewRequestWithContext(ctx, "GET", want, nil)
				if err != nil {
					return fmt.Errorf("NewRequest(%s): %w", want, err)
				}
				resp, err := d.cl.(dokuwiki.HttpRequestDoer).Do(req)
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
		h, err := doc.Html()
		if err != nil {
			errs = append(errs, err)
		} else {
			fn := filepath.Join(d.destDir, id2fn(elt.ID))
			os.MkdirAll(filepath.Dir(fn), 0775)
			errs = append(errs, renameio.WriteFile(fn, []byte(h), 0644))
		}
		return errors.Join(errs...)
	}()
	return elt, more, err
}

func (e element) HRef() string { return "./" + id2fn(e.ID) }

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
