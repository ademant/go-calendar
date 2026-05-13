# go-calendar REST API

A simple REST API for managing calendar events, backed by SQLite3.

## Requirements

- Go 1.22+
- SQLite3

## Project Structure

```
.
├── main.go          # Main application with API routes
├── users.go         # User management handlers
├── config.go        # Configuration loading
├── config.yaml      # Configuration file (port, etc.)
├── go.mod           # Go module definition
├── calendar.db      # SQLite database (created on first run)
└── README.md        # This file
```

## Configuration

Edit `config.yaml` to adjust server settings:

```yaml
server:
  port: 8000
```

- **port**: The port number the server listens on (default: 8000)

## Setup & Running

### Install dependencies

```bash
go mod download
```

### Build

```bash
go build -o calendar
```

### Run

```bash
go run main.go
```

The server will start on the port configured in `config.yaml` (default: `http://localhost:8000`).

**Initial Admin User Setup:**
On the first run, if no admin user exists, the system will automatically create an initial admin user with the following credentials:
- **Username**: `admin`
- **Email**: `admin@localhost`
- **Password**: A randomly generated 16-character password (printed to console on first run)

⚠️ **Important**: Save the initial admin password printed to the console during first run. You can change it later through the API.

## API Endpoints

### User Management

The API supports role-based user management with three roles:
- **admin**: Full access to all resources
- **user**: Standard user access (default)
- **viewer**: Read-only access

#### Get all users
```bash
GET /api/v1/users
```

**Response:**
```json
[
  {
    "id": 1,
    "username": "john_doe",
    "email": "john@example.com",
    "role": "admin",
    "created_at": "2026-05-13T12:00:00"
  }
]
```

#### Create user
```bash
POST /api/v1/users
Content-Type: application/json

{
  "username": "john_doe",
  "email": "john@example.com",
  "password": "secure_password_123",
  "role": "user"
}
```

Alternatively, use form-encoded data:
```bash
POST /api/v1/users
Content-Type: application/x-www-form-urlencoded

username=john_doe&email=john@example.com&password=secure_password_123&role=user
```

**Response:** `201 Created`
```json
{
  "id": 1,
  "username": "john_doe",
  "email": "john@example.com",
  "role": "user",
  "created_at": "2026-05-13T12:00:00"
}
```

#### Get single user
```bash
GET /api/v1/users/{id}
```

#### Update user (email and/or role)
```bash
PUT /api/v1/users/{id}
Content-Type: application/json

{
  "email": "newemail@example.com",
  "role": "admin"
}
```

#### Delete user
```bash
DELETE /api/v1/users/{id}
```

**Response:** `204 No Content`

### Locations
#### Create location
```bash
POST /api/v1/locations
Content-Type: application/json

{
  "location": "Bahnhof",
  "address": "Bahnhofstrasse 4",
  "zipcode": "12345",
  "town": "Berlin"
  "latitude": 48,
  "longitude": 10,
  "internetsite": "www.balfolk.site"
}
```
#### Get single location
```bash
GET /api/v1/locations/{id}
```

#### Update location (address, zipcode, town, latitude, longitude or internetsite)
```bash
PUT /api/v1/locations/{id}
Content-Type: application/json

{
  "address": "Bahnhofstrasse 4",
  "zipcode": "12345",
  "town": "Berlin"
  "latitude": 48,
  "longitude": 10,
  "internetsite": "www.balfolk.site"
}
```

#### Delete user
```bash
DELETE /api/v1/locations/{id}
```

### Events

#### Get all events
```bash
GET /api/v1/events
```

**Response:**
```json
[
  {
    "id": 1,
    "title": "Team Meeting",
    "description": "Monthly planning",
    "start_time": "2026-05-15T10:00:00",
    "end_time": "2026-05-15T11:00:00",
    "created_at": "2026-05-13T12:00:00"
  }
]
```

#### Create event
```bash
POST /api/v1/events
Content-Type: application/json

{
  "title": "Team Meeting",
  "description": "Monthly planning",
  "start_time": "2026-05-15T10:00:00",
  "end_time": "2026-05-15T11:00:00"
}
```

**Response:** `201 Created`
```json
{
  "id": 1,
  "title": "Team Meeting",
  "description": "Monthly planning",
  "start_time": "2026-05-15T10:00:00",
  "end_time": "2026-05-15T11:00:00",
  "created_at": "2026-05-13T12:00:00"
}
```

#### Get single event
```bash
GET /api/v1/events/{id}
```

#### Delete event
```bash
DELETE /api/v1/events/{id}
```

**Response:** `204 No Content`

## Example Usage

```bash
# Create a new user (JSON)
curl -X POST http://localhost:8000/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{
    "username": "john_doe",
    "email": "john@example.com",
    "password": "secure_password_123",
    "role": "user"
  }'

# Create a new user (form-encoded)
curl -X POST http://localhost:8000/api/v1/users \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "username=john_doe&email=john@example.com&password=secure_password_123&role=user"

# Get all users
curl http://localhost:8000/api/v1/users

# Get specific user
curl http://localhost:8000/api/v1/users/1

# Update user role to admin
curl -X PUT http://localhost:8000/api/v1/users/1 \
  -H "Content-Type: application/json" \
  -d '{
    "role": "admin"
  }'

# Delete user
curl -X DELETE http://localhost:8000/api/v1/users/1

# Create an event
curl -X POST http://localhost:8000/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Go Meeting",
    "description": "Discuss new features",
    "start_time": "2026-05-15T14:00:00",
    "end_time": "2026-05-15T15:00:00"
  }'

# Get all events
curl http://localhost:8000/api/v1/events

# Get specific event
curl http://localhost:8000/api/v1/events/1

# Delete event
curl -X DELETE http://localhost:8000/api/v1/events/1
```

## Database

SQLite3 database is automatically created on first run as `calendar.db`. The database contains the following tables:

**Users table:**
```sql
CREATE TABLE users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT UNIQUE NOT NULL,
  email TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  role TEXT DEFAULT 'user' CHECK(role IN ('admin', 'user', 'viewer')),
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**Events table:**
```sql
CREATE TABLE events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  description TEXT,
  start_time DATETIME NOT NULL,
  end_time DATETIME NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## License

See LICENSE file.
