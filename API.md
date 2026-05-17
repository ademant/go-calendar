# dansal API

RESTful calendar API backed by SQLite. All timestamps are RFC3339. Events use epoch integers internally; responses return local time (Europe/Berlin).

## Base URL
```
http://localhost:8000
```

## Authentication

Protected endpoints require a Bearer token obtained from `/api/v1/login`, or an API key created via `/api/v1/apikeys`.

```
Authorization: Bearer <token-or-api-key>
```

API keys begin with `ak_` and never expire.

## Roles

| Role | Permissions |
|------|-------------|
| `admin` | Full access; bypasses all organization checks |
| `user` | Read + write; must be org member for org-linked resources |
| `publisher` | Read + create; no update or delete |
| `viewer` | Read published events only |

---

## Info

### GET /api/v1/info
Returns server version and build time. Public.

```json
{ "version": "1.2.3", "build_time": "2026-05-15T10:00:00Z" }
```

---

## Authentication

### POST /api/v1/login

```json
{ "username": "admin", "password": "secret" }
```

Response `200`:
```json
{
  "token": "string",
  "expires_at": "2026-06-15T10:00:00Z",
  "user": { "id": 1, "username": "admin", "email": "admin@localhost", "role": "admin", "created_at": "..." }
}
```

---

## Users

All user endpoints require admin role.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/users` | List all users |
| POST | `/api/v1/users` | Create user |
| GET | `/api/v1/users/{id}` | Get user |
| PUT | `/api/v1/users/{id}` | Update user |
| DELETE | `/api/v1/users/{id}` | Delete user |

**Create/Update body:**
```json
{ "username": "string", "email": "string", "password": "string", "role": "user" }
```

Valid roles: `admin`, `user`, `publisher`, `viewer`.

---

## API Keys

Requires authentication. Users manage their own keys; admins can manage any.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/apikeys` | List own API keys |
| POST | `/api/v1/apikeys` | Create API key |
| DELETE | `/api/v1/apikeys/{id}` | Delete API key |

**Create body:**
```json
{ "name": "my-script" }
```

**Create response `201`** — the `key` field is only returned once:
```json
{ "id": 1, "name": "my-script", "key": "ak_...", "created_at": "..." }
```

---

## Organizations

| Method | Path | Description | Role |
|--------|------|-------------|------|
| GET | `/api/v1/organizations` | List | any |
| POST | `/api/v1/organizations` | Create | admin |
| GET | `/api/v1/organizations/{id}` | Get | any |
| PUT | `/api/v1/organizations/{id}` | Update | admin |
| DELETE | `/api/v1/organizations/{id}` | Delete | admin |
| GET | `/api/v1/organizations/{id}/members` | List members | any |
| POST | `/api/v1/organizations/{id}/members` | Add member | admin |
| DELETE | `/api/v1/organizations/{id}/members/{user_id}` | Remove member | admin |

**Organization object:**
```json
{ "id": 1, "name": "string", "description": "string", "created_at": "..." }
```

**Add member body:** `{ "user_id": 42 }`

---

## Locations

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/locations` | List |
| POST | `/api/v1/locations` | Create |
| GET | `/api/v1/locations/{id}` | Get |
| PUT | `/api/v1/locations/{id}` | Update |
| DELETE | `/api/v1/locations/{id}` | Delete |

**Location object:**
```json
{
  "id": 1,
  "location": "Kulturzentrum",
  "address": "Hauptstr. 1",
  "zipcode": "10115",
  "town": "Berlin",
  "latitude": "52.5200",
  "longitude": "13.4050",
  "internetsite": "https://example.com",
  "organization_id": 3,
  "created_at": "..."
}
```

**Create body** — `organization_id` required for `user`/`publisher` (must be org member):
```json
{
  "location": "string", "address": "string", "zipcode": "string",
  "town": "string", "latitude": "string", "longitude": "string",
  "internetsite": "string", "organization_id": 3
}
```

**Update body** — all fields optional:
```json
{ "address": "string", "zipcode": "string", "town": "string",
  "latitude": "string", "longitude": "string", "internetsite": "string" }
