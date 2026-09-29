package script

import "testing"

func TestIsScenarioYaml(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want bool
	}{
		{
			name: "object root with array scripts key",
			yaml: "name: Checkout regression\nscripts:\n  - flows/login.yml\n  - flows/checkout.yml\n",
			want: true,
		},
		{
			name: "object root with empty array scripts key",
			yaml: "name: Empty\nscripts: []\n",
			want: true,
		},
		{
			name: "bare script step list has no scripts key",
			yaml: "- launchApp\n- tap: Continue\n",
			want: false,
		},
		{
			name: "script config document has no scripts key",
			yaml: "appId: com.example.app\nname: Smoke\n",
			want: false,
		},
		{
			name: "scripts key present but not an array",
			yaml: "name: Checkout\nscripts: flows/login.yml\n",
			want: false,
		},
		{
			name: "malformed YAML",
			yaml: "name: [unterminated\n",
			want: false,
		},
		{
			name: "empty document",
			yaml: "",
			want: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsScenarioYaml(c.yaml); got != c.want {
				t.Errorf("IsScenarioYaml(%q) = %v, want %v", c.yaml, got, c.want)
			}
		})
	}
}
