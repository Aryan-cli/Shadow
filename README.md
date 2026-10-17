# Shadow

Shadow is a Go-based community web platform that brings real-time-style messaging, user profiles, forums, a digital marketplace, and cryptocurrency payment flows into one application.

The project uses server-rendered HTML templates with lightweight JavaScript/HTMX on the frontend, PostgreSQL for persistent application data, and Redis for session storage.

> **Project status:** Personal/independent project. Review, test, and harden the code before deploying it publicly or handling real users or funds.

## Features

- **Accounts and profiles** — registration, login, profile pages, display name, bio, avatar, and banner management.
- **Messaging** — friend requests, conversations, message send/edit/delete endpoints, typing indicators, user status, message requests, and notification polling.
- **Community forums** — posts, comments, voting, categories, community/server pages, user profiles, search, and follow actions.
- **Marketplace** — product listings, product detail pages, purchase history, and checkout flow.
- **Crypto payment integration** — Bitcoin payment utilities and Monero-related payment support, with configurable enablement and background payment workers.
- **Account settings** — profile and privacy settings, password changes, and account deletion with a recovery window.
- **Support** — support pages and ticket workflows.
- **Admin area** — administrative dashboard and management pages for users, marketplace, payments, support, and site settings.
- **Security foundations** — CSRF checks, security response headers, HTTP-only session cookies, and rate-limiting utilities.

## Tech stack

- **Backend:** Go (`net/http`)
- **Database:** PostgreSQL
- **Session store:** Redis
- **Frontend:** HTML templates, CSS, JavaScript, HTMX
- **Payments / crypto utilities:** Bitcoin key/address and transaction helpers; Monero address/payment helpers
- **External services:** CoinGecko and BlockCypher are referenced by the Bitcoin payment utilities; a Monero light-wallet service may be configured.

## Requirements

- Go 1.25 or compatible with the version declared in `go.mod`
- PostgreSQL
- Redis
- Git (optional)

## Configuration

The application reads configuration from environment variables. Create a local `.env` file for development, but **never commit real secrets or wallet credentials**.

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | PostgreSQL connection string (required) |
| `REDIS_ADDR` | Redis address; defaults to `127.0.0.1:6379` |
| `PORT` | HTTP port; defaults to `8080` |
| `APP_ENV` | Set to `production` to enable the session cookie's `Secure` flag |
| `ADMIN_USERNAME` | Admin account/configuration value |
| `ADMIN_PASSWORD` | Admin password/configuration value |
| `BTC_HD_MNEMONIC` | Optional Bitcoin HD-wallet mnemonic; treat as highly sensitive |
| `XMR_SPEND_KEY` | Optional Monero private spend key; highly sensitive |
| `XMR_VIEW_KEY` | Optional Monero private view key |
| `XMR_LIGHT_SERVER` | Optional Monero light-wallet server override |

Example development configuration (replace every placeholder with your own local values):

```dotenv
DATABASE_URL=postgres://USER:PASSWORD@localhost:5432/shadow?sslmode=disable
REDIS_ADDR=127.0.0.1:6379
PORT=8080
APP_ENV=development

# Optional admin/payment configuration:
ADMIN_USERNAME=change-me
ADMIN_PASSWORD=change-me
# BTC_HD_MNEMONIC=
# XMR_SPEND_KEY=
# XMR_VIEW_KEY=
# XMR_LIGHT_SERVER=
```

Do not use example credentials in a public or production deployment. Prefer a secrets manager or deployment environment variables for production.

## Run locally

1. Clone the repository and enter its directory:

   ```bash
   git clone https://github.com/Aryan-cli/shadow.git
   cd shadow
   ```

2. Create and configure your local `.env` file as shown above.

3. Create a PostgreSQL database and ensure PostgreSQL is running.

4. Start Redis and make sure it is reachable at `REDIS_ADDR`.

5. Download Go dependencies:

   ```bash
   go mod download
   ```

6. Start the application. The included `run.sh` expects a configured `.env` file and a prebuilt `shadow` binary:

   ```bash
   go build -o shadow ./cmd/server
   chmod +x run.sh
   ./run.sh
   ```

   Alternatively, after exporting the required environment variables, run:

   ```bash
   go run ./cmd/server
   ```

7. Open [http://localhost:8080](http://localhost:8080), or the port configured in `PORT`.

On startup, the application connects to PostgreSQL, runs its database migrations, initializes Redis-backed sessions, and starts payment-related background workers.

## Repository layout

```text
.
├── cmd/server/        # Application entry point and HTTP route registration
├── internal/
│   ├── auth/          # Password/authentication helpers
│   ├── db/            # PostgreSQL queries and migrations
│   ├── handlers/      # HTTP handlers for app features
│   ├── middleware/    # CSRF, security headers, rate limiting
│   ├── payment/       # Bitcoin and Monero payment utilities
│   └── session/       # Redis-backed sessions
├── static/            # CSS, JavaScript, HTMX, uploaded media
├── templates/         # Server-rendered HTML pages
├── go.mod
└── go.sum
```

## Security notes

- **Before pushing to GitHub:** remove `.env`, database dumps such as `dump.rdb`, compiled binaries, private wallet keys/mnemonics, real user data, and any other secrets or generated runtime data from the repository.
- If any secret has already been committed or shared, rotate it. Adding a file to `.gitignore` does not remove it from Git history.
- Add a `.gitignore` for `.env`, `shadow`, `dump.rdb`, local uploads/data, and other generated files as appropriate for your deployment.
- The code includes security-related middleware, but that alone is not a security audit. Review authentication and authorization on every route, session handling, CSRF coverage, rate-limit placement, upload validation, payment verification, admin access, and production HTTPS configuration.
- Do not use real cryptocurrency funds until the payment lifecycle, confirmations, key management, error handling, and recovery procedures have been independently tested.
- Back up PostgreSQL and Redis according to your own retention and privacy requirements.

## Contributing

Contributions, bug reports, and suggestions are welcome. For changes involving authentication, payment handling, or security, include a clear explanation of the threat or failure mode and the tests performed.

## License

No license file was present in the inspected project archive. Add a `LICENSE` file before presenting this repository as open source, and choose a license that matches how you want others to use the code.
