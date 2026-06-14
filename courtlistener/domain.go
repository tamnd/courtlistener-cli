package courtlistener

import (
	"context"
	"fmt"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes courtlistener as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/courtlistener-cli/courtlistener"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// courtlistener:// URIs by routing to the operations Register installs.
func init() { kit.Register(Domain{}) }

// Domain is the courtlistener driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "courtlistener",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "courtlistener",
			Short:  "Search US court opinions from CourtListener.",
			Long: `Search US court opinions from CourtListener.

courtlistener reads public CourtListener data over plain HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools. No API
key required for read-only search.`,
			Site: "www.courtlistener.com",
			Repo: "https://github.com/tamnd/courtlistener-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// search op: full-text opinion search.
	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "read",
		List:    true,
		Summary: "Search US court opinions",
		URIType: "opinion",
		Args:    []kit.Arg{{Name: "query", Help: "search query"}},
	}, searchOpinions)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClientWithConfig(c), nil
}

// searchInput is the input struct for the search op.
type searchInput struct {
	Query  string  `kit:"arg"          help:"search query"`
	Court  string  `kit:"flag"         help:"court ID filter (e.g. scotus, ca9, nysd)"`
	Type   string  `kit:"flag"         help:"result type: o=opinions, oa=oral-args, r=recaps"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

func searchOpinions(ctx context.Context, in searchInput, emit func(*Opinion) error) error {
	if in.Query == "" {
		return errs.Usage("search requires a query argument")
	}
	opinions, err := in.Client.SearchOpinions(ctx, in.Query, in.Court, in.Type, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for i := range opinions {
		if err := emit(&opinions[i]); err != nil {
			return err
		}
	}
	return nil
}

// Classify turns any accepted input into (uriType, id).
//   - A numeric string (like "12345") is classified as "opinion".
//   - Any other string is a "query" (full-text search term).
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty courtlistener reference")
	}
	// Pure numeric → opinion ID.
	if isNumeric(input) {
		return "opinion", input, nil
	}
	return "query", input, nil
}

// Locate is the inverse of Classify: returns the live https URL for (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "opinion":
		return fmt.Sprintf("https://%s/opinion/%s/", Host, id), nil
	case "query":
		return fmt.Sprintf("https://%s/?q=%s&type=o", Host, id), nil
	default:
		return "", errs.Usage("courtlistener has no resource type %q", uriType)
	}
}

// isNumeric returns true if s is a non-empty string of ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// mapErr converts a library error into a kit error kind with the right exit code.
func mapErr(err error) error {
	return err
}
