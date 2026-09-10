package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// An asset drop is one of three things in a creator-commerce catalogue, and each
// one gets a different signature. This file is the whole decision; the HTTP
// layer just applies it.
type Tier string

const (
	TierPreview  Tier = "preview"  // cover art, shown to anyone browsing the shop
	TierDelivery Tier = "delivery" // the paid file a buyer downloads
	TierInbox    Tier = "inbox"    // raw footage the creator sends to processing
)

type Plan struct {
	Tier           Tier
	Key            string
	MaxBytes       int64
	ExpiresSeconds int
	ContentType    string
	IdempotencyKey string
}

type Drop struct {
	CreatorID   string
	ProductID   string
	Filename    string
	ContentType string
	SizeBytes   int64
}

var (
	ErrUnknownType = errors.New("asset type is not accepted")
	ErrTooLarge    = errors.New("asset exceeds the tier limit")
	ErrBadName     = errors.New("filename must be a plain name")
)

const (
	mib          = int64(1) << 20
	previewLimit = 8 * mib
	deliveryCap  = 512 * mib
	inboxCap     = 4096 * mib
)

var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// tierFor maps a content type to the tier it belongs in. Images are previews,
// packaged goods are deliveries, camera masters go to the processing inbox.
func tierFor(ct string) (Tier, bool) {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "image/png", "image/jpeg", "image/webp":
		return TierPreview, true
	case "application/pdf", "application/zip", "audio/mpeg", "audio/wav":
		return TierDelivery, true
	case "video/mp4", "video/quicktime":
		return TierInbox, true
	}
	return "", false
}

func limitFor(t Tier) int64 {
	switch t {
	case TierPreview:
		return previewLimit
	case TierDelivery:
		return deliveryCap
	default:
		return inboxCap
	}
}

// A preview link is short-lived because the browser uses it immediately; an
// inbox master takes longer to push, so its window is wider.
func windowFor(t Tier) int {
	switch t {
	case TierPreview:
		return 300
	case TierDelivery:
		return 900
	default:
		return 3600
	}
}

// PlanUpload turns a drop into the exact presign arguments, or refuses it.
func PlanUpload(d Drop) (Plan, error) {
	if !safeName.MatchString(d.Filename) {
		return Plan{}, ErrBadName
	}
	tier, ok := tierFor(d.ContentType)
	if !ok {
		return Plan{}, ErrUnknownType
	}
	limit := limitFor(tier)
	if d.SizeBytes <= 0 || d.SizeBytes > limit {
		return Plan{}, ErrTooLarge
	}
	return Plan{
		Tier:           tier,
		Key:            fmt.Sprintf("%s/%s/%s/%s", d.CreatorID, string(tier), d.ProductID, d.Filename),
		MaxBytes:       limit,
		ExpiresSeconds: windowFor(tier),
		ContentType:    strings.ToLower(strings.TrimSpace(d.ContentType)),
		// The browser retries a dropped upload; the same drop must sign the
		// same slot rather than open a second one.
		IdempotencyKey: fmt.Sprintf("drop:%s:%s:%s", d.CreatorID, d.ProductID, d.Filename),
	}, nil
}

// PresignBody is the request body for storage.object.presign, built from a Plan.
func (p Plan) PresignBody() map[string]any {
	return map[string]any{
		"op":              "put",
		"expires_seconds": p.ExpiresSeconds,
		"content_type":    p.ContentType,
		"max_bytes":       p.MaxBytes,
		"idempotency_key": p.IdempotencyKey,
	}
}
