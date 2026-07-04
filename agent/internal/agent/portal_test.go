package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestPortal() *portal {
	return newPortal("/nonexistent/agent.json", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPortalForm(t *testing.T) {
	srv := httptest.NewServer(newTestPortal().handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{"Logos setup", `action="/enroll"`, "claim code"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("portal page missing %q", want)
		}
	}
}

func TestPortalCaptiveRedirect(t *testing.T) {
	srv := httptest.NewServer(newTestPortal().handler())
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/generate_204")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Errorf("probe not redirected to the portal: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestPortalEnroll(t *testing.T) {
	p := newTestPortal()
	var gotServer, gotCode string
	p.enroll = func(_ context.Context, _, server, code string) error {
		gotServer, gotCode = server, code
		if code == "LG-BAD" {
			return fmt.Errorf("enrollment rejected: invalid or expired claim code")
		}
		return nil
	}
	srv := httptest.NewServer(p.handler())
	defer srv.Close()

	// Failed attempt: error is shown, portal stays open.
	resp, err := http.PostForm(srv.URL+"/enroll", url.Values{"server": {"http://cp:8080"}, "code": {"LG-BAD"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "invalid or expired claim code") {
		t.Errorf("error not rendered:\n%s", body)
	}
	select {
	case <-p.done:
		t.Fatal("done closed after a failed enrollment")
	default:
	}

	// Successful attempt: success page + done closed.
	resp, err = http.PostForm(srv.URL+"/enroll", url.Values{"server": {"http://cp:8080"}, "code": {"LG-GOOD"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Enrolled") {
		t.Errorf("success page missing confirmation:\n%s", body)
	}
	if gotServer != "http://cp:8080" || gotCode != "LG-GOOD" {
		t.Errorf("enroll called with %q %q", gotServer, gotCode)
	}
	select {
	case <-p.done:
	default:
		t.Error("done not closed after successful enrollment")
	}
}
