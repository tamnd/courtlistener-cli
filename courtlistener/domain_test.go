package courtlistener

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring, which need no network. The client's HTTP behaviour is
// covered in courtlistener_test.go (external package tests).

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "courtlistener" {
		t.Errorf("Scheme = %q, want courtlistener", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "courtlistener" {
		t.Errorf("Identity.Binary = %q, want courtlistener", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		// Numeric input → opinion type.
		{"12345", "opinion", "12345"},
		{"1", "opinion", "1"},
		// Non-numeric → query type.
		{"civil rights", "query", "civil rights"},
		{"Roe v. Wade", "query", "Roe v. Wade"},
		{"due process", "query", "due process"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil {
			t.Errorf("Classify(%q) error: %v", tc.in, err)
			continue
		}
		if typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q), want (%q, %q)",
				tc.in, typ, id, tc.typ, tc.id)
		}
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return an error")
	}
}

func TestLocate(t *testing.T) {
	cases := []struct {
		uriType string
		id      string
		want    string
	}{
		{"opinion", "12345", "https://www.courtlistener.com/opinion/12345/"},
		{"query", "civil rights", "https://www.courtlistener.com/?q=civil rights&type=o"},
	}
	for _, tc := range cases {
		got, err := Domain{}.Locate(tc.uriType, tc.id)
		if err != nil {
			t.Errorf("Locate(%q, %q) error: %v", tc.uriType, tc.id, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Locate(%q, %q) = %q, want %q", tc.uriType, tc.id, got, tc.want)
		}
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "foo")
	if err == nil {
		t.Error("Locate with unknown type should return an error")
	}
}

func TestIsNumeric(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"12345", true},
		{"0", true},
		{"abc", false},
		{"12a3", false},
		{"", false},
		{" 123", false},
	}
	for _, tc := range cases {
		got := isNumeric(tc.s)
		if got != tc.want {
			t.Errorf("isNumeric(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestDomainRegistered(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Domain("courtlistener"); !ok {
		t.Fatal("courtlistener domain not registered")
	}
}

func TestResolveOn(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.ResolveOn("courtlistener", "12345")
	if err != nil {
		t.Fatalf("ResolveOn: %v", err)
	}
	if got.String() != "courtlistener://opinion/12345" {
		t.Errorf("ResolveOn = %q, want courtlistener://opinion/12345", got.String())
	}
}
