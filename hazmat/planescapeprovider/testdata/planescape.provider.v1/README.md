# Planescape provider v1 fixtures

This is the pinned shared conformance corpus consumed by Hazmat's Go client
tests. It is mirrored from Planescape commit `5878eed1d5c7a57fcac92a72158e05d993b6b873`.

The manifest and `CORPUS.sha256` pin the complete corpus. Update all three
together from the published Planescape release artifact; do not edit individual
fixtures locally.

The `wire/` vectors are mirrored byte-for-byte from Planescape commit
`c2856f9497ff0d63de51c725c910a0d4e859aa1c`. `WIRE_VECTORS.sha256` pins
`vectors.json`; the vectors are the sole cross-language wire parity oracle.
