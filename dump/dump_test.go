// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: AGPL-3.0

package dump

import (
	"testing"
)

func TestRelHRef(t *testing.T) {
	const defaultBase = "unosoft:alfa:kezikonyv:bruno3"
	t.Run("hier", func(t *testing.T) {
		for other, tC := range map[string]struct {
			Base, Want string
		}{
			"unosoft:alfa:kezikonyv:bruno3:09_giro": {Want: "./bruno3/09_giro.html"},
			"unosoft:alfa:kezikonyv:bruno3:01:02":   {Want: "./bruno3/01/02.html"},
			"unosoft:alfa:kezikonyv:bruno":          {Want: "./bruno.html"},
			"xxx":                                   {Want: "../../../xxx.html"},
		} {
			base := tC.Base
			if base == "" {
				base = defaultBase
			}
			got := (&Element{ID: base}).RelHRef(other)
			t.Logf("%s[%s]: %s", other, base, got)
			if got != tC.Want {
				t.Errorf("%s[%s]: got %s, wanted %s", other, base, got, tC.Want)
			}
		}
	})

	t.Run("flat", func(t *testing.T) {
		vis := visitor{flat: true}
		for other, tC := range map[string]struct {
			Base, Want string
		}{
			"yyy":            {Want: "./yyy.html"},
			"01_x:02_y:03_z": {Want: "./01.02.03.html"},
		} {
			base := tC.Base
			if base == "" {
				base = defaultBase
			}
			got := (&Element{ID: base, visitor: &vis}).RelHRef(
				other,
			)
			t.Logf("%s[%s]: %s", other, base, got)
			if got != tC.Want {
				t.Errorf("%s[%s]: got %s, wanted %s", other, base, got, tC.Want)
			}
		}
	})
}

func TestHRef(t *testing.T) {
	t.Run("hier", func(t *testing.T) {
		for id, want := range map[string]string{
			"01_x:02_y": "./01_x/02_y.html",
			"unosoft:alfa:kezikonyv:bruno3:01_x:02_y": "./unosoft/alfa/kezikonyv/bruno3/01_x/02_y.html",
		} {
			got := (&Element{ID: id}).HRef()
			t.Logf("%s -> %s", id, got)
			if got != want {
				t.Errorf("got %s, wanted %s (id=%s)", got, want, id)
			}
		}
	})

	t.Run("flat", func(t *testing.T) {
		vis := visitor{flat: true, root: "unosoft:alfa:kezikonyv:bruno3"}
		for id, want := range map[string]string{
			"01_x:02_y":                               "./01.02.html",
			"unosoft:alfa:kezikonyv:bruno3":           "./00.html",
			"unosoft:alfa:kezikonyv:bruno3:01_x:02_y": "./01.02.html",
		} {
			got := (&Element{ID: id, visitor: &vis}).HRef()
			t.Logf("%s -> %s", id, got)
			if got != want {
				t.Errorf("got %s, wanted %s (id=%s)", got, want, id)
			}
		}
	})
}
