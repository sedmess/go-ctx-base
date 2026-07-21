package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/spaolacci/murmur3"
)

type authenticationResponseWriter struct {
	*httptest.ResponseRecorder
}

func (writer *authenticationResponseWriter) EncodeJson(value interface{}) ([]byte, error) {
	return json.Marshal(value)
}

func (writer *authenticationResponseWriter) WriteJson(value interface{}) error {
	payload, err := writer.EncodeJson(value)
	if err != nil {
		return err
	}
	writer.Header().Set("Content-Type", "application/json")
	_, err = writer.Write(payload)
	return err
}

func invokeAuthenticationMiddleware(t *testing.T, middleware Middleware, configure func(*http.Request)) (int, int64, bool, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/secured", nil)
	if configure != nil {
		configure(request)
	}
	restRequest := &rest.Request{Request: request}
	recorder := httptest.NewRecorder()
	writer := &authenticationResponseWriter{ResponseRecorder: recorder}
	handlerCalled := false
	err := middleware(func(response rest.ResponseWriter, request *rest.Request) {
		handlerCalled = true
		response.WriteHeader(http.StatusOK)
	}, writer, restRequest)
	credential := (*RequestData)(restRequest).Credential()
	operationalOutput := fmt.Sprint(recorder.Header(), recorder.Body.String(), err)
	return recorder.Code, credential, handlerCalled, operationalOutput
}

