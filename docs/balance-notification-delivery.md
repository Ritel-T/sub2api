# Balance reminder delivery

Balance threshold crossings and insufficient-balance rejections share the existing per-user, per-recipient UTC daily delivery identity. Recipient verification, opt-out preferences, templates and final SMTP recipient-domain mapping stay in their existing paths.

Before a balance email can reach SMTP, a separate durable settings row claims that delivery identity with a random owner. The settings key's unique constraint selects one sender across service instances and restarts. This attempt is not a sent receipt. The regular success marker is written only on a successful send.

Only an error certified by the SMTP transport as occurring before message DATA was submitted permits the claim to be released. Release compares both key and owner. Unknown SMTP outcomes, process interruption, and a successful send whose success-marker write fails retain the attempt without a success receipt; later balance rejections do not resend that daily identity. There is no automatic expiry or reclaim of indeterminate attempts. Check independent SMTP/provider evidence before any manual reconciliation. A fresh day's reminder has its own existing daily identity.

If the durable attempt store is unavailable or the repository does not implement atomic claims, balance emails are suppressed. Other event types keep their existing notification semantics. No new database schema is required.
