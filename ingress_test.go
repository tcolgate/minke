package minke

import (
	"net/http"
	"regexp"
	"testing"
)

func TestIngressHostGroup_matchRule(t *testing.T) {
	tests := []struct {
		name           string
		rules          []ingressRule
		reqURL         string
		expMatch       bool
		expMatchedRule int
	}{
		{
			name:     "no rules",
			rules:    []ingressRule{},
			reqURL:   "http://localhost/",
			expMatch: false,
		},

		{
			name: "simple prefix",
			rules: []ingressRule{
				{pathType: prefix, path: "/"},
			},
			reqURL:         "http://localhost/",
			expMatch:       true,
			expMatchedRule: 0,
		},
		{
			name: "simple prefix - no match",
			rules: []ingressRule{
				{pathType: prefix, path: "/path1"},
			},
			reqURL:   "http://localhost/",
			expMatch: false,
		},
		{
			name: "simple prefix - match",
			rules: []ingressRule{
				{pathType: prefix, path: "/path"},
			},
			reqURL:         "http://localhost/path",
			expMatch:       true,
			expMatchedRule: 0,
		},
		{
			name: "simple prefix - match under",
			rules: []ingressRule{
				{pathType: prefix, path: "/path"},
			},
			reqURL:         "http://localhost/path/hello",
			expMatch:       true,
			expMatchedRule: 0,
		},
		{
			name: "simple prefix - match trail",
			rules: []ingressRule{
				{pathType: prefix, path: "/path"},
			},
			reqURL:         "http://localhost/path/",
			expMatch:       true,
			expMatchedRule: 0,
		},

		{
			name: "simple glob",
			rules: []ingressRule{
				{pathType: glob, path: "/"},
			},
			reqURL:         "http://localhost/",
			expMatch:       true,
			expMatchedRule: 0,
		},
		{
			name: "simple glob - no match",
			rules: []ingressRule{
				{pathType: glob, path: "/path"},
			},
			reqURL:   "http://localhost/",
			expMatch: false,
		},

		{
			name: "simple glob - match",
			rules: []ingressRule{
				{pathType: glob, path: "/*"},
			},
			reqURL:         "http://localhost/path",
			expMatch:       true,
			expMatchedRule: 0,
		},
		{
			name: "simple glob - match under",
			rules: []ingressRule{
				{pathType: glob, path: "/path/*"},
			},
			reqURL:         "http://localhost/path/hello",
			expMatch:       true,
			expMatchedRule: 0,
		},
		{
			name: "simple glob - match trail",
			rules: []ingressRule{
				{pathType: glob, path: "/path"},
			},
			reqURL:         "http://localhost/path/",
			expMatch:       true,
			expMatchedRule: 0,
		},

		{
			name: "prefix - longest match",
			rules: []ingressRule{
				{pathType: prefix, path: "/"},
				{pathType: prefix, path: "/path/path2"},
				{pathType: prefix, path: "/path"},
			},
			reqURL:         "http://localhost/path/path2/something",
			expMatch:       true,
			expMatchedRule: 1,
		},

		{
			name: "glob - longest match",
			rules: []ingressRule{
				{pathType: glob, path: "/*"},
				{pathType: glob, path: "/path/path2/*"},
				{pathType: glob, path: "/path/*"},
			},
			reqURL:         "http://localhost/path/path2/something",
			expMatch:       true,
			expMatchedRule: 1,
		},

		{
			name: "exact wins 1",
			rules: []ingressRule{
				{pathType: prefix, path: "/path"},
				{pathType: exact, path: "/path"},
			},
			reqURL:         "http://localhost/path",
			expMatch:       true,
			expMatchedRule: 1,
		},
		{
			name: "exact wins 2",
			rules: []ingressRule{
				{pathType: exact, path: "/path"},
				{pathType: prefix, path: "/path"},
			},
			reqURL:         "http://localhost/path",
			expMatch:       true,
			expMatchedRule: 0,
		},

		{
			name: "re2",
			rules: []ingressRule{
				{pathType: re2, path: "^/"},
			},
			reqURL:         "http://localhost/",
			expMatch:       true,
			expMatchedRule: 0,
		},
		{
			name: "re2 - no match",
			rules: []ingressRule{
				{pathType: re2, path: "^/path"},
			},
			reqURL:   "http://localhost/",
			expMatch: false,
		},
		{
			name: "re2 - longest match",
			rules: []ingressRule{
				{pathType: re2, path: "^/"},
				{pathType: re2, path: "^/path/path2/"},
				{pathType: re2, path: "^/path/"},
			},
			reqURL:         "http://localhost/path/path2/something",
			expMatch:       true,
			expMatchedRule: 1,
		},
	}

	for _, st := range tests {
		st := st
		t.Run(st.name, func(t *testing.T) {
			ing := ingress{
				rules: st.rules,
			}
			for i := range ing.rules {
				if ing.rules[i].pathType == re2 {
					ing.rules[i].re = regexp.MustCompile(ing.rules[i].path)
				}
			}

			hg := ingressHostGroup{ing}
			var expRule *ingressRule
			if st.expMatch {
				expRule = &ing.rules[st.expMatchedRule]
			}
			req, err := http.NewRequest("GET", st.reqURL, nil)
			if err != nil {
				t.Skipf("invalid http req in test asset, %v", err)
				return
			}
			_, gotRule := hg.matchRule(req)
			if !st.expMatch && gotRule != nil {
				t.Fatalf("we did not expect a match, but we matched %#v", *gotRule)
			}
			if !st.expMatch && gotRule == nil {
				return
			}
			if gotRule == nil {
				t.Fatalf("we expected a match, but got none")
			}
			if expRule != gotRule {
				t.Fatalf("matched wrong rule, %#v", *gotRule)
			}
		})
	}
}

