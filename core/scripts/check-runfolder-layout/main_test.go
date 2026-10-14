package main

import "testing"

func TestCheck(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"const with trailing comment", "package p\nconst d = \"screenshots\" // out\n", 1},
		{"var with trailing comment", "package p\nvar d = \"runs\" // root\n", 1},
		{"join split over lines", "package p\nimport \"path/filepath\"\nvar p = filepath.Join(\n\tbase,\n\t\"report\",\n)\n", 1},
		{"os mkdir with v1", "package p\nimport \"os\"\nfunc f() { _ = os.MkdirAll(\"v1\", 0o755) }\n", 1},
		{"composite literal", "package p\nvar parts = []string{\"runs\", \"v1\"}\n", 1},
		{"raw string path", "package p\nvar p = `runs/v1`\n", 1},
		{"slashed report segment", "package p\nvar p = \"x/report/index.html\"\n", 1},
		{"link format", "package p\nvar f = \"screenshots/%s\"\n", 1},
		{"artifacts link format", "package p\nvar f = \"../artifacts/%s\"\n", 1},
		{"v1 in a longer path", "package p\nvar p = \"x/v1/y\"\n", 1},

		{"map value beside slashed string", "package p\nvar m = map[string]string{\"a\": \"b/c\", \"k\": \"ok\"}\n", 0},
		{"struct tag with slashes", "package p\ntype T struct {\n\tA string `json:\"a/runs\"`\n\tB string `yaml:\"report\"`\n}\n", 0},
		{"call argument v1 on another receiver", "package p\nfunc f(cfg interface{ Get(...string) string }) { _ = cfg.Get(\"api\", \"v1\") }\n", 0},
		{"bare report outside a path call", "package p\nvar k = \"report\"\n", 0},
		{"bare v1 outside a path call", "package p\nvar k = \"v1\"\n", 0},
		{"format identifier", "package p\nvar id = \"atomyx/v1\"\n", 0},
		{"import path", "package p\nimport _ \"example.com/runs/v1\"\n", 0},
		{"prose with a slash", "package p\nvar m = \"see runs/v1 for details\"\n", 0},
		{"url", "package p\nvar u = \"https://example.com/runs\"\n", 0},
		{"http api route", "package p\nvar r = \"/api/v1/auth\"\n", 0},
		{"index file name", "package p\nvar n = \"index.json\"\n", 1},
		{"junit file name", "package p\nvar n = \"junit.xml\"\n", 1},
		{"index file in a path", "package p\nvar n = \"x/index.json\"\n", 1},
		{"junit file in a path", "package p\nvar n = \"report/junit.xml\"\n", 1},
		{"video prefix alone", "package p\nvar n = \"video-\"\n", 1},
		{"video prefix format", "package p\nvar n = \"video-%s.mp4\"\n", 1},
		{"video prefix in a path", "package p\nvar n = \"a/video-1.mp4\"\n", 1},
		{"file name inside an extension", "package p\nvar n = \"my-index.json.bak\"\n", 0},
		{"prose beginning with the prefix", "package p\nvar n = \"video-based checks run later\"\n", 0},
		{"word video alone", "package p\nvar n = \"video\"\n", 0},
		{"unrelated path", "package p\nvar p = \"a/b/c\"\n", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := check("x.go", []byte(c.src))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != c.want {
				t.Fatalf("got %d findings %+v, want %d", len(got), got, c.want)
			}
		})
	}
}

func TestCheckModuleRouterMapKey(t *testing.T) {
	const keyed = "package p\nvar m = map[string]int{\"runs\": 1}\n"
	const valued = "package p\nvar m = map[string]string{\"a\": \"runs\"}\n"
	cases := []struct {
		name     string
		filename string
		src      string
		want     int
	}{
		{"map key in the router file", moduleRouterFile, keyed, 0},
		{"map key in another file", "internal/cli/other.go", keyed, 1},
		{"map value in the router file", moduleRouterFile, valued, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := check(c.filename, []byte(c.src))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != c.want {
				t.Fatalf("got %d findings %+v, want %d", len(got), got, c.want)
			}
		})
	}
}
