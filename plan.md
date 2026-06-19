# Plan: subscription snapshot replacement

1. Treat imported subscription URLs as a tag-prefix snapshot, not a union.
   - Same prefix + same URL: replace previous nodes with the latest parsed result.
   - Same prefix + different URL(s): delete previous nodes under that prefix and keep only the latest URL set.
   - Multi-line subscription import is one snapshot for the prefix.
2. Add backend support for multi-line URL import in one Parse call.
   - Fetch and parse all URLs first.
   - Only after all fetch/parse work succeeds, remove old nodes for the tag prefix from store and runtime config.
   - Save the new parsed nodes as one import job and let existing commit/test flow continue.
3. Preserve failure safety.
   - If the new subscription fetch or parse fails, do not delete existing pool/candidate/failed nodes.
4. Update WebUI.
   - Submit multi-line subscriptions as one import operation.
   - Register subscription config by replacing previous URLs for the same prefix, not merging forever.
   - Add visible manual refresh controls for current import sources where possible.
5. Update README with the current replacement strategy and subscription behavior.
6. Verify with tests/build and real import scenarios.
