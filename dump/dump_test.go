// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: AGPL-3.0

package dump

import (
	"testing"
)

func TestRelHRef(t *testing.T) {
	const defaultBase = "unosoft:alfa:kezikonyv:bruno3"
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
}
