// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: AGPL-3.0

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
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
		seen                 map[string]struct{}
		destDir, mediaSubdir string
		base                 *url.URL
		cl                   dokuwiki.ClientWithResponsesInterface
		mu                   sync.Mutex
	}
	element struct {
		ID, Title string
	}
)

func newDumper(cl dokuwiki.ClientWithResponsesInterface, wikiURL, destDir, mediaSubdir string) (*dumper, error) {
	if err := os.MkdirAll(filepath.Join(destDir, mediaSubdir), 0755); err != nil {
		return nil, err
	}
	base, err := url.Parse(wikiURL)
	if err != nil {
		return nil, err
	}
	return &dumper{base: base, cl: cl, destDir: destDir, mediaSubdir: mediaSubdir, seen: make(map[string]struct{})}, nil
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
		logger.Info("got", "title", elt.Title, "id", elt.ID, "page", a, "status", resp.Status())
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

	var more []string
	err := func() error {
		resp, err := d.cl.CoreGetPageHTMLWithResponse(ctx, dokuwiki.CoreGetPageHTMLJSONRequestBody{Page: elt.ID})
		if err != nil {
			return err
		}
		logger.Info("got", "status", resp.Status())
		result := *resp.GetJSON200().Result
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(result))
		if err != nil {
			return fmt.Errorf("parse %s: %w", result, err)
		}
		mediaDir := filepath.Join(d.destDir, d.mediaSubdir)
		var errs []error
		doc.Find("a[data-wiki-id]").Each(func(_ int, sel *goquery.Selection) {
			a, ok := sel.Attr("data-wiki-id")
			if !ok {
				errs = append(errs, fmt.Errorf("no data-wiki-id: %v", sel))
			}
			more = append(more, a)
			sel.SetAttr("href", id2fn(a))
		})
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
				_, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
				fn := params["filename"]
				ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
				exts, _ := mime.ExtensionsByType(ct)
				if len(exts) != 0 && filepath.Ext(fn) == "" {
					fn += exts[0]
				}
				fh, err := os.CreateTemp(mediaDir, fn+"-*")
				if err != nil {
					return fmt.Errorf("CreateTemp: %s", err)
				}
				if err := func() error {
					defer fh.Close()
					hsh := sha256.New()
					n, err := io.Copy(io.MultiWriter(fh, hsh), resp.Body)
					if err != nil {
						return fmt.Errorf("read %s: %w", want, err)
					}
					if err = fh.Close(); err != nil {
						return fmt.Errorf("close %s: %w", fh.Name(), err)
					}
					var a [sha256.Size]byte
					fn = base64.URLEncoding.EncodeToString(hsh.Sum(a[:0])) + "-" + fn
					logger.Info("got", "url", want, "length", n, "fn", fn)
					os.Rename(fh.Name(), filepath.Join(d.destDir, "media", fn))
					sel.SetAttr("src", path.Join(".", d.mediaSubdir, fn))
					return nil
				}(); err != nil {
					os.Remove(fh.Name())
					return err
				}
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
			fn := elt.ID + ".html"
			errs = append(errs, renameio.WriteFile(filepath.Join(d.destDir, fn), []byte(h), 0644))
		}
		return errors.Join(errs...)
	}()
	return elt, more, err
}

func (e element) HRef() string { return id2fn(e.ID) }

func id2fn(id string) string { return path.Join(".", template.URLQueryEscaper(id)+".html") }
