// Package calwrite mirrors locally-authored calendar events out to the user's
// Google Calendar (one-way: Deneb → Google). It is the WRITE counterpart to the
// read-only platform/calendar client, kept a separate package on purpose: the
// calendar client's invariant is "works without a Google write scope", so its
// read surface must not grow POST/PATCH/DELETE. localcal (the local store) is
// the source of truth and must not import the Google API; the handler layer
// orchestrates the mirror by calling this package after a successful local write.
//
// Everything here is OPT-IN and OFF by default: DefaultSyncer returns an error
// (so the handler degrades to local-only, exactly as before) unless
// DENEB_CALENDAR_GOOGLE_WRITE is truthy AND a write-scoped OAuth token is present
// at ~/.deneb/credentials/calendar_token.json. Writes are best-effort — a Google
// outage logs a warning but never fails the local write.
package calwrite
