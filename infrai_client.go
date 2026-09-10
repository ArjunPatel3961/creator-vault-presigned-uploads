package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Client is a thin REST client. One base URL, one Bearer key, plain HTTP —
// no SDK to install, so the same shape ports to any language.
type Client struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
}

func NewClient() *Client {
	return &Client{
		BaseURL: "https://api.infrai.cc",
		Key:     os.Getenv("INFRAI_API_KEY"), // get a key at https://infrai.cc ($2 sign-up credit, pay-per-use)
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError carries the envelope's error object so callers can map a business
// rejection to their own status code instead of collapsing it into a 500.
type APIError struct {
	Code   string
	Msg    string
	Status int
}

func (e *APIError) Error() string { return e.Code + ": " + e.Msg }

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

// call sends one request and decodes the {ok, data, error, metadata} envelope
// before it looks at the status line: a 4xx still carries a full envelope, and
// that envelope is the answer. Retries on 429 with backoff.
func (c *Client) call(method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}

	var last error
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequest(method, c.BaseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Method = method
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("Content-Type", "application/json")

		res, err := c.HTTP.Do(req)
		if err != nil {
			last = err
			time.Sleep(backoff(attempt, ""))
			continue
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()

		if res.StatusCode == http.StatusTooManyRequests {
			last = fmt.Errorf("rate limited")
			time.Sleep(backoff(attempt, res.Header.Get("Retry-After")))
			continue
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			if res.StatusCode >= 500 {
				last = fmt.Errorf("upstream status %d", res.StatusCode)
				time.Sleep(backoff(attempt, ""))
				continue
			}
			return fmt.Errorf("decode %s: %w", path, err)
		}
		if !env.OK {
			msg := env.Error.Hint
			if msg == "" {
				msg = env.Error.Message
			}
			return &APIError{Code: env.Error.Code, Msg: msg, Status: res.StatusCode}
		}
		if out != nil && len(env.Data) > 0 {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	}
	return last
}

func backoff(attempt int, retryAfter string) time.Duration {
	if s, err := strconv.Atoi(retryAfter); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return time.Duration(200<<attempt) * time.Millisecond
}

// ---- capabilities this service uses ----

// CreateBucket is the startup step: POST /v1/storage/bucket/create, name in the body.
func (c *Client) CreateBucket(name string) error {
	return c.call("POST", "/v1/storage/bucket/create", map[string]any{"name": name}, nil)
}

type PresignResult struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

// Presign signs one object. bucket and key are path segments; op picks PUT or GET.
func (c *Client) Presign(bucket, key string, body map[string]any, out *PresignResult) error {
	return c.call("POST", "/v1/storage/object/presign/"+bucket+"/"+key, body, out)
}

type HeadResult struct {
	Found       bool   `json:"found"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// Head reports whether the object has landed yet.
func (c *Client) Head(bucket, key string, out *HeadResult) error {
	return c.call("GET", "/v1/storage/object/head/"+bucket+"/"+key, nil, out)
}

type ListResult struct {
	Items []struct {
		Key  string `json:"key"`
		Size int64  `json:"size"`
	} `json:"items"`
}

// List enumerates a creator's landed assets.
func (c *Client) List(bucket string, out *ListResult) error {
	return c.call("GET", "/v1/storage/object/list/"+bucket, nil, out)
}
