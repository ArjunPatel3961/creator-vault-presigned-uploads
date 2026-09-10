package main

import (
	"errors"
	"testing"
)

func TestPlanUpload(t *testing.T) {
	cases := []struct {
		name     string
		drop     Drop
		wantErr  error
		wantTier Tier
		wantKey  string
		wantExp  int
	}{
		{
			name:     "cover art is a preview with a short window",
			drop:     Drop{CreatorID: "mara", ProductID: "loop-kit-02", Filename: "cover.png", ContentType: "image/png", SizeBytes: 400 << 10},
			wantTier: TierPreview,
			wantKey:  "mara/preview/loop-kit-02/cover.png",
			wantExp:  300,
		},
		{
			name:     "the paid zip is a delivery",
			drop:     Drop{CreatorID: "mara", ProductID: "loop-kit-02", Filename: "kit.zip", ContentType: "application/zip", SizeBytes: 120 * mib},
			wantTier: TierDelivery,
			wantKey:  "mara/delivery/loop-kit-02/kit.zip",
			wantExp:  900,
		},
		{
			name:     "camera master lands in the processing inbox",
			drop:     Drop{CreatorID: "ivo", ProductID: "course-1", Filename: "lesson01.mp4", ContentType: "video/mp4", SizeBytes: 900 * mib},
			wantTier: TierInbox,
			wantKey:  "ivo/inbox/course-1/lesson01.mp4",
			wantExp:  3600,
		},
		{
			name:    "an 11 MiB png is over the preview limit even though 11 MiB is fine for a delivery",
			drop:    Drop{CreatorID: "mara", ProductID: "loop-kit-02", Filename: "cover.png", ContentType: "image/jpeg", SizeBytes: 11 * mib},
			wantErr: ErrTooLarge,
		},
		{
			name:    "executables are not part of the catalogue",
			drop:    Drop{CreatorID: "mara", ProductID: "loop-kit-02", Filename: "installer.bin", ContentType: "application/x-msdownload", SizeBytes: 1 << 20},
			wantErr: ErrUnknownType,
		},
		{
			name:    "a path in the filename cannot escape the creator prefix",
			drop:    Drop{CreatorID: "mara", ProductID: "loop-kit-02", Filename: "../ivo/cover.png", ContentType: "image/png", SizeBytes: 1 << 10},
			wantErr: ErrBadName,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanUpload(tc.drop)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if plan.Tier != tc.wantTier {
				t.Errorf("tier = %q, want %q", plan.Tier, tc.wantTier)
			}
			if plan.Key != tc.wantKey {
				t.Errorf("key = %q, want %q", plan.Key, tc.wantKey)
			}
			if plan.ExpiresSeconds != tc.wantExp {
				t.Errorf("expires_seconds = %d, want %d", plan.ExpiresSeconds, tc.wantExp)
			}
		})
	}
}

func TestPresignBodyIsStableAcrossRetries(t *testing.T) {
	d := Drop{CreatorID: "mara", ProductID: "loop-kit-02", Filename: "kit.zip", ContentType: "Application/Zip", SizeBytes: 4 * mib}
	first, err := PlanUpload(d)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := PlanUpload(d)
	if first.IdempotencyKey != second.IdempotencyKey {
		t.Fatalf("idempotency_key drifted: %q vs %q", first.IdempotencyKey, second.IdempotencyKey)
	}
	body := first.PresignBody()
	if body["op"] != "put" {
		t.Errorf(`op = %v, want "put"`, body["op"])
	}
	if body["content_type"] != "application/zip" {
		t.Errorf("content_type = %v, want normalised application/zip", body["content_type"])
	}
	if body["max_bytes"].(int64) != deliveryCap {
		t.Errorf("max_bytes = %v, want %d", body["max_bytes"], deliveryCap)
	}
}
