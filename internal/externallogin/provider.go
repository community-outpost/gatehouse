package externallogin

import (
	"context"
	"net/url"
)

// Identity is the stable provider-authenticated subject.
type Identity struct {
	Subject string
}

// Provider starts a browser authentication flow and verifies its return.
type Provider interface {
	AuthorizationURL(state string) (string, error)
	Authenticate(context.Context, url.Values) (Identity, error)
}
