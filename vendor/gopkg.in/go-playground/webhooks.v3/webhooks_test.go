package webhooks

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"os"
	"testing"
	"time"

	"net/http/httptest"

	. "gopkg.in/go-playground/assert.v1"
)

// NOTES:
// - Run "go test" to run tests
// - Run "gocov test | gocov report" to report on test converage by file
// - Run "gocov test | gocov annotate -" to report on all code and functions, those ,marked with "MISS" were never called
//
// or
//
// -- may be a good idea to change to output path to somewherelike /tmp
// go test -coverprofile cover.out && go tool cover -html=cover.out -o cover.html
//

type FakeWebhook struct {
	provider Provider
}

func (fhook FakeWebhook) Provider() Provider {
	return fhook.provider
}

func (fhook FakeWebhook) ParsePayload(w http.ResponseWriter, r *http.Request) {
}

var fakeHook Webhook

func TestMain(m *testing.M) {

	// setup
	fakeHook = &FakeWebhook{
		provider: GitHub,
	}

	os.Exit(m.Run())

	// teardown
}

func TestHandler(t *testing.T) {

	mux := http.NewServeMux()
	mux.Handle("/webhooks", Handler(fakeHook))

	s := httptest.NewServer(Handler(fakeHook))
	defer s.Close()

	payload := "{}"

	req, err := http.NewRequest("POST", s.URL+"/webhooks", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	client := &http.Client{}
	resp, err := client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusOK)

	// Test BAD METHOD
	req, err = http.NewRequest("GET", s.URL+"/webhooks", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	resp, err = client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusMethodNotAllowed)
}

func TestRun(t *testing.T) {

	go Run(fakeHook, "127.0.0.1:3006", "/webhooks")
	time.Sleep(5000)

	payload := "{}"

	req, err := http.NewRequest("POST", "http://127.0.0.1:3006/webhooks", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	client := &http.Client{}
	resp, err := client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusOK)

	// While HTTP Server is running test some bad input

	// Test BAD URL
	req, err = http.NewRequest("POST", "http://127.0.0.1:3006", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	resp, err = client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusNotFound)

	// Test BAD METHOD
	req, err = http.NewRequest("GET", "http://127.0.0.1:3006/webhooks", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	resp, err = client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusMethodNotAllowed)
}

func TestRunServer(t *testing.T) {
	DefaultLog = NewLogger(true)
	server := &http.Server{Addr: "127.0.0.1:3007", Handler: nil}
	go RunServer(server, fakeHook, "/webhooks")
	time.Sleep(5000)

	payload := "{}"

	req, err := http.NewRequest("POST", "http://127.0.0.1:3007/webhooks", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	client := &http.Client{}
	resp, err := client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusOK)

	// While HTTP Server is running test some bad input

	// Test BAD URL
	req, err = http.NewRequest("POST", "http://127.0.0.1:3007", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	resp, err = client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusNotFound)

	// Test BAD METHOD
	req, err = http.NewRequest("GET", "http://127.0.0.1:3007/webhooks", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	resp, err = client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusMethodNotAllowed)
}

func TestRunTLSServer(t *testing.T) {

	var err error

	// can have certificates in static variables or load from disk
	cert := `-----BEGIN CERTIFICATE-----
MIIB0zCCAX2gAwIBAgIJAI/M7BYjwB+uMA0GCSqGSIb3DQEBBQUAMEUxCzAJBgNV
BAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRlcm5ldCBX
aWRnaXRzIFB0eSBMdGQwHhcNMTIwOTEyMjE1MjAyWhcNMTUwOTEyMjE1MjAyWjBF
MQswCQYDVQQGEwJBVTETMBEGA1UECAwKU29tZS1TdGF0ZTEhMB8GA1UECgwYSW50
ZXJuZXQgV2lkZ2l0cyBQdHkgTHRkMFwwDQYJKoZIhvcNAQEBBQADSwAwSAJBANLJ
hPHhITqQbPklG3ibCVxwGMRfp/v4XqhfdQHdcVfHap6NQ5Wok/4xIA+ui35/MmNa
rtNuC+BdZ1tMuVCPFZcCAwEAAaNQME4wHQYDVR0OBBYEFJvKs8RfJaXTH08W+SGv
zQyKn0H8MB8GA1UdIwQYMBaAFJvKs8RfJaXTH08W+SGvzQyKn0H8MAwGA1UdEwQF
MAMBAf8wDQYJKoZIhvcNAQEFBQADQQBJlffJHybjDGxRMqaRmDhX0+6v02TUKZsW
r5QuVbpQhH6u+0UgcW0jp9QwpxoPTLTWGXEWBBBurxFwiCBhkQ+V
-----END CERTIFICATE-----
    `
	key := `-----BEGIN RSA PRIVATE KEY-----
MIIBOwIBAAJBANLJhPHhITqQbPklG3ibCVxwGMRfp/v4XqhfdQHdcVfHap6NQ5Wo
k/4xIA+ui35/MmNartNuC+BdZ1tMuVCPFZcCAwEAAQJAEJ2N+zsR0Xn8/Q6twa4G
6OB1M1WO+k+ztnX/1SvNeWu8D6GImtupLTYgjZcHufykj09jiHmjHx8u8ZZB/o1N
MQIhAPW+eyZo7ay3lMz1V01WVjNKK9QSn1MJlb06h/LuYv9FAiEA25WPedKgVyCW
SmUwbPw8fnTcpqDWE3yTO3vKcebqMSsCIBF3UmVue8YU3jybC3NxuXq3wNm34R8T
xVLHwDXh/6NJAiEAl2oHGGLz64BuAfjKrqwz7qMYr9HCLIe/YsoWq/olzScCIQDi
D2lWusoe2/nEqfDVVWGWlyJ7yOmqaVm/iNUN9B2N2g==
-----END RSA PRIVATE KEY-----
`

	server := &http.Server{Addr: "127.0.0.1:3008", Handler: nil, TLSConfig: &tls.Config{}}
	server.TLSConfig.Certificates = make([]tls.Certificate, 1)

	server.TLSConfig.Certificates[0], err = tls.X509KeyPair([]byte(cert), []byte(key))
	Equal(t, err, nil)

	go RunTLSServer(server, fakeHook, "/webhooks")
	time.Sleep(5000)

	payload := "{}"

	req, err := http.NewRequest("POST", "https://127.0.0.1:3008/webhooks", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{Transport: tr}
	resp, err := client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusOK)

	// While HTTP Server is running test some bad input

	// Test BAD URL
	req, err = http.NewRequest("POST", "https://127.0.0.1:3008", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	resp, err = client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusNotFound)

	// Test BAD METHOD
	req, err = http.NewRequest("GET", "https://127.0.0.1:3008/webhooks", bytes.NewBuffer([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	Equal(t, err, nil)

	resp, err = client.Do(req)
	Equal(t, err, nil)

	defer resp.Body.Close()

	Equal(t, resp.StatusCode, http.StatusMethodNotAllowed)
}

func TestProviderString(t *testing.T) {

	Equal(t, GitHub.String(), "GitHub")
	Equal(t, Bitbucket.String(), "Bitbucket")
	Equal(t, GitLab.String(), "GitLab")
	Equal(t, Provider(999999).String(), "Unknown")
}
