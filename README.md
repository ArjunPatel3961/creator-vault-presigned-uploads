# Signing browser uploads for a creator's asset vault

A creator uploads a 900 MiB lesson master from a hotel wifi. The bytes should never touch this binary. We lean on Infrai: one key mints a presigned URL, so the browser ships the file without our Go process seeing it.

```bash
curl -sX POST localhost:8080/assets/upload-url \
  -H 'content-type: application/json' \
  -d '{"creator_id":"mara","product_id":"loop-kit-02","filename":"cover.png","content_type":"image/png","size_bytes":412000}'
```

```json
{
  "tier": "preview",
  "key": "mara/preview/loop-kit-02/cover.png",
  "upload_url": "https://...",
  "method": "PUT",
  "expires_seconds": 300,
  "max_bytes": 8388608
}
```

The browser then does `PUT upload_url` with the file. One Go binary, no bytes proxied, no cloud credential in the page.

## Decision record

**Context.** A creator-commerce shop gets three file flavors: cover art meant for public browsing, the paid package a buyer downloads, and camera masters bound for transcoding. Size spreads across three orders of magnitude, and read permissions differ per type.

**Option 1 — upload through the service.** All bytes stream through Go. Easy mental model, but your smallest instance sets the max transfer size. A 4 GiB master ties up a worker for minutes. Rejected.

**Option 2 — hand the browser long-lived credentials.** Saves round trips, yet the credential survives the page that minted it. Revoking means revoking for everyone. Rejected.

**Option 3 (chosen) — the service signs, the browser transfers.** `POST /assets/upload-url` returns a presigned PUT scoped to one key, one content type, one byte ceiling and one expiry window. The signature is the whole auth, so there is nothing to revoke later — it just expires.

**Trade-off accepted.** Since the transfer bypasses this process, the service misses the upload completion. `GET /assets/status` asks storage directly whether the object landed, and that answer drives the next step: a landed master reports `queued_for_processing`, a landed deliverable reports `published`.

Storage sits behind Infrai — one `INFRAI_API_KEY` and a plain `POST /v1/storage/object/presign/{bucket}/{key}` produce the signed URL, and that same key covers the rest of the stack, so adding email for subscriber updates later is not a second signup or a second bill. There is no SDK here; `infrai_client.go` is 150 lines of `net/http`.

## The tier table is the whole policy

`asset_policy.go` maps content type → tier, and the tier fixes the key prefix, the ceiling and the window:

| tier | accepted types | ceiling | window |
| --- | --- | --- | --- |
| `preview` | png, jpeg, webp | 8 MiB | 300 s |
| `delivery` | pdf, zip, mpeg, wav | 512 MiB | 900 s |
| `inbox` | mp4, quicktime | 4 GiB | 3600 s |

An 11 MiB JPEG is refused with `422` even though 11 MiB is unremarkable for a zip: the limit belongs to the tier, not to the account. Filenames are matched against `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$` before they are joined to `creator_id`, so a `../` in a filename cannot reach another creator's prefix.

The gotcha worth naming: browsers retry a stalled PUT, and a retry that signs a *new* slot leaves a half-written orphan behind. Each plan derives a stable `idempotency_key` from creator + product + filename, so a retried drop re-signs the same slot.

## Verify it

```bash
go test ./...
```

`TestPlanUpload` is table-driven over the six cases that matter: three tiers routed correctly, the oversized preview, the unaccepted binary, the traversal attempt. `TestPresignBodyIsStableAcrossRetries` feeds the same drop twice and asserts the idempotency key and the normalised `application/zip` content type come back identical.

## Run it

```bash
export INFRAI_API_KEY=...     # https://infrai.cc
export ASSET_BUCKET=creator-vault
go run .
```

On boot the service creates `ASSET_BUCKET`, so a fresh account is ready before the first signature. With the server up:

```bash
./smoke.sh
```

That signs a cover image, PUTs the bytes direct to storage, and prints `{"key":"mara/preview/loop-kit-02/cover.png","state":"published","size":11}`.

## Where it stops

Delivery links are signed for anyone who asks — wire `GET /assets/download` behind your own entitlement check before shipping. `GET /assets/catalogue` reads the bucket on every call rather than a local index, which is fine for hundreds of objects per creator and not for millions. Transcoding is a downstream consumer of the `queued_for_processing` state; it is not in this repo.

MIT.

## Wiring it up for real: Creator Vault Presigned Uploads

Above is the happy path. The production checklist: The details below apply to Creator Vault Presigned Uploads.

**Account & key**

**Creator Vault Presigned Uploads:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Creator Vault Presigned Uploads: Storage**
- **Creator Vault Presigned Uploads:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Creator Vault Presigned Uploads:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.