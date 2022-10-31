package web

import (
	"embed"
	"html/template"
	"io"

	"github.com/diamondburned/tmplutil"
)

//go:embed routes/*.html components/*.html
var embedFS embed.FS

// Templates is a collection of templates.
var Templates = tmplutil.Templater{
	FileSystem: embedFS,
	Includes: map[string]string{
		"error":  "components/error.html",
		"title":  "components/title.html",
		"header": "components/header.html",
	},
	Functions: template.FuncMap{},
	OnRenderFail: func(sub *tmplutil.Subtemplate, w io.Writer, err error) {
		self := sub.Templater()
		self.Subtemplate("error").Execute(w, err)
	},
}
