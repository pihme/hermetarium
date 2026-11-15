# Open web

`squid.conf` decrypts HTTPS. Hosts on the `passthrough` ACL keep their own certificate. That list is for programs that pin a certificate or need HTTP/2. Squid speaks only HTTP/1.1 after it decrypts.

`api.github.com` uses the gate token. Squid drops any `Authorization` the box sends and sets the token from this file. The token is not in the image. Reads are allowed. The writes that are allowed are creating a repository, a pull request, an issue, or a comment. Deletes, contents writes, Git data writes, and repository settings are denied.

Use two fine-grained tokens, for all repositories:

- Push token, held by Git Warden. Contents write. No Workflows. No Administration.
- API token, held by the gate. Repository creation, Pull requests, and Issues. No Contents. No Administration. Put this token in `squid.conf` in place of `__GITHUB_TOKEN__`.
