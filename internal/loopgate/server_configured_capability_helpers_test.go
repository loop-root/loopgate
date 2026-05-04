package loopgate

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func newLocalhostTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	server := httptest.NewUnstartedServer(handler)
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen on localhost: %v", err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func localhostRedirectURL(t *testing.T, targetServerURL string, requestPath string) string {
	t.Helper()

	parsedTargetURL, err := url.Parse(targetServerURL)
	if err != nil {
		t.Fatalf("parse target server url: %v", err)
	}
	parsedTargetURL.Host = "localhost:" + parsedTargetURL.Port()
	parsedTargetURL.Path = requestPath
	parsedTargetURL.RawQuery = ""
	parsedTargetURL.Fragment = ""
	return parsedTargetURL.String()
}
