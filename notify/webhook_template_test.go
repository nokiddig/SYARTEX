package notify

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// This file locks down the **capability boundary** of the generic webhook template.
//
// This is the only place in the whole package where "a user-supplied string is evaluated as code", so it must be
// clear what it can and cannot do, and tests pin those properties down: otherwise someone could later add a
// method to the template context, or a readFile to the FuncMap, and the capability surface would silently grow,
// while the diff looks like just a harmless small function.

// TestTemplateContextHasNoMethods is the most important one.
//
// text/template calls exported methods ({{.Foo}} can fetch a field or call a method). So as soon as the template
// context can reach **any** type that has exported methods, those methods are exposed to the template author.
// The context of this feature is deliberately pure data (exported fields only, zero methods).
//
// If this fails: someone added a method to webhookTemplateData / webhookItem.
// Before deciding to allow it, think through whether a template could use that method to read something that should not be exposed.
func TestTemplateContextHasNoMethods(t *testing.T) {
	for _, v := range []any{webhookTemplateData{}, webhookItem{}} {
		typ := reflect.TypeOf(v)
		if n := typ.NumMethod(); n != 0 {
			var names []string
			for i := 0; i < n; i++ {
				names = append(names, typ.Method(i).Name)
			}
			t.Fatalf("%s exposes %d methods (%s): text/template can call them, "+
				"which hands the capabilities of those methods to the template author", typ.Name(), n, strings.Join(names, ", "))
		}
	}
}

// TestTemplateFuncsAreMinimal locks down the set of functions exposed to templates.
//
// Every extra function in the FuncMap adds a capability. Currently there are only json / jsons, which serialise a
// value into a JSON fragment: they cannot read files, send requests or execute commands.
func TestTemplateFuncsAreMinimal(t *testing.T) {
	var got []string
	for name := range webhookTemplateFuncs {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"json", "jsons"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the template function set changed: got %v, want %v. Before adding a function, make sure it does not widen the capability surface "+
			"(no reading or writing files, no network requests, no command execution)", got, want)
	}
}

// TestTemplateCannotReachUnknownData covers out-of-bounds access in templates:
// accessing something that does not exist must fail instead of echoing something back, and the failure message must not leak internal data.
func TestTemplateCannotReachUnknownData(t *testing.T) {
	_, err := renderWebhookBody(`{"x": {{.Environment}}, "y": {{.Env}}}`, singleMsg())
	if err == nil {
		t.Fatal("accessing a non-existent field should return an error")
	}
	// The error must not contain real content from the template context (finding title/summary).
	it := singleMsg().Items[0]
	for _, leak := range []string{it.VulnClass, strings.SplitN(it.Summary, "\n", 2)[0]} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("template error leaked message content %q: %v", leak, err)
		}
	}
}

// TestTemplateRenderFailsPermanently: a wrong template is a configuration error, and retrying will not heal it.
// If it were judged retryable, one bad template would make every delivery waste three rounds of backoff.
func TestTemplateRenderFailsPermanently(t *testing.T) {
	cfg := map[string]any{
		"url":           "https://example.com/hook",
		"body_template": `{{.Items.`,
	}
	if err := (webhookChannel{}).Validate(cfg); err == nil {
		t.Fatal("a template syntax error should be rejected at save time")
	}
	// Even if validation is bypassed and delivery happens directly, it must be judged a permanent failure instead of being retried repeatedly.
	_, err := (webhookChannel{}).Send(context.Background(), cfg, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("a bad template should be judged a permanent failure, got %v", err)
	}
}

// TestTemplateCanOnlyProduceJSON covers the constraint that "the template rendering result must be valid JSON".
// It also blocks uses such as "generate plain text with a template to trigger some other protocol".
func TestTemplateCanOnlyProduceJSON(t *testing.T) {
	// A valid template passes.
	ok := map[string]any{"url": "https://example.com/hook", "body_template": `{"t":{{json .Title}}}`}
	if err := (webhookChannel{}).Validate(ok); err != nil {
		t.Fatalf("a valid template should pass validation: %v", err)
	}
	// When the rendering result is not JSON it must be rejected (instead of being sent as is).
	bad := map[string]any{"url": "http://127.0.0.1:1/hook", "body_template": `not json {{.Count}}`}
	_, err := (webhookChannel{}).Send(context.Background(), bad, singleMsg())
	if err == nil || !IsPermanent(err) {
		t.Fatalf("a rendering result that is not JSON should be judged a permanent failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("the error message should say it is a JSON problem, got %v", err)
	}
}
