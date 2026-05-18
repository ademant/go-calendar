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

## Telegram integration

Dansal supports Telegram in two ways:

1. **Account verification** — users prove they control a Telegram account by clicking a deep link that sends a one-time token to the bot.
2. **Admin messaging** — admins can send a plain-text message to any user who has completed Telegram verification.

### How it works

Because Telegram bots cannot initiate a conversation, the verification flow relies on a deep link:

1. The user adds their Telegram handle (`@username`) in **Settings**.
2. They click **Get Telegram verification link**. The server mints a short-lived token and returns a deep link of the form `https://t.me/BOTNAME?start=TOKEN`.
3. The user clicks the link. Telegram opens the bot and automatically sends `/start TOKEN`.
4. The bot webhook receives the `/start TOKEN` command, validates the token, marks `telegram_verified = 1` on the user record, and stores the numeric Telegram `chat_id` needed for future outbound messages.
5. The bot replies with a confirmation message in Telegram.

### Create a bot

1. Open Telegram and start a conversation with **@BotFather**.
2. Send `/newbot` and follow the prompts to choose a name and a username (must end in `bot`).
3. BotFather gives you a **bot token** (format `123456789:AABBcc…`). Keep this secret.

### Configure dansal

Add the following keys to the backend's `config.yaml`:

```yaml
server:
  telegram_bot_token: "123456789:AABBccDDeeFFggHH"   # from BotFather
  telegram_bot_name: "myDansalBot"                    # username without the leading @
```

`telegram_bot_token` is used to call the Telegram Bot API (send messages, etc.).  
`telegram_bot_name` is used to construct the deep link shown to users in Settings.

### Register the webhook

Telegram must know where to forward incoming messages. Register the webhook once after deploying:

```bash
curl -X POST "https://api.telegram.org/bot<TOKEN>/setWebhook" \
     -H "Content-Type: application/json" \
     -d '{"url": "https://yourdomain.example.com/telegram/webhook"}'
```

Replace `<TOKEN>` with your bot token and the URL with your public server address. The endpoint `/telegram/webhook` is served by the dansal backend and requires no authentication — Telegram calls it directly.

You can verify the webhook is registered:

```bash
curl "https://api.telegram.org/bot<TOKEN>/getWebhookInfo"
```

### Sending a message to a user (admin)

Once a user has verified their Telegram account, an admin can send them a message via the API:

```bash
curl -X POST "https://yourdomain.example.com/api/v1/users/42/telegram/message" \
     -H "Authorization: Bearer <ADMIN_TOKEN>" \
     -H "Content-Type: application/json" \
     -d '{"text": "Hello from dansal!"}'
```

Returns `204 No Content` on success. Returns `400` if the user has no verified Telegram account.

### Verification token lifetime

Verification tokens expire after the number of hours set by `verification_expiry_hours` (default `24`). If a user lets the link expire, they can request a new one by clicking the button again in Settings.

### Notes

- The `/telegram/webhook` route is unauthenticated by design — Telegram servers call it directly. The endpoint validates the token from the payload; there is no secret exposed.
- If `telegram_bot_token` or `telegram_bot_name` is not set, the verification request returns a `500` error and no token is stored.
- Changing the Telegram handle in Settings clears the existing verification status and stored `chat_id`, requiring the user to re-verify.
