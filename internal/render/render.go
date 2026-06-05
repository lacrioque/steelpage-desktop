package render

import (
	"bytes"
	"fmt"
	"html"
	"regexp"

	"github.com/markusfluer/steelpage-desktop/internal/config"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

type Renderer struct {
	md        goldmark.Markdown
	sanitizer *bluemonday.Policy
	mermaid   bool
}

func New(cfg config.Render) *Renderer {
	opts := []goldmark.Option{
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	}

	if cfg.CodeHighlighting {
		opts = append(opts, goldmark.WithExtensions(
			highlighting.NewHighlighting(highlighting.WithStyle("github")),
		))
	}

	if cfg.AllowRawHTML {
		opts = append(opts, goldmark.WithRendererOptions(gmhtml.WithUnsafe()))
	} else {
		opts = append(opts, goldmark.WithRendererOptions(gmhtml.WithUnsafe()))
	}

	r := &Renderer{
		md:        goldmark.New(opts...),
		sanitizer: buildSanitizer(),
		mermaid:   cfg.Mermaid,
	}
	return r
}

var mermaidFence = regexp.MustCompile("(?ms)^[ \\t]{0,3}```mermaid[ \\t]*\\r?\\n(.*?)\\r?\\n[ \\t]{0,3}```[ \\t]*$")

func (r *Renderer) Render(markdown []byte) (string, error) {
	source := markdown
	if r.mermaid {
		source = mermaidFence.ReplaceAllFunc(source, func(match []byte) []byte {
			sub := mermaidFence.FindSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			escaped := html.EscapeString(string(sub[1]))
			return []byte("\n<pre class=\"mermaid\">" + escaped + "</pre>\n")
		})
	}

	var buf bytes.Buffer
	if err := r.md.Convert(source, &buf); err != nil {
		return "", fmt.Errorf("goldmark convert: %w", err)
	}

	return r.sanitizer.Sanitize(buf.String()), nil
}

func buildSanitizer() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").OnElements("pre", "code", "span", "div")
	p.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowAttrs("style").OnElements("pre", "code", "span")
	p.AllowAttrs("checked", "disabled", "type").OnElements("input")
	return p
}