```

---

## Musicians

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/musicians` | List |
| POST | `/api/v1/musicians` | Create |
| GET | `/api/v1/musicians/{id}` | Get |
| PUT | `/api/v1/musicians/{id}` | Update |
| DELETE | `/api/v1/musicians/{id}` | Delete |

**Musician object:**
```json
{ "id": 1, "bandname": "string", "internetsite": "string", "created_at": "..." }
```

---

## Events

### Event object

```json
{
  "id": 1,
  "uid": "abc123@example.com",
  "title": "Bal Folk",
  "description": "string",
  "start_time": "2026-05-15T20:00:00+02:00",
  "end_time": "2026-05-15T23:00:00+02:00",
  "has_ball": true,
  "has_workshop": false,
  "tags": ["Bal Folk", "Köln"],
  "is_published": true,
  "short_code": "8b911390",
  "image_url": "/api/v1/images/1",
  "organization_id": 3,
  "pricing": {
    "type": "multiple",
    "currency": "EUR",
    "prices": [
      { "label": "normal", "amount": 12 },
      { "label": "student", "amount": 8 }
    ]
  },
  "created_at": "..."
}
```

The `pricing` field is optional. `type` must be one of:

| Value | Description |
|-------|-------------|
| `free` | No admission fee |
| `donation` | Pay what you want |
| `single` | One fixed price; set `amount` (and optionally `currency`) |
| `multiple` | Tiered pricing; set `prices` as an array of `{label, amount}` objects (and optionally `currency`) |

`uid` is set from the iCal `UID` field when importing; used for deduplication on re-import. When `uid` is present, re-importing the same feed updates the event rather than creating a duplicate. Without `uid`, deduplication falls back to matching by title + location + start time (±3 hours).

### Protected endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/events` | List events |
| POST | `/api/v1/events` | Create event(s) |
| GET | `/api/v1/events/{id}` | Get event |
| DELETE | `/api/v1/events/{id}` | Delete event |
| POST | `/api/v1/events/{id}/publish` | Publish event |

**Write access:** `user`/`publisher` must be members of the event's `organization_id`. `admin` bypasses this.

**Publication:** events created by `user` or `admin` are published immediately; `publisher` and `viewer` create unpublished events.

### GET /api/v1/events

Query parameters:

| Parameter | Description |
|-----------|-------------|
| `title` | Partial match |
| `description` | Partial match |
| `start_time_after` | RFC3339 |
| `start_time_before` | RFC3339 |
| `end_time_after` | RFC3339 |
| `end_time_before` | RFC3339 |
| `location` | Partial match |
| `tag` | Partial match |
| `has_ball` | `true`/`false` |
| `has_workshop` | `true`/`false` |
| `is_published` | `true`/`false` (admin/user only) |
| `limit` | Default 100, max 1000 |
| `offset` | Default 0 |

`viewer` role and the public endpoint only return published events.

### POST /api/v1/events — JSON

**Single event:**
```json
{
  "uid": "optional-stable-id",
  "title": "Bal Folk",
  "description": "string",
  "start_time": "2026-05-15T20:00:00",
  "end_time": "2026-05-15T23:00:00",
  "has_ball": true,
  "has_workshop": false,
  "tags": ["Bal Folk"],
  "organization_id": 3,
  "location": {
    "location": "Kulturzentrum", "address": "Hauptstr. 1",
    "zipcode": "10115", "town": "Berlin",
    "latitude": "52.52", "longitude": "13.40",
    "eventsite": "https://example.com/event/42"
  },
  "pricing": { "type": "single", "amount": 10, "currency": "EUR" }
}
```

