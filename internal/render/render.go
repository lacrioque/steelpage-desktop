package render

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"

	"github.com/markusfluer/steelpage-desktop/internal/config"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type Renderer struct {
	md        goldmark.Markdown
	sanitizer *bluemonday.Policy
	mermaid   bool
}

func New(cfg config.Render) *Renderer {
	opts := []goldmark.Option{
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			// Tag rendered blocks with their source line so the read view
			// can place comment markers (comments anchor to source lines).
			parser.WithASTTransformers(util.Prioritized(sourceLineTransformer{}, 100)),
		),
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
	// Keep the data-source-line / data-source-line-end markers emitted by
	// sourceLineTransformer so the read view can anchor comment markers.
	p.AllowDataAttributes()
	return p
}

// sourceLineTransformer annotates every block node that maps to a source
// range with `data-source-line` (1-based start) and, when it spans more
// than one line, `data-source-line-end`. The read view uses these to place
// comment markers next to the block a commented source line falls in.
type sourceLineTransformer struct{}

func (sourceLineTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	// Offsets of every newline, so a byte offset → line number is a binary
	// search instead of a rescan per node.
	var newlines []int
	for i, b := range src {
		if b == '\n' {
			newlines = append(newlines, i)
		}
	}
	lineAt := func(off int) int {
		return sort.Search(len(newlines), func(i int) bool { return newlines[i] >= off }) + 1
	}

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Type() != ast.TypeBlock {
			return ast.WalkContinue, nil
		}
		lines := n.Lines()
		if lines == nil || lines.Len() == 0 {
			return ast.WalkContinue, nil
		}
		start := lineAt(lines.At(0).Start)
		end := lineAt(lines.At(lines.Len() - 1).Stop)
		n.SetAttributeString("data-source-line", []byte(strconv.Itoa(start)))
		if end != start {
			n.SetAttributeString("data-source-line-end", []byte(strconv.Itoa(end)))
		}
		return ast.WalkContinue, nil
	})
}
