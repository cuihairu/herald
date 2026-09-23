package template

import (
	"context"
	"strings"
	"testing"
)

func TestPlainRenderer(t *testing.T) {
	r := &PlainRenderer{}
	if got := r.Format(); got != RenderFormatPlain {
		t.Errorf("Format() = %q, want %q", got, RenderFormatPlain)
	}

	data := &RenderedData{
		Title: "Disk alert",
		Fields: []RenderedField{
			{Label: "host", Value: "web-1"},
			{Label: "usage", Value: "92%"},
		},
	}

	out, err := r.RenderString(context.Background(), data)
	if err != nil {
		t.Fatalf("RenderString() error = %v", err)
	}
	want := "Disk alert\nhost: web-1\nusage: 92%\n"
	if out != want {
		t.Errorf("RenderString() = %q, want %q", out, want)
	}

	viaRender, err := r.Render(context.Background(), data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if s, ok := viaRender.(string); !ok || s != want {
		t.Errorf("Render() = %#v, want string %q", viaRender, want)
	}
}

func TestPlainRendererNoFields(t *testing.T) {
	r := &PlainRenderer{}
	out, err := r.RenderString(context.Background(), &RenderedData{Title: "ping"})
	if err != nil {
		t.Fatalf("RenderString() error = %v", err)
	}
	if out != "ping\n" {
		t.Errorf("RenderString() = %q, want %q", out, "ping\n")
	}
}

func TestJSONRendererToJSON(t *testing.T) {
	r := &JSONRenderer{}

	card, err := r.Render(context.Background(), &RenderedData{
		Title: "hello",
		Level: "info",
		Fields: []RenderedField{
			{Label: "k", Value: "v"},
		},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	s, err := r.ToJSON(card)
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}
	if !strings.Contains(s, `"msg_type":"interactive"`) {
		t.Errorf("ToJSON() = %q, want msg_type interactive", s)
	}
	if !strings.Contains(s, `"template":"blue"`) {
		t.Errorf("ToJSON() = %q, want default blue template", s)
	}

	// A value JSON cannot encode must surface as an error, not panic.
	if _, err := r.ToJSON(map[string]interface{}{"bad": make(chan int)}); err == nil {
		t.Error("ToJSON() with unencodable value should return error")
	}
}

func TestJSONRendererBuildCardLevels(t *testing.T) {
	r := &JSONRenderer{}
	cases := map[string]string{
		"error":   "red",
		"warning": "orange",
		"info":    "blue",
		"":        "blue",
	}
	for level, wantColor := range cases {
		card := r.buildCard(&RenderedData{Title: "t", Level: level})
		header, ok := card["header"].(map[string]interface{})
		if !ok {
			t.Fatalf("level %q: card header missing", level)
		}
		if got := header["template"]; got != wantColor {
			t.Errorf("level %q: template = %v, want %v", level, got, wantColor)
		}
	}

	empty := r.buildCard(&RenderedData{Title: "t"})
	if els, ok := empty["elements"].([]interface{}); !ok || len(els) != 0 {
		t.Errorf("elements = %#v, want empty slice", empty["elements"])
	}
}

func TestHTMLRendererEscaping(t *testing.T) {
	r := &HTMLRenderer{}
	data := &RenderedData{
		Title: `<b>"quoted" & 'single'</b>`,
		Fields: []RenderedField{
			{Label: "a<b & \"c\">'d'", Value: "x&y<z>"},
		},
	}
	out, err := r.RenderString(context.Background(), data)
	if err != nil {
		t.Fatalf("RenderString() error = %v", err)
	}
	for _, want := range []string{
		"&#39;", "&quot;", "&amp;", "&lt;b&gt;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderString() output missing escaped entity %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<b>") {
		t.Error("RenderString() leaked unescaped <b> tag")
	}
}

func TestHTMLRendererLevelColors(t *testing.T) {
	r := &HTMLRenderer{}
	for level, wantColor := range map[string]string{
		"error":   "#f44336",
		"warning": "#ff9800",
		"info":    "#2196F3",
		"":        "#2196F3",
	} {
		out, err := r.RenderString(context.Background(), &RenderedData{Title: "t", Level: level})
		if err != nil {
			t.Fatalf("level %q: RenderString() error = %v", level, err)
		}
		if !strings.Contains(out, ".header{background-color:"+wantColor) {
			t.Errorf("level %q: header color %q missing in output", level, wantColor)
		}
	}
}

func TestMarkdownRendererLinkFieldsAndLevels(t *testing.T) {
	r := &MarkdownRenderer{}
	data := &RenderedData{
		Title: "build",
		Level: "error",
		Fields: []RenderedField{
			{Label: "logs", Value: "https://example.com/log", Type: string(FieldTypeLink)},
			{Label: "code", Value: "42"},
		},
	}
	out, err := r.RenderString(context.Background(), data)
	if err != nil {
		t.Fatalf("RenderString() error = %v", err)
	}
	if !strings.HasPrefix(out, "### 🔴 build\n") {
		t.Errorf("RenderString() missing error icon prefix: %q", out)
	}
	if !strings.Contains(out, "**logs**: [https://example.com/log](https://example.com/log)\n") {
		t.Errorf("RenderString() missing link rendering: %q", out)
	}
	if !strings.Contains(out, "**code**: `42`\n") {
		t.Errorf("RenderString() missing code rendering: %q", out)
	}

	for level, icon := range map[string]string{"warning": "🟡", "info": "🔵"} {
		out, err := r.RenderString(context.Background(), &RenderedData{Title: "t", Level: level})
		if err != nil {
			t.Fatalf("level %q: RenderString() error = %v", level, err)
		}
		if !strings.HasPrefix(out, "### "+icon+" t\n") {
			t.Errorf("level %q: missing icon %q in %q", level, icon, out)
		}
	}

	noLevel, err := r.RenderString(context.Background(), &RenderedData{Title: "t"})
	if err != nil {
		t.Fatalf("RenderString() error = %v", err)
	}
	if !strings.HasPrefix(noLevel, "### t\n") {
		t.Errorf("unknown level should emit no icon, got %q", noLevel)
	}
}

func TestEngineExecuteErrors(t *testing.T) {
	e := NewEngine()

	if _, err := e.Execute("", nil); err != nil {
		t.Errorf("Execute(\"\") error = %v, want nil", err)
	}
	if out, err := e.Execute("plain text", nil); err != nil || out != "plain text" {
		t.Errorf("Execute(plain) = %q, %v; want passthrough", out, err)
	}

	// Invalid template syntax fails at parse time.
	if _, err := e.Execute("{{.unclosed", nil); err == nil {
		t.Error("Execute() with invalid syntax should return parse error")
	}

	// Valid syntax that fails during execution: field access on a non-struct.
	if _, err := e.Execute("{{.x.Foo}}", map[string]interface{}{"x": 42}); err == nil {
		t.Error("Execute() with non-struct field access should return execute error")
	}
}

func TestEngineRenderErrorPaths(t *testing.T) {
	e := NewEngine()

	// Title render failure.
	_, err := e.Render(&Template{ID: "t", Title: "{{.broken", Fields: []Field{}}, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to render title") {
		t.Errorf("Render() title error = %v, want wrapped title failure", err)
	}

	// Field render failure.
	_, err = e.Render(&Template{
		ID:     "t",
		Title:  "ok",
		Fields: []Field{{Label: "f", Value: "{{.broken.field"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to render field f") {
		t.Errorf("Render() field error = %v, want wrapped field failure", err)
	}
}

func TestManagerRenderEngineError(t *testing.T) {
	m := NewManager()
	err := m.Register(&Template{ID: "bad", Name: "bad", Title: "{{.x.y.z"})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := m.Render("bad", map[string]interface{}{"x": nil}); err == nil {
		t.Error("Render() should propagate engine render error")
	}
}

func TestManagerLoadFromMapInvalid(t *testing.T) {
	m := NewManager()
	err := m.LoadFromMap(map[string]TemplateConfig{
		"broken": {Name: "", Title: "t"},
	})
	if err == nil || !strings.Contains(err.Error(), "failed to register template broken") {
		t.Errorf("LoadFromMap() error = %v, want wrapped register failure", err)
	}
	if _, err := m.Get("broken"); err == nil {
		t.Error("failed LoadFromMap should not register the template")
	}
}
