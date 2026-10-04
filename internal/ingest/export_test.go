package ingest

// SanitizeField exposes sanitizeField to the external test package.
var SanitizeField = sanitizeField

// DecodeWithLimit is Decode with a payload cap small enough to test.
var DecodeWithLimit = decode
