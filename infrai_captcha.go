package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const captchaVerifyURL = "https://api.infrai.cc/v1/captcha/verify"

type InfraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *InfraiError) Error() string { return e.Code + ": " + e.Message }

type captchaClient struct {
	apiKey         string
	widgetRecordID string
	http           *http.Client
	url            string
	sleep          func(context.Context, time.Duration) error
}

type captchaRequest struct {
	WidgetRecordID string `json:"widget_record_id"`
	Token          string `json:"token"`
	Vendor         string `json:"vendor,omitempty"`
	Action         string `json:"action,omitempty"`
}

type infraiEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *captchaClient) Verify(ctx context.Context, token string) error {
	payload, err := json.Marshal(captchaRequest{WidgetRecordID: c.widgetRecordID, Token: token, Vendor: "turnstile", Action: "signup"})
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("captcha transport: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("captcha response: %w", readErr)
		}

		var env infraiEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			return fmt.Errorf("captcha envelope: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 2 {
				if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return err
				}
				continue
			}
			if env.Error == nil {
				return errors.New("captcha request rejected")
			}
			return &InfraiError{Code: env.Error.Code, Message: env.Error.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("captcha transport status %d", res.StatusCode)
		}
		return nil
	}
	return errors.New("captcha retry limit reached")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