func authenticatedRequest(t *testing.T, name string, middleware Middleware, configure func(*http.Request)) (int, int64) {
	t.Helper()
	server := newLifecycleTestServer(t, name)
	if err := server.Init(); err != nil {
		t.Fatal(err)
	}
	defer server.Dispose()

	credential := make(chan int64, 1)
	RegisterRoute(server, http.MethodGet, "/secured").
		Middleware(middleware).
		Handler(func(request *RequestData) (response Response) {
			credential <- request.Credential()
			response.Ok()
			return
		})
	server.AfterStart()

	request, err := http.NewRequest(http.MethodGet, testServerURL(t, server, "/secured"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(request)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return response.StatusCode, 0
	}
	return response.StatusCode, <-credential
}

func TestBearerAuthenticationContract(t *testing.T) {
	var callbacks atomic.Int32
	middleware := BearerTokenAuthenticator(func(_ string, token string) AuthenticationResultCode {
		callbacks.Add(1)
		if token == "shared-identity" {
			return Authorized
		}
		return Forbidden
	})

	status, credential := authenticatedRequest(t, "bearer-success", middleware, func(request *http.Request) {
		request.Header.Set("Authorization", "Bearer shared-identity")
	})
	if status != http.StatusOK {
		t.Fatalf("success status = %d", status)
	}
	expected := int64(murmur3.Sum64([]byte("shared-identity")))
	if credential != expected {
		t.Fatalf("credential = %d, want %d", credential, expected)
	}

	status, _ = authenticatedRequest(t, "bearer-forbidden", middleware, func(request *http.Request) {
		request.Header.Set("Authorization", "Bearer rejected")
	})
	if status != http.StatusForbidden {
		t.Fatalf("forbidden status = %d", status)
	}

	callbacksBeforeMalformed := callbacks.Load()
	for index, header := range []string{"", "Basic abc", "Bearer", "Bearer one two"} {
		status, _ = authenticatedRequest(t, "bearer-malformed-"+string(rune('a'+index)), middleware, func(request *http.Request) {
			request.Header.Set("Authorization", header)
		})
		if status != http.StatusUnauthorized {
			t.Fatalf("header %q status = %d", header, status)
		}
	}
	if callbacks.Load() != callbacksBeforeMalformed {
		t.Fatal("malformed Bearer input reached the authorization callback")
	}
}

func TestBasicAuthenticationUsesNumericCredential(t *testing.T) {
	middleware := BasicAuthenticator(func(_ string, username string, password string) AuthenticationResultCode {
		if username == "shared-identity" && password == "synthetic-password" {
			return Authorized
		}
		return Forbidden
	})
	status, credential := authenticatedRequest(t, "basic-success", middleware, func(request *http.Request) {
		request.SetBasicAuth("shared-identity", "synthetic-password")
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	expected := int64(murmur3.Sum64([]byte("shared-identity")))
	if credential != expected {
		t.Fatalf("credential = %d, want %d", credential, expected)
	}
	if credential == int64(murmur3.Sum64([]byte("synthetic-password"))) {
		t.Fatal("password was used as credential identity")
	}
}

func TestCredentialReturnsZeroForAbsentOrInvalidState(t *testing.T) {
	request := &RequestData{}
	if request.Credential() != 0 {
		t.Fatal("absent credential was non-zero")
	}
	request.Env = map[string]interface{}{credentialEnvKey: "wrong-type"}
	if request.Credential() != 0 {
		t.Fatal("wrong-type credential was non-zero")
	}
}

func TestAuthenticationMethodsShareFailureAndSecretSafeContract(t *testing.T) {
	const (
		identity       = "shared-identity"
		bearerSecret   = "synthetic-bearer-token"
		basicPassword  = "synthetic-basic-password"
		requiredBearer = "synthetic-renew-token"
	)
	bearer := BearerTokenAuthenticator(func(_ string, token string) AuthenticationResultCode {
		switch token {
		case identity:
			return Authorized
		case requiredBearer:
			return AuthenticationRequired
		default:
			return Forbidden
		}
	})
	basic := BasicAuthenticator(func(_ string, username string, _ string) AuthenticationResultCode {
		switch username {
		case identity:
			return Authorized
		case "renew-identity":
			return AuthenticationRequired
		default:
			return Forbidden
		}
	})
	expectedCredential := int64(murmur3.Sum64([]byte(identity)))

	testCases := []struct {
		name           string
		middleware     Middleware
		configure      func(*http.Request)
		wantStatus     int
		wantCredential int64
		wantHandler    bool
		secrets        []string
	}{
		{name: "bearer missing", middleware: bearer, wantStatus: http.StatusUnauthorized},
		{
			name:       "bearer authentication required",
			middleware: bearer,
			configure: func(request *http.Request) {
				request.Header.Set("Authorization", "Bearer "+requiredBearer)
			},
			wantStatus: http.StatusUnauthorized,
			secrets:    []string{requiredBearer},
		},
		{
			name:       "bearer forbidden",
			middleware: bearer,
			configure: func(request *http.Request) {
				request.Header.Set("Authorization", "Bearer "+bearerSecret)
			},
			wantStatus: http.StatusForbidden,
			secrets:    []string{bearerSecret},
		},
		{
			name:       "bearer success",
			middleware: bearer,
			configure: func(request *http.Request) {
				request.Header.Set("Authorization", "Bearer "+identity)
			},
			wantStatus:     http.StatusOK,
			wantCredential: expectedCredential,
			wantHandler:    true,
			secrets:        []string{identity},
		},
		{name: "basic missing", middleware: basic, wantStatus: http.StatusUnauthorized},
		{
			name:       "basic authentication required",
			middleware: basic,
			configure: func(request *http.Request) {
				request.SetBasicAuth("renew-identity", basicPassword)
			},
			wantStatus: http.StatusUnauthorized,
			secrets:    []string{basicPassword},
		},
		{
			name:       "basic forbidden",
			middleware: basic,
			configure: func(request *http.Request) {
				request.SetBasicAuth("denied-identity", basicPassword)
			},
			wantStatus: http.StatusForbidden,
			secrets:    []string{basicPassword},
		},
		{
			name:       "basic success",
			middleware: basic,
			configure: func(request *http.Request) {
				request.SetBasicAuth(identity, basicPassword)
			},
			wantStatus:     http.StatusOK,
			wantCredential: expectedCredential,
			wantHandler:    true,
			secrets:        []string{basicPassword},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			status, credential, handlerCalled, output := invokeAuthenticationMiddleware(t, testCase.middleware, testCase.configure)
			if status != testCase.wantStatus || credential != testCase.wantCredential || handlerCalled != testCase.wantHandler {
				t.Fatalf("status/credential/handler = %d/%d/%t, want %d/%d/%t", status, credential, handlerCalled, testCase.wantStatus, testCase.wantCredential, testCase.wantHandler)
			}
			for _, secret := range testCase.secrets {
				if strings.Contains(output, secret) {
					t.Fatalf("operational output exposed %q: %q", secret, output)
				}
			}
		})
	}
}

func TestCredentialHashIsDeterministicAcrossProcesses(t *testing.T) {
	const helperKey = "GO_CTX_BASE_AUTH_HASH_HELPER"
	if identity := os.Getenv(helperKey); identity != "" {
		fmt.Print(int64(murmur3.Sum64([]byte(identity))))
		os.Exit(0)
	}
	const identity = "cross-process-identity"
	outputs := make([]string, 2)
	for index := range outputs {
		command := exec.Command(os.Args[0], "-test.run=^TestCredentialHashIsDeterministicAcrossProcesses$")
		command.Env = append(os.Environ(), helperKey+"="+identity)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("credential helper: %v: %s", err, output)
		}
		outputs[index] = strings.TrimSpace(string(output))
	}
	expected := strconv.FormatInt(int64(murmur3.Sum64([]byte(identity))), 10)
	if outputs[0] != expected || outputs[1] != expected {
		t.Fatalf("cross-process credentials = %q/%q, want %q", outputs[0], outputs[1], expected)
	}
}
