# Go Calendar REST API Documentation

## Overview

The Go Calendar API is a RESTful service for managing calendar events, users, locations, and musicians. It supports role-based authentication and provides both public and protected endpoints.

## Base URL
```
http://localhost:8000
```

## Authentication

The API uses Bearer token authentication for protected endpoints. Obtain a token by logging in:

```bash
POST /api/v1/login
Content-Type: application/json

{
  "username": "admin",
  "password": "your_password"
}
```

Include the token in requests:
```
Authorization: Bearer <your_token>
```

## Roles

- **admin**: Full access to all resources
- **user**: Standard user access (can publish events)
- **viewer**: Read-only access (only published events)

## Endpoints

### Authentication

#### Login
```http
GET|POST /api/v1/login
```

**Request Body (POST):**
```json
{
  "username": "string",
  "email": "string", // optional
  "password": "string"
}
```

**Response (200):**
```json
{
  "token": "string",
  "expires_at": "2026-05-15T07:21:56Z",
  "user": {
    "id": 1,
    "username": "string",
    "email": "string",
    "role": "admin|user|viewer",
    "created_at": "2026-05-14T07:21:19Z"
  }
}
```

### Users

#### Get All Users
```http
GET /api/v1/users
```

**Authorization:** Required (admin only)

**Response (200):**
```json
[
  {
    "id": 1,
    "username": "string",
    "email": "string",
    "role": "admin|user|viewer",
    "created_at": "2026-05-14T07:21:19Z"
  }
]
```

#### Create User
```http
POST /api/v1/users
```

**Authorization:** Required (admin only)

**Request Body:**
```json
{
  "username": "string",
  "email": "string",
  "password": "string",
  "role": "user" // optional, defaults to "user"
}
```

**Response (201):**
```json
{
  "id": 1,
  "username": "string",
  "email": "string",
  "role": "user",
  "created_at": "2026-05-14T07:21:19Z"
}
```

#### Get User by ID
```http
GET /api/v1/users/{id}
```

**Authorization:** Required (admin only)

**Response (200):** Same as individual user object above

#### Update User
```http
PUT /api/v1/users/{id}
```

**Authorization:** Required (admin only)

**Request Body:**
```json
{
  "email": "newemail@example.com",
  "role": "admin"
}
```

**Response (200):** Updated user object

#### Delete User
```http
DELETE /api/v1/users/{id}
```

**Authorization:** Required (admin only)

**Response (204):** No Content

### Locations

#### Get All Locations
```http
GET /api/v1/locations
```

**Authorization:** Required

**Response (200):**
```json
[
  {
    "id": 1,
    "location": "string",
    "address": "string",
    "zipcode": "string",
    "town": "string",
    "latitude": "string",
    "longitude": "string",
    "internetsite": "string",
    "created_at": "2026-05-14T07:21:19Z"
  }
]
```

#### Create Location
```http
POST /api/v1/locations
```

**Authorization:** Required

**Request Body:**
```json
{
  "location": "string",
  "address": "string",
  "zipcode": "string",
  "town": "string",
  "latitude": "string",
  "longitude": "string",
  "internetsite": "string"
}
```

**Response (201):** Location object

#### Get Location by ID
```http
GET /api/v1/locations/{id}
```

**Authorization:** Required

**Response (200):** Location object

#### Update Location
```http
PUT /api/v1/locations/{id}
```

**Authorization:** Required

**Request Body:** Partial location object with fields to update

**Response (200):** Updated location object

#### Delete Location
```http
DELETE /api/v1/locations/{id}
```

**Authorization:** Required

**Response (204):** No Content

### Musicians

#### Get All Musicians
```http
GET /api/v1/musicians
```

**Authorization:** Required

**Response (200):**
```json
[
  {
    "id": 1,
    "bandname": "string",
    "internetsite": "string",
    "created_at": "2026-05-14T07:21:19Z"
  }
]
```

#### Create Musician
```http
POST /api/v1/musicians
```

**Authorization:** Required

**Request Body:**
```json
{
  "bandname": "string",
  "internetsite": "string"
}
```

**Response (201):** Musician object

#### Get Musician by ID
```http
GET /api/v1/musicians/{id}
```

**Authorization:** Required

**Response (200):** Musician object

#### Update Musician
```http
PUT /api/v1/musicians/{id}
```

**Authorization:** Required

**Request Body:**
```json
{
  "bandname": "string",
  "internetsite": "string"
}
```

**Response (200):** Updated musician object

#### Delete Musician
```http
DELETE /api/v1/musicians/{id}
```

**Authorization:** Required

**Response (204):** No Content

### Events

#### Get All Events (Protected)
```http
GET /api/v1/events
```

**Authorization:** Required

**Query Parameters:**
- `title`: Filter by title (partial match)
- `description`: Filter by description (partial match)
- `start_time_after`: Events starting after this time (RFC3339)
- `start_time_before`: Events starting before this time (RFC3339)
- `end_time_after`: Events ending after this time (RFC3339)
- `end_time_before`: Events ending before this time (RFC3339)
- `location`: Filter by location name (partial match)
- `has_ball`: Filter by ball flag (`true`/`false`)
- `has_workshop`: Filter by workshop flag (`true`/`false`)
- `tag`: Filter by tag (partial match)
- `is_published`: Filter by publication status (`true`/`false`) - admin/user only
- `limit`: Maximum number of results (default: 100, max: 1000)
- `offset`: Number of results to skip (default: 0)

**Response (200):** Array of event objects

