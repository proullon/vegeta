package vegeta

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAttackRate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)
	tr := NewStaticTargeter(&Target{Method: "GET", URL: server.URL})
	rate := uint64(100)
	atk := NewAttacker(DefaultRedirects, DefaultTimeout, DefaultLocalAddr, DefaultTLSConfig)
	var hits uint64
	for _ = range atk.Attack(tr, rate, 1*time.Second) {
		hits++
	}
	if hits != rate {
		t.Fatalf("Wrong number of hits: want %d, got %d\n", rate, hits)
	}
}

func TestDefaultAttackerCertConfig(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)
	atk := NewAttacker(DefaultRedirects, DefaultTimeout, DefaultLocalAddr, DefaultTLSConfig)
	request, _ := http.NewRequest("GET", server.URL, nil)
	_, err := atk.client.Do(request)
	if err != nil && strings.Contains(err.Error(), "x509: certificate signed by unknown authority") {
		t.Errorf("Invalid certificates should be ignored: Got `%s`", err)
	}
}

func TestRedirects(t *testing.T) {
	t.Parallel()

	var servers [2]*httptest.Server
	var hits uint64

	for i := range servers {
		servers[i] = httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddUint64(&hits, 1)
				http.Redirect(w, r, servers[(i+1)%2].URL, 302)
			}),
		)
	}

	atk := NewAttacker(2, DefaultTimeout, DefaultLocalAddr, DefaultTLSConfig)
	tr := NewStaticTargeter(&Target{Method: "GET", URL: servers[0].URL})
	var rate uint64 = 10
	results := atk.Attack(tr, rate, 1*time.Second)

	want := fmt.Sprintf("stopped after %d redirects", 2)
	for result := range results {
		if !strings.Contains(result.Error, want) {
			t.Fatalf("Expected error to be: %s, Got: %s", want, result.Error)
		}
	}

	if want, got := rate*(2+1), hits; want != got {
		t.Fatalf("Expected hits to be: %d, Got: %d", want, got)
	}
}

func TestTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-time.After(20 * time.Millisecond)
		}),
	)

	atk := NewAttacker(DefaultRedirects, 10*time.Millisecond, DefaultLocalAddr, DefaultTLSConfig)
	tr := NewStaticTargeter(&Target{Method: "GET", URL: server.URL})
	results := atk.Attack(tr, 1, 1*time.Second)

	want := "net/http: timeout awaiting response headers"
	for result := range results {
		if !strings.Contains(result.Error, want) {
			t.Fatalf("Expected error to be: %s, Got: %s", want, result.Error)
		}
	}
}

func TestLocalAddr(t *testing.T) {
	t.Parallel()

	addr, err := net.ResolveIPAddr("ip", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				t.Fatal(err)
			}

			if host != addr.String() {
				t.Fatalf("Wrong source address. Want %s, Got %s", addr, host)
			}
		}),
	)

	atk := NewAttacker(DefaultRedirects, DefaultTimeout, *addr, DefaultTLSConfig)
	tr := NewStaticTargeter(&Target{Method: "GET", URL: server.URL})

	for result := range atk.Attack(tr, 1, 1*time.Second) {
		if result.Error != "" {
			t.Fatal(result.Error)
		}
	}
}
