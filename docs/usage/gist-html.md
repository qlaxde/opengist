# Render a Gist as HTML

Appending `.html` to a gist URL returns the gist's first HTML file (by filename
order), served with `Content-Type: text/html; charset=utf-8` and without the
`X-Content-Type-Options: nosniff` header, so the browser renders it as a page:

```
https://opengist.example.com/<user>/<gist>.html
```

If the gist contains no `.html` file, the URL returns `404 Not Found`.

The endpoint honors the same visibility rules as the gist page itself:

- **Public** gists — anyone can render.
- **Unlisted** gists — anyone with the URL can render.
- **Private** gists — only the owner, or a request with an access token
  (`Authorization: Token <token>`) belonging to the owner with at least the
  Read scope, can render.

## Security note

This endpoint deliberately drops the `nosniff` header so browsers execute the
HTML as a page. Hosted on a public, signup-open instance, that means any
visitor can publish executable HTML at a URL on your domain. Operators of
public instances should be aware of the XSS surface this introduces; private,
authenticated deployments (e.g. OIDC-only) are the intended target.

The raw endpoint (`/raw/HEAD/<file>`) is unchanged — it still serves files as
`text/plain` with `X-Content-Type-Options: nosniff`.