**Event series** — share title/location, differ by date:
```json
{
  "title": "Weekly Dance",
  "has_ball": true,
  "tags": ["Bal Folk"],
  "location": { "location": "Kulturzentrum" },
  "date": [
    { "description": "Week 1", "start_time": "2026-05-15T20:00:00", "end_time": "2026-05-15T23:00:00" },
    { "description": "Week 2", "start_time": "2026-05-22T20:00:00", "end_time": "2026-05-22T23:00:00" }
  ]
}
```

**Multiple events** — POST a JSON array of event objects.

Response `201`: array of created event objects.

### POST /api/v1/events — iCal

```
Content-Type: text/calendar
```

Send a `.ics` file body. Optional query parameter `organization_id` assigns all imported events to an organization. iCal fields mapped:

| iCal field | Event field |
|------------|-------------|
| `UID` | `uid` (dedup key) |
| `SUMMARY` | `title` |
| `DESCRIPTION` | `description` |
| `DTSTART` / `DTEND` | `start_time` / `end_time` |
| `LOCATION` | location name |
| `CATEGORIES` | `tags` |
| `ORGANIZER` | organization (find or create by CN or email) |
| `ATTACH` with `FMTTYPE=image/*` | event image |

### POST /api/v1/events/{id}/publish

Publishes an unpublished event. Requires `user` or `admin` role and org membership.

Response `200`: updated event object.

---

## Images

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/images/{event_id}` | Get event image (AVIF) |
| POST | `/api/v1/images/{event_id}` | Upload event image |
| DELETE | `/api/v1/images/{event_id}` | Delete event image |

Upload accepts any common image format. The image is resized to fit within configured dimensions and stored as AVIF.

---

## Tags

### GET /api/v1/tags

Returns all distinct tags across all events.

```json
["Bal Folk", "Workshop", "Köln"]
```

---

## Fetch Sources (iCal subscriptions)

Stores iCal URLs and imports events from them on demand.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/fetchurl` | List sources |
| POST | `/api/v1/fetchurl` | Add source and import |
| GET | `/api/v1/fetchurl/{id}` | Get source |
| POST | `/api/v1/fetchurl/{id}/fetch` | Re-import from source |
| POST | `/api/v1/fetchurl/fetch-all` | Re-import from all sources |

**Fetch source object:**
```json
{
  "id": 1,
  "url": "https://example.com/calendar.ics",
  "type": "ical",
  "tags": ["Bal Folk"],
  "last_fetched_at": "2026-05-15T10:00:00Z",
  "created_at": "..."
}
```

**Add source body:**
```json
{ "url": "https://example.com/calendar.ics", "type": "ical", "tags": ["Bal Folk"] }
```

Tags listed here are merged with tags parsed from the iCal `CATEGORIES` field. Events are deduplicated by `uid`; re-fetching the same source updates existing events.

**fetch-all response:**
```json
[
  { "source_id": 1, "url": "...", "events": 12, "all_created": false },
  { "source_id": 2, "url": "...", "events": 0, "error": "remote returned 404" }
]
```

---

## Public endpoints

No authentication required.

| Path | Description |
|------|-------------|
| `GET /events` | Published events (JSON); supports same query params as protected endpoint plus `code` for short-code lookup |
| `GET /events.ics` | All published events as iCal feed |
| `GET /events/tag/{tag}.ics` | Published events for a tag as iCal feed |
| `GET /events/town/{town}.ics` | Published events for a town as iCal feed |

---

## Status codes

| Code | Meaning |
|------|---------|
| 200 | OK |
| 201 | Created |
| 204 | No content (delete) |
| 400 | Bad request / invalid input |
| 401 | Missing or invalid credentials |
| 403 | Forbidden (wrong role or not org member) |
| 404 | Not found |
| 415 | Unsupported media type |
| 429 | Rate limit exceeded |
| 502 | Bad gateway (upstream fetch failed) |
| 500 | Internal server error |
