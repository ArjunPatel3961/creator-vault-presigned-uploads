package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

type Vault struct {
	API    *Client
	Bucket string
}

type dropRequest struct {
	CreatorID   string `json:"creator_id"`
	ProductID   string `json:"product_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail turns an API error into the caller's status code. A rejected request is
// a 4xx for our caller too; only transport trouble becomes a 502.
func fail(w http.ResponseWriter, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		writeJSON(w, apiErr.Status, map[string]string{"error": apiErr.Code, "detail": apiErr.Msg})
		return
	}
	log.Printf("storage call: %v", err)
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "storage_unavailable"})
}

// POST /assets/upload-url — hands the browser a signed PUT. Bytes go
// browser -> storage; this binary never touches the file.
func (v *Vault) uploadURL(w http.ResponseWriter, r *http.Request) {
	var req dropRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed_json"})
		return
	}
	plan, err := PlanUpload(Drop(req))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	var signed PresignResult
	if err := v.API.Presign(v.Bucket, plan.Key, plan.PresignBody(), &signed); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tier":            string(plan.Tier),
		"key":             plan.Key,
		"upload_url":      signed.URL,
		"method":          "PUT",
		"expires_seconds": plan.ExpiresSeconds,
		"max_bytes":       plan.MaxBytes,
	})
}

// GET /assets/status?key=... — has the object landed, and what happens next.
func (v *Vault) status(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Query().Get("key"), "/")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}
	var head HeadResult
	if err := v.API.Head(v.Bucket, key, &head); err != nil {
		fail(w, err)
		return
	}
	if !head.Found {
		writeJSON(w, http.StatusOK, map[string]any{"key": key, "state": "awaiting_upload"})
		return
	}
	state := "published"
	if strings.Contains(key, "/"+string(TierInbox)+"/") {
		// A landed master is the trigger for the transcode step downstream.
		state = "queued_for_processing"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key":   key,
		"state": state,
		"size":  head.Size,
	})
}

// GET /assets/download?key=...&as=filename — a signed GET for one buyer.
func (v *Vault) download(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Query().Get("key"), "/")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}
	name := r.URL.Query().Get("as")
	if name == "" {
		name = key[strings.LastIndex(key, "/")+1:]
	}
	body := map[string]any{
		"op":                   "get",
		"expires_seconds":      600,
		"response_disposition": `attachment; filename="` + name + `"`,
	}
	var signed PresignResult
	if err := v.API.Presign(v.Bucket, key, body, &signed); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"download_url": signed.URL, "expires_seconds": 600})
}

// GET /assets/catalogue — what has landed, for the subscriber update mailer.
func (v *Vault) catalogue(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("creator_id")
	var listed ListResult
	if err := v.API.List(v.Bucket, &listed); err != nil {
		fail(w, err)
		return
	}
	out := make([]map[string]any, 0, len(listed.Items))
	for _, it := range listed.Items {
		if prefix != "" && !strings.HasPrefix(it.Key, prefix+"/") {
			continue
		}
		if !strings.Contains(it.Key, "/"+string(TierDelivery)+"/") {
			continue
		}
		out = append(out, map[string]any{"key": it.Key, "size": it.Size})
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliverables": out})
}
