package render

import (
	"strings"
	"testing"

	"github.com/markusfluer/steelpage-desktop/internal/config"
)

func TestSourceLineAttributes(t *testing.T) {
	r := New(config.Render{Mermaid: true, CodeHighlighting: false, SanitizeHTML: true})
	md := "# Title\n\nFirst para.\n\nSecond para line five.\n"
	out, err := r.Render([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `data-source-line="1"`) {
		t.Errorf("heading missing source line 1:\n%s", out)
	}
	if !strings.Contains(out, `data-source-line="3"`) {
		t.Errorf("first para missing source line 3:\n%s", out)
	}
	if !strings.Contains(out, `data-source-line="5"`) {
		t.Errorf("second para missing source line 5:\n%s", out)
	}
}

func TestMermaidPreservesSourceLines(t *testing.T) {
	r := New(config.Render{Mermaid: true})
	// Para on line 1, mermaid block lines 3-6, trailing para on line 8.
	md := "intro\n\n```mermaid\ngraph TD\nA-->B\n```\n\nafter the diagram\n"
	out, err := r.Render([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `data-source-line="1"`) {
		t.Errorf("intro should be line 1:\n%s", out)
	}
	if !strings.Contains(out, `data-source-line="8"`) {
		t.Errorf("trailing para should still be line 8 after mermaid:\n%s", out)
	}
}
