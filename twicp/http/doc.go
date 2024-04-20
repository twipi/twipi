// Package http provides an HTTP API handler that lets users configure
// services.
//
// # API Spec
//
// All data exchanged over the API are encoded in Protojson format.
// For more information, see
// https://protobuf.dev/programming-guides/proto3/#json.
//
// The following endpoints are available:
//
//   - `GET /schema`: Returns the schema describing the options.
//   - `GET /`: Returns the current values of the options.
//   - `PATCH /`: Applies the given values.
//
// These endpoints must be appended after the base URL, which is the URL of the
// server plus the path to the [httpcp.Handler].
package http
