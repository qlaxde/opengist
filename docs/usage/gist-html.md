# Render a gist as a mini-site

Append `/site` to a gist URL to render its first HTML file as a page:

```
https://opengist.example.com/<user>/<gist>/site
```

The response is served as `text/html; charset=utf-8` without the
`X-Content-Type-Options: nosniff` header so the browser executes the page.
If the gist contains no `.html` file, the URL returns `404 Not Found`.

## Sibling files

Any file in the gist is reachable at `/site/<filename>` with its real
`Content-Type`, so relative paths inside the HTML just work:

```html
<!-- index.html in a gist -->
<link rel="stylesheet" href="style.css">
<script type="module" src="./app.js"></script>
<img src="logo.png">
```

The handler picks the MIME by file extension. The common web ones are
mapped explicitly (`.html`, `.htm`, `.js`, `.mjs`, `.css`, `.json`, `.svg`,
`.wasm`); everything else falls back to Go's `mime` package extension table
(images, fonts, audio, video, PDF, etc.) and finally to the gist's detected
MIME.

## Pinning to a revision

By default `/site` serves the latest revision (`HEAD`). The response is
returned with `Cache-Control: no-cache`, so a refresh after editing the
gist always shows the new version.

To share a stable link that survives later edits, prefix the path with
`@<revision>/`:

```
https://opengist.example.com/<user>/<gist>/site/@<commit>
https://opengist.example.com/<user>/<gist>/site/@<commit>/app.js
```

Pinned responses are returned with `Cache-Control: public, max-age=31536000,
immutable`, since the content at a given commit never changes. Relative
references inside the pinned HTML resolve to siblings at the same revision,
so a shared link is fully self-contained.

The "View site" button on the gist page automatically pins to whatever
revision you're currently viewing.

## Visibility

The endpoint honors the same visibility rules as the gist page itself:

- **Public** gists — anyone can render.
- **Unlisted** gists — anyone with the URL can render.
- **Private** gists — only the owner, or a request with an
  `Authorization: Token <token>` header whose owner is the gist owner and
  whose scope is at least Read.

## Security note

The endpoint deliberately drops the `nosniff` header so browsers execute
HTML, modules, and stylesheets. On a public, signup-open instance that
means any visitor can publish executable HTML at a URL on your domain.
Operators of public instances should be aware of the XSS surface this
introduces; private, authenticated deployments (e.g. OIDC-only) are the
intended target.

The raw endpoint (`/raw/HEAD/<file>`) is unchanged — it still serves files
as `text/plain` with `X-Content-Type-Options: nosniff`.
