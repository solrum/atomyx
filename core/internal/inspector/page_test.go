package inspector

import (
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"testing"
)

var (
	tagPattern       = regexp.MustCompile(`(?is)<([a-z][a-z0-9-]*)((?:\s+[^<>]*?)?)\s*/?>`)
	attrPattern      = regexp.MustCompile(`(?is)([a-z_:][-a-z0-9_:.]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+)))?`)
	scriptBodyRegexp = regexp.MustCompile(`(?is)<script\b[^>]*>(.*?)</script>`)
)

type htmlTag struct {
	name  string
	attrs map[string]string
}

func tagsOf(src string) []htmlTag {
	var out []htmlTag
	for _, m := range tagPattern.FindAllStringSubmatch(src, -1) {
		tag := htmlTag{name: strings.ToLower(m[1]), attrs: map[string]string{}}
		for _, a := range attrPattern.FindAllStringSubmatch(m[2], -1) {
			tag.attrs[strings.ToLower(a[1])] = a[2] + a[3] + a[4]
		}
		out = append(out, tag)
	}
	return out
}

func embeddedFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := fs.WalkDir(mustSub(embeddedAssets, "assets"), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no embedded assets")
	}
	return files
}

func pageServer(t *testing.T) *Server {
	t.Helper()
	srv, _ := newTestServer(t, PageRoutes)
	return srv
}

func TestPageServesIndexWithSecurityHeaders(t *testing.T) {
	rec := pageServer(t).do(call{method: http.MethodGet, path: "/"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("content type = %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("csp = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("cache control = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "<title>Atomyx Inspector</title>") {
		t.Errorf("body is not the inspector page: %.80q", rec.Body.String())
	}
}

func TestPageNeedsNoToken(t *testing.T) {
	srv := pageServer(t)
	for _, p := range []string{"/", assetsPrefix + "main.js"} {
		if rec := srv.do(call{method: http.MethodGet, path: p}); rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200 without a token", p, rec.Code)
		}
	}
}

func TestEveryEmbeddedAssetIsServedWithItsContentType(t *testing.T) {
	srv := pageServer(t)
	for _, file := range embeddedFiles(t) {
		want, ok := assetContentTypes[path.Ext(file)]
		if !ok {
			t.Errorf("%s has an extension with no content type", file)
			continue
		}
		rec := srv.do(call{method: http.MethodGet, path: assetsPrefix + file})
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d", file, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != want {
			t.Errorf("%s content type = %q, want %q", file, got, want)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s nosniff = %q", file, got)
		}
	}
}

func TestUnknownAssetsAreNotFound(t *testing.T) {
	srv := pageServer(t)
	for _, p := range []string{"/assets/missing.js", "/assets/", "/assets/a.txt", "/nope"} {
		if rec := srv.do(call{method: http.MethodGet, path: p}); rec.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", p, rec.Code)
		}
	}
}

func TestEmbeddedHTMLHasNothingTheCSPBlocks(t *testing.T) {
	assets := mustSub(embeddedAssets, "assets")
	for _, file := range embeddedFiles(t) {
		if path.Ext(file) != ".html" {
			continue
		}
		raw, err := fs.ReadFile(assets, file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		for _, m := range scriptBodyRegexp.FindAllStringSubmatch(src, -1) {
			if strings.TrimSpace(m[1]) != "" {
				t.Errorf("%s holds an inline script", file)
			}
		}
		for _, tag := range tagsOf(src) {
			if tag.name == "style" {
				t.Errorf("%s holds a style element", file)
			}
			for attr := range tag.attrs {
				switch {
				case attr == "style":
					t.Errorf("%s: <%s> has a style attribute", file, tag.name)
				case strings.HasPrefix(attr, "on"):
					t.Errorf("%s: <%s> has the %s event handler", file, tag.name, attr)
				}
			}
		}
	}
}

func TestEveryReferencedScriptAndStylesheetIsEmbedded(t *testing.T) {
	assets := mustSub(embeddedAssets, "assets")
	raw, err := fs.ReadFile(assets, pageFile)
	if err != nil {
		t.Fatal(err)
	}
	refs := 0
	for _, tag := range tagsOf(string(raw)) {
		var ref string
		switch {
		case tag.name == "script":
			ref = tag.attrs["src"]
		case tag.name == "link" && strings.EqualFold(tag.attrs["rel"], "stylesheet"):
			ref = tag.attrs["href"]
		default:
			continue
		}
		refs++
		if !strings.HasPrefix(ref, assetsPrefix) {
			t.Errorf("reference %q is outside %s", ref, assetsPrefix)
			continue
		}
		if _, err := fs.Stat(assets, strings.TrimPrefix(ref, assetsPrefix)); err != nil {
			t.Errorf("reference %q does not resolve to an embedded file: %v", ref, err)
		}
	}
	if refs == 0 {
		t.Error("index.html references no script or stylesheet")
	}
}

func TestEveryModuleImportIsEmbedded(t *testing.T) {
	assets := mustSub(embeddedAssets, "assets")
	importPattern := regexp.MustCompile(`from '\./([^']+)'`)
	for _, file := range embeddedFiles(t) {
		if path.Ext(file) != ".js" {
			continue
		}
		raw, err := fs.ReadFile(assets, file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range importPattern.FindAllStringSubmatch(string(raw), -1) {
			if _, err := fs.Stat(assets, m[1]); err != nil {
				t.Errorf("%s imports ./%s, which is not embedded", file, m[1])
			}
		}
	}
}

// Device text reaches the page in tree attributes, so a module that
// parsed it as markup or code would let a screen name run script in the
// inspector origin. Modules build their output with textContent and
// element children only.
func TestEmbeddedScriptsNeverParseDeviceTextAsMarkupOrCode(t *testing.T) {
	assets := mustSub(embeddedAssets, "assets")
	banned := regexp.MustCompile(`\.innerHTML\b|\.outerHTML\b|insertAdjacentHTML|document\.write|\beval\(|new Function\(`)
	for _, file := range embeddedFiles(t) {
		if path.Ext(file) != ".js" {
			continue
		}
		raw, err := fs.ReadFile(assets, file)
		if err != nil {
			t.Fatal(err)
		}
		if loc := banned.FindString(string(raw)); loc != "" {
			t.Errorf("%s uses %q, which parses text as markup or code", file, loc)
		}
	}
}

// The text a person types into the run step control may be a password,
// so its field must be a password input that the browser does not show.
func TestRunStepTextFieldIsAPasswordInput(t *testing.T) {
	raw, err := fs.ReadFile(mustSub(embeddedAssets, "assets"), "act-panel.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "text.type = 'password'") {
		t.Error("act-panel.js does not make the text field a password input")
	}
}
