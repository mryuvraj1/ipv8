# Deployment and Cloudflare Tunnel

IPv8 serves the browser panel, JSON API, and `/healthz` from one Go process on the configured `IPV8_ADDR` (default `127.0.0.1:8080`). There is no separate frontend port. The default loopback listener keeps the origin private; a Cloudflare Tunnel connector publishes the HTTPS hostname to this local HTTP service.

## Configure the application

Set these variables in the existing systemd unit or service manager for the Go process. The application does not load `.env` files automatically.

```ini
IPV8_ADDR=127.0.0.1:8080
IPV8_DATA=/var/lib/ipv8/ipv8.json
IPV8_PUBLIC_URL=https://panel.example.com
IPV8_API_URL=https://panel.example.com
IPV8_COOKIE_SECURE=true
```

Replace `panel.example.com` with the hostname actually configured in your Cloudflare account. `IPV8_PUBLIC_URL` and `IPV8_API_URL` must be the same HTTPS origin because the panel currently uses same-origin requests and cookies; split frontend/API origins and CORS are not implemented. HTTPS public configuration enables Secure session cookies. The server rejects a remote HTTP URL and rejects an explicitly insecure cookie setting with a public HTTPS URL. Local development can leave both URLs empty and use `http://127.0.0.1:8080`.

The frontend reads only `publicURL` and `apiURL` from `GET /api/runtime-config`. Tunnel tokens are never read by the Go application or returned by an API. A remote install command is offered only while using the configured public HTTPS origin; no domain is hardcoded or invented.

## Create and run a remotely managed tunnel

1. Install `cloudflared` using [Cloudflare's current Linux installation instructions](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/). The persistent service setup below requires cloudflared 2025.4.0 or later for `--token-file` support.
2. In the Cloudflare dashboard, open **Networks → Tunnels**, create a remotely managed tunnel, and add a **Published application route** for the chosen hostname. Configure the service URL as `http://127.0.0.1:8080`. Cloudflare creates/updates the tunnel DNS record when you publish the hostname. See [Cloudflare Tunnel routing](https://developers.cloudflare.com/tunnel/concepts/routing/) and [remote tunnel setup](https://developers.cloudflare.com/tunnel/get-started/).
3. Confirm the Go panel is running locally and responds to `curl -fsS http://127.0.0.1:8080/healthz`.
4. Copy the tunnel token from the Cloudflare dashboard without pasting it into a command or repository. In an interactive Bash or Zsh terminal, enter it without echo and pass only the variable name through sudo:

   ```sh
   read -r -s -p 'Cloudflare Tunnel token: ' CLOUDFLARE_TUNNEL_TOKEN; printf '\n'
   export CLOUDFLARE_TUNNEL_TOKEN
   sudo --preserve-env=CLOUDFLARE_TUNNEL_TOKEN ./deploy/configure-cloudflare.sh
   unset CLOUDFLARE_TUNNEL_TOKEN
   ```

   The helper refuses to replace an existing token or unit, writes the token to `/etc/ipv8/cloudflared-token` with mode `0600`, installs `ipv8-cloudflared.service`, and runs `cloudflared` with a systemd transient credential. The token is not passed as a process argument and is not stored in the Go service environment. If the unit already exists, manage it explicitly instead of rerunning the helper.
5. Set `IPV8_PUBLIC_URL` and `IPV8_API_URL` to the same actual `https://` hostname in the Go service configuration, set `IPV8_COOKIE_SECURE=true`, and restart the Go service. The tunnel forwards external HTTPS requests to the local HTTP origin; do not expose port 8080 publicly.
6. Check the hostname in a browser, then verify `https://panel.example.com/healthz`, login, and agent enrollment. Create the first admin using `IPV8_ADMIN_EMAIL` and `IPV8_ADMIN_PASSWORD` only for the one-time `bootstrap-admin` command; do not put these in the tunnel service.

The `.env.example` value for `CLOUDFLARE_TUNNEL_TOKEN` is a placeholder only. Do not save a real tunnel token in `.env`, export it to the Go process, frontend, or agent, or paste it into terminal command arguments. The helper accepts it only in its environment long enough to create the protected file. Rotate a compromised token in Cloudflare and replace the protected file through an explicit maintenance procedure.

## Service and health checks

```sh
sudo systemctl status ipv8-cloudflared.service
sudo journalctl -u ipv8-cloudflared.service -n 100 --no-pager
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS https://panel.example.com/healthz
```

The application process should itself be managed by a persistent service manager and retain its `IPV8_DATA` volume. This repository does not install or manage the host's Go panel service. Cloudflare Tunnel publishes web requests; it does not provide IPv8 packet forwarding, VPN connectivity, or node-to-node transport.

## Troubleshooting

- **Hostname/DNS does not resolve:** verify the hostname is in the correct Cloudflare zone and the published application route is attached to the intended healthy tunnel. The route hostname must match `IPV8_PUBLIC_URL`.
- **Tunnel shows disconnected:** inspect `systemctl status ipv8-cloudflared` and `journalctl -u ipv8-cloudflared`; confirm outbound connectivity and a valid tunnel credential. Revoke/rotate the token in Cloudflare if it may have leaked.
- **HTTP 502:** first run `curl -fsS http://127.0.0.1:8080/healthz` on the VPS. If that fails, check the Go service, `IPV8_ADDR`, and its logs. If local health works, check that the tunnel route points to `http://127.0.0.1:8080`, not a guessed port or HTTPS origin, and confirm the connector is healthy.
- **Login cookie is not retained:** serve the panel at HTTPS and set `IPV8_COOKIE_SECURE=true`; keep the configured public/API origins identical and access the panel at that same host.
- **Install IPv8 is unavailable:** configure `IPV8_PUBLIC_URL` with the deployed HTTPS origin, restart the panel, and open that origin directly. The installer does not use a localhost URL for remote users.
- **Agent is Offline:** tunnel health alone does not mean agent enrollment. Check the agent service logs and allow outbound HTTPS to the panel; the UI reports Online only after a valid heartbeat within 90 seconds.

## Remove or rotate the tunnel connector

Disable and remove only the IPv8-specific unit installed by the helper:

```sh
sudo systemctl disable --now ipv8-cloudflared.service
sudo rm /etc/systemd/system/ipv8-cloudflared.service
sudo rm /etc/ipv8/cloudflared-token
sudo systemctl daemon-reload
```

Then delete or rotate the connector in Cloudflare. Removing the connector does not revoke user agent enrollments or delete the IPv8 panel database.