#### Get All Events (Public)
```http
GET /events
```

**Authorization:** None required

**Query Parameters:** Same as protected endpoint, but only published events are returned

**Additional Parameter:**
- `code`: Short code to retrieve a specific event

**Response (200):** Array of published events or single event if `code` is provided

#### Create Events
```http
POST /api/v1/events
```

**Authorization:** Required

**Content Types Supported:**
- `application/json`
- `text/calendar` (iCal format)

**Request Body (JSON - Single Event):**
```json
{
  "title": "string",
  "description": "string",
  "start_time": "2026-05-15T10:00:00",
  "end_time": "2026-05-15T11:00:00",
  "has_ball": false,
  "has_workshop": true,
  "tags": ["tag1", "tag2"],
  "location": {
    "location": "string",
    "address": "string",
    "zipcode": "string",
    "town": "string",
    "latitude": "string",
    "longitude": "string",
    "eventsite": "string"
  }
}
```

**Request Body (JSON - Event Series):**
```json
{
  "title": "string",
  "description": "string",
  "date": [
    {
      "description": "string",
      "start_time": "2026-05-15T10:00:00",
      "end_time": "2026-05-15T11:00:00"
    }
  ],
  "has_ball": false,
  "has_workshop": true,
  "tags": ["tag1", "tag2"],
  "location": {
    "location": "string",
    "address": "string",
    "zipcode": "string",
    "town": "string",
    "latitude": "string",
    "longitude": "string",
    "eventsite": "string"
  }
}
```

**Request Body (JSON - Multiple Events):**
```json
[
  {
    "title": "Event 1",
    "start_time": "2026-05-15T10:00:00",
    "end_time": "2026-05-15T11:00:00",
    "location": { "location": "Venue A" }
  },
  {
    "title": "Event 2",
    "start_time": "2026-05-16T14:00:00",
    "end_time": "2026-05-16T15:00:00",
    "location": { "location": "Venue B" }
  }
]
```

**Response (201):** Array of created event objects

#### Get Event by ID
```http
GET /api/v1/events/{id}
```

**Authorization:** Required

**Response (200):** Event object

#### Delete Event
```http
DELETE /api/v1/events/{id}
```

**Authorization:** Required

**Response (204):** No Content

### Event Object Structure

```json
{
  "id": 1,
  "title": "string",
  "description": "string",
  "start_time": "2026-05-15T10:00:00Z",
  "end_time": "2026-05-15T11:00:00Z",
  "has_ball": false,
  "has_workshop": true,
  "tags": ["tag1", "tag2"],
  "is_published": true,
  "short_code": "8b911390",
  "created_at": "2026-05-14T07:37:43Z"
}
```

### Tags

#### Get All Tags
```http
GET /api/v1/tags
```

**Authorization:** Required

**Response (200):** Array of unique tag strings

```json
[
  "jazz",
  "rock",
  "classical",
  "workshop",
  "ball"
]
```

## HTTP Status Codes

- `200 OK`: Successful request
- `201 Created`: Resource created successfully
- `204 No Content`: Resource deleted successfully
- `400 Bad Request`: Invalid request data
- `401 Unauthorized`: Authentication required or invalid token
- `403 Forbidden`: Insufficient permissions
- `404 Not Found`: Resource not found
- `405 Method Not Allowed`: HTTP method not supported
- `415 Unsupported Media Type`: Content-Type not supported
- `429 Too Many Requests`: Rate limit exceeded
- `500 Internal Server Error`: Server error

## CORS Support

All endpoints support OPTIONS method for CORS preflight requests with the following headers:
- `Access-Control-Allow-Origin: *`
- `Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type, Authorization, X-User-Role, X-User-ID`
- `Access-Control-Max-Age: 86400`

## Content Types

- **Request**: `application/json`, `text/calendar` (events only), `application/x-www-form-urlencoded` (login)
- **Response**: `application/json`, `text/calendar` (events only)

## Rate Limiting

The API implements rate limiting based on IP address (100 requests per minute).

## Examples

### Create a User
```bash
curl -X POST http://localhost:8000/api/v1/users \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "username": "john_doe",
    "email": "john@example.com",
    "password": "secure_password_123",
    "role": "user"
  }'
```

### Create Multiple Events
```bash
curl -X POST http://localhost:8000/api/v1/events \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '[
    {
      "title": "Workshop Session",
      "description": "Advanced Go programming",
      "start_time": "2026-05-20T10:00:00",
      "end_time": "2026-05-20T12:00:00",
      "has_workshop": true,
      "tags": ["golang", "workshop"],
      "location": {
        "location": "Tech Hub",
        "town": "Berlin"
      }
    },
    {
      "title": "Community Meetup",
      "description": "Monthly networking event",
      "start_time": "2026-05-25T18:00:00",
      "end_time": "2026-05-25T20:00:00",
      "has_ball": true,
      "tags": ["community", "networking"],
      "location": {
        "location": "Club House",
        "town": "Berlin"
      }
    }
  ]'
```

### Get Events with Filters
```bash
curl "http://localhost:8000/events?title=Workshop&has_workshop=true&limit=10"
```

### Get Event by Short Code
```bash
curl "http://localhost:8000/events?code=8b911390"
```

## Notes

- All timestamps use RFC3339 format (ISO 8601)
- Event series creation allows multiple dates in a single request
- Short codes are automatically generated for shareable event URLs
- Public `/events` endpoint only shows published events
- Authentication is required for all write operations and some read operations
- The API supports both JSON and iCal formats for events