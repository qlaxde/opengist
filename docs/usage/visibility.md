# Gist visibility

Every gist has one visibility level. Three of them gate access among
authenticated users; two of them deliberately expose a gist to visitors with
**no login at all**, which is useful on otherwise login-required deployments
(for example an OIDC-only instance with `require-login` enabled) that still need
to share the occasional artifact publicly.

| Level           | Wire value | Requires login | Listed        | Reachable without login |
|-----------------|-----------|----------------|---------------|-------------------------|
| **Internal**    | `internal` (0) | yes       | yes (All gists) | — |
| **Unlisted**    | `unlisted` (1) | yes       | no, link-only | — |
| **Private**     | `private` (2)  | owner/admin only | no    | — |
| **Public**      | `public` (3)   | **no**    | no, link-only | the whole gist (page, raw, `/site`) |
| **Public site** | `public_site` (4) | **no** | no, link-only | only the rendered `/site` |

Notes:

- **Internal** is the default for a new gist. On a deployment without
  `require-login`, it behaves exactly like a classic "public" gist (listed and
  readable by anyone); with `require-login` on, it is readable by any
  authenticated user.
- **Public** and **Public site** are never listed and never indexed — they are
  link-only. They are the only levels that bypass `require-login`.
- **Public site** exposes the rendered site (`/site` and its assets) to anonymous
  visitors, while the source view (`/<user>/<gist>`), raw files and downloads
  still require login. Use it to share a rendered page without revealing its
  source.

## Setting visibility

- In the UI: the visibility dropdown on the create and edit screens.
- Via the metadata API (PAT-authenticated):

  ```
  POST /api/gists/<user>/<gist>/metadata
  Authorization: Token <token>
  Content-Type: application/json

  {"visibility": "public_site"}
  ```

## Security note

`public` and `public_site` make a gist reachable by anyone on the internet with
the link, with no authentication. Because the `/site` renderer drops the
`X-Content-Type-Options: nosniff` header so browsers execute the page, only mark
artifacts public that are safe to expose. Private and internal gists are
unaffected.
