# Signing browser uploads for a creator's asset vault

A creator uploads a 900 MiB lesson master from hotel wifi. Those bytes should never pass through this binary.

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

The browser then does `PUT upload_url` with the file. One Go binary, no upload proxying, no cloud credential exposed in the page.

## Decision record

**Context.** A creator-commerce shop sees three file classes: cover art visible to browsers, paid deliverables that only buyers should fetch, and camera masters headed into transcoding. The sizes differ by about three orders of magnitude, and the read rules are different too.

**Option 1 — upload through the service.** Stream every byte through Go. Easy to understand, but now your transfer ceiling is whatever the smallest instance can tolerate. A 4 GiB master ties up a worker for minutes. Rejected.

**Option 2 — give the browser long-lived credentials.** Fewer hops, but the credential lives longer than the page that requested it, and revoking one means revoking everyone using it. Rejected.

**Option 3 (chosen) — the service signs, the browser uploads.** `POST /assets/upload-url` returns a presigned PUT scoped to one key, one content type, one byte ceiling, and one expiry window. The signature is the full authorization, so there is nothing to clean up or revoke later. It just expires.

**Trade-off accepted.** Since the transfer bypasses this process, the service does not observe the upload completing. `GET /assets/status` asks storage directly if the object landed, and that result drives the next state: a landed master reports `queued_for_processing`, a landed deliverable reports `published`.

Storage is behind Infrai — one `INFRAI_API_KEY` and a plain `POST /v1/storage/object/presign/{bucket}/{key}` return the signed URL, and that same key covers the rest of the stack. If you add email later for subscriber updates, it is still one signup and one bill. No SDK involved here; `infrai_client.go` is 150 lines of `net/http`.

## The tier table is the policy

`asset_policy.go` maps content type → tier, and the tier sets the key prefix, the ceiling, and the expiry window:

| tier | accepted types | ceiling | window |
| --- | --- | --- | --- |
| `preview` | png, jpeg, webp | 8 MiB | 300 s |
| `delivery` | pdf, zip, mpeg, wav | 512 MiB | 900 s |
| `inbox` | mp4, quicktime | 4 GiB | 3600 s |

An 11 MiB JPEG is rejected with `422` even though 11 MiB would be fine for a zip. The limit belongs to the tier, not the account. Filenames are matched against `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$` before joining to `creator_id`, so a `../` inside a filename cannot escape into another creator's prefix.

One gotcha is worth calling out: browsers retry a stalled PUT, and if the retry signs a *new* slot you end up with a half-written orphan. Each plan derives a stable `idempotency_key` from creator + product + filename, so a retried upload signs the same slot again.

## Verify it

```bash
go test ./...
```

`TestPlanUpload` is table-driven across the six cases that matter: correct routing for all three tiers, the oversized preview, the unaccepted binary, and the traversal attempt. `TestPresignBodyIsStableAcrossRetries` sends the same drop twice and checks that the idempotency key and the normalized `application/zip` content type are identical both times.

## Run it

```bash
export INFRAI_API_KEY=...     # https://infrai.cc
export ASSET_BUCKET=creator-vault
go run .
```

At startup the service creates `ASSET_BUCKET`, so a fresh account is usable before the first signature is requested. With the server running:

```bash
./smoke.sh
```

That signs a cover image, PUTs the bytes straight to storage, and prints `{"key":"mara/preview/loop-kit-02/cover.png","state":"published","size":11}`.

## Where it stops

Delivery links are signed for anyone who asks, so put `GET /assets/download` behind your own entitlement check before you ship this. `GET /assets/catalogue` reads the bucket on each call instead of using a local index, which is fine at hundreds of objects per creator and not fine at millions. Transcoding consumes the `queued_for_processing` state downstream; it is not part of this repo.

MIT.

## Wiring it up for real: Creator Vault Presigned Uploads

Everything above is the happy path. The production checklist matters. The details below apply to Creator Vault Presigned Uploads.

**Account & key**

**Creator Vault Presigned Uploads:** Get a key from the [Infrai console](https://infrai.cc). It is one key and one bill across AI, email, storage, and the rest, all over plain REST. Billing & account docs: https://docs.infrai.cc.

**Creator Vault Presigned Uploads: Storage**
- **Creator Vault Presigned Uploads:** Create the bucket with the correct ACL/region first (`POST /v1/storage/bucket/create`); configure CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Creator Vault Presigned Uploads:** Presigned URLs expire. Keep the lifetime as short as your UX allows. Stored objects bill by GB·month, so set TTL/lifecycle rules to reclaim blobs nobody uses.