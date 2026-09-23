// Copyright 2026 Tamás Gulácsi.
//
// SPDX-License-Identifier: AGPL-3.0

package dump

import (
	"fmt"
	"html/template"
	"io"
	"strings"
)

func WriteIndex(w io.Writer, tmpl *template.Template, elts []Element) error {
	if tmpl == nil {
		var err error
		if tmpl, err = template.New("index").Parse(`<!DOCTYPE html>
	<body>
		<ul>
			{{range .}}
			<li><a href="{{.HRef}}">{{.Title}}</a></li>
			{{end}}
		</ul>
	</body>
</html>`); err != nil {
			return err
		}
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, []Element{
		// Csak minta amivel mennie kell
		{ID: "unosoft:alfa:kezikonyv:bruno3", Title: "BRUNO3 Kézikönyv"},
	}); err != nil {
		return err
	}
	// fmt.Println(buf.String())
	if s := buf.String(); strings.Contains(s, "ZgotmplZ") {
		return fmt.Errorf("bad template:\n%s", s)
	}

	return tmpl.Execute(w, elts)
}
