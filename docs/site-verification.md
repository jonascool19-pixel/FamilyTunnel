# Google Search Console

Browser warnings from Google Safe Browsing are separate from TLS and tunnel reachability. Inspect the Security Issues report and actual published content before requesting a review. Do not claim that a reachable service is free of phishing or malware.

For a URL-prefix property, an administrator can keep Google's HTML-file proof in the server's `/var/lib/familytunnel/site-verification.json` (root-owned, mode 0600):

```json
[{"domain":"home.example.com","file":"google0123456789abcdef.html"}]
```

Use the exact filename supplied by Google, not the example. The controller serves the exact verification response on that path on existing enabled HTTPS web routes for the matching domain. It preserves the proof during health checks, configuration synchronization and service restart, without exposing other files or changing the service backend. No new route or port is opened. The next normal configuration reconciliation applies changes. Removing a proof may revoke ownership verification in Google; retain it after verification.

The file is not shipped in installers; proofs are specific to the operator's Google account. The full backup includes this file; keep the archive private.
