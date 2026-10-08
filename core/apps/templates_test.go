package apps

import (
	"errors"
	"testing"

	"github.com/cuihairu/herald/core/template"
)

func seedTemplatesRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry([]SeedApp{{Name: "demo-app", Tokens: []SeedToken{
		{Secret: "s", Scopes: []string{"config"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func tmpl(id string) *template.Template {
	return &template.Template{ID: id, Name: id + " name", Title: "{{node}} down", Level: "error"}
}

func TestAppTemplates(t *testing.T) {
	r := seedTemplatesRegistry(t)

	// Same template id in two namespaces stays strangers.
	other, err := NewRegistry([]SeedApp{{Name: "app-b", Tokens: []SeedToken{
		{Secret: "x", Scopes: []string{"config"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	mgr, err := r.Templates("demo-app")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Register(tmpl("node_down")); err != nil {
		t.Fatal(err)
	}
	otherMgr, err := other.Templates("app-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := otherMgr.Register(tmpl("node_down")); err != nil {
		t.Fatal(err)
	}

	list, err := r.ListTemplates("demo-app")
	if err != nil || len(list) != 1 || list[0].ID != "node_down" {
		t.Fatalf("ListTemplates = %v/%v, want one node_down", list, err)
	}
	got, err := r.GetTemplate("demo-app", "node_down")
	if err != nil || got.Title != "{{node}} down" {
		t.Fatalf("GetTemplate = %v/%v", got, err)
	}
	if _, err := r.GetTemplate("demo-app", "ghost"); err == nil {
		t.Error("unknown template resolved")
	}
	// Isolation: demo-app's list never shows app-b's registration.
	otherList, _ := other.ListTemplates("app-b")
	if len(otherList) != 1 {
		t.Fatalf("app-b templates = %v, want exactly its own one", otherList)
	}

	if err := r.DeleteTemplate("demo-app", "node_down"); err != nil {
		t.Fatalf("DeleteTemplate = %v", err)
	}
	if err := r.DeleteTemplate("demo-app", "node_down"); err == nil {
		t.Error("deleting an unknown template must be an error, not a no-op")
	}
	if _, err := r.Templates("ghost"); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("Templates(ghost) = %v, want ErrUnknownApp", err)
	}
	if _, err := r.ListTemplates("ghost"); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("ListTemplates(ghost) = %v, want ErrUnknownApp", err)
	}
	if err := r.DeleteTemplate("ghost", "x"); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("DeleteTemplate(ghost) = %v, want ErrUnknownApp", err)
	}
	if _, err := r.GetTemplate("ghost", "x"); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("GetTemplate(ghost) = %v, want ErrUnknownApp", err)
	}
}
