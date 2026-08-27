package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestCaptchaRetriesRateLimitAndPreservesRequest(t *testing.T) {
	attempts := 0
	client := &captchaClient{
		apiKey: "test-key",
		url:    captchaVerifyURL,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if req.Method != http.MethodPost || req.Header.Get("Authorization") != "Bearer test-key" {
				t.Fatalf("unexpected request boundary")
			}
			status, body := http.StatusOK, `{"ok":true,"data":{"valid":true},"metadata":{}}`
			if attempts == 1 {
				status, body = http.StatusTooManyRequests, `{"ok":false,"error":{"code":"RATE_LIMITED","message":"retry later"},"metadata":{}}`
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})},
		sleep: func(context.Context, time.Duration) error { return nil },
	}
	if err := client.Verify(context.Background(), "captcha-token"); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}
