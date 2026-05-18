# dansal

Calendar REST API backed by SQLite. See [API.md](API.md) for full endpoint documentation.

This server should present dancing events. Events can be fetched by iCal-Feeds and inserted.
Existing events can be exported via iCal, JSON-Feed to existing instances. Events are publiches via activitypub into fediverse.

## Structure
For managing events the service offer following data types to describe events:
### locations
Beside the normal information like street and town, geo coordinates are saved, so this location can be shown on a map. Also more information like link to different ressources can be placed.

For each location the upcoming events are shown.

### Musicians
A list of musicians is integrated. If possible a link to musicbrainz is shown, where discography and more information are stored (band member etc.).

Musicians can be attached to an event and for each musician the list of upcoming events are shown.

### Organisations
A layer over locations are organisations, like folk clubs. To an organisation several locations can be attached. Also fetch url can be stored for organisation, where to fetch iCal information of upcoming events.

For each organisation the list of upcoming events are presented.

### Events
Events are the central object of this page. They have description, overall time and location. They are assigned to an organisation. To the event musicians can be assigned. In an optional timetable detailed information can be stored, e.g. which musician is playing in which room at defined time.
Also pricing informations can be attached.

## Display
On the main page a map of the next events are shown. The time range is set by the weekly table below. Default is the actual week shown with all events for each day. At the table the week can be change to one week earlier or later.

Each events can be displayed in detail.


## Access
Viewing is possible for all.

Importing can be done via iCal-Feeds for feeds which offer events.

Creating inside is possible with an account. Assigned to an organisation the user can create events for this organisation only.

## Requirements

- Go 1.22+
- SQLite3

## Run

```bash
go run .
```

The server starts on the port set in `config.yaml` (default `8000`). On first run an admin user is created and the password is printed to the console.

## Configuration

`config.yaml`:

```yaml
server:
  port: 8000
```

## Build

```bash
go build -o dansal
./dansal
```

## Legal pages (Contact & Impressum)

The web frontend supports an optional contact line in the page footer and an Impressum page at `/impressum`. Both are configured via a separate YAML file referenced by `pages_file` in the web config.

### Configuration

Add `pages_file` to the web frontend's `config.yaml`:

```yaml
pages_file: pages.yaml
```

### pages.yaml format

Both `contact` and `impressum` are maps from language code to text. The fallback language is `de`; if the visitor's language has no entry the German text is shown.

```yaml
contact:
  de: "Kontakt: info@example.org"
  en: "Contact: info@example.org"
  fr: "Contact : info@example.org"

impressum:
  de: |
    Angaben gemäß § 5 TMG
    Max Mustermann
    Musterstraße 1
    12345 Musterstadt
    E-Mail: info@example.org
  en: |
    Legal notice
    Max Mustermann
    1 Example Street
    12345 Example Town
    E-mail: info@example.org
```

### Behaviour

- **Contact** — the text is rendered as a plain line in the site footer on every page. If the entry for the current language is missing, the `de` entry is used as fallback. If neither exists, the footer line is omitted entirely.
- **Impressum** — the text is rendered verbatim (whitespace preserved) on `/impressum`. A link to that page appears in the footer only when an impressum text is configured. Language fallback works the same way as for contact.

If `pages_file` is not set in the config, both features are silently disabled — no footer line, no `/impressum` route content.
