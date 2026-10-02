package main

import "testing"

func TestCheck(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"net listen on all interfaces", "package p\nimport \"net\"\nfunc f() { _, _ = net.Listen(\"tcp\", \":8080\") }\n", 1},
		{"net listen on the zero address", "package p\nimport \"net\"\nfunc f() { _, _ = net.Listen(\"tcp\", \"0.0.0.0:0\") }\n", 1},
		{"net listen on the v6 wildcard", "package p\nimport \"net\"\nfunc f() { _, _ = net.Listen(\"tcp\", \"[::]:0\") }\n", 1},
		{"net listen with an empty host", "package p\nimport \"net\"\nfunc f() { _, _ = net.Listen(\"tcp\", \":0\") }\n", 1},
		{"net listen on a named host", "package p\nimport \"net\"\nfunc f() { _, _ = net.Listen(\"tcp\", \"example.com:80\") }\n", 1},
		{"listen config on all interfaces", "package p\nimport (\"context\"; \"net\")\nfunc f() { _, _ = new(net.ListenConfig).Listen(context.Background(), \"tcp\", \":0\") }\n", 1},
		{"listen packet on all interfaces", "package p\nimport \"net\"\nfunc f() { _, _ = net.ListenPacket(\"udp\", \":0\") }\n", 1},
		{"http listen and serve on all interfaces", "package p\nimport \"net/http\"\nfunc f() { _ = http.ListenAndServe(\":8080\", nil) }\n", 1},
		{"http listen and serve with an empty address", "package p\nimport \"net/http\"\nfunc f() { _ = http.ListenAndServe(\"\", nil) }\n", 1},
		{"http listen and serve tls on all interfaces", "package p\nimport \"net/http\"\nfunc f() { _ = http.ListenAndServeTLS(\":443\", \"c\", \"k\", nil) }\n", 1},
		{"server addr on all interfaces", "package p\nimport \"net/http\"\nvar s = &http.Server{Addr: \":8080\"}\n", 1},
		{"address behind a constant", "package p\nimport \"net\"\nconst addr = \"0.0.0.0:1\"\nfunc f() { _, _ = net.Listen(\"tcp\", addr) }\n", 1},
		{"address built by concatenation", "package p\nimport \"net\"\nconst host = \"\"\nfunc f() { _, _ = net.Listen(\"tcp\", host+\":0\") }\n", 1},
		{"address not provably constant", "package p\nimport \"net\"\nfunc f(a string) { _, _ = net.Listen(\"tcp\", a) }\n", 1},
		{"tcp listener with a computed address", "package p\nimport \"net\"\nfunc f(a *net.TCPAddr) { _, _ = net.ListenTCP(\"tcp\", a) }\n", 1},
		{"wildcard literal outside a listen call", "package p\nvar a = \"0.0.0.0:9\"\n", 1},
		{"v6 wildcard literal outside a listen call", "package p\nvar a = \"[::]:9\"\n", 1},

		{"loopback v4", "package p\nimport \"net\"\nfunc f() { _, _ = net.Listen(\"tcp\", \"127.0.0.1:0\") }\n", 0},
		{"loopback v6", "package p\nimport \"net\"\nfunc f() { _, _ = net.Listen(\"tcp\", \"[::1]:0\") }\n", 0},
		{"localhost", "package p\nimport \"net\"\nfunc f() { _, _ = net.Listen(\"tcp\", \"localhost:0\") }\n", 0},
		{"loopback behind a constant", "package p\nimport \"net\"\nconst addr = \"127.0.0.1:0\"\nfunc f() { _, _ = net.Listen(\"tcp\", addr) }\n", 0},
		{"loopback built by concatenation", "package p\nimport \"net\"\nconst host = \"127.0.0.1\"\nfunc f() { _, _ = net.Listen(\"tcp\", host+\":0\") }\n", 0},
		{"unix socket", "package p\nimport \"net\"\nfunc f(p string) { _, _ = net.Listen(\"unix\", p) }\n", 0},
		{"listen config on loopback", "package p\nimport (\"context\"; \"net\")\nfunc f() { _, _ = new(net.ListenConfig).Listen(context.Background(), \"tcp\", \"127.0.0.1:0\") }\n", 0},
		{"server addr on loopback", "package p\nimport \"net/http\"\nvar s = &http.Server{Addr: \"127.0.0.1:0\"}\n", 0},
		{"unrelated listen method", "package p\ntype q struct{}\nfunc (q) Listen() {}\nfunc f(x q) { x.Listen() }\n", 0},
		{"wildcard word in prose", "package p\nvar m = \"binds 0.0.0.0 never\"\n", 0},
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
