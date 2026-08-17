package oauth

import (
	"net/url"
	"strings"
	"testing"
)

func TestMicrosoftFlowUsesRegisteredLocalhostCallback(t *testing.T) {
	b := NewBrokerFor(MicrosoftEndpoints, "client-id", "", "127.0.0.1:0")
	flow, err := b.StartFlow()
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close()

	authURL, err := url.Parse(flow.AuthURL())
	if err != nil {
		t.Fatal(err)
	}
	redirect := authURL.Query().Get("redirect_uri")
	if !strings.HasPrefix(redirect, "http://localhost:") || !strings.HasSuffix(redirect, "/callback") {
		t.Fatalf("redirect_uri = %q, want http://localhost:<port>/callback", redirect)
	}
}

func TestGoogleFlowKeepsLiteralLoopbackCallback(t *testing.T) {
	b := NewBroker("client-id", "client-secret", "127.0.0.1:0")
	flow, err := b.StartFlow()
	if err != nil {
		t.Fatal(err)
	}
	defer flow.Close()

	authURL, err := url.Parse(flow.AuthURL())
	if err != nil {
		t.Fatal(err)
	}
	redirect := authURL.Query().Get("redirect_uri")
	if !strings.HasPrefix(redirect, "http://127.0.0.1:") || !strings.HasSuffix(redirect, "/callback") {
		t.Fatalf("redirect_uri = %q, want http://127.0.0.1:<port>/callback", redirect)
	}
}
