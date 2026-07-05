-- v1.0 (PRD §5.2 "2FA"): optional TOTP second factor per user. Null means
-- disabled; the secret is only persisted after the user proves possession
-- by verifying a code, so there is no half-enrolled state.

alter table users add column totp_secret text;