func BenchmarkIngressHostGroup_matchRule(b *testing.B) {
	tests := []struct {
		name           string
		rules          []ingressRule
		reqURL         string
		expMatch       bool
		expMatchedRule int
	}{
		{
			name: "prefix match",
			rules: []ingressRule{
				{pathType: prefix, path: "/"},
				{pathType: prefix, path: "/path/path2"},
				{pathType: prefix, path: "/path"},
			},
			reqURL:         "http://localhost/path/path2/something",
			expMatch:       true,
			expMatchedRule: 1,
		},
		{
			name: "glob match",
			rules: []ingressRule{
				{pathType: glob, path: "/*"},
				{pathType: glob, path: "/path/path2/*"},
				{pathType: glob, path: "/path/*"},
			},
			reqURL:         "http://localhost/path/path2/something",
			expMatch:       true,
			expMatchedRule: 1,
		},
		{
			name: "re2 match",
			rules: []ingressRule{
				{pathType: re2, path: "^/"},
				{pathType: re2, path: "^/path/path2"},
				{pathType: re2, path: "^/path"},
			},
			reqURL:         "http://localhost/path/path2/something",
			expMatch:       true,
			expMatchedRule: 1,
		},
	}

	for _, st := range tests {
		st := st
		b.Run(st.name, func(b *testing.B) {
			b.ReportAllocs()
			ing := ingress{
				rules: st.rules,
			}

			for i := range ing.rules {
				if ing.rules[i].pathType == re2 {
					ing.rules[i].re = regexp.MustCompile(ing.rules[i].path)
				}
			}

			hg := ingressHostGroup{ing}
			var expRule *ingressRule
			if st.expMatch {
				expRule = &ing.rules[st.expMatchedRule]
			}
			req, err := http.NewRequest("GET", st.reqURL, nil)
			if err != nil {
				b.Skipf("invalid http req in test asset, %v", err)
				return
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, gotRule := hg.matchRule(req)
				if !st.expMatch && gotRule != nil {
					b.Fatalf("we did not expect a match, but we matched %#v", *gotRule)
				}
				if !st.expMatch && gotRule == nil {
					return
				}
				if gotRule == nil {
					b.Fatalf("we expected a match, but got none")
				}
				if expRule != gotRule {
					b.Fatalf("matched wrong rule, %#v", *gotRule)
				}
			}
		})
	}
}
